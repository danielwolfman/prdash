package logging

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/danielwolfman/prdash/internal/config"
)

type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

type Logger struct {
	mu              sync.Mutex
	enabled         bool
	level           Level
	path            string
	maxBytes        int64
	maxFiles        int
	redactTokens    bool
	includeAPIURLs  bool
	includePRTitles bool
}

func New(cfg config.LoggingConfig) (*Logger, error) {
	path, err := ResolvePath(cfg.Path)
	if err != nil {
		return nil, err
	}
	if cfg.MaxSizeMB <= 0 {
		cfg.MaxSizeMB = 10
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = 3
	}
	return &Logger{
		enabled:         cfg.Enabled,
		level:           parseLevel(cfg.Level),
		path:            path,
		maxBytes:        int64(cfg.MaxSizeMB) * 1024 * 1024,
		maxFiles:        cfg.MaxFiles,
		redactTokens:    cfg.RedactTokens,
		includeAPIURLs:  cfg.IncludeAPIURLs,
		includePRTitles: cfg.IncludePRTitles,
	}, nil
}

func ResolvePath(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return explicit, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve log dir: %w", err)
	}
	return filepath.Join(cacheDir, "prdash", "prdash.log"), nil
}

func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *Logger) Debug(message string, fields map[string]any) {
	l.write(Debug, message, fields)
}

func (l *Logger) Info(message string, fields map[string]any) {
	l.write(Info, message, fields)
}

func (l *Logger) Warn(message string, fields map[string]any) {
	l.write(Warn, message, fields)
}

func (l *Logger) Error(message string, fields map[string]any) {
	l.write(Error, message, fields)
}

func (l *Logger) write(level Level, message string, fields map[string]any) {
	if l == nil || !l.enabled || level < l.level {
		return
	}
	record := map[string]any{
		"time":    time.Now().UTC().Format(time.RFC3339Nano),
		"level":   level.String(),
		"message": message,
	}
	for key, value := range fields {
		if !l.includeAPIURLs && (key == "api_url" || key == "endpoint") {
			continue
		}
		if !l.includePRTitles && key == "pr_title" {
			continue
		}
		record[key] = l.cleanValue(key, value)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	data = append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return
	}
	l.rotateIfNeeded(int64(len(data)))
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(data)
}

func (l *Logger) rotateIfNeeded(incoming int64) {
	if l.maxBytes <= 0 {
		return
	}
	info, err := os.Stat(l.path)
	if err != nil || info.Size()+incoming <= l.maxBytes {
		return
	}
	for i := l.maxFiles - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", l.path, i)
		dst := fmt.Sprintf("%s.%d", l.path, i+1)
		if i == l.maxFiles-1 {
			_ = os.Remove(dst)
		}
		_ = os.Rename(src, dst)
	}
	_ = os.Rename(l.path, l.path+".1")
}

func (l *Logger) cleanValue(key string, value any) any {
	if !l.redactTokens {
		return value
	}
	if strings.Contains(strings.ToLower(key), "token") {
		return "redacted"
	}
	switch value := value.(type) {
	case string:
		if strings.Contains(strings.ToLower(value), "bearer ") {
			return "redacted"
		}
	}
	return value
}

func Tail(path string, lines int) ([]string, error) {
	if lines <= 0 {
		lines = 80
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	ring := make([]string, lines)
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		ring[count%lines] = scanner.Text()
		count++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	total := count
	if total > lines {
		total = lines
	}
	out := make([]string, 0, total)
	start := count - total
	for i := 0; i < total; i++ {
		out = append(out, ring[(start+i)%lines])
	}
	return out, nil
}

func parseLevel(value string) Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return Debug
	case "warn", "warning":
		return Warn
	case "error":
		return Error
	default:
		return Info
	}
}

func (l Level) String() string {
	switch l {
	case Debug:
		return "debug"
	case Warn:
		return "warn"
	case Error:
		return "error"
	default:
		return "info"
	}
}
