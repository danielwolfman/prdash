package app

import (
	"bytes"
	"context"
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
	want := 202
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

func TestVersionCommand(t *testing.T) {
	out, err := executeTestCommand("version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "prdash ") || !strings.Contains(out, "commit ") {
		t.Fatalf("unexpected version output: %q", out)
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
