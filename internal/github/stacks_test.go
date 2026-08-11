package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielwolfman/prdash/internal/model"
)

func TestPopulateStackReadiness(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/octo-org/prdash/stacks":
			if r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") != "1" {
				t.Fatalf("unexpected stack query: %s", r.URL.RawQuery)
			}
			writeJSON(t, w, []map[string]any{
				{
					"number": 42,
					"base":   map[string]any{"ref": "main"},
					"pull_requests": []map[string]any{
						{"number": 7, "state": "open", "head": map[string]any{"ref": "feature/one", "sha": "head-one"}},
						{"number": 8, "state": "open", "head": map[string]any{"ref": "feature/two", "sha": "head-two"}},
					},
				},
			})
		case "/repos/octo-org/prdash/branches/main":
			writeJSON(t, w, map[string]any{"commit": map[string]any{"sha": "main-now"}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	prs := []model.PullRequest{
		{RepoFullName: "octo-org/prdash", Number: 7, BaseRefName: "main", BaseSHA: "main-before"},
		{RepoFullName: "octo-org/prdash", Number: 8, BaseRefName: "feature/one", BaseSHA: "head-one"},
		{RepoFullName: "octo-org/prdash", Number: 9, BaseRefName: "main", BaseSHA: "main-before"},
	}
	client := NewClient("test-token", WithBaseURLs(server.URL, server.URL+"/graphql"))
	if err := client.PopulateStackReadiness(context.Background(), prs); err != nil {
		t.Fatal(err)
	}

	if prs[0].StackNumber != 42 || prs[0].StackPosition != 1 || prs[0].StackSize != 2 || !prs[0].StackNeedsRebase {
		t.Fatalf("first PR stack state = %+v", prs[0])
	}
	if prs[1].StackNumber != 42 || prs[1].StackPosition != 2 || prs[1].StackNeedsRebase {
		t.Fatalf("second PR stack state = %+v", prs[1])
	}
	if prs[2].StackNumber != 0 || prs[2].StackNeedsRebase {
		t.Fatalf("unstacked PR state = %+v", prs[2])
	}
}
