package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/danielwolfman/prdash/internal/model"
)

type Model struct {
	dashboard  Dashboard
	cursor     int
	jobCursor  map[int]int
	offset     int
	width      int
	height     int
	expanded   map[int]bool
	frame      int
	now        time.Time
	styles     styles
	symbols    symbols
	events     chan LoadEvent
	refresh    chan struct{}
	loading    bool
	loadText   string
	loadError  string
	confirm    *confirmation
	actionBusy bool
	actionText string
}

type tickMsg time.Time
type actionResultMsg ActionResult
type openResultMsg struct {
	URL   string
	Label string
	Error string
}

type selectionKind int

const (
	selectionPR selectionKind = iota
	selectionJob
)

type confirmation struct {
	request ActionRequest
	text    string
}

type selection struct {
	Kind  selectionKind
	Row   int
	Job   int
	URL   string
	Label string
	Line  int
}

func New(dashboard Dashboard) Model {
	now := dashboard.SnapshotAt
	if now.IsZero() {
		now = time.Now()
	}
	if dashboard.AnimationFPS <= 0 {
		dashboard.AnimationFPS = 6
	}
	events := make(chan LoadEvent, 64)
	refresh := make(chan struct{}, 1)
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
		jobCursor: map[int]int{},
		now:       now,
		styles:    newStyles(),
		symbols:   chooseSymbols(dashboard.Symbols),
		events:    events,
		refresh:   refresh,
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
		if m.confirm != nil {
			return m.updateConfirmation(msg)
		}
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "j", "down":
			m.moveSelection(1)
			m.keepCursorVisible()
		case "k", "up":
			m.moveSelection(-1)
			m.keepCursorVisible()
		case "enter", " ":
			if len(m.dashboard.Rows) > 0 {
				m.expanded[m.cursor] = !m.expanded[m.cursor]
				m.normalizeJobCursor()
			}
		case "home":
			m.cursor = 0
			m.jobCursor[m.cursor] = -1
			m.keepCursorVisible()
		case "end":
			if len(m.dashboard.Rows) > 0 {
				m.cursor = len(m.dashboard.Rows) - 1
				m.jobCursor[m.cursor] = -1
			}
			m.keepCursorVisible()
		case "o":
			cmd := m.openSelection()
			return m, cmd
		case "r":
			m.planRerunFailedJobs()
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
		if !msg.Closed {
			return m, m.waitForLoadEvent()
		}
	case actionResultMsg:
		m.actionBusy = false
		result := ActionResult(msg)
		if result.Error != "" {
			m.actionText = "rerun failed: " + result.Error
		} else if result.Message != "" {
			m.actionText = result.Message
			m.requestRefresh()
		} else {
			m.actionText = "rerun requested"
			m.requestRefresh()
		}
	case openResultMsg:
		if msg.Error != "" {
			m.actionText = "open failed: " + msg.Error
		} else {
			m.actionText = "opened " + valueOr(msg.Label, msg.URL)
		}
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder
	bodyHeight := max(1, m.height-4)
	if m.confirm != nil {
		bodyHeight = max(1, m.height-5)
	}

	b.WriteString(m.header())
	b.WriteByte('\n')

	rows := m.renderRows(bodyHeight)
	if len(rows) == 0 {
		if m.loading {
			rows = []string{m.loadingLine()}
		} else if m.loadError != "" {
			rows = []string{m.styles.error.Render("load failed: " + m.loadError)}
		} else {
			rows = []string{m.styles.muted.Render("No open monitored PRs found.")}
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
	if m.confirm != nil {
		b.WriteByte('\n')
		b.WriteString(m.confirmLine())
	}
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
	status := m.footerStatus()
	renderedStatus := status
	if m.loadError != "" {
		renderedStatus = m.styles.error.Render(status)
	}
	action := "r disabled"
	if m.dashboard.ActionsEnabled {
		action = "r rerun failed"
	}
	if m.actionText != "" {
		action += " · " + m.actionText
	}
	prefix := fmt.Sprintf(" ↑/↓ j/k move · enter expand · o open · %s · q quit · symbols %s · ", action, mode)
	text := prefix + renderedStatus + " "
	if m.loadError != "" {
		maxStatusWidth := max(1, m.width-lipgloss.Width(prefix)-1)
		text = prefix + m.styles.error.Render(fitPlain(status, maxStatusWidth)) + " "
		return m.styles.footer.Width(max(1, m.width)).Render(fitANSI(text, max(1, m.width)))
	}
	return m.styles.footer.Width(max(1, m.width)).Render(fitPlain(text, max(1, m.width)))
}

func (m Model) footerStatus() string {
	if m.loadError != "" {
		return "load error: " + m.loadError
	}
	if m.dashboard.RefreshInterval > 0 && !m.loading {
		return fmt.Sprintf("%s · refresh %s", m.loadText, shortDuration(m.dashboard.RefreshInterval))
	}
	return m.loadText
}

func (m Model) confirmLine() string {
	if m.confirm == nil {
		return ""
	}
	text := " " + m.confirm.text + " Enter/y confirm · Esc/n cancel "
	return m.styles.confirm.Width(max(1, m.width)).Render(fitPlain(text, max(1, m.width)))
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
	selectedJob := -1
	if focused {
		selectedJob = m.currentJobCursor()
	}
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
	if row.Loading && (len(row.Runs) > 0 || row.FetchError != "") {
		statusSuffix += " " + m.styles.running.Render(m.symbols.forState(model.CheckRunning, m.frame)+" refreshing")
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
	if focused && selectedJob < 0 {
		heading = m.styles.focused.Render(heading)
	} else if stale {
		heading = m.styles.stale.Render(heading)
	} else {
		heading = m.styles.row.Render(heading)
	}

	var lines []string
	lines = append(lines, heading)
	if row.Loading && len(row.Runs) == 0 && row.FetchError == "" {
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

	visibleJobs := m.visibleJobs(row.Runs, expanded)
	if len(visibleJobs) > 0 {
		for idx, job := range visibleJobs {
			line := m.jobLine(job)
			if focused && selectedJob == idx {
				line = m.styles.focused.Render(line)
			}
			lines = append(lines, "  "+line)
		}
		total := len(allJobs(row.Runs))
		if total > len(visibleJobs) {
			lines = append(lines, m.styles.muted.Render(fmt.Sprintf("  ... %d successful/older jobs hidden", total-len(visibleJobs))))
		}
	}
	if rowFullyGreen(row, summary) {
		return m.greenBox(lines)
	}
	return lines
}

func (m Model) greenBox(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	boxWidth := max(20, m.width-2)
	return strings.Split(m.styles.greenBox.Width(boxWidth).Render(strings.Join(lines, "\n")), "\n")
}

func rowFullyGreen(row Row, summary model.CheckSummary) bool {
	return !row.Loading &&
		row.FetchError == "" &&
		summary.Total > 0 &&
		summary.Failure == 0 &&
		summary.Cancelled == 0 &&
		summary.ActionRequired == 0 &&
		summary.Running == 0 &&
		summary.Waiting == 0 &&
		summary.Unknown == 0 &&
		summary.Stale == 0
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

func (m Model) visibleJobs(runs []model.WorkflowRun, expanded bool) []displayJob {
	limit := 6
	if expanded {
		limit = 8
	}
	return prioritizedJobs(runs, limit)
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
	status := string(job.State)
	if job.Conclusion != "" {
		status = job.Conclusion
	}
	jobWidth := clamp(m.width-len(status)-6, 16, 96)
	text := fmt.Sprintf("%s %-*s %s",
		symbol,
		jobWidth,
		fitPlain(job.Name, jobWidth),
		status,
	)
	if job.State == model.CheckSuccess || job.State == model.CheckNeutral {
		text = fmt.Sprintf("%s %-*s %s",
			symbol,
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
	refresh := m.refresh
	return func() tea.Msg {
		go func() {
			loader(context.Background(), refresh, events)
			close(events)
		}()
		return nil
	}
}

func (m *Model) requestRefresh() {
	if m.dashboard.Loader == nil || m.refresh == nil {
		return
	}
	m.loading = true
	m.loadText = "refresh requested after rerun"
	select {
	case m.refresh <- struct{}{}:
	default:
	}
}

func (m Model) updateConfirmation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y", "Y":
		request := m.confirm.request
		m.confirm = nil
		m.actionBusy = true
		m.actionText = "requesting rerun..."
		return m, m.runAction(request)
	case "esc", "n", "N":
		m.confirm = nil
		m.actionText = "rerun cancelled"
		return m, nil
	default:
		return m, nil
	}
}

func (m *Model) planRerunFailedJobs() {
	if !m.dashboard.ActionsEnabled || m.dashboard.ActionExecutor == nil {
		m.actionText = "rerun disabled"
		return
	}
	if m.actionBusy {
		m.actionText = "rerun already in progress"
		return
	}
	if len(m.dashboard.Rows) == 0 {
		m.actionText = "no PR selected"
		return
	}
	request := rerunFailedJobsRequest(m.dashboard.Rows[m.cursor])
	if request.JobCount == 0 || len(request.RunIDs) == 0 {
		m.actionText = "selected PR has no completed failed jobs to rerun"
		return
	}
	m.confirm = &confirmation{
		request: request,
		text: fmt.Sprintf("Rerun %d failed jobs across %d workflow runs for %s/%s#%d?",
			request.JobCount,
			request.WorkflowCount,
			request.Owner,
			request.Repo,
			request.PRNumber,
		),
	}
}

func rerunFailedJobsRequest(row Row) ActionRequest {
	request := ActionRequest{
		Kind:     ActionRerunFailedJobs,
		Owner:    row.PR.Owner,
		Repo:     row.PR.Repo,
		PRNumber: row.PR.Number,
		PRTitle:  row.PR.Title,
	}
	for _, run := range row.Runs {
		if run.ID == 0 || !strings.EqualFold(run.Status, "completed") {
			continue
		}
		failedJobs := 0
		for _, job := range run.Jobs {
			switch job.State {
			case model.CheckActionRequired, model.CheckFailure, model.CheckCancelled:
				failedJobs++
			}
		}
		if failedJobs == 0 {
			continue
		}
		request.RunIDs = append(request.RunIDs, run.ID)
		request.JobCount += failedJobs
		request.WorkflowCount++
	}
	return request
}

func (m Model) runAction(request ActionRequest) tea.Cmd {
	executor := m.dashboard.ActionExecutor
	return func() tea.Msg {
		if executor == nil {
			return actionResultMsg{Request: request, Error: "action executor unavailable"}
		}
		return actionResultMsg(executor(context.Background(), request))
	}
}

func (m *Model) openSelection() tea.Cmd {
	selected, ok := m.currentSelection()
	if !ok || strings.TrimSpace(selected.URL) == "" {
		m.actionText = "nothing to open"
		return nil
	}
	opener := m.dashboard.OpenURL
	if opener == nil {
		m.actionText = "open unavailable"
		return nil
	}
	m.actionText = "opening " + valueOr(selected.Label, selected.URL)
	return func() tea.Msg {
		if err := opener(context.Background(), selected.URL); err != nil {
			return openResultMsg{URL: selected.URL, Label: selected.Label, Error: err.Error()}
		}
		return openResultMsg{URL: selected.URL, Label: selected.Label}
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
	if !event.Closed && event.Error == "" && (event.Message != "" || event.Row != nil || event.ReplaceRows) {
		m.loadError = ""
	}
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
	if event.ReplaceRows {
		m.replaceRows(event.Rows)
		m.loadText = fmt.Sprintf("loaded %d/%d PRs", len(m.dashboard.Rows), max(m.dashboard.TotalDiscovered, len(m.dashboard.Rows)))
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

func (m *Model) replaceRows(rows []Row) {
	oldRows := m.dashboard.Rows
	nextRows := make([]Row, 0, len(rows))
	for _, row := range rows {
		if row.LastFetched.IsZero() && !row.Loading {
			row.LastFetched = m.now
		}
		for _, old := range oldRows {
			if samePR(old.PR, row.PR) {
				row = m.mergeRowState(old, row)
				break
			}
		}
		nextRows = append(nextRows, row)
	}
	m.dashboard.Rows = nextRows
	if len(m.dashboard.Rows) == 0 {
		m.cursor = 0
		m.jobCursor = map[int]int{}
		return
	}
	if m.cursor >= len(m.dashboard.Rows) {
		m.cursor = len(m.dashboard.Rows) - 1
	}
	nextJobCursor := make(map[int]int, len(m.jobCursor))
	for row := range m.jobCursor {
		if row < len(m.dashboard.Rows) {
			nextJobCursor[row] = m.jobCursor[row]
		}
	}
	m.jobCursor = nextJobCursor
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

func (m Model) currentJobCursor() int {
	if len(m.dashboard.Rows) == 0 {
		return -1
	}
	value, ok := m.jobCursor[m.cursor]
	if !ok {
		return -1
	}
	return value
}

func (m *Model) setSelection(row, job int) {
	if len(m.dashboard.Rows) == 0 {
		m.cursor = 0
		return
	}
	m.cursor = clamp(row, 0, len(m.dashboard.Rows)-1)
	if job < 0 {
		m.jobCursor[m.cursor] = -1
		return
	}
	maxJob := len(m.visibleJobs(m.dashboard.Rows[m.cursor].Runs, m.expanded[m.cursor])) - 1
	if maxJob < 0 {
		m.jobCursor[m.cursor] = -1
		return
	}
	m.jobCursor[m.cursor] = clamp(job, 0, maxJob)
}

func (m *Model) normalizeJobCursor() {
	if len(m.dashboard.Rows) == 0 {
		return
	}
	job := m.currentJobCursor()
	if job < 0 {
		return
	}
	maxJob := len(m.visibleJobs(m.dashboard.Rows[m.cursor].Runs, m.expanded[m.cursor])) - 1
	if maxJob < 0 {
		m.jobCursor[m.cursor] = -1
		return
	}
	if job > maxJob {
		m.jobCursor[m.cursor] = maxJob
	}
}

func (m *Model) moveSelection(delta int) {
	items := m.selectableItems()
	if len(items) == 0 {
		return
	}
	current := 0
	for idx, item := range items {
		if item.Row == m.cursor && item.Job == m.currentJobCursor() {
			current = idx
			break
		}
	}
	next := clamp(current+delta, 0, len(items)-1)
	m.setSelection(items[next].Row, items[next].Job)
}

func (m Model) currentSelection() (selection, bool) {
	for _, item := range m.selectableItems() {
		if item.Row == m.cursor && item.Job == m.currentJobCursor() {
			return item, true
		}
	}
	items := m.selectableItems()
	if len(items) == 0 {
		return selection{}, false
	}
	return items[0], true
}

func (m Model) selectableItems() []selection {
	var items []selection
	line := 0
	for rowIdx, row := range m.dashboard.Rows {
		rowLines := m.renderRow(rowIdx, row)
		items = append(items, selection{
			Kind:  selectionPR,
			Row:   rowIdx,
			Job:   -1,
			URL:   row.PR.URL,
			Label: fmt.Sprintf("%s#%d", row.PR.RepoFullName, row.PR.Number),
			Line:  line,
		})
		for jobIdx, job := range m.visibleJobs(row.Runs, m.expanded[rowIdx]) {
			items = append(items, selection{
				Kind:  selectionJob,
				Row:   rowIdx,
				Job:   jobIdx,
				URL:   job.Job.URL,
				Label: job.Job.Name,
				Line:  line + 1 + jobIdx,
			})
		}
		line += len(rowLines)
	}
	return items
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
	m.normalizeJobCursor()
	bodyLines := max(1, m.height-4)
	start := m.rowStartLine(m.cursor)
	end := start + len(m.renderRow(m.cursor, m.dashboard.Rows[m.cursor]))
	if selected, ok := m.currentSelection(); ok && selected.Kind == selectionJob {
		start = selected.Line
		end = selected.Line + 1
	}
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
	var best selection
	found := false
	for _, item := range m.selectableItems() {
		if item.Line <= targetLine {
			best = item
			found = true
		}
	}
	if found {
		m.setSelection(best.Row, best.Job)
	}
	m.keepCursorVisible()
}

func (m *Model) scrollDown() {
	if len(m.dashboard.Rows) == 0 {
		return
	}
	bodyLines := max(1, m.height-4)
	maxOffset := max(0, m.totalRenderedLines()-bodyLines)
	m.offset = min(maxOffset, m.offset+3)
	m.focusLine(m.offset)
}

func (m *Model) scrollUp() {
	if len(m.dashboard.Rows) == 0 {
		return
	}
	m.offset = max(0, m.offset-3)
	m.focusLine(m.offset)
}

func (m *Model) focusLine(targetLine int) {
	var best selection
	found := false
	for _, item := range m.selectableItems() {
		if item.Line <= targetLine {
			best = item
			found = true
		}
	}
	if found {
		m.setSelection(best.Row, best.Job)
	}
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
