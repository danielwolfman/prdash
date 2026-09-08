package tui

import (
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func fitPlain(value string, width int) string {
	value = strings.TrimSpace(value)
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(value, width, "…")
}

func fitANSI(value string, width int) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	return ansi.Truncate(value, max(0, width), "…")
}

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

func relativeTime(then, now time.Time) string {
	if then.IsZero() {
		return "unknown"
	}
	if now.IsZero() {
		now = time.Now()
	}
	d := now.Sub(then)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "m")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "h")
	default:
		return plural(int(d.Hours()/24), "d")
	}
}

func shortDuration(d time.Duration) string {
	if d < time.Minute {
		return plural(int(d.Seconds()), "s")
	}
	if d < time.Hour {
		return plural(int(d.Minutes()), "m")
	}
	return plural(int(d.Hours()), "h")
}

func plural(value int, unit string) string {
	if value <= 0 {
		value = 1
	}
	return strings.TrimSpace(strings.Join([]string{strconvItoa(value), unit}, ""))
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func strconvItoa(value int) string {
	switch value {
	case 0:
		return "0"
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	case 4:
		return "4"
	case 5:
		return "5"
	case 6:
		return "6"
	case 7:
		return "7"
	case 8:
		return "8"
	case 9:
		return "9"
	default:
		var digits []byte
		for value > 0 {
			digits = append([]byte{byte('0' + value%10)}, digits...)
			value /= 10
		}
		return string(digits)
	}
}
