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
	m := New(Dashboard{Animations: false, Loader: func(ctx context.Context, events chan<- LoadEvent) {}})

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
						Name:       "CI",
						RunAttempt: 2,
						UpdatedAt:  now.Add(-2 * time.Minute),
						Jobs: []model.Job{
							{Name: "build", State: model.CheckSuccess},
							{Name: "integration tests", State: model.CheckFailure},
							{Name: "e2e", State: model.CheckRunning},
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
