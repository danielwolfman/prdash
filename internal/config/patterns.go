package config

import (
	"path"
	"strings"
)

func RepoExcluded(fullName string, patterns []string) bool {
	fullName = strings.TrimSpace(strings.ToLower(fullName))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(strings.ToLower(pattern))
		if pattern == "" {
			continue
		}
		matched, err := path.Match(pattern, fullName)
		if err == nil && matched {
			return true
		}
		if err != nil && pattern == fullName {
			return true
		}
	}
	return false
}
