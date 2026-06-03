package config

import (
	"path"
	"strings"
)

func RepoAllowedByOwner(fullName string, owners []string) bool {
	if len(owners) == 0 {
		return true
	}
	repoOwner := strings.TrimSpace(fullName)
	if slash := strings.Index(repoOwner, "/"); slash >= 0 {
		repoOwner = repoOwner[:slash]
	}
	for _, owner := range owners {
		if strings.EqualFold(strings.TrimSpace(owner), repoOwner) {
			return true
		}
	}
	return false
}

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
