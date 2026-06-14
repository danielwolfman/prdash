package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/danielwolfman/prdash/internal/config"
)

func TestCalculateRefreshIntervalClampsToMinimum(t *testing.T) {
	cfg := config.Default()
	cfg.Limits.MinRefreshIntervalSecond = 30
	cfg.Limits.MaxRefreshIntervalSecond = 300
	cfg.Limits.TargetRateBudgetPercent = 60

	got := calculateRefreshInterval(cfg, 3)
	if got != 30*time.Second {
		t.Fatalf("refresh interval = %s, want 30s", got)
	}
}

func TestCalculateRefreshIntervalExpandsWithLargeVisibleSet(t *testing.T) {
	cfg := config.Default()
	cfg.Limits.MinRefreshIntervalSecond = 1
	cfg.Limits.MaxRefreshIntervalSecond = 300
	cfg.Limits.TargetRateBudgetPercent = 10

	got := calculateRefreshInterval(cfg, 40)
	if got <= time.Minute {
		t.Fatalf("refresh interval = %s, want over 1m for constrained budget", got)
	}
}

func TestEstimateRefreshRequestsAllowsPaginatedJobLists(t *testing.T) {
	got := estimateRefreshRequests(40)
	want := 242
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
	if out, err := executeTestCommand("--config", path, "config", "include-author", "dependabot"); err != nil || !strings.Contains(out, "included author") {
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
	if len(cfg.Filters.IncludeAuthors) != 1 || cfg.Filters.IncludeAuthors[0] != "dependabot" {
		t.Fatalf("include authors = %#v", cfg.Filters.IncludeAuthors)
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
