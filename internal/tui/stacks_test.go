package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/danielwolfman/prdash/internal/model"
)

func stackRow(number int, base, head string, updated int) Row {
	return Row{PR: model.PullRequest{RepoFullName: "org/repo", HeadRepoFullName: "org/repo", Number: number, BaseRefName: base, HeadRefName: head, UpdatedAt: time.Unix(int64(updated), 0)}}
}

func TestGroupsNativeAndBranchStacksInDependencyOrder(t *testing.T) {
	lower := stackRow(1, "main", "lower", 10)
	upper := stackRow(2, "lower", "upper", 90)
	lower.PR.StackNumber, lower.PR.StackPosition = 100, 1
	upper.PR.StackNumber, upper.PR.StackPosition = 100, 3 // closed/hidden middle member
	extension := stackRow(3, "upper", "extension", 80)
	extension.PR.StackNumber, extension.PR.StackPosition = 200, 1
	independent := stackRow(4, "main", "independent", 100)
	rows := groupRows([]Row{extension, lower, independent, upper})
	var numbers []int
	for _, row := range rows {
		numbers = append(numbers, row.PR.Number)
	}
	if !reflect.DeepEqual(numbers, []int{4, 1, 2, 3}) {
		t.Fatalf("order = %v", numbers)
	}
	if rows[0].group.Key != "" {
		t.Fatal("independent PR was grouped")
	}
	for i, row := range rows[1:] {
		if row.group.Key != rows[1].group.Key || row.group.Position != i+1 || row.group.Size != 3 {
			t.Fatalf("group = %+v", row.group)
		}
		if !strings.Contains(row.group.Label, "#100 + #200") {
			t.Fatalf("label = %s", row.group.Label)
		}
	}
}

func TestBranchGroupsRespectRepositoryAndForkIdentity(t *testing.T) {
	parent := stackRow(1, "main", "feature", 1)
	child := stackRow(2, "feature", "child", 2)
	other := stackRow(3, "feature", "other", 3)
	other.PR.RepoFullName, other.PR.HeadRepoFullName = "other/repo", "other/repo"
	fork := stackRow(4, "main", "feature", 4)
	fork.PR.HeadRepoFullName = "fork/repo"
	rows := groupRows([]Row{child, parent, other, fork})
	for _, row := range rows {
		if (row.PR.Number == 1 || row.PR.Number == 2) != (row.group.Key != "") {
			t.Fatalf("unexpected grouping: %+v", row)
		}
	}
}

func TestCyclicBranchesRemainVisible(t *testing.T) {
	rows := groupRows([]Row{stackRow(1, "b", "a", 1), stackRow(2, "a", "b", 2)})
	if len(rows) != 2 || !strings.Contains(rows[0].group.Label, "cyclic") {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestRefreshPreservesSelectionExpansionAndGroup(t *testing.T) {
	a, b := stackRow(1, "main", "a", 1), stackRow(2, "main", "b", 2)
	m := New(Dashboard{Rows: []Row{a, b}})
	m.cursor = 1 // a
	m.expanded[1], m.jobCursor[1] = true, -1
	a.PR.UpdatedAt = time.Unix(3, 0)
	m.replaceRows([]Row{a, b})
	if m.dashboard.Rows[m.cursor].PR.Number != 1 || !m.expanded[m.cursor] {
		t.Fatal("selection moved to a different PR")
	}
	b.PR.BaseRefName = "a"
	m.replaceRows([]Row{b, a})
	m.applyLoadEvent(LoadEvent{Row: &b})
	if m.dashboard.Rows[1].group.Position != 2 {
		t.Fatal("job refresh erased group")
	}
}

func TestLayoutWidthsBranchesAndMouseTargets(t *testing.T) {
	d := sampleDashboard("unicode")
	d.HideOwner = true
	d.EventsDisabled = true
	d.Rows[0].PR.BaseRefName, d.Rows[0].PR.HeadRefName = "main", "feature/dashboard"
	d.Rows[0].PR.Title = strings.Repeat("界", 100)
	m := New(d)
	for _, width := range []int{40, 80, 140, 220} {
		m.width, m.height = width, 80
		for _, line := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d overflow: %q", width, line)
			}
		}
	}
	m.width = 140
	view := stripANSI(m.View())
	for _, want := range []string{"main ← feature/dashboard", "events off", "prdash#12"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(view, "octo-org/") {
		t.Fatal("owner still visible")
	}
	lines := m.renderRows(100)
	for _, item := range m.selectableItems() {
		if item.Kind != selectionJob {
			continue
		}
		if !strings.Contains(stripANSI(lines[item.Line]), item.Label) && item.Label != "build" {
			t.Fatalf("wrong job line %d: %s", item.Line, lines[item.Line])
		}
		updated, _ := m.Update(tea.MouseMsg{Type: tea.MouseLeft, Y: item.Line + 1})
		got := updated.(Model)
		if got.cursor != item.Row || got.currentJobCursor() != item.Job {
			t.Fatal("mouse selected wrong job")
		}
	}
}

func TestJobDuration(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	m := New(Dashboard{SnapshotAt: now})
	m.width = 160
	job := model.Job{Name: "test", State: model.CheckRunning, StartedAt: now.Add(-65 * time.Second)}
	line := stripANSI(m.jobLine(job))
	for _, want := range []string{"1m5s", "running"} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q: %s", want, line)
		}
	}
	job.State, job.CompletedAt = model.CheckSuccess, now.Add(-5*time.Second)
	if line := stripANSI(m.jobLine(job)); !strings.Contains(line, "1m0s") {
		t.Fatalf("duration must stop: %s", line)
	}
}

func TestJobNameUsesAvailableWindowWidth(t *testing.T) {
	m := New(Dashboard{})
	job := model.Job{Name: strings.Repeat("Long job name ", 12) + "END", State: model.CheckSuccess}
	m.width = 140
	narrow := stripANSI(m.jobLine(job))
	if !strings.Contains(narrow, "…") || strings.Contains(narrow, "END") {
		t.Fatalf("expected truncated name: %s", narrow)
	}
	m.width = 240
	wide := stripANSI(m.jobLine(job))
	if !strings.Contains(wide, job.Name) {
		t.Fatalf("wide window should show the full name: %s", wide)
	}
	for _, width := range []int{80, 140, 240} {
		m.width = width
		line := stripANSI(m.jobLine(job))
		if lipgloss.Width(line) != width-4 || !strings.HasSuffix(strings.TrimSpace(line), "—") {
			t.Fatalf("width %d lost status alignment: %s", width, line)
		}
	}
}
