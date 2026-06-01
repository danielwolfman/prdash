package config

import (
	"testing"
)

func TestDefaultConfigMatchesV1Decisions(t *testing.T) {
	cfg := Default()

	if cfg.GitHub.Host != "github.com" {
		t.Fatalf("host = %q, want github.com", cfg.GitHub.Host)
	}
	if cfg.GitHub.AuthSource != "gh" {
		t.Fatalf("auth_source = %q, want gh", cfg.GitHub.AuthSource)
	}
	if cfg.Limits.MaxVisiblePRs != 40 {
		t.Fatalf("max_visible_prs = %d, want 40", cfg.Limits.MaxVisiblePRs)
	}
	if !cfg.UI.Animations || cfg.UI.Density != "dense" || cfg.UI.Symbols != "auto" {
		t.Fatalf("unexpected UI defaults: %+v", cfg.UI)
	}
	if cfg.Actions.AllowRerun {
		t.Fatalf("alpha should keep rerun disabled by default")
	}
	if !cfg.Logging.Enabled || cfg.Logging.IncludePRTitles {
		t.Fatalf("unexpected logging defaults: %+v", cfg.Logging)
	}
}

func TestRepoExcluded(t *testing.T) {
	patterns := []string{
		"owner/exact",
		"old-org/*",
		"*/scratch-*",
	}

	tests := []struct {
		repo string
		want bool
	}{
		{"owner/exact", true},
		{"old-org/repo", true},
		{"any/scratch-one", true},
		{"owner/other", false},
		{"new-org/repo", false},
	}

	for _, tt := range tests {
		if got := RepoExcluded(tt.repo, patterns); got != tt.want {
			t.Fatalf("RepoExcluded(%q) = %v, want %v", tt.repo, got, tt.want)
		}
	}
}
