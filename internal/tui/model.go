package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	return Model{
		dashboard: dashboard,
		width:     120,
		height:    36,
		expanded:  map[int]bool{},
		now:       now,
		styles:    newStyles(),
		symbols:   chooseSymbols(dashboard.Symbols),
	}
}

func (m Model) Init() tea.Cmd {
	if !m.dashboard.Animations {
		return nil
	}
	return m.tick()
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
			if m.cursor < len(m.dashboard.Rows)-1 {
				m.cursor++
				m.keepCursorVisible()
			}
		case tea.MouseWheelUp:
			if m.cursor > 0 {
				m.cursor--
				m.keepCursorVisible()
			}
		case tea.MouseLeft:
			m.focusApproximateRow(msg.Y)
		}
	case tickMsg:
		m.frame++
		m.now = time.Time(msg)
		if m.dashboard.Animations {
			return m, m.tick()
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
		rows = []string{m.styles.muted.Render("No open authored PRs found.")}
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
	text := fmt.Sprintf(" prdash · %s · %s%s · static snapshot %s ",
		valueOr(m.dashboard.User, "unknown"),
		loaded,
		hidden,
		relativeTime(m.dashboard.SnapshotAt, m.now),
	)
	return m.styles.header.Width(max(1, m.width)).Render(fitPlain(text, max(1, m.width)))
}

func (m Model) footer() string {
	mode := "unicode"
	if m.symbols.ascii {
		mode = "ascii"
	}
	text := fmt.Sprintf(" ↑/↓ j/k move · enter expand · q quit · symbols %s · no polling/rerun in this milestone ", mode)
	return m.styles.footer.Width(max(1, m.width)).Render(fitPlain(text, max(1, m.width)))
}

func (m Model) renderRows(maxLines int) []string {
	var lines []string
	for idx := m.offset; idx < len(m.dashboard.Rows) && len(lines) < maxLines; idx++ {
		rowLines := m.renderRow(idx, m.dashboard.Rows[idx])
		for _, line := range rowLines {
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

	marker := " "
	if focused {
		marker = m.symbols.focus
	}

	titleWidth := clamp(m.width-95, 18, 56)
	title := fitPlain(row.PR.Title, titleWidth)
	badges := m.badges(row)
	summaryText := m.summary(summary, row.FetchError)
	updated := relativeTime(row.PR.UpdatedAt, m.now)
	heading := fmt.Sprintf("%s %s#%d %-*s %s %s updated %s",
		marker,
		row.PR.RepoFullName,
		row.PR.Number,
		titleWidth,
		title,
		badges,
		summaryText,
		updated,
	)
	if focused {
		heading = m.styles.focused.Render(heading)
	} else {
		heading = m.styles.row.Render(heading)
	}

	var lines []string
	lines = append(lines, heading)
	if row.FetchError != "" {
		lines = append(lines, "  "+m.styles.error.Render(fitPlain("actions unavailable: "+row.FetchError, max(20, m.width-4))))
		return lines
	}
	if len(row.Runs) == 0 {
		lines = append(lines, "  "+m.styles.muted.Render("no current GitHub Actions runs found"))
		return lines
	}

	if expanded {
		for _, run := range row.Runs {
			lines = append(lines, "  "+m.workflowLine(run, true))
		}
	} else {
		lines = append(lines, "  "+m.compactWorkflowLines(row.Runs))
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

func (m Model) compactWorkflowLines(runs []model.WorkflowRun) string {
	available := max(24, m.width-4)
	var parts []string
	for _, run := range runs {
		part := m.workflowLine(run, false)
		if lipgloss.Width(strings.Join(append(parts, part), "  ")) > available {
			if len(parts) == 0 {
				return fitANSI(part, available)
			}
			parts = append(parts, m.styles.muted.Render("..."))
			break
		}
		parts = append(parts, part)
	}
	return fitANSI(strings.Join(parts, "  "), available)
}

func (m Model) workflowLine(run model.WorkflowRun, expanded bool) string {
	nameWidth := 18
	if expanded {
		nameWidth = 28
	}
	label := m.styles.workflow.Render(fitPlain(run.Name, nameWidth) + ":")
	var chips []string
	for _, job := range run.Jobs {
		chips = append(chips, m.jobChip(job, expanded))
	}
	if len(chips) == 0 {
		chips = append(chips, m.styles.muted.Render("no jobs"))
	}
	return label + " " + strings.Join(chips, " ")
}

func (m Model) jobChip(job model.Job, expanded bool) string {
	symbol := m.symbols.forState(job.State, m.frame)
	nameWidth := 8
	if expanded {
		nameWidth = 18
	}
	text := symbol
	if expanded {
		text += " " + fitPlain(abbreviate(job.Name), nameWidth)
	} else {
		text += fitPlain(abbreviate(job.Name), nameWidth)
	}

	style := m.styles.forState(job.State)
	return style.Render(text)
}

func (m *Model) keepCursorVisible() {
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.dashboard.Rows) {
		m.cursor = max(0, len(m.dashboard.Rows)-1)
	}
	bodyRows := max(1, (m.height-4)/2)
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+bodyRows {
		m.offset = m.cursor - bodyRows + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *Model) focusApproximateRow(y int) {
	if y <= 0 || len(m.dashboard.Rows) == 0 {
		return
	}
	idx := m.offset + (y-1)/2
	if idx >= len(m.dashboard.Rows) {
		idx = len(m.dashboard.Rows) - 1
	}
	m.cursor = idx
	m.keepCursorVisible()
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
