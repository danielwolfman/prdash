package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/danielwolfman/prdash/internal/model"
)

type Model struct {
	dashboard Dashboard
	cursor    int
	offset    int
	width     int
	height    int
	expanded  map[int]bool
	frame     int
	now       time.Time
	styles    styles
	symbols   symbols
	events    chan LoadEvent
	loading   bool
	loadText  string
	loadError string
}

type tickMsg time.Time

func New(dashboard Dashboard) Model {
	now := dashboard.SnapshotAt
	if now.IsZero() {
		now = time.Now()
	}
	if dashboard.AnimationFPS <= 0 {
		dashboard.AnimationFPS = 6
	}
	events := make(chan LoadEvent, 64)
	loading := dashboard.Loader != nil
	loadText := "loading GitHub auth and PR list"
	if len(dashboard.Rows) > 0 {
		loadText = "loaded"
	}
	return Model{
		dashboard: dashboard,
		width:     120,
		height:    36,
		expanded:  map[int]bool{},
		now:       now,
		styles:    newStyles(),
		symbols:   chooseSymbols(dashboard.Symbols),
		events:    events,
		loading:   loading,
		loadText:  loadText,
	}
}

func (m Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if !m.dashboard.Animations {
		if m.dashboard.Loader == nil {
			return nil
		}
	} else {
		cmds = append(cmds, m.tick())
	}
	if m.dashboard.Loader != nil {
		cmds = append(cmds, m.startLoader(), m.waitForLoadEvent())
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.keepCursorVisible()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(m.dashboard.Rows)-1 {
				m.cursor++
			}
			m.keepCursorVisible()
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
			m.keepCursorVisible()
		case "enter", " ":
			if len(m.dashboard.Rows) > 0 {
				m.expanded[m.cursor] = !m.expanded[m.cursor]
			}
		case "home":
			m.cursor = 0
			m.keepCursorVisible()
		case "end":
			if len(m.dashboard.Rows) > 0 {
				m.cursor = len(m.dashboard.Rows) - 1
			}
			m.keepCursorVisible()
		}
	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelDown:
			m.scrollDown()
		case tea.MouseWheelUp:
			m.scrollUp()
		case tea.MouseLeft:
			m.focusApproximateRow(msg.Y)
		}
	case tickMsg:
		m.frame++
		m.now = time.Time(msg)
		if m.dashboard.Animations {
			return m, m.tick()
		}
	case LoadEvent:
		m.applyLoadEvent(msg)
		if !msg.Closed && msg.Error == "" {
			return m, m.waitForLoadEvent()
		}
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	bodyHeight := max(1, m.height-4)

	b.WriteString(m.header())
	b.WriteByte('\n')

	rows := m.renderRows(bodyHeight)
	if len(rows) == 0 {
		if m.loading {
			rows = []string{m.loadingLine()}
		} else if m.loadError != "" {
			rows = []string{m.styles.error.Render("load failed: " + m.loadError)}
		} else {
			rows = []string{m.styles.muted.Render("No open authored PRs found.")}
		}
	}
	for _, line := range rows {
		b.WriteString(fitANSI(line, m.width))
		b.WriteByte('\n')
	}

	for used := len(rows); used < bodyHeight; used++ {
		b.WriteByte('\n')
	}

	b.WriteString(m.footer())
	return b.String()
}

func (m Model) header() string {
	hidden := ""
	if m.dashboard.ExcludedCount > 0 {
		hidden = fmt.Sprintf(" · hidden %d", m.dashboard.ExcludedCount)
	}
	loaded := fmt.Sprintf("%d PRs", len(m.dashboard.Rows))
	if m.dashboard.TotalDiscovered > 0 {
		loaded = fmt.Sprintf("%d/%d PRs", len(m.dashboard.Rows), m.dashboard.TotalDiscovered)
	}
	state := "live loading"
	if !m.loading {
		state = "live"
	}
	text := fmt.Sprintf(" prdash · %s · %s%s · %s %s ",
		valueOr(m.dashboard.User, "unknown"),
		loaded,
		hidden,
		state,
		relativeTime(m.dashboard.SnapshotAt, m.now),
	)
	return m.styles.header.Width(max(1, m.width)).Render(fitPlain(text, max(1, m.width)))
}

func (m Model) footer() string {
	mode := "unicode"
	if m.symbols.ascii {
		mode = "ascii"
	}
	status := m.loadText
	if m.loadError != "" {
		status = "load error: " + m.loadError
	} else if m.dashboard.RefreshInterval > 0 && !m.loading {
		status = fmt.Sprintf("%s · refresh %s", status, shortDuration(m.dashboard.RefreshInterval))
	}
	text := fmt.Sprintf(" ↑/↓ j/k move · enter expand · q quit · symbols %s · %s ", mode, status)
	return m.styles.footer.Width(max(1, m.width)).Render(fitPlain(text, max(1, m.width)))
}

func (m Model) loadingLine() string {
	spinner := m.symbols.forState(model.CheckRunning, m.frame)
	return m.styles.running.Render(spinner+" ") + m.styles.muted.Render(m.loadText)
}

func (m Model) renderRows(maxLines int) []string {
	var lines []string
	skipped := 0
	for idx := 0; idx < len(m.dashboard.Rows) && len(lines) < maxLines; idx++ {
		rowLines := m.renderRow(idx, m.dashboard.Rows[idx])
		if skipped+len(rowLines) <= m.offset {
			skipped += len(rowLines)
			continue
		}
		for _, line := range rowLines {
			if skipped < m.offset {
				skipped++
				continue
			}
			if len(lines) >= maxLines {
				break
			}
			lines = append(lines, line)
		}
	}
	return lines
}

func (m Model) renderRow(index int, row Row) []string {
	focused := index == m.cursor
	expanded := m.expanded[index]
	jobs := allJobs(row.Runs)
	summary := model.SummarizeJobs(jobs)
	stale := m.rowStale(row)

	marker := " "
	if focused {
		marker = m.symbols.focus
	} else if stale {
		marker = m.symbols.stale
	}

	titleWidth := clamp(m.width-95, 18, 56)
	title := fitPlain(row.PR.Title, titleWidth)
	badges := m.badges(row)
	summaryText := m.summary(summary, row.FetchError)
	statusSuffix := fmt.Sprintf("updated %s", relativeTime(row.PR.UpdatedAt, m.now))
	if stale {
		statusSuffix += " " + m.styles.stale.Render("stale")
	}
	if m.now.Before(row.ChangedUntil) {
		statusSuffix += " " + m.styles.forState(row.ChangeState).Render("changed")
	}
	if !row.LastFetched.IsZero() {
		statusSuffix += " checked " + relativeTime(row.LastFetched, m.now)
	}
	heading := fmt.Sprintf("%s %s#%d %-*s %s %s %s",
		marker,
		row.PR.RepoFullName,
		row.PR.Number,
		titleWidth,
		title,
		badges,
		summaryText,
		statusSuffix,
	)
	if focused {
		heading = m.styles.focused.Render(heading)
	} else if stale {
		heading = m.styles.stale.Render(heading)
	} else {
		heading = m.styles.row.Render(heading)
	}

	var lines []string
	lines = append(lines, heading)
	if row.Loading {
		lines = append(lines, "  "+m.styles.running.Render(m.symbols.forState(model.CheckRunning, m.frame)+" loading jobs..."))
		return lines
	}
	if row.FetchError != "" {
		lines = append(lines, "  "+m.styles.error.Render(fitPlain("actions unavailable: "+row.FetchError, max(20, m.width-4))))
		return lines
	}
	if len(row.Runs) == 0 {
		lines = append(lines, "  "+m.styles.muted.Render("no current GitHub Actions runs found"))
		return lines
	}

	if expanded {
		for _, line := range m.verticalJobLines(row.Runs, 8) {
			lines = append(lines, "  "+line)
		}
	} else {
		for _, line := range m.verticalJobLines(row.Runs, 6) {
			lines = append(lines, "  "+line)
		}
	}
	return lines
}

func (m Model) badges(row Row) string {
	var badges []string
	if row.PR.IsDraft {
		badges = append(badges, m.styles.badge.Render("draft"))
	}
	if row.PR.MergeStateStatus != "" {
		badges = append(badges, m.styles.badge.Render(strings.ToLower(row.PR.MergeStateStatus)))
	}
	if row.PR.ReviewDecision != "" {
		badges = append(badges, m.styles.badge.Render(strings.ToLower(row.PR.ReviewDecision)))
	}
	if len(badges) == 0 {
		return ""
	}
	return strings.Join(badges, " ")
}

func (m Model) summary(summary model.CheckSummary, fetchError string) string {
	if fetchError != "" {
		return m.styles.error.Render("actions error")
	}
	if summary.Total == 0 {
		return m.styles.muted.Render("no jobs")
	}
	parts := []string{}
	if summary.ActionRequired > 0 {
		parts = append(parts, m.styles.actionRequired.Render(fmt.Sprintf("%d action", summary.ActionRequired)))
	}
	if summary.Failure > 0 {
		parts = append(parts, m.styles.failure.Render(fmt.Sprintf("%d fail", summary.Failure)))
	}
	if summary.Cancelled > 0 {
		parts = append(parts, m.styles.cancelled.Render(fmt.Sprintf("%d cancelled", summary.Cancelled)))
	}
	if summary.Running > 0 {
		parts = append(parts, m.styles.running.Render(fmt.Sprintf("%d run", summary.Running)))
	}
	if summary.Waiting > 0 {
		parts = append(parts, m.styles.waiting.Render(fmt.Sprintf("%d wait", summary.Waiting)))
	}
	ok := summary.Success + summary.Neutral
	if ok > 0 {
		parts = append(parts, m.styles.success.Render(fmt.Sprintf("%d ok", ok)))
	}
	if summary.Unknown > 0 {
		parts = append(parts, m.styles.unknown.Render(fmt.Sprintf("%d unknown", summary.Unknown)))
	}
	return strings.Join(parts, " ")
}

func (m Model) verticalJobLines(runs []model.WorkflowRun, limit int) []string {
	jobs := prioritizedJobs(runs, limit)
	if len(jobs) == 0 {
		return []string{m.styles.muted.Render("no jobs")}
	}
	lines := make([]string, 0, len(jobs)+1)
	for _, job := range jobs {
		lines = append(lines, m.jobLine(job))
	}
	total := len(allJobs(runs))
	if total > len(jobs) {
		lines = append(lines, m.styles.muted.Render(fmt.Sprintf("... %d successful/older jobs hidden", total-len(jobs))))
	}
	return lines
}

type displayJob struct {
	Workflow string
	Job      model.Job
}

func prioritizedJobs(runs []model.WorkflowRun, limit int) []displayJob {
	var jobs []displayJob
	for _, run := range runs {
		for _, job := range run.Jobs {
			jobs = append(jobs, displayJob{Workflow: run.Name, Job: job})
		}
	}
	sort.SliceStable(jobs, func(i, j int) bool {
		ip, jp := jobPriority(jobs[i].Job.State), jobPriority(jobs[j].Job.State)
		if ip != jp {
			return ip < jp
		}
		return jobs[i].Job.StartedAt.After(jobs[j].Job.StartedAt)
	})
	if limit > 0 && len(jobs) > limit {
		return jobs[:limit]
	}
	return jobs
}

func jobPriority(state model.CheckState) int {
	switch state {
	case model.CheckActionRequired:
		return 0
	case model.CheckFailure:
		return 1
	case model.CheckCancelled:
		return 2
	case model.CheckRunning:
		return 3
	case model.CheckWaiting:
		return 4
	case model.CheckUnknown:
		return 5
	case model.CheckSuccess:
		return 6
	case model.CheckNeutral:
		return 7
	default:
		return 8
	}
}

func (m Model) jobLine(display displayJob) string {
	job := display.Job
	symbol := m.symbols.forState(job.State, m.frame)
	workflowWidth := clamp(m.width/5, 10, 28)
	jobWidth := clamp(m.width-workflowWidth-24, 16, 80)
	status := string(job.State)
	if job.Conclusion != "" {
		status = job.Conclusion
	}
	text := fmt.Sprintf("%s %-*s %-*s %s",
		symbol,
		workflowWidth,
		fitPlain(display.Workflow, workflowWidth),
		jobWidth,
		fitPlain(job.Name, jobWidth),
		status,
	)
	if job.State == model.CheckSuccess || job.State == model.CheckNeutral {
		text = fmt.Sprintf("%s %-*s %-*s %s",
			symbol,
			workflowWidth,
			fitPlain(display.Workflow, workflowWidth),
			jobWidth,
			fitPlain(abbreviate(job.Name), jobWidth),
			status,
		)
	}

	style := m.styles.forState(job.State)
	return style.Render(text)
}

func (m Model) startLoader() tea.Cmd {
	loader := m.dashboard.Loader
	events := m.events
	return func() tea.Msg {
		go func() {
			loader(context.Background(), events)
			close(events)
		}()
		return nil
	}
}

func (m Model) waitForLoadEvent() tea.Cmd {
	events := m.events
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return LoadEvent{Done: true, Closed: true}
		}
		return event
	}
}

func (m *Model) applyLoadEvent(event LoadEvent) {
	if event.User != "" {
		m.dashboard.User = event.User
	}
	if !event.SnapshotAt.IsZero() {
		m.dashboard.SnapshotAt = event.SnapshotAt
		m.now = event.SnapshotAt
	}
	if event.RefreshInterval > 0 {
		m.dashboard.RefreshInterval = event.RefreshInterval
		if m.dashboard.StaleAfter == 0 {
			m.dashboard.StaleAfter = event.RefreshInterval * 2
		}
	}
	if event.TotalDiscovered > 0 {
		m.dashboard.TotalDiscovered = event.TotalDiscovered
	}
	m.dashboard.ExcludedCount = event.ExcludedCount
	if event.Message != "" {
		m.loadText = event.Message
	}
	if event.Row != nil {
		row := *event.Row
		if row.LastFetched.IsZero() && !row.Loading {
			row.LastFetched = m.now
		}
		replaced := false
		for i := range m.dashboard.Rows {
			if samePR(m.dashboard.Rows[i].PR, row.PR) {
				m.dashboard.Rows[i] = m.mergeRowState(m.dashboard.Rows[i], row)
				replaced = true
				break
			}
		}
		if !replaced {
			m.dashboard.Rows = append(m.dashboard.Rows, row)
		}
		m.loadText = fmt.Sprintf("loaded %d/%d PRs", len(m.dashboard.Rows), max(m.dashboard.TotalDiscovered, len(m.dashboard.Rows)))
	}
	if event.Error != "" {
		m.loadError = event.Error
		m.loading = false
	}
	if event.Done {
		m.loading = false
		if m.loadError == "" {
			m.loadText = fmt.Sprintf("loaded %d PRs", len(m.dashboard.Rows))
		}
	}
	m.keepCursorVisible()
}

func (m Model) mergeRowState(old, next Row) Row {
	if next.Loading {
		if !old.LastFetched.IsZero() {
			next.LastFetched = old.LastFetched
		}
		next.Runs = old.Runs
		next.FetchError = old.FetchError
		next.ChangedUntil = old.ChangedUntil
		next.ChangeState = old.ChangeState
		return next
	}
	oldSummary := model.SummarizeJobs(allJobs(old.Runs)).State
	nextSummary := model.SummarizeJobs(allJobs(next.Runs)).State
	if !old.LastFetched.IsZero() && oldSummary != nextSummary {
		next.ChangedUntil = m.now.Add(8 * time.Second)
		next.ChangeState = nextSummary
	}
	if next.LastFetched.IsZero() {
		next.LastFetched = m.now
	}
	return next
}

func (m Model) rowStale(row Row) bool {
	if row.Loading || row.LastFetched.IsZero() {
		return false
	}
	staleAfter := m.dashboard.StaleAfter
	if staleAfter <= 0 {
		staleAfter = 2 * time.Minute
	}
	return m.now.Sub(row.LastFetched) > staleAfter
}

func samePR(a, b model.PullRequest) bool {
	return a.RepoFullName == b.RepoFullName && a.Number == b.Number
}

func (m *Model) keepCursorVisible() {
	if len(m.dashboard.Rows) == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.dashboard.Rows) {
		m.cursor = max(0, len(m.dashboard.Rows)-1)
	}
	bodyLines := max(1, m.height-4)
	start := m.rowStartLine(m.cursor)
	end := start + len(m.renderRow(m.cursor, m.dashboard.Rows[m.cursor]))
	if start < m.offset {
		m.offset = start
	}
	if end > m.offset+bodyLines {
		m.offset = end - bodyLines
	}
	if m.offset < 0 {
		m.offset = 0
	}
	maxOffset := max(0, m.totalRenderedLines()-bodyLines)
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
}

func (m *Model) focusApproximateRow(y int) {
	if y <= 0 || len(m.dashboard.Rows) == 0 {
		return
	}
	targetLine := m.offset + y - 1
	line := 0
	for idx := range m.dashboard.Rows {
		line += len(m.renderRow(idx, m.dashboard.Rows[idx]))
		if targetLine < line {
			m.cursor = idx
			m.keepCursorVisible()
			return
		}
	}
	m.cursor = len(m.dashboard.Rows) - 1
	m.keepCursorVisible()
}

func (m *Model) scrollDown() {
	if len(m.dashboard.Rows) == 0 {
		return
	}
	bodyLines := max(1, m.height-4)
	maxOffset := max(0, m.totalRenderedLines()-bodyLines)
	m.offset = min(maxOffset, m.offset+3)
	m.cursor = m.firstVisibleRow()
}

func (m *Model) scrollUp() {
	if len(m.dashboard.Rows) == 0 {
		return
	}
	m.offset = max(0, m.offset-3)
	m.cursor = m.firstVisibleRow()
}

func (m Model) firstVisibleRow() int {
	line := 0
	for idx := range m.dashboard.Rows {
		next := line + len(m.renderRow(idx, m.dashboard.Rows[idx]))
		if next > m.offset {
			return idx
		}
		line = next
	}
	return max(0, len(m.dashboard.Rows)-1)
}

func (m Model) rowStartLine(row int) int {
	line := 0
	for idx := 0; idx < row && idx < len(m.dashboard.Rows); idx++ {
		line += len(m.renderRow(idx, m.dashboard.Rows[idx]))
	}
	return line
}

func (m Model) totalRenderedLines() int {
	total := 0
	for idx := range m.dashboard.Rows {
		total += len(m.renderRow(idx, m.dashboard.Rows[idx]))
	}
	return total
}

func (m Model) tick() tea.Cmd {
	interval := time.Second / time.Duration(max(1, m.dashboard.AnimationFPS))
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func allJobs(runs []model.WorkflowRun) []model.Job {
	var jobs []model.Job
	for _, run := range runs {
		jobs = append(jobs, run.Jobs...)
	}
	return jobs
}
