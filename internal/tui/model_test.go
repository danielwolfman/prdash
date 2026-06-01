package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/danielwolfman/prdash/internal/model"
)

func TestViewRendersDenseDashboard(t *testing.T) {
	m := New(sampleDashboard("unicode"))
	m.width = 140
	m.height = 18

	view := stripANSI(m.View())
	for _, want := range []string{
		"prdash",
		"octo-user",
		"octo-org/prdash#12",
		"Add dense dashboard",
		"draft",
		"1 fail",
		"1 run",
		"1 ok",
		"CI",
		"integ",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}
}

func TestViewSupportsASCIISymbols(t *testing.T) {
	m := New(sampleDashboard("ascii"))
	m.width = 120
	m.height = 14

	view := stripANSI(m.View())
	if strings.Contains(view, "✓") || strings.Contains(view, "✗") || strings.Contains(view, "▸") {
		t.Fatalf("ascii view contains unicode symbols:\n%s", view)
	}
	for _, want := range []string{">", "x CI", "v CI", "bld"} {
		if !strings.Contains(view, want) {
			t.Fatalf("ascii view missing %q:\n%s", want, view)
		}
	}
}

func TestNarrowViewKeepsJobRowsVisible(t *testing.T) {
	m := New(sampleDashboard("unicode"))
	m.width = 80
	m.height = 12

	view := stripANSI(m.View())
	if !strings.Contains(view, "CI") || !strings.Contains(view, "integration") {
		t.Fatalf("narrow view should keep job rows visible:\n%s", view)
	}
}

func TestLoadEventsAppendAndUpdateRows(t *testing.T) {
	m := New(Dashboard{Animations: false, Loader: func(ctx context.Context, refresh <-chan struct{}, events chan<- LoadEvent) {}})

	m.applyLoadEvent(LoadEvent{User: "octo-user", TotalDiscovered: 1, Message: "loading jobs"})
	m.applyLoadEvent(LoadEvent{Row: &Row{PR: model.PullRequest{RepoFullName: "octo-org/prdash", Number: 12}, Loading: true}})
	if len(m.dashboard.Rows) != 1 || !m.dashboard.Rows[0].Loading {
		t.Fatalf("expected loading skeleton row: %+v", m.dashboard.Rows)
	}

	m.applyLoadEvent(LoadEvent{Row: &Row{PR: model.PullRequest{RepoFullName: "octo-org/prdash", Number: 12}, Runs: []model.WorkflowRun{{Name: "CI"}}}})
	if len(m.dashboard.Rows) != 1 || m.dashboard.Rows[0].Loading || len(m.dashboard.Rows[0].Runs) != 1 {
		t.Fatalf("expected row update in place: %+v", m.dashboard.Rows)
	}
}

func TestLoadingRefreshKeepsCachedJobsVisible(t *testing.T) {
	m := New(sampleDashboard("unicode"))
	m.width = 220
	m.height = 16

	m.applyLoadEvent(LoadEvent{Row: &Row{
		PR:      m.dashboard.Rows[0].PR,
		Loading: true,
	}})

	if len(m.dashboard.Rows[0].Runs) == 0 {
		t.Fatalf("expected cached runs to be preserved")
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "integration tests") {
		t.Fatalf("cached job details should stay visible during refresh:\n%s", view)
	}
	if strings.Contains(view, "loading jobs...") {
		t.Fatalf("refresh should not replace cached job details with loading only:\n%s", view)
	}
	if !strings.Contains(view, "refreshing") {
		t.Fatalf("refresh indicator missing:\n%s", view)
	}
}

func TestLoadEventsMarkChangedWhenSummaryChanges(t *testing.T) {
	now := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	m := New(Dashboard{SnapshotAt: now, Animations: false})
	pr := model.PullRequest{RepoFullName: "octo-org/prdash", Number: 12}

	m.applyLoadEvent(LoadEvent{SnapshotAt: now, Row: &Row{
		PR:          pr,
		LastFetched: now,
		Runs:        []model.WorkflowRun{{Jobs: []model.Job{{State: model.CheckSuccess}}}},
	}})
	m.applyLoadEvent(LoadEvent{SnapshotAt: now.Add(time.Second), Row: &Row{
		PR:   pr,
		Runs: []model.WorkflowRun{{Jobs: []model.Job{{State: model.CheckFailure}}}},
	}})

	row := m.dashboard.Rows[0]
	if row.ChangeState != model.CheckFailure || !row.ChangedUntil.After(m.now) {
		t.Fatalf("expected failure change marker, got %+v", row)
	}
}

func TestRowsBecomeStaleAfterRefreshWindow(t *testing.T) {
	now := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	m := New(sampleDashboard("unicode"))
	m.dashboard.StaleAfter = time.Minute
	m.dashboard.Rows[0].LastFetched = now.Add(-2 * time.Minute)
	m.now = now

	if !m.rowStale(m.dashboard.Rows[0]) {
		t.Fatalf("expected row to be stale")
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "stale") {
		t.Fatalf("view missing stale marker:\n%s", view)
	}
}

func TestKeyboardNavigationAndExpansion(t *testing.T) {
	m := New(sampleDashboard("unicode"))
	m.width = 120
	m.height = 20

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !m.expanded[1] {
		t.Fatalf("row 1 should be expanded")
	}
}

func TestMouseWheelScrollsByRenderedLines(t *testing.T) {
	dashboard := sampleDashboard("unicode")
	for i := 0; i < 6; i++ {
		row := dashboard.Rows[0]
		row.PR.Number = 100 + i
		row.PR.Title = "Extra PR"
		dashboard.Rows = append(dashboard.Rows, row)
	}
	m := New(dashboard)
	m.width = 100
	m.height = 10

	for i := 0; i < 4; i++ {
		updated, _ := m.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
		m = updated.(Model)
	}
	if m.offset == 0 {
		t.Fatalf("expected mouse wheel to advance line offset")
	}
	if m.cursor == 0 {
		t.Fatalf("expected cursor to follow first visible row")
	}

	view := stripANSI(m.View())
	if strings.Contains(view, "octo-org/prdash#12") && !strings.Contains(view, "octo-org/prdash#100") {
		t.Fatalf("view did not scroll into later rendered rows:\n%s", view)
	}
}

func TestRerunFailedJobsPlanningAndConfirmation(t *testing.T) {
	dashboard := sampleDashboard("unicode")
	dashboard.ActionsEnabled = true
	var captured ActionRequest
	dashboard.ActionExecutor = func(ctx context.Context, request ActionRequest) ActionResult {
		captured = request
		return ActionResult{Request: request, Message: "rerun requested"}
	}
	m := New(dashboard)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if cmd != nil {
		t.Fatalf("rerun planning should wait for confirmation")
	}
	if m.confirm == nil {
		t.Fatalf("expected confirmation")
	}
	if m.confirm.request.JobCount != 1 || m.confirm.request.WorkflowCount != 1 || len(m.confirm.request.RunIDs) != 1 || m.confirm.request.RunIDs[0] != 123 {
		t.Fatalf("unexpected request: %+v", m.confirm.request)
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatalf("confirming rerun should return command")
	}
	if !m.actionBusy {
		t.Fatalf("expected action to be busy")
	}

	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(Model)
	if m.actionBusy {
		t.Fatalf("expected action to finish")
	}
	if captured.Kind != ActionRerunFailedJobs || captured.Owner != "octo-org" || captured.Repo != "prdash" || captured.PRNumber != 12 {
		t.Fatalf("unexpected captured request: %+v", captured)
	}
	if !strings.Contains(m.actionText, "rerun requested") {
		t.Fatalf("expected action text to show result, got %q", m.actionText)
	}
}

func TestRerunDisabledDoesNotOpenConfirmation(t *testing.T) {
	m := New(sampleDashboard("unicode"))

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if cmd != nil {
		t.Fatalf("disabled rerun should not return command")
	}
	if m.confirm != nil {
		t.Fatalf("disabled rerun should not open confirmation")
	}
	if !strings.Contains(m.actionText, "disabled") {
		t.Fatalf("expected disabled action text, got %q", m.actionText)
	}
}

func TestRerunSkipsActiveWorkflowRuns(t *testing.T) {
	dashboard := sampleDashboard("unicode")
	dashboard.ActionsEnabled = true
	dashboard.ActionExecutor = func(ctx context.Context, request ActionRequest) ActionResult {
		t.Fatalf("active workflow rerun should not execute: %+v", request)
		return ActionResult{}
	}
	dashboard.Rows[0].Runs[0].Status = "in_progress"
	m := New(dashboard)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if cmd != nil {
		t.Fatalf("active workflow rerun should not return command")
	}
	if m.confirm != nil {
		t.Fatalf("active workflow rerun should not open confirmation")
	}
	if !strings.Contains(m.actionText, "no completed failed jobs") {
		t.Fatalf("expected no failed jobs action text, got %q", m.actionText)
	}
}

func TestSuccessfulActionRequestsHotRefresh(t *testing.T) {
	m := New(Dashboard{
		Animations: false,
		Loader:     func(ctx context.Context, refresh <-chan struct{}, events chan<- LoadEvent) {},
	})

	updated, _ := m.Update(actionResultMsg{Message: "rerun requested"})
	m = updated.(Model)

	if !m.loading {
		t.Fatalf("expected successful action to mark loading")
	}
	if !strings.Contains(m.loadText, "refresh requested") {
		t.Fatalf("expected refresh load text, got %q", m.loadText)
	}
	select {
	case <-m.refresh:
	default:
		t.Fatalf("expected hot refresh signal")
	}
}

func TestFailedActionDoesNotRequestHotRefresh(t *testing.T) {
	m := New(Dashboard{
		Animations: false,
		Loader:     func(ctx context.Context, refresh <-chan struct{}, events chan<- LoadEvent) {},
	})

	updated, _ := m.Update(actionResultMsg{Error: "github api 403"})
	m = updated.(Model)

	select {
	case <-m.refresh:
		t.Fatalf("failed action should not request refresh")
	default:
	}
}

func sampleDashboard(symbols string) Dashboard {
	now := time.Date(2026, 6, 1, 15, 0, 0, 0, time.UTC)
	return Dashboard{
		User:            "octo-user",
		SnapshotAt:      now,
		TotalDiscovered: 2,
		Symbols:         symbols,
		Animations:      false,
		Rows: []Row{
			{
				PR: model.PullRequest{
					Owner:            "octo-org",
					Repo:             "prdash",
					RepoFullName:     "octo-org/prdash",
					Number:           12,
					Title:            "Add dense dashboard",
					IsDraft:          true,
					UpdatedAt:        now.Add(-4 * time.Minute),
					MergeStateStatus: "BLOCKED",
					ReviewDecision:   "REVIEW_REQUIRED",
				},
				Runs: []model.WorkflowRun{
					{
						ID:         123,
						Name:       "CI",
						RunAttempt: 2,
						Status:     "completed",
						UpdatedAt:  now.Add(-2 * time.Minute),
						Jobs: []model.Job{
							{Name: "build", RunID: 123, State: model.CheckSuccess},
							{Name: "integration tests", RunID: 123, State: model.CheckFailure},
							{Name: "e2e", RunID: 123, State: model.CheckRunning},
						},
					},
				},
			},
			{
				PR: model.PullRequest{
					Owner:        "octo-org",
					Repo:         "api",
					RepoFullName: "octo-org/api",
					Number:       99,
					Title:        "Handle partial Actions errors",
					UpdatedAt:    now.Add(-10 * time.Minute),
				},
				FetchError: "403 forbidden",
			},
		},
	}
}
