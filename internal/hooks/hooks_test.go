package hooks

import (
	"context"
	"encoding/json"
	"errors"
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

func TestDispatcherFiresCompletionAgainAfterRerun(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()

	dispatcher.Observe(context.Background(), pr, []model.WorkflowRun{{
		ID:         10,
		Name:       "ci",
		RunAttempt: 1,
		HeadSHA:    pr.HeadSHA,
		Jobs: []model.Job{
			{
				ID:          101,
				RunID:       10,
				Name:        "unit",
				State:       model.CheckFailure,
				Status:      "completed",
				Conclusion:  "failure",
				CompletedAt: time.Date(2026, 6, 8, 8, 1, 0, 0, time.UTC),
			},
		},
	}})
	initialCalls := calls.collect(t, 2)
	if _, ok := findPayload(initialCalls, EventFirstCheckFailure); !ok {
		t.Fatalf("events = %#v, want %q", initialCalls, EventFirstCheckFailure)
	}
	if _, ok := findPayload(initialCalls, EventChecksCompleted); !ok {
		t.Fatalf("events = %#v, want %q", initialCalls, EventChecksCompleted)
	}

	dispatcher.Observe(context.Background(), pr, []model.WorkflowRun{{
		ID:         10,
		Name:       "ci",
		RunAttempt: 2,
		HeadSHA:    pr.HeadSHA,
		Jobs: []model.Job{
			{ID: 201, RunID: 10, Name: "unit", State: model.CheckRunning, Status: "in_progress"},
		},
	}})
	calls.assertNoMore(t)

	dispatcher.Observe(context.Background(), pr, []model.WorkflowRun{{
		ID:         10,
		Name:       "ci",
		RunAttempt: 2,
		HeadSHA:    pr.HeadSHA,
		Jobs: []model.Job{
			{
				ID:          201,
				RunID:       10,
				Name:        "unit",
				State:       model.CheckSuccess,
				Status:      "completed",
				Conclusion:  "success",
				CompletedAt: time.Date(2026, 6, 8, 8, 10, 0, 0, time.UTC),
			},
		},
	}})

	rerunCalls := calls.collect(t, 1)
	if rerunCalls[0].Event != EventChecksCompleted {
		t.Fatalf("event = %q, want %q", rerunCalls[0].Event, EventChecksCompleted)
	}
	if rerunCalls[0].Summary.State != model.CheckSuccess {
		t.Fatalf("summary state = %q, want %q", rerunCalls[0].Summary.State, model.CheckSuccess)
	}
	calls.assertNoMore(t)
}

func TestDispatcherFiresCompletionForLegacyStateAfterRerun(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	key := stateKey(pr)
	dispatcher.state.PRHeads[key] = headState{
		FirstCheckFailureFired: true,
		ChecksCompletedFired:   true,
		LastState:              string(model.CheckRunning),
	}

	dispatcher.Observe(context.Background(), pr, []model.WorkflowRun{{
		ID:         10,
		Name:       "ci",
		RunAttempt: 2,
		HeadSHA:    pr.HeadSHA,
		Jobs: []model.Job{
			{
				ID:          201,
				RunID:       10,
				Name:        "unit",
				State:       model.CheckSuccess,
				Status:      "completed",
				Conclusion:  "success",
				CompletedAt: time.Date(2026, 6, 8, 8, 10, 0, 0, time.UTC),
			},
		},
	}})

	gotCalls := calls.collect(t, 1)
	if gotCalls[0].Event != EventChecksCompleted {
		t.Fatalf("event = %q, want %q", gotCalls[0].Event, EventChecksCompleted)
	}
	if dispatcher.state.PRHeads[key].LastChecksCompletedKey == "" {
		t.Fatal("last checks completed key was not recorded")
	}
	calls.assertNoMore(t)
}

func TestDispatcherDoesNotDuplicateLegacyTerminalCompletion(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	key := stateKey(pr)
	dispatcher.state.PRHeads[key] = headState{
		FirstCheckFailureFired: true,
		ChecksCompletedFired:   true,
		LastState:              string(model.CheckFailure),
	}

	dispatcher.Observe(context.Background(), pr, []model.WorkflowRun{{
		ID:         10,
		Name:       "ci",
		RunAttempt: 1,
		HeadSHA:    pr.HeadSHA,
		Jobs: []model.Job{
			{
				ID:          101,
				RunID:       10,
				Name:        "unit",
				State:       model.CheckFailure,
				Status:      "completed",
				Conclusion:  "failure",
				CompletedAt: time.Date(2026, 6, 8, 8, 1, 0, 0, time.UTC),
			},
		},
	}})

	if dispatcher.state.PRHeads[key].LastChecksCompletedKey == "" {
		t.Fatal("last checks completed key was not recorded")
	}
	calls.assertNoMore(t)
}

func TestDispatcherEmitsMergeConflictForDirtyMergeState(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	pr.MergeStateStatus = "DIRTY"

	dispatcher.Observe(context.Background(), pr, nil)
	dispatcher.Observe(context.Background(), pr, nil)

	gotCalls := calls.collect(t, 1)
	if gotCalls[0].Event != EventMergeConflict {
		t.Fatalf("event = %q, want %q", gotCalls[0].Event, EventMergeConflict)
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

func TestDispatcherFiresMergeConflictAfterCheckFailureOnSameHead(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	key := stateKey(pr)
	dispatcher.state.PRHeads[key] = headState{FirstCheckFailureFired: true}
	pr.MergeStateStatus = "DIRTY"

	dispatcher.Observe(context.Background(), pr, nil)

	gotCalls := calls.collect(t, 1)
	if gotCalls[0].Event != EventMergeConflict {
		t.Fatalf("event = %q, want %q", gotCalls[0].Event, EventMergeConflict)
	}
	calls.assertNoMore(t)
}

func TestDispatcherFiresMergeConflictAgainAfterItClears(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	pr.MergeStateStatus = "DIRTY"

	dispatcher.Observe(context.Background(), pr, nil)
	first := calls.collect(t, 1)
	if first[0].Event != EventMergeConflict {
		t.Fatalf("event = %q, want %q", first[0].Event, EventMergeConflict)
	}

	pr.MergeStateStatus = "CLEAN"
	dispatcher.Observe(context.Background(), pr, nil)
	pr.MergeStateStatus = "DIRTY"
	dispatcher.Observe(context.Background(), pr, nil)

	second := calls.collect(t, 1)
	if second[0].Event != EventMergeConflict {
		t.Fatalf("event = %q, want %q", second[0].Event, EventMergeConflict)
	}
	calls.assertNoMore(t)
}

func TestDispatcherEmitsStackRebaseRequiredOnTransition(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	pr.BaseSHA = "old-base"
	pr.StackNumber = 42
	pr.StackPosition = 2
	pr.StackSize = 4
	pr.StackNeedsRebase = true

	dispatcher.ObserveStackReadiness(context.Background(), pr)
	dispatcher.ObserveStackReadiness(context.Background(), pr)

	gotCalls := calls.collect(t, 1)
	if gotCalls[0].Event != EventStackRebaseRequired {
		t.Fatalf("event = %q, want %q", gotCalls[0].Event, EventStackRebaseRequired)
	}
	if gotCalls[0].PR.StackNumber != 42 || gotCalls[0].PR.StackPosition != 2 || !gotCalls[0].PR.StackNeedsRebase {
		t.Fatalf("stack payload = %#v", gotCalls[0].PR)
	}
	if gotCalls[0].PR.BaseSHA != "old-base" {
		t.Fatalf("base SHA = %q, want old-base", gotCalls[0].PR.BaseSHA)
	}
	calls.assertNoMore(t)
}

func TestDispatcherEmitsStackRebaseRequiredAgainAfterItClears(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	pr.StackNumber = 42
	pr.StackNeedsRebase = true

	dispatcher.ObserveStackReadiness(context.Background(), pr)
	first := calls.collect(t, 1)
	if first[0].Event != EventStackRebaseRequired {
		t.Fatalf("event = %q, want %q", first[0].Event, EventStackRebaseRequired)
	}

	pr.StackNeedsRebase = false
	dispatcher.ObserveStackReadiness(context.Background(), pr)
	pr.StackNeedsRebase = true
	dispatcher.ObserveStackReadiness(context.Background(), pr)

	second := calls.collect(t, 1)
	if second[0].Event != EventStackRebaseRequired {
		t.Fatalf("event = %q, want %q", second[0].Event, EventStackRebaseRequired)
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

func TestDispatcherEmitsNewOrChangedUnresolvedReviewThreads(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	line := 42
	originalLine := 41
	initial := model.PullRequestReviewThread{
		ID:            "PRRT_1",
		Path:          "internal/app/app.go",
		Line:          &line,
		OriginalLine:  &originalLine,
		DiffSide:      "RIGHT",
		StartDiffSide: "LEFT",
		Comments: []model.PullRequestReviewComment{
			{
				ID:        "PRRC_1",
				Author:    "reviewer",
				URL:       "https://github.com/octo-org/prdash/pull/7#discussion_r1",
				BodyText:  "existing comment",
				CreatedAt: time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC),
			},
		},
	}

	dispatcher.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{initial})
	initialCall := calls.collect(t, 1)[0]
	if initialCall.ReviewThread == nil || initialCall.ReviewThread.ID != "PRRT_1" {
		t.Fatalf("initial review thread payload = %#v", initialCall.ReviewThread)
	}
	if initialCall.ReviewThread.OriginalLine == nil || *initialCall.ReviewThread.OriginalLine != 41 || initialCall.ReviewThread.StartDiffSide != "LEFT" {
		t.Fatalf("initial review thread location = %#v", initialCall.ReviewThread)
	}
	dispatcher.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{initial})
	calls.assertNoMore(t)

	changed := initial
	changed.Comments = append([]model.PullRequestReviewComment(nil), initial.Comments...)
	changed.Comments[0].BodyText = "edited comment"
	changed.Comments[0].UpdatedAt = time.Date(2026, 6, 8, 8, 5, 0, 0, time.UTC)
	dispatcher.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{changed})

	changedCall := calls.collect(t, 1)[0]
	if changedCall.Event != EventReviewThreadChanged {
		t.Fatalf("event = %q, want %q", changedCall.Event, EventReviewThreadChanged)
	}
	if changedCall.ReviewThread == nil || changedCall.ReviewThread.ID != "PRRT_1" || changedCall.ReviewThread.IsResolved {
		t.Fatalf("review thread payload = %#v", changedCall.ReviewThread)
	}
	if len(changedCall.ReviewThread.Comments) != 1 || changedCall.ReviewThread.Comments[0].BodyText != "edited comment" {
		t.Fatalf("review comments = %#v", changedCall.ReviewThread.Comments)
	}

	resolved := changed
	resolved.IsResolved = true
	dispatcher.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{resolved})
	calls.assertNoMore(t)

	dispatcher.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{changed})
	reopenedCall := calls.collect(t, 1)[0]
	if reopenedCall.ReviewThread == nil || reopenedCall.ReviewThread.IsResolved {
		t.Fatalf("reopened review thread payload = %#v", reopenedCall.ReviewThread)
	}

	newThread := initial
	newThread.ID = "PRRT_2"
	newThread.Comments = append([]model.PullRequestReviewComment(nil), initial.Comments...)
	newThread.Comments[0].ID = "PRRC_2"
	dispatcher.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{changed, newThread})
	newCall := calls.collect(t, 1)[0]
	if newCall.ReviewThread == nil || newCall.ReviewThread.ID != "PRRT_2" {
		t.Fatalf("new review thread payload = %#v", newCall.ReviewThread)
	}
	calls.assertNoMore(t)
}

func TestReviewThreadFingerprintIncludesEmissionTriggers(t *testing.T) {
	line := 42
	startLine := 40
	originalLine := 41
	originalStartLine := 39
	base := model.PullRequestReviewThread{
		ID:                "PRRT_1",
		Path:              "internal/app/app.go",
		Line:              &line,
		StartLine:         &startLine,
		OriginalLine:      &originalLine,
		OriginalStartLine: &originalStartLine,
		DiffSide:          "RIGHT",
		StartDiffSide:     "RIGHT",
		Comments: []model.PullRequestReviewComment{
			{ID: "PRRC_1", BodyText: "please update this"},
		},
	}
	baseline := reviewThreadFingerprint(base)
	tests := []struct {
		name   string
		mutate func(*model.PullRequestReviewThread)
	}{
		{name: "reply", mutate: func(thread *model.PullRequestReviewThread) {
			thread.Comments = append(thread.Comments, model.PullRequestReviewComment{ID: "PRRC_2", BodyText: "follow-up"})
		}},
		{name: "outdated", mutate: func(thread *model.PullRequestReviewThread) {
			thread.IsOutdated = true
		}},
		{name: "line", mutate: func(thread *model.PullRequestReviewThread) {
			changedLine := 43
			thread.Line = &changedLine
		}},
		{name: "range", mutate: func(thread *model.PullRequestReviewThread) {
			changedStartLine := 38
			thread.OriginalStartLine = &changedStartLine
		}},
		{name: "diff side", mutate: func(thread *model.PullRequestReviewThread) {
			thread.StartDiffSide = "LEFT"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			changed.Comments = append([]model.PullRequestReviewComment(nil), base.Comments...)
			test.mutate(&changed)
			if reviewThreadFingerprint(changed) == baseline {
				t.Fatal("fingerprint did not change")
			}
		})
	}
}

func TestDispatcherRetriesReviewThreadAfterStateSaveFailure(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "hooks-state.json")
	dispatcher, calls := testDispatcherAt(t, statePath)
	pr := testPR()
	thread := model.PullRequestReviewThread{
		ID:         "PRRT_1",
		IsResolved: true,
		Path:       "internal/app/app.go",
		Comments:   []model.PullRequestReviewComment{{ID: "PRRC_1", BodyText: "please update this"}},
	}

	dispatcher.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{thread})
	calls.assertNoMore(t)
	persistedFingerprint := dispatcher.state.PRReviewThreads[activityStateKey(pr)].Seen[thread.ID]
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0o755); err != nil {
		t.Fatal(err)
	}

	thread.IsResolved = false
	dispatcher.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{thread})
	dispatcher.dispatchWG.Wait()
	if len(calls.ch) != 0 {
		t.Fatalf("dispatched payloads after failed state save = %d, want 0", len(calls.ch))
	}
	if got := dispatcher.state.PRReviewThreads[activityStateKey(pr)].Seen[thread.ID]; got != persistedFingerprint {
		t.Fatalf("fingerprint after failed state save = %q, want %q", got, persistedFingerprint)
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}

	dispatcher.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{thread})
	calls.collect(t, 1)
	dispatcher.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{thread})
	calls.assertNoMore(t)
}

func TestDispatcherPersistsReviewThreadStateAcrossRestarts(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "hooks-state.json")
	line := 42
	thread := model.PullRequestReviewThread{
		ID:   "PRRT_1",
		Path: "internal/app/app.go",
		Line: &line,
		Comments: []model.PullRequestReviewComment{
			{ID: "PRRC_1", BodyText: "please update this"},
		},
	}
	pr := testPR()

	first, firstCalls := testDispatcherAt(t, statePath)
	first.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{thread})
	firstCalls.collect(t, 1)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, secondCalls := testDispatcherAt(t, statePath)
	second.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{thread})
	secondCalls.assertNoMore(t)
	resolved := thread
	resolved.IsResolved = true
	second.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{resolved})
	secondCalls.assertNoMore(t)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	third, thirdCalls := testDispatcherAt(t, statePath)
	third.ObserveReviewThreads(context.Background(), pr, []model.PullRequestReviewThread{thread})
	reopened := thirdCalls.collect(t, 1)[0]
	if reopened.ReviewThread == nil || reopened.ReviewThread.IsResolved {
		t.Fatalf("reopened review thread payload = %#v", reopened.ReviewThread)
	}
	thirdCalls.assertNoMore(t)
}

func TestDispatcherAllowsOnlyOneMonitorWhenHooksAreDisabled(t *testing.T) {
	cfg := config.Default()
	cfg.Hooks.StatePath = filepath.Join(t.TempDir(), "hooks-state.json")

	first, err := NewDispatcher(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if _, err := NewDispatcher(cfg, nil); !errors.Is(err, ErrStateLocked) {
		t.Fatalf("second dispatcher error = %v, want %v", err, ErrStateLocked)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	replacement, err := NewDispatcher(cfg, nil)
	if err != nil {
		t.Fatalf("replacement dispatcher: %v", err)
	}
	t.Cleanup(func() { _ = replacement.Close() })
}

func TestDispatcherBoundsConcurrentHookCommands(t *testing.T) {
	dispatcher, _ := testDispatcher(t)
	started := make(chan struct{}, 20)
	executed := make(chan struct{}, 20)
	release := make(chan struct{})
	dispatcher.execute = func(_ context.Context, _ config.HookCommandConfig, _ Payload) error {
		executed <- struct{}{}
		started <- struct{}{}
		<-release
		return nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 20 {
			dispatcher.dispatch(context.Background(), Payload{Event: EventReviewThreadChanged})
		}
	}()
	for range maxConcurrentHookCommands {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for hook command")
		}
	}
	select {
	case <-started:
		t.Fatalf("more than %d hook commands started concurrently", maxConcurrentHookCommands)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out draining hook commands")
	}
	if err := dispatcher.Close(); err != nil {
		t.Fatal(err)
	}
	if len(executed) != 20 {
		t.Fatalf("executed hook commands = %d, want 20", len(executed))
	}
}

func TestDispatcherBaselinesThenFiresNewPRLifecycle(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	pr.Author = "octo-user"
	pr.State = "OPEN"
	pr.IsDraft = true
	pr.CreatedAt = time.Date(2026, 6, 8, 8, 0, 0, 0, time.UTC)

	dispatcher.ObserveLifecycles(context.Background(), []model.PullRequest{pr}, nil)
	calls.assertNoMore(t)

	newPR := pr
	newPR.Number = 8
	newPR.URL = "https://github.com/octo-org/prdash/pull/8"
	newPR.HeadSHA = "def456"
	dispatcher.ObserveLifecycles(context.Background(), []model.PullRequest{pr, newPR}, nil)

	gotCalls := calls.collect(t, 1)
	if gotCalls[0].Event != EventNewPRByAuthor {
		t.Fatalf("event = %q, want %q", gotCalls[0].Event, EventNewPRByAuthor)
	}
	if gotCalls[0].PR.Author != "octo-user" || gotCalls[0].PR.Number != 8 || gotCalls[0].PR.State != "OPEN" {
		t.Fatalf("pr payload = %#v", gotCalls[0].PR)
	}
	calls.assertNoMore(t)
}

func TestDispatcherFiresPRDiscoveredOncePerProcess(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "hooks-state.json")
	newDispatcher := func() (*Dispatcher, payloadCollector) {
		cfg := config.Default()
		cfg.Hooks.Enabled = true
		cfg.Hooks.StatePath = statePath
		cfg.Hooks.Commands = []config.HookCommandConfig{
			{Event: EventPRDiscovered, Command: []string{"hook"}},
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

	pr := testPR()
	dispatcher, calls := newDispatcher()
	dispatcher.ObserveLifecycles(context.Background(), []model.PullRequest{pr}, nil)
	got := calls.collect(t, 1)
	if got[0].Event != EventPRDiscovered || got[0].PR.Number != pr.Number {
		t.Fatalf("payload = %#v, want pr_discovered for PR %d", got[0], pr.Number)
	}
	dispatcher.ObserveLifecycles(context.Background(), []model.PullRequest{pr}, nil)
	calls.assertNoMore(t)
	if err := dispatcher.Close(); err != nil {
		t.Fatal(err)
	}

	// Discovery is process-local even though lifecycle dedupe state persists.
	restarted, restartedCalls := newDispatcher()
	restarted.ObserveLifecycles(context.Background(), []model.PullRequest{pr}, nil)
	restartedPayload := restartedCalls.collect(t, 1)
	if restartedPayload[0].Event != EventPRDiscovered {
		t.Fatalf("event after restart = %q, want %q", restartedPayload[0].Event, EventPRDiscovered)
	}
	restartedCalls.assertNoMore(t)
}

func TestDispatcherFiresReadyForReviewOncePerHead(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	pr.State = "OPEN"
	pr.IsDraft = true
	dispatcher.ObserveLifecycles(context.Background(), []model.PullRequest{pr}, nil)
	calls.assertNoMore(t)

	ready := pr
	ready.IsDraft = false
	dispatcher.ObserveLifecycles(context.Background(), []model.PullRequest{ready}, nil)
	calls.assertNoMore(t)
	dispatcher.ObserveLifecycles(context.Background(), []model.PullRequest{ready}, nil)

	gotCalls := calls.collect(t, 1)
	if gotCalls[0].Event != EventPRReadyForReview {
		t.Fatalf("event = %q, want %q", gotCalls[0].Event, EventPRReadyForReview)
	}
	if gotCalls[0].PR.IsDraft {
		t.Fatalf("ready payload still marked draft: %#v", gotCalls[0].PR)
	}
	calls.assertNoMore(t)
}

func TestDispatcherDoesNotFireReadyForReviewForTransientReadyObservation(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	pr.State = "OPEN"
	pr.IsDraft = true
	dispatcher.ObserveLifecycles(context.Background(), []model.PullRequest{pr}, nil)
	calls.assertNoMore(t)

	ready := pr
	ready.IsDraft = false
	dispatcher.ObserveLifecycles(context.Background(), []model.PullRequest{ready}, nil)
	calls.assertNoMore(t)

	dispatcher.ObserveLifecycles(context.Background(), []model.PullRequest{pr}, nil)
	calls.assertNoMore(t)
}

func TestDispatcherFiresMergedAfterVerifiedMissingPR(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	pr.State = "OPEN"
	dispatcher.ObserveLifecycles(context.Background(), []model.PullRequest{pr}, nil)
	calls.assertNoMore(t)

	merged := pr
	merged.State = "MERGED"
	merged.Merged = true
	merged.MergedAt = time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)
	dispatcher.ObserveLifecycles(context.Background(), nil, func(context.Context, model.PullRequest) (model.PullRequest, error) {
		return merged, nil
	})
	dispatcher.ObserveLifecycles(context.Background(), nil, func(context.Context, model.PullRequest) (model.PullRequest, error) {
		return merged, nil
	})

	gotCalls := calls.collect(t, 1)
	if gotCalls[0].Event != EventPRMerged {
		t.Fatalf("event = %q, want %q", gotCalls[0].Event, EventPRMerged)
	}
	if !gotCalls[0].PR.Merged || gotCalls[0].PR.MergedAt == "" {
		t.Fatalf("merged payload = %#v", gotCalls[0].PR)
	}
	calls.assertNoMore(t)
}

func TestDispatcherFiresClosedAfterVerifiedMissingPR(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	pr.State = "OPEN"
	dispatcher.ObserveLifecycles(context.Background(), []model.PullRequest{pr}, nil)
	calls.assertNoMore(t)

	closed := pr
	closed.State = "CLOSED"
	closed.ClosedAt = time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC)
	dispatcher.ObserveLifecycles(context.Background(), nil, func(context.Context, model.PullRequest) (model.PullRequest, error) {
		return closed, nil
	})

	gotCalls := calls.collect(t, 1)
	if gotCalls[0].Event != EventPRClosed {
		t.Fatalf("event = %q, want %q", gotCalls[0].Event, EventPRClosed)
	}
	if gotCalls[0].PR.ClosedAt == "" || gotCalls[0].PR.Merged {
		t.Fatalf("closed payload = %#v", gotCalls[0].PR)
	}
	calls.assertNoMore(t)
}

func TestDispatcherDoesNotCloseMissingButStillOpenPR(t *testing.T) {
	dispatcher, calls := testDispatcher(t)
	pr := testPR()
	pr.State = "OPEN"
	dispatcher.ObserveLifecycles(context.Background(), []model.PullRequest{pr}, nil)
	calls.assertNoMore(t)

	dispatcher.ObserveLifecycles(context.Background(), nil, func(context.Context, model.PullRequest) (model.PullRequest, error) {
		return pr, nil
	})
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
	return testDispatcherAt(t, filepath.Join(t.TempDir(), "hooks-state.json"))
}

func testDispatcherAt(t *testing.T, statePath string) (*Dispatcher, payloadCollector) {
	t.Helper()
	cfg := config.Default()
	cfg.Hooks.Enabled = true
	cfg.Hooks.StatePath = statePath
	cfg.Hooks.Commands = []config.HookCommandConfig{
		{Event: EventFirstCheckFailure, Command: []string{"hook"}},
		{Event: EventMergeConflict, Command: []string{"hook"}},
		{Event: EventStackRebaseRequired, Command: []string{"hook"}},
		{Event: EventChecksCompleted, Command: []string{"hook"}},
		{Event: EventNewPRActivity, Command: []string{"hook"}},
		{Event: EventReviewThreadChanged, Command: []string{"hook"}},
		{Event: EventNewPRByAuthor, Command: []string{"hook"}},
		{Event: EventPRReadyForReview, Command: []string{"hook"}},
		{Event: EventPRClosed, Command: []string{"hook"}},
		{Event: EventPRMerged, Command: []string{"hook"}},
	}
	dispatcher, err := NewDispatcher(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dispatcher.Close() })
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
		Author:       "octo-user",
		State:        "OPEN",
		HeadRefName:  "feature",
		HeadSHA:      "abc123",
		BaseRefName:  "main",
	}
}
