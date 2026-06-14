package config

import (
	"path/filepath"
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
	if cfg.Hooks.Enabled || len(cfg.Hooks.Commands) != 0 {
		t.Fatalf("unexpected hook defaults: %+v", cfg.Hooks)
	}
	if !cfg.Logging.Enabled || cfg.Logging.IncludePRTitles {
		t.Fatalf("unexpected logging defaults: %+v", cfg.Logging)
	}
	if cfg.Logging.Level != "debug" {
		t.Fatalf("logging level = %q, want debug", cfg.Logging.Level)
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

func TestRepoAllowedByOwner(t *testing.T) {
	if !RepoAllowedByOwner("octo-org/prdash", nil) {
		t.Fatalf("empty owner filter should allow all repos")
	}
	if !RepoAllowedByOwner("octo-org/prdash", []string{"OCTO-ORG"}) {
		t.Fatalf("expected matching owner to be allowed")
	}
	if RepoAllowedByOwner("other-org/prdash", []string{"octo-org"}) {
		t.Fatalf("expected non-matching owner to be filtered")
	}
}

func TestAddAndRemoveExcludedRepo(t *testing.T) {
	cfg := Default()

	if !AddExcludedRepo(&cfg, "octo-org/prdash") {
		t.Fatalf("expected first add to change config")
	}
	if AddExcludedRepo(&cfg, "OCTO-ORG/PRDASH") {
		t.Fatalf("duplicate add should not change config")
	}
	if len(cfg.Filters.ExcludeRepos) != 1 {
		t.Fatalf("exclude repos = %#v", cfg.Filters.ExcludeRepos)
	}
	if !RemoveExcludedRepo(&cfg, "octo-org/prdash") {
		t.Fatalf("expected remove to change config")
	}
	if RemoveExcludedRepo(&cfg, "octo-org/prdash") {
		t.Fatalf("second remove should not change config")
	}
}

func TestAddAndRemoveIncludedOwner(t *testing.T) {
	cfg := Default()

	if !AddIncludedOwner(&cfg, "octo-org") {
		t.Fatalf("expected first add to change config")
	}
	if AddIncludedOwner(&cfg, "OCTO-ORG") {
		t.Fatalf("duplicate add should not change config")
	}
	if len(cfg.Filters.IncludeOwners) != 1 {
		t.Fatalf("include owners = %#v", cfg.Filters.IncludeOwners)
	}
	if !RemoveIncludedOwner(&cfg, "octo-org") {
		t.Fatalf("expected remove to change config")
	}
	if RemoveIncludedOwner(&cfg, "octo-org") {
		t.Fatalf("second remove should not change config")
	}
}

func TestAddAndRemoveIncludedAuthor(t *testing.T) {
	cfg := Default()

	if !AddIncludedAuthor(&cfg, "dependabot") {
		t.Fatalf("expected first add to change config")
	}
	if AddIncludedAuthor(&cfg, "DEPENDABOT") {
		t.Fatalf("duplicate add should not change config")
	}
	if len(cfg.Filters.IncludeAuthors) != 1 {
		t.Fatalf("include authors = %#v", cfg.Filters.IncludeAuthors)
	}
	if !RemoveIncludedAuthor(&cfg, "dependabot") {
		t.Fatalf("expected remove to change config")
	}
	if RemoveIncludedAuthor(&cfg, "dependabot") {
		t.Fatalf("second remove should not change config")
	}

	if !AddIncludedAuthor(&cfg, "dependabot", "octo-org/prdash", "octo-org/prdash", "octo-org/other") {
		t.Fatalf("expected scoped add to change config")
	}
	if len(cfg.Filters.IncludeAuthors) != 0 {
		t.Fatalf("scoped include should remove global author include: %#v", cfg.Filters.IncludeAuthors)
	}
	if len(cfg.Filters.IncludeAuthorRules) != 1 {
		t.Fatalf("include author rules = %#v", cfg.Filters.IncludeAuthorRules)
	}
	rule := cfg.Filters.IncludeAuthorRules[0]
	if rule.Author != "dependabot" || len(rule.Repos) != 2 || rule.Repos[0] != "octo-org/prdash" || rule.Repos[1] != "octo-org/other" {
		t.Fatalf("unexpected scoped author rule: %#v", rule)
	}
	if !RemoveIncludedAuthor(&cfg, "dependabot") {
		t.Fatalf("expected scoped remove to change config")
	}
	if len(cfg.Filters.IncludeAuthorRules) != 0 {
		t.Fatalf("expected scoped rules removed: %#v", cfg.Filters.IncludeAuthorRules)
	}
}

func TestSaveCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	cfg := Default()
	cfg.Filters.ExcludeRepos = []string{"octo-org/prdash"}

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Filters.ExcludeRepos) != 1 || loaded.Filters.ExcludeRepos[0] != "octo-org/prdash" {
		t.Fatalf("loaded exclude repos = %#v", loaded.Filters.ExcludeRepos)
	}
}
