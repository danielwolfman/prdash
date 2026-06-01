package auth

import (
	"strings"
	"testing"
)

func TestParseScopes(t *testing.T) {
	out := []byte("  - Token scopes: 'admin:public_key', 'gist', 'read:org', 'repo', 'workflow'\n")
	scopes := parseScopes(out)
	want := []string{"admin:public_key", "gist", "read:org", "repo", "workflow"}
	if strings.Join(scopes, ",") != strings.Join(want, ",") {
		t.Fatalf("scopes = %#v, want %#v", scopes, want)
	}
}

func TestStatusHasRequiredScopes(t *testing.T) {
	status := StatusResult{Scopes: []string{"repo", "read:org", "workflow"}}
	if !status.HasRequiredScopes() {
		t.Fatalf("expected required scopes to be satisfied")
	}
}

func TestRefreshScopesCommand(t *testing.T) {
	cmd := RefreshScopesCommand("github.com")
	for _, part := range []string{"gh auth refresh", "-h github.com", "-s repo", "-s read:org", "-s workflow"} {
		if !strings.Contains(cmd, part) {
			t.Fatalf("command %q missing %q", cmd, part)
		}
	}
}
