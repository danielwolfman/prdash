package hooks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/danielwolfman/prdash/internal/config"
	"github.com/danielwolfman/prdash/internal/model"
)

const (
	EventFirstCheckFailure = "first_check_failure"
	EventChecksCompleted   = "checks_completed"
	EventNewPRActivity     = "new_pr_comment_or_review"
	EventNewPRByAuthor     = "new_pr_by_author"
	EventPRReadyForReview  = "pr_ready_for_review"
	EventPRClosed          = "pr_closed"
	EventPRMerged          = "pr_merged"
)

const defaultTimeout = 30 * time.Second

type Logger interface {
	Info(string, map[string]any)
	Warn(string, map[string]any)
	Error(string, map[string]any)
}

type Executor func(context.Context, config.HookCommandConfig, Payload) error

type Dispatcher struct {
	enabled   bool
	host      string
	commands  []config.HookCommandConfig
	statePath string
	logger    Logger
	execute   Executor
	now       func() time.Time

	mu    sync.Mutex
	state stateFile
}

type Payload struct {
	SchemaVersion int              `json:"schema_version"`
	Event         string           `json:"event"`
	ObservedAt    string           `json:"observed_at"`
	GitHubHost    string           `json:"github_host"`
	PR            PRPayload        `json:"pr"`
	Summary       SummaryPayload   `json:"summary"`
	Activity      *ActivityPayload `json:"activity,omitempty"`
	PrimaryJob    *JobPayload      `json:"primary_job,omitempty"`
	FailedJobs    []JobPayload     `json:"failed_jobs,omitempty"`
	WorkflowRuns  []RunPayload     `json:"workflow_runs"`
}

type PRPayload struct {
	Owner            string `json:"owner"`
	Repo             string `json:"repo"`
	RepoFullName     string `json:"repo_full_name"`
	Number           int    `json:"number"`
	URL              string `json:"url"`
	Author           string `json:"author"`
	State            string `json:"state"`
	Merged           bool   `json:"merged"`
	IsDraft          bool   `json:"is_draft"`
	CreatedAt        string `json:"created_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
	ClosedAt         string `json:"closed_at,omitempty"`
	MergedAt         string `json:"merged_at,omitempty"`
	HeadRefName      string `json:"head_ref_name"`
	HeadSHA          string `json:"head_sha"`
	BaseRefName      string `json:"base_ref_name"`
	MergeStateStatus string `json:"merge_state_status"`
	ReviewDecision   string `json:"review_decision"`
}

type SummaryPayload struct {
	State          model.CheckState `json:"state"`
	Total          int              `json:"total"`
	ActionRequired int              `json:"action_required"`
	Failure        int              `json:"failure"`
	Cancelled      int              `json:"cancelled"`
	Running        int              `json:"running"`
	Waiting        int              `json:"waiting"`
	Unknown        int              `json:"unknown"`
	Stale          int              `json:"stale"`
	Success        int              `json:"success"`
	Neutral        int              `json:"neutral"`
}

type RunPayload struct {
	ID         int64        `json:"id"`
	Name       string       `json:"name"`
	WorkflowID int64        `json:"workflow_id"`
	RunNumber  int          `json:"run_number"`
	RunAttempt int          `json:"run_attempt"`
	Event      string       `json:"event"`
	Status     string       `json:"status"`
	Conclusion string       `json:"conclusion"`
	URL        string       `json:"url"`
	HeadSHA    string       `json:"head_sha"`
	UpdatedAt  string       `json:"updated_at,omitempty"`
	Jobs       []JobPayload `json:"jobs"`
}

type JobPayload struct {
	ID          int64            `json:"id"`
	RunID       int64            `json:"run_id"`
	Name        string           `json:"name"`
	Status      string           `json:"status"`
	Conclusion  string           `json:"conclusion"`
	State       model.CheckState `json:"state"`
	URL         string           `json:"url"`
	StartedAt   string           `json:"started_at,omitempty"`
	CompletedAt string           `json:"completed_at,omitempty"`
}

type ActivityPayload struct {
	ID        string                        `json:"id"`
	Kind      model.PullRequestActivityKind `json:"kind"`
	Author    string                        `json:"author"`
	URL       string                        `json:"url"`
	BodyText  string                        `json:"body_text"`
	State     string                        `json:"state,omitempty"`
	CreatedAt string                        `json:"created_at,omitempty"`
	UpdatedAt string                        `json:"updated_at,omitempty"`
}

type stateFile struct {
	PRHeads                map[string]headState      `json:"pr_heads"`
	PRActivities           map[string]activityState  `json:"pr_activities"`
	PRLifecycles           map[string]lifecycleState `json:"pr_lifecycles"`
	PRLifecycleInitialized bool                      `json:"pr_lifecycle_initialized,omitempty"`
}

type headState struct {
	FirstCheckFailureFired bool   `json:"first_check_failure_fired,omitempty"`
	ChecksCompletedFired   bool   `json:"checks_completed_fired,omitempty"`
	LastChecksCompletedKey string `json:"last_checks_completed_key,omitempty"`
	LastState              string `json:"last_state,omitempty"`
	UpdatedAt              string `json:"updated_at,omitempty"`
}

type activityState struct {
	Initialized bool              `json:"initialized,omitempty"`
	Seen        map[string]string `json:"seen,omitempty"`
	UpdatedAt   string            `json:"updated_at,omitempty"`
}

type lifecycleState struct {
	Owner                 string `json:"owner,omitempty"`
	Repo                  string `json:"repo,omitempty"`
	RepoFullName          string `json:"repo_full_name,omitempty"`
	Number                int    `json:"number,omitempty"`
	URL                   string `json:"url,omitempty"`
	Author                string `json:"author,omitempty"`
	State                 string `json:"state,omitempty"`
	Merged                bool   `json:"merged,omitempty"`
	Open                  bool   `json:"open,omitempty"`
	IsDraft               bool   `json:"is_draft,omitempty"`
	HeadSHA               string `json:"head_sha,omitempty"`
	ReadyForReviewHeadSHA string `json:"ready_for_review_head_sha,omitempty"`
	ClosedEventFired      bool   `json:"closed_event_fired,omitempty"`
	MergedEventFired      bool   `json:"merged_event_fired,omitempty"`
	CreatedAt             string `json:"created_at,omitempty"`
	UpdatedAt             string `json:"updated_at,omitempty"`
	ClosedAt              string `json:"closed_at,omitempty"`
	MergedAt              string `json:"merged_at,omitempty"`
	ObservedAt            string `json:"observed_at,omitempty"`
}

type PullRequestLookup func(context.Context, model.PullRequest) (model.PullRequest, error)

func NewDispatcher(cfg config.Config, logger Logger) (*Dispatcher, error) {
	commands := validCommands(cfg.Hooks.Commands, logger)
	statePath, err := ResolveStatePath(cfg.Hooks.StatePath)
	if err != nil {
		return nil, err
	}
	dispatcher := &Dispatcher{
		enabled:   cfg.Hooks.Enabled && len(commands) > 0,
		host:      cfg.GitHub.Host,
		commands:  commands,
		statePath: statePath,
		logger:    logger,
		execute:   runCommand,
		now:       time.Now,
		state: stateFile{
			PRHeads:      map[string]headState{},
			PRActivities: map[string]activityState{},
			PRLifecycles: map[string]lifecycleState{},
		},
	}
	if dispatcher.enabled {
		if err := dispatcher.loadState(); err != nil {
			return nil, err
		}
	}
	return dispatcher, nil
}

func (d *Dispatcher) WantsPullRequestActivity() bool {
	if d == nil || !d.enabled {
		return false
	}
	for _, command := range d.commands {
		if command.Event == EventNewPRActivity {
			return true
		}
	}
	return false
}

func (d *Dispatcher) WantsPullRequestLifecycle() bool {
	if d == nil || !d.enabled {
		return false
	}
	for _, command := range d.commands {
		if isLifecycleEvent(command.Event) {
			return true
		}
	}
	return false
}

func ResolveStatePath(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return explicit, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve hook state dir: %w", err)
	}
	return filepath.Join(cacheDir, "prdash", "hooks-state.json"), nil
}

func (d *Dispatcher) Observe(ctx context.Context, pr model.PullRequest, runs []model.WorkflowRun) {
	if d == nil || !d.enabled {
		return
	}

	jobs := allJobs(runs)
	mergeDirty := isDirtyMergeState(pr.MergeStateStatus)
	if len(jobs) == 0 && !mergeDirty {
		return
	}
	summary := model.SummarizeJobs(jobs)
	if mergeDirty && summary.Failure == 0 {
		summary.State = model.CheckFailure
	}
	key := stateKey(pr)
	now := d.now().UTC()

	var payloads []Payload
	var stateChanged bool
	d.mu.Lock()
	head := d.state.PRHeads[key]
	if firstFailureObserved(summary, mergeDirty) && !head.FirstCheckFailureFired {
		head.FirstCheckFailureFired = true
		payloads = append(payloads, d.payload(EventFirstCheckFailure, now, pr, runs, summary))
		stateChanged = true
	}
	if checksTerminal(summary) {
		completionKey := checksCompletionKey(runs, summary)
		if shouldFireChecksCompleted(head, completionKey) {
			head.ChecksCompletedFired = true
			head.LastChecksCompletedKey = completionKey
			payloads = append(payloads, d.payload(EventChecksCompleted, now, pr, runs, summary))
			stateChanged = true
		} else if head.ChecksCompletedFired && head.LastChecksCompletedKey == "" && isTerminalState(head.LastState) {
			head.LastChecksCompletedKey = completionKey
			stateChanged = true
		}
	}
	if head.LastState != string(summary.State) {
		head.LastState = string(summary.State)
		stateChanged = true
	}
	if stateChanged {
		head.UpdatedAt = now.Format(time.RFC3339Nano)
	}
	d.state.PRHeads[key] = head
	if stateChanged {
		if err := d.saveStateLocked(); err != nil && d.logger != nil {
			d.logger.Error("hook_state_save_error", map[string]any{
				"state_path": d.statePath,
				"error":      err.Error(),
			})
		}
	}
	d.mu.Unlock()

	for _, payload := range payloads {
		d.dispatch(ctx, payload)
	}
}

func (d *Dispatcher) ObserveActivities(ctx context.Context, pr model.PullRequest, activities []model.PullRequestActivity) {
	if d == nil || !d.enabled || !d.WantsPullRequestActivity() {
		return
	}
	key := activityStateKey(pr)
	now := d.now().UTC()

	var payloads []Payload
	d.mu.Lock()
	state := d.state.PRActivities[key]
	if state.Seen == nil {
		state.Seen = map[string]string{}
	}
	if !state.Initialized {
		for _, activity := range activities {
			if activity.ID != "" {
				state.Seen[activity.ID] = string(activity.Kind)
			}
		}
		state.Initialized = true
		state.UpdatedAt = now.Format(time.RFC3339Nano)
		d.state.PRActivities[key] = state
		if err := d.saveStateLocked(); err != nil && d.logger != nil {
			d.logger.Error("hook_state_save_error", map[string]any{
				"state_path": d.statePath,
				"error":      err.Error(),
			})
		}
		d.mu.Unlock()
		return
	}
	for _, activity := range activities {
		if activity.ID == "" {
			continue
		}
		if _, ok := state.Seen[activity.ID]; ok {
			continue
		}
		state.Seen[activity.ID] = string(activity.Kind)
		payloads = append(payloads, d.activityPayload(EventNewPRActivity, now, pr, activity))
	}
	state.UpdatedAt = now.Format(time.RFC3339Nano)
	d.state.PRActivities[key] = state
	if len(payloads) > 0 {
		if err := d.saveStateLocked(); err != nil && d.logger != nil {
			d.logger.Error("hook_state_save_error", map[string]any{
				"state_path": d.statePath,
				"error":      err.Error(),
			})
		}
	}
	d.mu.Unlock()

	for _, payload := range payloads {
		d.dispatch(ctx, payload)
	}
}

func (d *Dispatcher) ObserveLifecycles(ctx context.Context, prs []model.PullRequest, lookup PullRequestLookup) {
	if d == nil || !d.enabled || !d.WantsPullRequestLifecycle() {
		return
	}
	now := d.now().UTC()
	seen := make(map[string]bool, len(prs))
	var payloads []Payload
	var missing []lifecycleState

	d.mu.Lock()
	if d.state.PRLifecycles == nil {
		d.state.PRLifecycles = map[string]lifecycleState{}
	}
	firstObservation := !d.state.PRLifecycleInitialized
	for _, pr := range prs {
		key := lifecycleStateKey(pr)
		if key == "" {
			continue
		}
		seen[key] = true
		current := lifecycleStateFromPR(pr, now)
		previous, ok := d.state.PRLifecycles[key]
		if !ok {
			if !firstObservation {
				payloads = append(payloads, d.lifecyclePayload(EventNewPRByAuthor, now, pr))
			}
			d.state.PRLifecycles[key] = current
			continue
		}
		if previous.Open && previous.IsDraft && !pr.IsDraft && previous.ReadyForReviewHeadSHA != pr.HeadSHA {
			current.ReadyForReviewHeadSHA = pr.HeadSHA
			payloads = append(payloads, d.lifecyclePayload(EventPRReadyForReview, now, pr))
		} else {
			current.ReadyForReviewHeadSHA = previous.ReadyForReviewHeadSHA
		}
		current.ClosedEventFired = previous.ClosedEventFired
		current.MergedEventFired = previous.MergedEventFired
		d.state.PRLifecycles[key] = current
	}
	if firstObservation {
		d.state.PRLifecycleInitialized = true
	}
	for key, previous := range d.state.PRLifecycles {
		if seen[key] || !previous.Open || previous.Number == 0 || previous.RepoFullName == "" {
			continue
		}
		missing = append(missing, previous)
	}
	if err := d.saveStateLocked(); err != nil && d.logger != nil {
		d.logger.Error("hook_state_save_error", map[string]any{
			"state_path": d.statePath,
			"error":      err.Error(),
		})
	}
	d.mu.Unlock()

	for _, previous := range missing {
		if lookup == nil {
			continue
		}
		live, err := lookup(ctx, pullRequestFromLifecycleState(previous))
		if err != nil {
			if d.logger != nil {
				d.logger.Warn("hook_lifecycle_lookup_error", map[string]any{
					"repo":      previous.RepoFullName,
					"pr_number": previous.Number,
					"error":     err.Error(),
				})
			}
			continue
		}
		event := ""
		if live.Merged {
			event = EventPRMerged
		} else if strings.EqualFold(strings.TrimSpace(live.State), "CLOSED") {
			event = EventPRClosed
		}
		if event == "" {
			continue
		}
		shouldDispatch := false
		d.mu.Lock()
		current := d.state.PRLifecycles[lifecycleStateKey(live)]
		if event == EventPRMerged && !current.MergedEventFired {
			current.MergedEventFired = true
			current.Open = false
			shouldDispatch = true
		}
		if event == EventPRClosed && !current.ClosedEventFired {
			current.ClosedEventFired = true
			current.Open = false
			shouldDispatch = true
		}
		updated := lifecycleStateFromPR(live, now)
		updated.ReadyForReviewHeadSHA = current.ReadyForReviewHeadSHA
		updated.ClosedEventFired = current.ClosedEventFired
		updated.MergedEventFired = current.MergedEventFired
		updated.Open = current.Open
		d.state.PRLifecycles[lifecycleStateKey(live)] = updated
		if err := d.saveStateLocked(); err != nil && d.logger != nil {
			d.logger.Error("hook_state_save_error", map[string]any{
				"state_path": d.statePath,
				"error":      err.Error(),
			})
		}
		d.mu.Unlock()
		if shouldDispatch {
			payloads = append(payloads, d.lifecyclePayload(event, now, live))
		}
	}

	for _, payload := range payloads {
		d.dispatch(ctx, payload)
	}
}

func (d *Dispatcher) dispatch(ctx context.Context, payload Payload) {
	for _, command := range d.commands {
		if command.Event != payload.Event {
			continue
		}
		command := command
		go func() {
			if d.logger != nil {
				d.logger.Info("hook_dispatch_start", map[string]any{
					"event":     payload.Event,
					"repo":      payload.PR.RepoFullName,
					"pr_number": payload.PR.Number,
					"head_sha":  payload.PR.HeadSHA,
					"command":   firstCommandWord(command.Command),
				})
			}
			if err := d.execute(ctx, command, payload); err != nil {
				if d.logger != nil {
					d.logger.Error("hook_dispatch_error", map[string]any{
						"event":     payload.Event,
						"repo":      payload.PR.RepoFullName,
						"pr_number": payload.PR.Number,
						"head_sha":  payload.PR.HeadSHA,
						"command":   firstCommandWord(command.Command),
						"error":     err.Error(),
					})
				}
				return
			}
			if d.logger != nil {
				d.logger.Info("hook_dispatch_success", map[string]any{
					"event":     payload.Event,
					"repo":      payload.PR.RepoFullName,
					"pr_number": payload.PR.Number,
					"head_sha":  payload.PR.HeadSHA,
					"command":   firstCommandWord(command.Command),
				})
			}
		}()
	}
}

func (d *Dispatcher) payload(event string, observedAt time.Time, pr model.PullRequest, runs []model.WorkflowRun, summary model.CheckSummary) Payload {
	failedJobs := failedJobPayloads(runs)
	var primary *JobPayload
	if len(failedJobs) > 0 {
		primary = &failedJobs[0]
	}
	return Payload{
		SchemaVersion: 1,
		Event:         event,
		ObservedAt:    observedAt.Format(time.RFC3339Nano),
		GitHubHost:    d.host,
		PR:            prPayload(pr),
		Summary: SummaryPayload{
			State:          summary.State,
			Total:          summary.Total,
			ActionRequired: summary.ActionRequired,
			Failure:        summary.Failure,
			Cancelled:      summary.Cancelled,
			Running:        summary.Running,
			Waiting:        summary.Waiting,
			Unknown:        summary.Unknown,
			Stale:          summary.Stale,
			Success:        summary.Success,
			Neutral:        summary.Neutral,
		},
		PrimaryJob:   primary,
		FailedJobs:   failedJobs,
		WorkflowRuns: runPayloads(runs),
	}
}

func (d *Dispatcher) activityPayload(event string, observedAt time.Time, pr model.PullRequest, activity model.PullRequestActivity) Payload {
	return Payload{
		SchemaVersion: 1,
		Event:         event,
		ObservedAt:    observedAt.Format(time.RFC3339Nano),
		GitHubHost:    d.host,
		PR:            prPayload(pr),
		Activity: &ActivityPayload{
			ID:        activity.ID,
			Kind:      activity.Kind,
			Author:    activity.Author,
			URL:       activity.URL,
			BodyText:  activity.BodyText,
			State:     activity.State,
			CreatedAt: formatTime(activity.CreatedAt),
			UpdatedAt: formatTime(activity.UpdatedAt),
		},
		WorkflowRuns: []RunPayload{},
	}
}

func (d *Dispatcher) lifecyclePayload(event string, observedAt time.Time, pr model.PullRequest) Payload {
	return Payload{
		SchemaVersion: 1,
		Event:         event,
		ObservedAt:    observedAt.Format(time.RFC3339Nano),
		GitHubHost:    d.host,
		PR:            prPayload(pr),
		WorkflowRuns:  []RunPayload{},
	}
}

func prPayload(pr model.PullRequest) PRPayload {
	return PRPayload{
		Owner:            pr.Owner,
		Repo:             pr.Repo,
		RepoFullName:     pr.RepoFullName,
		Number:           pr.Number,
		URL:              pr.URL,
		Author:           pr.Author,
		State:            pr.State,
		Merged:           pr.Merged,
		IsDraft:          pr.IsDraft,
		CreatedAt:        formatTime(pr.CreatedAt),
		UpdatedAt:        formatTime(pr.UpdatedAt),
		ClosedAt:         formatTime(pr.ClosedAt),
		MergedAt:         formatTime(pr.MergedAt),
		HeadRefName:      pr.HeadRefName,
		HeadSHA:          pr.HeadSHA,
		BaseRefName:      pr.BaseRefName,
		MergeStateStatus: pr.MergeStateStatus,
		ReviewDecision:   pr.ReviewDecision,
	}
}

func runCommand(parent context.Context, command config.HookCommandConfig, payload Payload) error {
	timeout := time.Duration(command.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	cmd := exec.CommandContext(ctx, command.Command[0], command.Command[1:]...)
	cmd.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("hook command timed out after %s", timeout)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}

func validCommands(commands []config.HookCommandConfig, logger Logger) []config.HookCommandConfig {
	valid := make([]config.HookCommandConfig, 0, len(commands))
	for _, command := range commands {
		command.Event = strings.TrimSpace(command.Event)
		if command.Event == "" || len(command.Command) == 0 || strings.TrimSpace(command.Command[0]) == "" {
			if logger != nil {
				logger.Warn("hook_command_ignored", map[string]any{
					"event":   command.Event,
					"command": command.Command,
				})
			}
			continue
		}
		for i := range command.Command {
			command.Command[i] = strings.TrimSpace(command.Command[i])
		}
		valid = append(valid, command)
	}
	return valid
}

func (d *Dispatcher) loadState() error {
	data, err := os.ReadFile(d.statePath)
	if err == nil {
		if err := json.Unmarshal(data, &d.state); err != nil {
			return fmt.Errorf("load hook state: %w", err)
		}
		if d.state.PRHeads == nil {
			d.state.PRHeads = map[string]headState{}
		}
		if d.state.PRActivities == nil {
			d.state.PRActivities = map[string]activityState{}
		}
		if d.state.PRLifecycles == nil {
			d.state.PRLifecycles = map[string]lifecycleState{}
		}
		return nil
	}
	if os.IsNotExist(err) {
		d.state.PRHeads = map[string]headState{}
		d.state.PRActivities = map[string]activityState{}
		d.state.PRLifecycles = map[string]lifecycleState{}
		return nil
	}
	return err
}

func (d *Dispatcher) saveStateLocked() error {
	if d.state.PRHeads == nil {
		d.state.PRHeads = map[string]headState{}
	}
	if d.state.PRActivities == nil {
		d.state.PRActivities = map[string]activityState{}
	}
	if d.state.PRLifecycles == nil {
		d.state.PRLifecycles = map[string]lifecycleState{}
	}
	data, err := json.MarshalIndent(d.state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(d.statePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(d.statePath, data, 0o600)
}

func stateKey(pr model.PullRequest) string {
	return fmt.Sprintf("%s#%d@%s", strings.ToLower(pr.RepoFullName), pr.Number, pr.HeadSHA)
}

func activityStateKey(pr model.PullRequest) string {
	return fmt.Sprintf("%s#%d", strings.ToLower(pr.RepoFullName), pr.Number)
}

func lifecycleStateKey(pr model.PullRequest) string {
	if pr.RepoFullName == "" || pr.Number <= 0 {
		return ""
	}
	return fmt.Sprintf("%s#%d", strings.ToLower(pr.RepoFullName), pr.Number)
}

func lifecycleStateFromPR(pr model.PullRequest, observedAt time.Time) lifecycleState {
	return lifecycleState{
		Owner:        pr.Owner,
		Repo:         pr.Repo,
		RepoFullName: pr.RepoFullName,
		Number:       pr.Number,
		URL:          pr.URL,
		Author:       pr.Author,
		State:        pr.State,
		Merged:       pr.Merged,
		Open:         !pr.Merged && !strings.EqualFold(strings.TrimSpace(pr.State), "CLOSED"),
		IsDraft:      pr.IsDraft,
		HeadSHA:      pr.HeadSHA,
		CreatedAt:    formatTime(pr.CreatedAt),
		UpdatedAt:    formatTime(pr.UpdatedAt),
		ClosedAt:     formatTime(pr.ClosedAt),
		MergedAt:     formatTime(pr.MergedAt),
		ObservedAt:   observedAt.Format(time.RFC3339Nano),
	}
}

func pullRequestFromLifecycleState(state lifecycleState) model.PullRequest {
	return model.PullRequest{
		Owner:        state.Owner,
		Repo:         state.Repo,
		RepoFullName: state.RepoFullName,
		Number:       state.Number,
		URL:          state.URL,
		Author:       state.Author,
		State:        state.State,
		Merged:       state.Merged,
		IsDraft:      state.IsDraft,
		HeadSHA:      state.HeadSHA,
	}
}

func isLifecycleEvent(event string) bool {
	switch event {
	case EventNewPRByAuthor, EventPRReadyForReview, EventPRClosed, EventPRMerged:
		return true
	default:
		return false
	}
}

func checksTerminal(summary model.CheckSummary) bool {
	return summary.Total > 0 &&
		summary.Running == 0 &&
		summary.Waiting == 0 &&
		summary.Unknown == 0 &&
		summary.Stale == 0
}

func firstFailureObserved(summary model.CheckSummary, mergeDirty bool) bool {
	return summary.Failure > 0 || mergeDirty
}

func shouldFireChecksCompleted(head headState, completionKey string) bool {
	if !head.ChecksCompletedFired {
		return true
	}
	if head.LastChecksCompletedKey != "" {
		return completionKey != head.LastChecksCompletedKey
	}
	return !isTerminalState(head.LastState)
}

func isTerminalState(value string) bool {
	switch model.CheckState(strings.TrimSpace(value)) {
	case model.CheckActionRequired, model.CheckFailure, model.CheckCancelled, model.CheckSuccess, model.CheckNeutral:
		return true
	default:
		return false
	}
}

func checksCompletionKey(runs []model.WorkflowRun, summary model.CheckSummary) string {
	parts := []string{
		fmt.Sprintf("summary:%s:%d:%d:%d:%d:%d:%d:%d:%d:%d:%d",
			summary.State,
			summary.Total,
			summary.ActionRequired,
			summary.Failure,
			summary.Cancelled,
			summary.Running,
			summary.Waiting,
			summary.Unknown,
			summary.Stale,
			summary.Success,
			summary.Neutral,
		),
	}
	for _, run := range runs {
		parts = append(parts, fmt.Sprintf("run:%d:%d:%d:%s:%s:%s",
			run.ID,
			run.WorkflowID,
			run.RunAttempt,
			run.Status,
			run.Conclusion,
			formatTime(run.UpdatedAt),
		))
		for _, job := range run.Jobs {
			parts = append(parts, fmt.Sprintf("job:%d:%d:%s:%s:%s:%s",
				job.ID,
				job.RunID,
				job.Name,
				job.Status,
				job.Conclusion,
				formatTime(job.CompletedAt),
			))
		}
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return fmt.Sprintf("v1:%x", sum)
}

func isDirtyMergeState(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "DIRTY")
}

func allJobs(runs []model.WorkflowRun) []model.Job {
	var jobs []model.Job
	for _, run := range runs {
		jobs = append(jobs, run.Jobs...)
	}
	return jobs
}

func failedJobPayloads(runs []model.WorkflowRun) []JobPayload {
	var failed []JobPayload
	for _, run := range runs {
		for _, job := range run.Jobs {
			if job.State == model.CheckFailure {
				failed = append(failed, jobPayload(job))
			}
		}
	}
	sort.Slice(failed, func(i, j int) bool {
		left := parsePayloadTime(failed[i].CompletedAt)
		right := parsePayloadTime(failed[j].CompletedAt)
		if left.IsZero() && !right.IsZero() {
			return false
		}
		if !left.IsZero() && right.IsZero() {
			return true
		}
		if !left.Equal(right) {
			return left.Before(right)
		}
		return failed[i].Name < failed[j].Name
	})
	return failed
}

func runPayloads(runs []model.WorkflowRun) []RunPayload {
	payloads := make([]RunPayload, 0, len(runs))
	for _, run := range runs {
		jobs := make([]JobPayload, 0, len(run.Jobs))
		for _, job := range run.Jobs {
			jobs = append(jobs, jobPayload(job))
		}
		payloads = append(payloads, RunPayload{
			ID:         run.ID,
			Name:       run.Name,
			WorkflowID: run.WorkflowID,
			RunNumber:  run.RunNumber,
			RunAttempt: run.RunAttempt,
			Event:      run.Event,
			Status:     run.Status,
			Conclusion: run.Conclusion,
			URL:        run.URL,
			HeadSHA:    run.HeadSHA,
			UpdatedAt:  formatTime(run.UpdatedAt),
			Jobs:       jobs,
		})
	}
	return payloads
}

func jobPayload(job model.Job) JobPayload {
	return JobPayload{
		ID:          job.ID,
		RunID:       job.RunID,
		Name:        job.Name,
		Status:      job.Status,
		Conclusion:  job.Conclusion,
		State:       job.State,
		URL:         job.URL,
		StartedAt:   formatTime(job.StartedAt),
		CompletedAt: formatTime(job.CompletedAt),
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parsePayloadTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func firstCommandWord(command []string) string {
	if len(command) == 0 {
		return ""
	}
	return command[0]
}
