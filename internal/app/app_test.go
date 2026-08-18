package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielwolfman/prdash/internal/config"
	ghapi "github.com/danielwolfman/prdash/internal/github"
	"github.com/danielwolfman/prdash/internal/hooks"
	logpkg "github.com/danielwolfman/prdash/internal/logging"
	"github.com/danielwolfman/prdash/internal/model"
	"github.com/danielwolfman/prdash/internal/tui"
)

func TestCalculateRefreshIntervalClampsToMinimum(t *testing.T) {
	cfg := config.Default()
	cfg.Limits.MinRefreshIntervalSecond = 30
	cfg.Limits.MaxRefreshIntervalSecond = 300
	cfg.Limits.TargetRateBudgetPercent = 60

	got := calculateRefreshInterval(cfg, 3, 0)
	if got != 30*time.Second {
		t.Fatalf("refresh interval = %s, want 30s", got)
	}
}

func TestCalculateRefreshIntervalExpandsWithLargeVisibleSet(t *testing.T) {
	cfg := config.Default()
	cfg.Limits.MinRefreshIntervalSecond = 1
	cfg.Limits.MaxRefreshIntervalSecond = 300
	cfg.Limits.TargetRateBudgetPercent = 10

	got := calculateRefreshInterval(cfg, 40, 0)
	if got <= time.Minute {
		t.Fatalf("refresh interval = %s, want over 1m for constrained budget", got)
	}
}

func TestEstimateRefreshRequestsAllowsPaginatedJobLists(t *testing.T) {
	got := estimateRefreshRequests(40, 0)
	want := 202
	if got != want {
		t.Fatalf("estimated requests = %d, want %d", got, want)
	}
}

func TestEstimateRefreshRequestsIncludesReviewThreadPagination(t *testing.T) {
	cfg := config.Default()
	cfg.Hooks.Enabled = true
	cfg.Hooks.StatePath = filepath.Join(t.TempDir(), "hooks-state.json")
	cfg.Hooks.Commands = []config.HookCommandConfig{{Event: hooks.EventReviewThreadChanged, Command: []string{"hook"}}}
	dispatcher, err := hooks.NewDispatcher(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dispatcher.Close() })

	got := estimateRefreshRequests(40, estimateHookRequestsPerRow(dispatcher))
	want := 322
	if got != want {
		t.Fatalf("estimated requests = %d, want %d", got, want)
	}
}

func TestWaitForRefreshWakesBeforeTimer(t *testing.T) {
	refresh := make(chan struct{}, 1)
	refresh <- struct{}{}

	refreshed, err := waitForRefresh(context.Background(), refresh, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !refreshed {
		t.Fatalf("expected refresh wake")
	}
}

func TestRunWatchReturnsLoaderError(t *testing.T) {
	err := runWatch(context.Background(), func(_ context.Context, _ <-chan struct{}, events chan<- tui.LoadEvent) {
		events <- tui.LoadEvent{Error: "load failed", Done: true}
	})
	if err == nil || err.Error() != "load failed" {
		t.Fatalf("error = %v, want load failed", err)
	}
}

func TestRunWatchStopsCleanlyWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runWatch(ctx, func(ctx context.Context, _ <-chan struct{}, _ chan<- tui.LoadEvent) {
		<-ctx.Done()
	})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
}

func TestDashboardLoaderReportsLockedMonitorAsFatal(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.toml")
	cfg := config.Default()
	cfg.Hooks.StatePath = filepath.Join(directory, "hooks-state.json")
	cfg.Logging.Enabled = false
	cfg.Logging.Path = filepath.Join(directory, "prdash.log")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	owner, err := hooks.NewDispatcher(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	logger, err := logpkg.New(cfg.Logging)
	if err != nil {
		t.Fatal(err)
	}

	events := make(chan tui.LoadEvent, 2)
	dashboardLoader(configPath, 0, logger)(context.Background(), nil, events)
	<-events
	fatal := <-events
	if !fatal.Fatal || fatal.Error == "" {
		t.Fatalf("event = %#v, want fatal ownership error", fatal)
	}
}

func TestStreamJobFetchesDispatchesReviewThreadAfterWorkflowFailure(t *testing.T) {
	reviewRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/octo-org/prdash/actions/runs":
			http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
		case "/graphql":
			reviewRequests++
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{
				"reviewThreads": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
					"nodes": []map[string]any{{
						"id": "PRRT_1", "isResolved": false, "isOutdated": false, "path": "internal/app/app.go", "line": 42,
						"comments": map[string]any{
							"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
							"nodes":    []map[string]any{{"id": "PRRC_1", "author": map[string]any{"login": "reviewer"}, "bodyText": "please fix", "url": "https://example.test/comment", "createdAt": "2026-06-01T14:00:00Z", "updatedAt": "2026-06-01T14:00:00Z"}},
						},
					}},
				},
			}}}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "hook-payload.json")
	cfg := config.Default()
	cfg.Hooks.Enabled = true
	cfg.Hooks.StatePath = filepath.Join(dir, "hooks-state.json")
	cfg.Hooks.Commands = []config.HookCommandConfig{{
		Event:   hooks.EventReviewThreadChanged,
		Command: []string{os.Args[0], "-test.run=^TestHookCommandHelper$", "--", "prdash-hook-helper", payloadPath},
	}}
	cfg.Logging.Enabled = false
	cfg.Logging.Path = filepath.Join(dir, "prdash.log")
	logger, err := logpkg.New(cfg.Logging)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := hooks.NewDispatcher(cfg, logger)
	if err != nil {
		t.Fatal(err)
	}

	client := ghapi.NewClient("test-token", ghapi.WithBaseURLs(server.URL, server.URL+"/graphql"))
	events := make(chan tui.LoadEvent, 1)
	streamJobFetches(context.Background(), client, []tui.Row{{PR: model.PullRequest{
		Owner: "octo-org", Repo: "prdash", RepoFullName: "octo-org/prdash", Number: 7, HeadSHA: "abc123",
	}}}, 1, 1, 0, events, logger, dispatcher)
	event := <-events
	if event.Row == nil || event.Row.FetchError == "" {
		t.Fatalf("row = %#v, want workflow fetch error", event.Row)
	}
	if reviewRequests != 1 {
		t.Fatalf("review requests = %d, want 1", reviewRequests)
	}
	if err := dispatcher.Close(); err != nil {
		t.Fatal(err)
	}
	payloadData, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	var payload hooks.Payload
	if err := json.Unmarshal(payloadData, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Event != hooks.EventReviewThreadChanged || payload.ReviewThread == nil || payload.ReviewThread.ID != "PRRT_1" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestHookCommandHelper(_ *testing.T) {
	for index, argument := range os.Args {
		if argument != "prdash-hook-helper" || index+1 >= len(os.Args) {
			continue
		}
		data, err := io.ReadAll(os.Stdin)
		if err != nil || os.WriteFile(os.Args[index+1], data, 0o600) != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
}

func TestConfigCommandsEditExcludeRepos(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	if out, err := executeTestCommand("--config", path, "init"); err != nil || !strings.Contains(out, "created config") {
		t.Fatalf("init out=%q err=%v", out, err)
	}
	if out, err := executeTestCommand("--config", path, "config", "exclude", "octo-org/prdash"); err != nil || !strings.Contains(out, "excluded") {
		t.Fatalf("exclude out=%q err=%v", out, err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Filters.ExcludeRepos) != 1 || cfg.Filters.ExcludeRepos[0] != "octo-org/prdash" {
		t.Fatalf("exclude repos = %#v", cfg.Filters.ExcludeRepos)
	}
	if out, err := executeTestCommand("--config", path, "config", "include", "octo-org/prdash"); err != nil || !strings.Contains(out, "included") {
		t.Fatalf("include out=%q err=%v", out, err)
	}
	cfg, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Filters.ExcludeRepos) != 0 {
		t.Fatalf("exclude repos = %#v", cfg.Filters.ExcludeRepos)
	}
}

func TestConfigCommandsEditIncludedOwnersAndRerun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	if out, err := executeTestCommand("--config", path, "init"); err != nil || !strings.Contains(out, "created config") {
		t.Fatalf("init out=%q err=%v", out, err)
	}
	if out, err := executeTestCommand("--config", path, "config", "include-owner", "my-company"); err != nil || !strings.Contains(out, "included owner") {
		t.Fatalf("include-owner out=%q err=%v", out, err)
	}
	if out, err := executeTestCommand("--config", path, "config", "include-author", "dependabot", "my-company/prdash"); err != nil || !strings.Contains(out, "included author dependabot for 1 repos") {
		t.Fatalf("include-author out=%q err=%v", out, err)
	}
	if out, err := executeTestCommand("--config", path, "config", "rerun", "enable"); err != nil || !strings.Contains(out, "rerun enabled") {
		t.Fatalf("rerun enable out=%q err=%v", out, err)
	}
	out, err := executeTestCommand("--config", path, "config", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "my-company") || !strings.Contains(out, "dependabot") || !strings.Contains(out, "allow_rerun: true") || !strings.Contains(out, "hooks_enabled: false") {
		t.Fatalf("unexpected config list: %q", out)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Filters.IncludeOwners) != 1 || cfg.Filters.IncludeOwners[0] != "my-company" {
		t.Fatalf("include owners = %#v", cfg.Filters.IncludeOwners)
	}
	if len(cfg.Filters.IncludeAuthors) != 0 {
		t.Fatalf("include authors = %#v", cfg.Filters.IncludeAuthors)
	}
	if len(cfg.Filters.IncludeAuthorRules) != 1 || cfg.Filters.IncludeAuthorRules[0].Author != "dependabot" || len(cfg.Filters.IncludeAuthorRules[0].Repos) != 1 || cfg.Filters.IncludeAuthorRules[0].Repos[0] != "my-company/prdash" {
		t.Fatalf("include author rules = %#v", cfg.Filters.IncludeAuthorRules)
	}
	if !cfg.Actions.AllowRerun {
		t.Fatalf("expected rerun enabled")
	}
}

func TestVersionCommand(t *testing.T) {
	out, err := executeTestCommand("version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "prdash ") || !strings.Contains(out, "commit ") {
		t.Fatalf("unexpected version output: %q", out)
	}
}

func TestLogsCommands(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	logPath := filepath.Join(dir, "prdash.log")
	cfg := config.Default()
	cfg.Logging.Path = logPath
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := executeTestCommand("--config", configPath, "logs", "path")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != logPath {
		t.Fatalf("logs path = %q, want %q", strings.TrimSpace(out), logPath)
	}

	out, err = executeTestCommand("--config", configPath, "logs", "tail", "--lines", "2")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "two\nthree" {
		t.Fatalf("logs tail = %q", out)
	}
}

func executeTestCommand(args ...string) (string, error) {
	cmd := New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}
