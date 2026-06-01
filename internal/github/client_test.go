package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielwolfman/prdash/internal/model"
)

func TestSearchAuthoredOpenPRs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}

		var req graphqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.Contains(req.Query, "ViewerLogin"):
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"viewer": map[string]any{"login": "octo-user"},
				},
			})
		case strings.Contains(req.Query, "SearchPullRequests"):
			if query := req.Variables["query"].(string); !strings.Contains(query, "author:octo-user") || !strings.Contains(query, "sort:updated-desc") {
				t.Fatalf("unexpected search query: %s", query)
			}
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"search": map[string]any{
						"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
						"nodes": []map[string]any{
							{
								"number":           12,
								"title":            "Add dashboard",
								"url":              "https://github.com/octo-org/prdash/pull/12",
								"isDraft":          true,
								"updatedAt":        "2026-06-01T14:00:00Z",
								"headRefName":      "feature/dashboard",
								"headRefOid":       "abc123",
								"baseRefName":      "main",
								"mergeStateStatus": "BLOCKED",
								"reviewDecision":   "REVIEW_REQUIRED",
								"repository": map[string]any{
									"name":          "prdash",
									"nameWithOwner": "octo-org/prdash",
									"isArchived":    false,
									"owner":         map[string]any{"login": "octo-org"},
								},
							},
							{
								"number":      99,
								"title":       "Archived noise",
								"url":         "https://github.com/octo-org/old/pull/99",
								"updatedAt":   "2026-06-01T13:00:00Z",
								"headRefName": "old",
								"headRefOid":  "def456",
								"baseRefName": "main",
								"repository": map[string]any{
									"name":          "old",
									"nameWithOwner": "octo-org/old",
									"isArchived":    true,
									"owner":         map[string]any{"login": "octo-org"},
								},
							},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected graphql query: %s", req.Query)
		}
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURLs(server.URL, server.URL+"/graphql"))
	prs, err := client.SearchAuthoredOpenPRs(context.Background(), 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 1 {
		t.Fatalf("len(prs) = %d, want 1: %+v", len(prs), prs)
	}
	pr := prs[0]
	if pr.Owner != "octo-org" || pr.Repo != "prdash" || pr.Number != 12 || pr.HeadSHA != "abc123" {
		t.Fatalf("unexpected pr: %+v", pr)
	}
	if !pr.IsDraft || pr.MergeStateStatus != "BLOCKED" || pr.ReviewDecision != "REVIEW_REQUIRED" {
		t.Fatalf("missing PR badges: %+v", pr)
	}
}

func TestCurrentWorkflowRunsWithJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/octo-org/prdash/actions/runs":
			if got := r.URL.Query().Get("head_sha"); got != "abc123" {
				t.Fatalf("head_sha = %q", got)
			}
			writeJSON(t, w, map[string]any{
				"workflow_runs": []map[string]any{
					{
						"id":          10,
						"name":        "CI",
						"workflow_id": 1,
						"run_number":  7,
						"run_attempt": 1,
						"event":       "pull_request",
						"status":      "completed",
						"conclusion":  "failure",
						"html_url":    "https://github.com/octo-org/prdash/actions/runs/10",
						"head_sha":    "abc123",
						"updated_at":  "2026-06-01T13:00:00Z",
					},
					{
						"id":          11,
						"name":        "CI",
						"workflow_id": 1,
						"run_number":  7,
						"run_attempt": 2,
						"event":       "pull_request",
						"status":      "in_progress",
						"conclusion":  nil,
						"html_url":    "https://github.com/octo-org/prdash/actions/runs/11",
						"head_sha":    "abc123",
						"updated_at":  "2026-06-01T14:00:00Z",
					},
					{
						"id":          12,
						"name":        "Old SHA",
						"workflow_id": 2,
						"run_number":  1,
						"run_attempt": 1,
						"event":       "pull_request",
						"status":      "completed",
						"conclusion":  "success",
						"head_sha":    "oldsha",
						"updated_at":  "2026-06-01T14:30:00Z",
					},
				},
			})
		case r.URL.Path == "/repos/octo-org/prdash/actions/runs/11/attempts/2/jobs":
			writeJSON(t, w, map[string]any{
				"jobs": []map[string]any{
					{
						"id":           100,
						"name":         "build",
						"status":       "completed",
						"conclusion":   "success",
						"html_url":     "https://github.com/octo-org/prdash/actions/runs/11/job/100",
						"started_at":   "2026-06-01T14:00:00Z",
						"completed_at": "2026-06-01T14:02:00Z",
					},
					{
						"id":           101,
						"name":         "integration",
						"status":       "completed",
						"conclusion":   "failure",
						"html_url":     "https://github.com/octo-org/prdash/actions/runs/11/job/101",
						"started_at":   "2026-06-01T14:00:00Z",
						"completed_at": "2026-06-01T14:05:00Z",
					},
					{
						"id":         102,
						"name":       "e2e",
						"status":     "in_progress",
						"conclusion": nil,
						"html_url":   "https://github.com/octo-org/prdash/actions/runs/11/job/102",
						"started_at": "2026-06-01T14:03:00Z",
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURLs(server.URL, server.URL+"/graphql"))
	runs, err := client.CurrentWorkflowRunsWithJobs(context.Background(), model.PullRequest{
		Owner:   "octo-org",
		Repo:    "prdash",
		HeadSHA: "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1: %+v", len(runs), runs)
	}
	if runs[0].ID != 11 || runs[0].RunAttempt != 2 {
		t.Fatalf("expected latest attempt run, got %+v", runs[0])
	}
	if len(runs[0].Jobs) != 3 {
		t.Fatalf("len(jobs) = %d, want 3", len(runs[0].Jobs))
	}
	summary := model.SummarizeJobs(runs[0].Jobs)
	if summary.State != model.CheckFailure || summary.Failure != 1 || summary.Running != 1 || summary.Success != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestGetRetriesTransientGitHubFailure(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, `{"message":"Server Error"}`, http.StatusBadGateway)
			return
		}
		writeJSON(t, w, map[string]any{"workflow_runs": []map[string]any{}})
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURLs(server.URL, server.URL+"/graphql"))
	_, err := client.WorkflowRunsForSHA(context.Background(), "octo-org", "prdash", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestGetDoesNotRetryPermissionFailure(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		http.Error(w, `{"message":"Forbidden"}`, http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURLs(server.URL, server.URL+"/graphql"))
	_, err := client.WorkflowRunsForSHA(context.Background(), "octo-org", "prdash", "abc123")
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRerunFailedJobsPostsExpectedEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/repos/octo-org/prdash/actions/runs/123/rerun-failed-jobs" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURLs(server.URL, server.URL+"/graphql"))
	if err := client.RerunFailedJobs(context.Background(), "octo-org", "prdash", 123); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatal(err)
	}
}
