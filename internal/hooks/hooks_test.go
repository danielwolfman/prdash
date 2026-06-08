package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danielwolfman/prdash/internal/config"
	"github.com/danielwolfman/prdash/internal/model"
)

func TestDispatcherFiresFirstFailureOncePerHead(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	runs := []model.WorkflowRun{{
		ID:      10,
		Name:    "ci",
		HeadSHA: pr.HeadSHA,
		Jobs: []model.Job{
			{
				ID:          101,
				RunID:       10,
				Name:        "unit",
				State:       model.CheckFailure,
				Status:      "completed",
				Conclusion:  "failure",
				URL:         "https://github.com/octo-org/prdash/actions/runs/10/job/101",
				CompletedAt: time.Date(2026, 6, 8, 8, 1, 0, 0, time.UTC),
			},
		},
	}}

	dispatcher.Observe(context.Background(), pr, runs)
	dispatcher.Observe(context.Background(), pr, runs)

	gotCalls := calls.collect(t, 2)
	if got := len(gotCalls); got != 2 {
		t.Fatalf("dispatch calls = %d, want first failure and completion", got)
	}
	failurePayload, ok := findPayload(gotCalls, EventFirstCheckFailure)
	if !ok {
		t.Fatalf("events = %#v, want %q", gotCalls, EventFirstCheckFailure)
	}
	if _, ok := findPayload(gotCalls, EventChecksCompleted); !ok {
		t.Fatalf("events = %#v, want %q", gotCalls, EventChecksCompleted)
	}
	if failurePayload.PrimaryJob == nil || failurePayload.PrimaryJob.Name != "unit" {
		t.Fatalf("primary job = %#v, want unit", failurePayload.PrimaryJob)
	}
	calls.assertNoMore(t)
}

func TestDispatcherDoesNotCompleteWhileChecksAreRunning(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()

	dispatcher.Observe(context.Background(), pr, []model.WorkflowRun{{
		ID:      10,
		Name:    "ci",
		HeadSHA: pr.HeadSHA,
		Jobs: []model.Job{
			{ID: 101, RunID: 10, Name: "unit", State: model.CheckSuccess, Status: "completed", Conclusion: "success"},
			{ID: 102, RunID: 10, Name: "integration", State: model.CheckRunning, Status: "in_progress"},
		},
	}})
	calls.assertNoMore(t)

	dispatcher.Observe(context.Background(), pr, []model.WorkflowRun{{
		ID:      10,
		Name:    "ci",
		HeadSHA: pr.HeadSHA,
		Jobs: []model.Job{
			{ID: 101, RunID: 10, Name: "unit", State: model.CheckSuccess, Status: "completed", Conclusion: "success"},
			{ID: 102, RunID: 10, Name: "integration", State: model.CheckSuccess, Status: "completed", Conclusion: "success"},
		},
	}})

	gotCalls := calls.collect(t, 1)
	if got := len(gotCalls); got != 1 {
		t.Fatalf("dispatch calls = %d, want completion only", got)
	}
	if gotCalls[0].Event != EventChecksCompleted {
		t.Fatalf("event = %q, want %q", gotCalls[0].Event, EventChecksCompleted)
	}
}

func TestRunCommandSendsPayloadOnStdin(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "payload.json")
	command := config.HookCommandConfig{
		Event:          EventFirstCheckFailure,
		Command:        []string{"sh", "-c", "cat > \"$1\"", "sh", out},
		TimeoutSeconds: 5,
	}
	payload := Payload{
		SchemaVersion: 1,
		Event:         EventFirstCheckFailure,
		PR:            PRPayload{RepoFullName: "octo-org/prdash", Number: 7},
		PrimaryJob:    &JobPayload{Name: "unit", URL: "https://example.test/job"},
	}

	if err := runCommand(context.Background(), command, payload); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var got Payload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Event != EventFirstCheckFailure || got.PrimaryJob == nil || got.PrimaryJob.Name != "unit" {
		t.Fatalf("payload = %#v", got)
	}
}

type payloadCollector struct {
	ch chan Payload
}

func (c payloadCollector) collect(t *testing.T, count int) []Payload {
	t.Helper()
	payloads := make([]Payload, 0, count)
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for len(payloads) < count {
		select {
		case payload := <-c.ch:
			payloads = append(payloads, payload)
		case <-timer.C:
			t.Fatalf("timed out waiting for %d payloads, got %d", count, len(payloads))
		}
	}
	return payloads
}

func (c payloadCollector) assertNoMore(t *testing.T) {
	t.Helper()
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case payload := <-c.ch:
		t.Fatalf("unexpected payload: %#v", payload)
	case <-timer.C:
	}
}

func findPayload(payloads []Payload, event string) (Payload, bool) {
	for _, payload := range payloads {
		if payload.Event == event {
			return payload, true
		}
	}
	return Payload{}, false
}

func testDispatcher(t *testing.T) (*Dispatcher, payloadCollector) {
	t.Helper()
	cfg := config.Default()
	cfg.Hooks.Enabled = true
	cfg.Hooks.StatePath = filepath.Join(t.TempDir(), "hooks-state.json")
	cfg.Hooks.Commands = []config.HookCommandConfig{
		{Event: EventFirstCheckFailure, Command: []string{"hook"}},
		{Event: EventChecksCompleted, Command: []string{"hook"}},
	}
	dispatcher, err := NewDispatcher(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := payloadCollector{ch: make(chan Payload, 10)}
	dispatcher.execute = func(_ context.Context, _ config.HookCommandConfig, payload Payload) error {
		calls.ch <- payload
		return nil
	}
	return dispatcher, calls
}

func testPR() model.PullRequest {
	return model.PullRequest{
		Owner:        "octo-org",
		Repo:         "prdash",
		RepoFullName: "octo-org/prdash",
		Number:       7,
		URL:          "https://github.com/octo-org/prdash/pull/7",
		HeadRefName:  "feature",
		HeadSHA:      "abc123",
		BaseRefName:  "main",
	}
}
