package github

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/danielwolfman/prdash/internal/model"
)

const jobsPageSize = 100

func (c *Client) WorkflowRunsForSHA(ctx context.Context, owner, repo, sha string) ([]model.WorkflowRun, error) {
	values := url.Values{}
	values.Set("head_sha", sha)
	values.Set("per_page", "100")

	var response workflowRunsResponse
	if err := c.get(ctx, repoPath(owner, repo, "/actions/runs"), values, &response); err != nil {
		return nil, err
	}

	runs := make([]model.WorkflowRun, 0, len(response.WorkflowRuns))
	for _, run := range response.WorkflowRuns {
		if run.HeadSHA != sha {
			continue
		}
		if runExcludedFromPRSummary(run.Event) {
			continue
		}
		runs = append(runs, model.WorkflowRun{
			ID:         run.ID,
			Name:       run.Name,
			WorkflowID: run.WorkflowID,
			RunNumber:  run.RunNumber,
			RunAttempt: run.RunAttempt,
			Event:      run.Event,
			Status:     run.Status,
			Conclusion: run.Conclusion,
			URL:        run.HTMLURL,
			HeadSHA:    run.HeadSHA,
			UpdatedAt:  run.UpdatedAt.Time,
		})
	}
	return collapseLatestRuns(runs), nil
}

func runExcludedFromPRSummary(event string) bool {
	return event == "workflow_dispatch"
}

func (c *Client) JobsForRun(ctx context.Context, owner, repo string, run model.WorkflowRun) ([]model.Job, error) {
	path := repoPath(owner, repo, "/actions/runs/"+strconv.FormatInt(run.ID, 10)+"/jobs")
	if run.RunAttempt > 0 {
		path = repoPath(owner, repo, "/actions/runs/"+strconv.FormatInt(run.ID, 10)+"/attempts/"+strconv.Itoa(run.RunAttempt)+"/jobs")
	}

	var jobs []model.Job
	for page := 1; ; page++ {
		var response jobsResponse
		values := url.Values{}
		values.Set("per_page", strconv.Itoa(jobsPageSize))
		values.Set("page", strconv.Itoa(page))
		if err := c.get(ctx, path, values, &response); err != nil {
			return nil, err
		}
		for _, job := range response.Jobs {
			jobs = append(jobs, model.Job{
				ID:          job.ID,
				RunID:       run.ID,
				Name:        job.Name,
				Status:      job.Status,
				Conclusion:  job.Conclusion,
				State:       model.NormalizeCheckState(job.Status, job.Conclusion),
				URL:         job.HTMLURL,
				StartedAt:   job.StartedAt.Time,
				CompletedAt: job.CompletedAt.Time,
			})
		}
		if len(response.Jobs) < jobsPageSize || response.TotalCount > 0 && len(jobs) >= response.TotalCount {
			break
		}
	}
	return jobs, nil
}

func (c *Client) CurrentWorkflowRunsWithJobs(ctx context.Context, pr model.PullRequest) ([]model.WorkflowRun, error) {
	runs, err := c.WorkflowRunsForSHA(ctx, pr.Owner, pr.Repo, pr.HeadSHA)
	if err != nil {
		return nil, err
	}
	var checkRuns []model.Job
	var checkRunsLoaded bool
	for i := range runs {
		jobs, err := c.JobsForRun(ctx, pr.Owner, pr.Repo, runs[i])
		if err != nil {
			if c.logger != nil {
				c.logger.Warn("jobs_endpoint_failed_using_checks_fallback", map[string]any{
					"owner":    pr.Owner,
					"repo":     pr.Repo,
					"run_id":   runs[i].ID,
					"head_sha": pr.HeadSHA,
					"error":    err.Error(),
				})
			}
			if !checkRunsLoaded {
				checkRuns, _ = c.CheckRunsForRef(ctx, pr.Owner, pr.Repo, pr.HeadSHA)
				checkRunsLoaded = true
			}
			jobs = jobsForWorkflowRun(checkRuns, runs[i])
			if len(jobs) == 0 {
				return nil, err
			}
			if c.logger != nil {
				c.logger.Info("checks_fallback_jobs_loaded", map[string]any{
					"owner":  pr.Owner,
					"repo":   pr.Repo,
					"run_id": runs[i].ID,
					"jobs":   len(jobs),
				})
			}
		}
		runs[i].Jobs = jobs
	}
	return runs, nil
}

func (c *Client) CheckRunsForRef(ctx context.Context, owner, repo, ref string) ([]model.Job, error) {
	path := repoPath(owner, repo, "/commits/"+ref+"/check-runs")

	var jobs []model.Job
	for page := 1; ; page++ {
		var response checkRunsResponse
		values := url.Values{}
		values.Set("per_page", strconv.Itoa(jobsPageSize))
		values.Set("page", strconv.Itoa(page))
		if err := c.get(ctx, path, values, &response); err != nil {
			return nil, err
		}
		for _, check := range response.CheckRuns {
			jobs = append(jobs, model.Job{
				ID:          check.ID,
				RunID:       runIDFromActionsURL(check.HTMLURL),
				Name:        check.Name,
				Status:      check.Status,
				Conclusion:  check.Conclusion,
				State:       model.NormalizeCheckState(check.Status, check.Conclusion),
				URL:         firstNonEmpty(check.HTMLURL, check.DetailsURL),
				StartedAt:   check.StartedAt.Time,
				CompletedAt: check.CompletedAt.Time,
			})
		}
		if len(response.CheckRuns) < jobsPageSize || response.TotalCount > 0 && len(jobs) >= response.TotalCount {
			break
		}
	}
	return jobs, nil
}

func (c *Client) RerunFailedJobs(ctx context.Context, owner, repo string, runID int64) error {
	path := repoPath(owner, repo, "/actions/runs/"+strconv.FormatInt(runID, 10)+"/rerun-failed-jobs")
	return c.post(ctx, path, nil)
}

func (c *Client) RerunWorkflowRun(ctx context.Context, owner, repo string, runID int64) error {
	path := repoPath(owner, repo, "/actions/runs/"+strconv.FormatInt(runID, 10)+"/rerun")
	return c.post(ctx, path, nil)
}

func (c *Client) RerunJob(ctx context.Context, owner, repo string, jobID int64) error {
	path := repoPath(owner, repo, "/actions/jobs/"+strconv.FormatInt(jobID, 10)+"/rerun")
	return c.post(ctx, path, nil)
}

func collapseLatestRuns(runs []model.WorkflowRun) []model.WorkflowRun {
	byWorkflow := map[int64]model.WorkflowRun{}
	for _, run := range runs {
		existing, ok := byWorkflow[run.WorkflowID]
		if !ok || newerRun(run, existing) {
			byWorkflow[run.WorkflowID] = run
		}
	}

	collapsed := make([]model.WorkflowRun, 0, len(byWorkflow))
	for _, run := range byWorkflow {
		collapsed = append(collapsed, run)
	}
	sort.Slice(collapsed, func(i, j int) bool {
		return collapsed[i].UpdatedAt.After(collapsed[j].UpdatedAt)
	})
	return collapsed
}

func newerRun(candidate, existing model.WorkflowRun) bool {
	if candidate.RunAttempt != existing.RunAttempt {
		return candidate.RunAttempt > existing.RunAttempt
	}
	if candidate.RunNumber != existing.RunNumber {
		return candidate.RunNumber > existing.RunNumber
	}
	return candidate.UpdatedAt.After(existing.UpdatedAt)
}

func repoPath(owner, repo, suffix string) string {
	return fmt.Sprintf("/repos/%s/%s%s", url.PathEscape(owner), url.PathEscape(repo), suffix)
}

func jobsForWorkflowRun(jobs []model.Job, run model.WorkflowRun) []model.Job {
	var matched []model.Job
	for _, job := range jobs {
		if job.RunID == run.ID {
			matched = append(matched, job)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].CompletedAt.Equal(matched[j].CompletedAt) {
			return matched[i].Name < matched[j].Name
		}
		return matched[i].CompletedAt.After(matched[j].CompletedAt)
	})
	return matched
}

func runIDFromActionsURL(raw string) int64 {
	parts := strings.Split(raw, "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] != "runs" {
			continue
		}
		id, err := strconv.ParseInt(parts[i+1], 10, 64)
		if err == nil {
			return id
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type workflowRunsResponse struct {
	WorkflowRuns []struct {
		ID         int64      `json:"id"`
		Name       string     `json:"name"`
		WorkflowID int64      `json:"workflow_id"`
		RunNumber  int        `json:"run_number"`
		RunAttempt int        `json:"run_attempt"`
		Event      string     `json:"event"`
		Status     string     `json:"status"`
		Conclusion string     `json:"conclusion"`
		HTMLURL    string     `json:"html_url"`
		HeadSHA    string     `json:"head_sha"`
		UpdatedAt  githubTime `json:"updated_at"`
	} `json:"workflow_runs"`
}

type jobsResponse struct {
	TotalCount int `json:"total_count"`
	Jobs       []struct {
		ID          int64      `json:"id"`
		Name        string     `json:"name"`
		Status      string     `json:"status"`
		Conclusion  string     `json:"conclusion"`
		HTMLURL     string     `json:"html_url"`
		StartedAt   githubTime `json:"started_at"`
		CompletedAt githubTime `json:"completed_at"`
	} `json:"jobs"`
}

type checkRunsResponse struct {
	TotalCount int `json:"total_count"`
	CheckRuns  []struct {
		ID          int64      `json:"id"`
		Name        string     `json:"name"`
		Status      string     `json:"status"`
		Conclusion  string     `json:"conclusion"`
		HTMLURL     string     `json:"html_url"`
		DetailsURL  string     `json:"details_url"`
		StartedAt   githubTime `json:"started_at"`
		CompletedAt githubTime `json:"completed_at"`
	} `json:"check_runs"`
}
