package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielwolfman/prdash/internal/model"
)

func TestSearchAuthoredOpenPRs(t *testing.T) {
	searchRequests := 0
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
			searchRequests++
			query := req.Variables["query"].(string)
			if !strings.Contains(query, "sort:updated-desc") {
				t.Fatalf("unexpected search query: %s", query)
			}
			var nodes []map[string]any
			switch {
			case strings.Contains(query, "author:octo-user"):
				if !strings.Contains(query, "org:octo-org") {
					t.Fatalf("expected owner-scoped authenticated user query: %s", query)
				}
				nodes = []map[string]any{
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
				}
			case strings.Contains(query, "author:app/agent-pr-manager"):
				if !strings.Contains(query, "repo:octo-org/prdash") || strings.Contains(query, "org:octo-org") {
					t.Fatalf("expected repo-scoped app author query: %s", query)
				}
				nodes = []map[string]any{
					{
						"number":           13,
						"title":            "Update compatibility pins",
						"url":              "https://github.com/octo-org/prdash/pull/13",
						"isDraft":          false,
						"updatedAt":        "2026-06-01T15:00:00Z",
						"headRefName":      "automation/update-pins",
						"headRefOid":       "fed789",
						"baseRefName":      "main",
						"mergeStateStatus": "CLEAN",
						"reviewDecision":   "",
						"repository": map[string]any{
							"name":          "prdash",
							"nameWithOwner": "octo-org/prdash",
							"isArchived":    false,
							"owner":         map[string]any{"login": "octo-org"},
						},
					},
				}
			case strings.Contains(query, "author:agent-pr-manager"):
				if !strings.Contains(query, "repo:octo-org/prdash") {
					t.Fatalf("expected repo-scoped fallback author query: %s", query)
				}
				writeJSON(t, w, map[string]any{
					"errors": []map[string]any{
						{"message": "The listed users cannot be searched either because the users do not exist or you do not have permission to view the users."},
					},
				})
				return
			default:
				t.Fatalf("unexpected search query: %s", query)
			}
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"search": map[string]any{
						"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
						"nodes":    nodes,
					},
				},
			})
		default:
			t.Fatalf("unexpected graphql query: %s", req.Query)
		}
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURLs(server.URL, server.URL+"/graphql"))
	prs, err := client.SearchAuthoredOpenPRs(context.Background(), 40, []string{"octo-org"}, []AuthorFilter{
		{Author: "agent-pr-manager", Repos: []string{"octo-org/prdash"}},
		{Author: "OCTO-USER"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if searchRequests != 3 {
		t.Fatalf("search requests = %d, want 3", searchRequests)
	}
	if len(prs) != 2 {
		t.Fatalf("len(prs) = %d, want 2: %+v", len(prs), prs)
	}
	if prs[0].Number != 13 || prs[0].HeadSHA != "fed789" {
		t.Fatalf("expected newest monitored-author PR first: %+v", prs)
	}
	pr := prs[1]
	if pr.Owner != "octo-org" || pr.Repo != "prdash" || pr.Number != 12 || pr.HeadSHA != "abc123" {
		t.Fatalf("unexpected pr: %+v", pr)
	}
	if !pr.IsDraft || pr.MergeStateStatus != "BLOCKED" || pr.ReviewDecision != "REVIEW_REQUIRED" {
		t.Fatalf("missing PR badges: %+v", pr)
	}
}

func TestPullRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req graphqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(req.Query, "PullRequest(") {
			t.Fatalf("unexpected graphql query: %s", req.Query)
		}
		if req.Variables["owner"] != "octo-org" || req.Variables["repo"] != "prdash" || req.Variables["number"] != float64(12) && req.Variables["number"] != 12 {
			t.Fatalf("unexpected variables: %#v", req.Variables)
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequest": map[string]any{
						"number":           12,
						"title":            "Add dashboard",
						"url":              "https://github.com/octo-org/prdash/pull/12",
						"author":           map[string]any{"login": "octo-user"},
						"state":            "MERGED",
						"merged":           true,
						"isDraft":          false,
						"createdAt":        "2026-06-01T13:00:00Z",
						"updatedAt":        "2026-06-01T14:00:00Z",
						"closedAt":         "2026-06-01T15:00:00Z",
						"mergedAt":         "2026-06-01T15:00:00Z",
						"headRefName":      "feature/dashboard",
						"headRefOid":       "abc123",
						"baseRefName":      "main",
						"mergeStateStatus": "CLEAN",
						"reviewDecision":   "APPROVED",
						"repository": map[string]any{
							"name":          "prdash",
							"nameWithOwner": "octo-org/prdash",
							"owner":         map[string]any{"login": "octo-org"},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURLs(server.URL, server.URL+"/graphql"))
	pr, err := client.PullRequest(context.Background(), "octo-org/prdash", 12)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Author != "octo-user" || pr.State != "MERGED" || !pr.Merged || pr.MergedAt.IsZero() {
		t.Fatalf("unexpected lifecycle metadata: %+v", pr)
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
					{
						"id":          13,
						"name":        "Manual lab proof",
						"workflow_id": 3,
						"run_number":  20,
						"run_attempt": 1,
						"event":       "workflow_dispatch",
						"status":      "completed",
						"conclusion":  "failure",
						"head_sha":    "abc123",
						"updated_at":  "2026-06-01T15:00:00Z",
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

func TestCurrentWorkflowRunsWithJobsPaginatesPastDispatchRuns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/octo-org/prdash/actions/runs":
			if got := r.URL.Query().Get("head_sha"); got != "abc123" {
				t.Fatalf("head_sha = %q", got)
			}
			switch r.URL.Query().Get("page") {
			case "1":
				dispatchRuns := make([]map[string]any, 0, 100)
				for i := 0; i < 100; i++ {
					dispatchRuns = append(dispatchRuns, map[string]any{
						"id":          1000 + i,
						"name":        "Agent Lab Test",
						"workflow_id": 99,
						"run_number":  2000 + i,
						"run_attempt": 1,
						"event":       "workflow_dispatch",
						"status":      "completed",
						"conclusion":  "success",
						"html_url":    fmt.Sprintf("https://github.com/octo-org/prdash/actions/runs/%d", 1000+i),
						"head_sha":    "abc123",
						"updated_at":  "2026-06-02T14:00:00Z",
					})
				}
				writeJSON(t, w, map[string]any{"total_count": 101, "workflow_runs": dispatchRuns})
			case "2":
				writeJSON(t, w, map[string]any{
					"total_count": 101,
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
							"updated_at":  "2026-06-01T14:00:00Z",
						},
					},
				})
			default:
				t.Fatalf("unexpected page: %s", r.URL.Query().Get("page"))
			}
		case r.URL.Path == "/repos/octo-org/prdash/actions/runs/10/attempts/1/jobs":
			writeJSON(t, w, map[string]any{
				"jobs": []map[string]any{
					{
						"id":           100,
						"name":         "summary",
						"status":       "completed",
						"conclusion":   "failure",
						"html_url":     "https://github.com/octo-org/prdash/actions/runs/10/job/100",
						"started_at":   "2026-06-01T14:00:00Z",
						"completed_at": "2026-06-01T14:02:00Z",
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
	if runs[0].ID != 10 || runs[0].Event != "pull_request" {
		t.Fatalf("expected paginated pull_request run, got %+v", runs[0])
	}
	if len(runs[0].Jobs) != 1 || runs[0].Jobs[0].Name != "summary" || runs[0].Jobs[0].State != model.CheckFailure {
		t.Fatalf("unexpected jobs: %+v", runs[0].Jobs)
	}
}

func TestPullRequestActivities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req graphqlRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(req.Query, "PullRequestActivities") {
			t.Fatalf("unexpected graphql query: %s", req.Query)
		}
		if req.Variables["owner"] != "octo-org" || req.Variables["repo"] != "prdash" || req.Variables["number"] != float64(12) && req.Variables["number"] != 12 {
			t.Fatalf("unexpected variables: %#v", req.Variables)
		}
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequest": map[string]any{
						"comments": map[string]any{
							"nodes": []map[string]any{
								{
									"id":        "IC_1",
									"author":    map[string]any{"login": "reviewer"},
									"bodyText":  "please update this",
									"url":       "https://github.com/octo-org/prdash/pull/12#issuecomment-1",
									"createdAt": "2026-06-01T14:00:00Z",
									"updatedAt": "2026-06-01T14:00:00Z",
								},
							},
						},
						"reviews": map[string]any{
							"nodes": []map[string]any{
								{
									"id":        "PRR_1",
									"author":    map[string]any{"login": "maintainer"},
									"bodyText":  "changes requested",
									"url":       "https://github.com/octo-org/prdash/pull/12#pullrequestreview-1",
									"state":     "CHANGES_REQUESTED",
									"createdAt": "2026-06-01T14:05:00Z",
									"updatedAt": "2026-06-01T14:05:00Z",
								},
							},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURLs(server.URL, server.URL+"/graphql"))
	activities, err := client.PullRequestActivities(context.Background(), model.PullRequest{
		Owner:  "octo-org",
		Repo:   "prdash",
		Number: 12,
	}, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(activities) != 2 {
		t.Fatalf("len(activities) = %d, want 2", len(activities))
	}
	if activities[0].Kind != model.ActivityIssueComment || activities[0].Author != "reviewer" {
		t.Fatalf("unexpected first activity: %+v", activities[0])
	}
	if activities[1].Kind != model.ActivityPullRequestReview || activities[1].State != "CHANGES_REQUESTED" {
		t.Fatalf("unexpected second activity: %+v", activities[1])
	}
}

func TestCurrentWorkflowRunsWithJobsFallsBackToCheckRuns(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/octo-org/prdash/actions/runs":
			writeJSON(t, w, map[string]any{
				"workflow_runs": []map[string]any{
					{
						"id":          10,
						"name":        "CI",
						"workflow_id": 1,
						"run_number":  7,
						"run_attempt": 4,
						"event":       "pull_request",
						"status":      "completed",
						"conclusion":  "failure",
						"html_url":    "https://github.com/octo-org/prdash/actions/runs/10",
						"head_sha":    "abc123",
						"updated_at":  "2026-06-01T14:00:00Z",
					},
				},
			})
		case r.URL.Path == "/repos/octo-org/prdash/actions/runs/10/attempts/4/jobs":
			http.Error(w, `{"message":"Server Error"}`, http.StatusBadGateway)
		case r.URL.Path == "/repos/octo-org/prdash/commits/abc123/check-runs":
			writeJSON(t, w, map[string]any{
				"total_count": 2,
				"check_runs": []map[string]any{
					{
						"id":           100,
						"name":         "build",
						"status":       "completed",
						"conclusion":   "success",
						"html_url":     "https://github.com/octo-org/prdash/actions/runs/10/job/100",
						"details_url":  "https://github.com/octo-org/prdash/actions/runs/10/job/100",
						"started_at":   "2026-06-01T14:00:00Z",
						"completed_at": "2026-06-01T14:02:00Z",
					},
					{
						"id":           101,
						"name":         "old run",
						"status":       "completed",
						"conclusion":   "failure",
						"html_url":     "https://github.com/octo-org/prdash/actions/runs/9/job/101",
						"details_url":  "https://github.com/octo-org/prdash/actions/runs/9/job/101",
						"started_at":   "2026-06-01T14:00:00Z",
						"completed_at": "2026-06-01T14:02:00Z",
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
	if len(runs) != 1 || len(runs[0].Jobs) != 1 {
		t.Fatalf("unexpected runs: %+v", runs)
	}
	job := runs[0].Jobs[0]
	if job.Name != "build" || job.RunID != 10 || job.State != model.CheckSuccess {
		t.Fatalf("unexpected fallback job: %+v", job)
	}
}

func TestJobsForRunPaginatesAllJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/octo-org/prdash/actions/runs/123/attempts/2/jobs" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Fatalf("per_page = %q, want 100", got)
		}
		switch r.URL.Query().Get("page") {
		case "1":
			jobs := make([]map[string]any, 0, 100)
			for i := 0; i < 100; i++ {
				jobs = append(jobs, map[string]any{
					"id":           i + 1,
					"name":         fmt.Sprintf("ok-%03d", i),
					"status":       "completed",
					"conclusion":   "success",
					"started_at":   "2026-06-01T14:00:00Z",
					"completed_at": "2026-06-01T14:01:00Z",
				})
			}
			writeJSON(t, w, map[string]any{"total_count": 101, "jobs": jobs})
		case "2":
			writeJSON(t, w, map[string]any{
				"total_count": 101,
				"jobs": []map[string]any{
					{
						"id":           101,
						"name":         "ci / Run remaining lab tests (suite=jboss-operations-app-server-restart, Ubuntu 20, 1800, Ubuntu, 20, ...)",
						"status":       "completed",
						"conclusion":   "failure",
						"started_at":   "2026-06-01T16:29:29Z",
						"completed_at": "2026-06-01T16:55:40Z",
					},
				},
			})
		default:
			t.Fatalf("unexpected page: %s", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	client := NewClient("test-token", WithBaseURLs(server.URL, server.URL+"/graphql"))
	jobs, err := client.JobsForRun(context.Background(), "octo-org", "prdash", model.WorkflowRun{ID: 123, RunAttempt: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 101 {
		t.Fatalf("len(jobs) = %d, want 101", len(jobs))
	}
	last := jobs[len(jobs)-1]
	if !strings.Contains(last.Name, "jboss-operations-app-server-restart") || last.State != model.CheckFailure {
		t.Fatalf("missing paginated failure job: %+v", last)
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
