package app

import (
	"context"
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
