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

func TestDispatcherTreatsDirtyMergeStateAsFirstFailure(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	pr.MergeStateStatus = "DIRTY"

	dispatcher.Observe(context.Background(), pr, nil)
	dispatcher.Observe(context.Background(), pr, nil)

	gotCalls := calls.collect(t, 1)
	if gotCalls[0].Event != EventFirstCheckFailure {
		t.Fatalf("event = %q, want %q", gotCalls[0].Event, EventFirstCheckFailure)
	}
	if gotCalls[0].Summary.State != model.CheckFailure {
		t.Fatalf("summary state = %q, want %q", gotCalls[0].Summary.State, model.CheckFailure)
	}
	if gotCalls[0].PR.MergeStateStatus != "DIRTY" {
		t.Fatalf("merge state = %q, want DIRTY", gotCalls[0].PR.MergeStateStatus)
	}
	if gotCalls[0].PrimaryJob != nil {
		t.Fatalf("primary job = %#v, want nil for dirty-only failure", gotCalls[0].PrimaryJob)
	}
	calls.assertNoMore(t)
}

func TestDispatcherBaselinesThenFiresNewPRActivity(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	initial := []model.PullRequestActivity{
		{
			ID:        "IC_1",
			Kind:      model.ActivityIssueComment,
			Author:    "reviewer",
			URL:       "https://github.com/octo-org/prdash/pull/7#issuecomment-1",
			BodyText:  "existing comment",
			CreatedAt: time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC),
		},
	}

	dispatcher.ObserveActivities(context.Background(), pr, initial)
	calls.assertNoMore(t)

	dispatcher.ObserveActivities(context.Background(), pr, append(initial, model.PullRequestActivity{
		ID:        "PRR_1",
		Kind:      model.ActivityPullRequestReview,
		Author:    "maintainer",
		URL:       "https://github.com/octo-org/prdash/pull/7#pullrequestreview-1",
		BodyText:  "please fix",
		State:     "CHANGES_REQUESTED",
		CreatedAt: time.Date(2026, 6, 8, 8, 5, 0, 0, time.UTC),
	}))

	gotCalls := calls.collect(t, 1)
	if gotCalls[0].Event != EventNewPRActivity {
		t.Fatalf("event = %q, want %q", gotCalls[0].Event, EventNewPRActivity)
	}
	if gotCalls[0].Activity == nil || gotCalls[0].Activity.Kind != model.ActivityPullRequestReview || gotCalls[0].Activity.State != "CHANGES_REQUESTED" {
		t.Fatalf("activity payload = %#v", gotCalls[0].Activity)
	}
	calls.assertNoMore(t)
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
		{Event: EventNewPRActivity, Command: []string{"hook"}},
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
