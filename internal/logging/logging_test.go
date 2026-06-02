package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danielwolfman/prdash/internal/config"
)

func TestLoggerWritesAndFiltersFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prdash.log")
	cfg := config.Default().Logging
	cfg.Path = path
	cfg.Level = "debug"
	cfg.IncludeAPIURLs = false
	cfg.IncludePRTitles = false

	logger, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug("github_request", map[string]any{
		"api_url":  "https://api.github.com/repos/octo/repo",
		"pr_title": "sensitive title",
		"token":    "secret",
		"status":   200,
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{"api.github.com", "sensitive title", "secret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("log should not contain %q: %s", forbidden, text)
		}
	}
	for _, want := range []string{"github_request", "redacted", `"status":200`} {
		if !strings.Contains(text, want) {
			t.Fatalf("log missing %q: %s", want, text)
		}
	}
}

func TestTailReturnsLastLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prdash.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := Tail(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, ",") != "two,three" {
		t.Fatalf("tail = %#v", lines)
	}
}

func TestLoggerRotatesBySize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prdash.log")
	cfg := config.Default().Logging
	cfg.Path = path
	cfg.Level = "debug"
	cfg.MaxSizeMB = 1
	cfg.MaxFiles = 2

	logger, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	logger.maxBytes = 80
	logger.Info("first", map[string]any{"payload": strings.Repeat("a", 120)})
	logger.Info("second", map[string]any{"payload": strings.Repeat("b", 120)})

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotated file: %v", err)
	}
}
