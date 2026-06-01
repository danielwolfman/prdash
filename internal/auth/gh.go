package auth

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

var RequiredScopes = []string{"repo", "read:org", "workflow"}

type StatusResult struct {
	Host          string
	Authenticated bool
	Account       string
	Scopes        []string
	RawStatus     string
}

func Status(ctx context.Context, host string) (StatusResult, error) {
	out, err := exec.CommandContext(ctx, "gh", "auth", "status", "-h", host).CombinedOutput()
	result := StatusResult{Host: host, RawStatus: string(out)}
	if err != nil {
		return result, fmt.Errorf("gh auth status failed: %w", err)
	}

	result.Authenticated = bytes.Contains(out, []byte("Logged in to"))
	result.Account = parseAccount(out)
	result.Scopes = parseScopes(out)
	return result, nil
}

func Token(ctx context.Context, host string) (string, error) {
	out, err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", host).Output()
	if err != nil {
		return "", fmt.Errorf("gh auth token failed: %w", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("gh auth token returned an empty token")
	}
	return token, nil
}

func (s StatusResult) HasRequiredScopes() bool {
	have := map[string]bool{}
	for _, scope := range s.Scopes {
		have[scope] = true
	}
	for _, scope := range RequiredScopes {
		if !have[scope] {
			return false
		}
	}
	return true
}

func (s StatusResult) MissingScopes() []string {
	have := map[string]bool{}
	for _, scope := range s.Scopes {
		have[scope] = true
	}
	var missing []string
	for _, scope := range RequiredScopes {
		if !have[scope] {
			missing = append(missing, scope)
		}
	}
	return missing
}

func (s StatusResult) String() string {
	authState := "not authenticated"
	if s.Authenticated {
		authState = "authenticated"
	}

	scopes := append([]string(nil), s.Scopes...)
	sort.Strings(scopes)
	if len(scopes) == 0 {
		scopes = []string{"none detected"}
	}

	lines := []string{
		fmt.Sprintf("host: %s", s.Host),
		fmt.Sprintf("status: %s", authState),
		fmt.Sprintf("account: %s", valueOrUnknown(s.Account)),
		fmt.Sprintf("scopes: %s", strings.Join(scopes, ", ")),
	}
	if missing := s.MissingScopes(); len(missing) > 0 {
		lines = append(lines, fmt.Sprintf("missing required scopes: %s", strings.Join(missing, ", ")))
	}
	return strings.Join(lines, "\n")
}

func RefreshScopesCommand(host string) string {
	args := []string{"gh auth refresh", "-h", shellQuote(host)}
	for _, scope := range RequiredScopes {
		args = append(args, "-s", shellQuote(scope))
	}
	return strings.Join(args, " ")
}

func parseAccount(out []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "account "); idx >= 0 {
			rest := strings.TrimSpace(line[idx+len("account "):])
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				return strings.Trim(fields[0], "()")
			}
		}
	}
	return ""
}

func parseScopes(out []byte) []string {
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.Index(line, "Token scopes:")
		if idx < 0 {
			continue
		}
		scopeText := strings.TrimSpace(line[idx+len("Token scopes:"):])
		scopeText = strings.Trim(scopeText, "'")
		parts := strings.Split(scopeText, ",")
		var scopes []string
		for _, part := range parts {
			scope := strings.Trim(strings.TrimSpace(part), "'")
			if scope != "" {
				scopes = append(scopes, scope)
			}
		}
		return scopes
	}
	return nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r == ':' || r == '.' || r == '-' || r == '_' || r == '/' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	}) < 0 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func valueOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}
