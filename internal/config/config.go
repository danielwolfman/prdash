package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	GitHub  GitHubConfig  `toml:"github"`
	Filters FiltersConfig `toml:"filters"`
	Limits  LimitsConfig  `toml:"limits"`
	UI      UIConfig      `toml:"ui"`
	Actions ActionsConfig `toml:"actions"`
	Logging LoggingConfig `toml:"logging"`
}

type GitHubConfig struct {
	Host       string `toml:"host"`
	AuthSource string `toml:"auth_source"`
}

type FiltersConfig struct {
	IncludeOwners   []string `toml:"include_owners"`
	ExcludeRepos    []string `toml:"exclude_repos"`
	IncludeDrafts   bool     `toml:"include_drafts"`
	IncludeArchived bool     `toml:"include_archived"`
}

type LimitsConfig struct {
	MaxVisiblePRs            int `toml:"max_visible_prs"`
	TargetRateBudgetPercent  int `toml:"target_rate_budget_percent"`
	MinRefreshIntervalSecond int `toml:"min_refresh_interval_seconds"`
	MaxRefreshIntervalSecond int `toml:"max_refresh_interval_seconds"`
	MaxConcurrentRequests    int `toml:"max_concurrent_requests"`
}

type UIConfig struct {
	Theme        string `toml:"theme"`
	Density      string `toml:"density"`
	Animations   bool   `toml:"animations"`
	AnimationFPS int    `toml:"animation_fps"`
	Symbols      string `toml:"symbols"`
}

type ActionsConfig struct {
	AllowRerun   bool `toml:"allow_rerun"`
	ConfirmRerun bool `toml:"confirm_rerun"`
}

type LoggingConfig struct {
	Enabled         bool   `toml:"enabled"`
	Level           string `toml:"level"`
	Path            string `toml:"path"`
	MaxSizeMB       int    `toml:"max_size_mb"`
	MaxFiles        int    `toml:"max_files"`
	RedactTokens    bool   `toml:"redact_tokens"`
	IncludeAPIURLs  bool   `toml:"include_api_urls"`
	IncludePRTitles bool   `toml:"include_pr_titles"`
}

func Default() Config {
	return Config{
		GitHub: GitHubConfig{
			Host:       "github.com",
			AuthSource: "gh",
		},
		Filters: FiltersConfig{
			IncludeOwners:   []string{},
			ExcludeRepos:    []string{},
			IncludeDrafts:   true,
			IncludeArchived: false,
		},
		Limits: LimitsConfig{
			MaxVisiblePRs:            40,
			TargetRateBudgetPercent:  60,
			MinRefreshIntervalSecond: 30,
			MaxRefreshIntervalSecond: 300,
			MaxConcurrentRequests:    2,
		},
		UI: UIConfig{
			Theme:        "auto",
			Density:      "dense",
			Animations:   true,
			AnimationFPS: 6,
			Symbols:      "auto",
		},
		Actions: ActionsConfig{
			AllowRerun:   false,
			ConfirmRerun: true,
		},
		Logging: LoggingConfig{
			Enabled:         true,
			Level:           "debug",
			MaxSizeMB:       10,
			MaxFiles:        3,
			RedactTokens:    true,
			IncludeAPIURLs:  true,
			IncludePRTitles: false,
		},
	}
}

func ResolvePath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve config dir: %w", err)
	}
	return filepath.Join(configDir, "prdash", "config.toml"), nil
}

func EnsureExists(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := Marshal(Default())
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func Save(path string, cfg Config) error {
	data, err := Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func AddExcludedRepo(cfg *Config, repo string) bool {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return false
	}
	for _, existing := range cfg.Filters.ExcludeRepos {
		if strings.EqualFold(strings.TrimSpace(existing), repo) {
			return false
		}
	}
	cfg.Filters.ExcludeRepos = append(cfg.Filters.ExcludeRepos, repo)
	return true
}

func RemoveExcludedRepo(cfg *Config, repo string) bool {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return false
	}
	next := cfg.Filters.ExcludeRepos[:0]
	removed := false
	for _, existing := range cfg.Filters.ExcludeRepos {
		if strings.EqualFold(strings.TrimSpace(existing), repo) {
			removed = true
			continue
		}
		next = append(next, existing)
	}
	cfg.Filters.ExcludeRepos = next
	return removed
}

func AddIncludedOwner(cfg *Config, owner string) bool {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return false
	}
	for _, existing := range cfg.Filters.IncludeOwners {
		if strings.EqualFold(strings.TrimSpace(existing), owner) {
			return false
		}
	}
	cfg.Filters.IncludeOwners = append(cfg.Filters.IncludeOwners, owner)
	return true
}

func RemoveIncludedOwner(cfg *Config, owner string) bool {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return false
	}
	next := cfg.Filters.IncludeOwners[:0]
	removed := false
	for _, existing := range cfg.Filters.IncludeOwners {
		if strings.EqualFold(strings.TrimSpace(existing), owner) {
			removed = true
			continue
		}
		next = append(next, existing)
	}
	cfg.Filters.IncludeOwners = next
	return removed
}

func Marshal(cfg Config) ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.SetIndentTables(true)
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
