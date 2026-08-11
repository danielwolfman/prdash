package github

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/danielwolfman/prdash/internal/model"
)

const stacksPageSize = 100

type stackSummary struct {
	Number int `json:"number"`
	Base   struct {
		Ref string `json:"ref"`
	} `json:"base"`
	PullRequests []struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Head   struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_requests"`
}

type branchResponse struct {
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// PopulateStackReadiness annotates monitored PRs with GitHub's native stack
// membership and needs-rebase state. GitHub records the base SHA on the PR,
// while the stack endpoint supplies membership and current member branch tips.
func (c *Client) PopulateStackReadiness(ctx context.Context, prs []model.PullRequest) error {
	byRepo := make(map[string][]int)
	for i := range prs {
		if strings.TrimSpace(prs[i].RepoFullName) != "" {
			byRepo[prs[i].RepoFullName] = append(byRepo[prs[i].RepoFullName], i)
		}
	}

	for repoFullName, indexes := range byRepo {
		stacks, err := c.repositoryStacks(ctx, repoFullName)
		if err != nil {
			return fmt.Errorf("load stacks for %s: %w", repoFullName, err)
		}
		monitored := make(map[int]int, len(indexes))
		for _, index := range indexes {
			monitored[prs[index].Number] = index
		}

		for _, stack := range stacks {
			matched := false
			refHeads := make(map[string]string, len(stack.PullRequests))
			for _, member := range stack.PullRequests {
				refHeads[member.Head.Ref] = member.Head.SHA
				if _, ok := monitored[member.Number]; ok {
					matched = true
				}
			}
			if !matched {
				continue
			}

			baseHeads := make(map[string]string)
			for position, member := range stack.PullRequests {
				index, ok := monitored[member.Number]
				if !ok || !strings.EqualFold(member.State, "open") {
					continue
				}
				pr := &prs[index]
				pr.StackNumber = stack.Number
				pr.StackPosition = position + 1
				pr.StackSize = len(stack.PullRequests)

				liveBaseSHA := refHeads[pr.BaseRefName]
				if liveBaseSHA == "" {
					liveBaseSHA = baseHeads[pr.BaseRefName]
				}
				if liveBaseSHA == "" {
					liveBaseSHA, err = c.branchHeadSHA(ctx, repoFullName, pr.BaseRefName)
					if err != nil {
						return fmt.Errorf("load branch %s in %s: %w", pr.BaseRefName, repoFullName, err)
					}
					baseHeads[pr.BaseRefName] = liveBaseSHA
				}
				pr.StackNeedsRebase = pr.BaseSHA != "" && liveBaseSHA != "" && pr.BaseSHA != liveBaseSHA
			}
		}
	}
	return nil
}

func (c *Client) repositoryStacks(ctx context.Context, repoFullName string) ([]stackSummary, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(repoFullName), "/")
	if !ok || owner == "" || repo == "" {
		return nil, fmt.Errorf("invalid repository %q", repoFullName)
	}
	path := fmt.Sprintf("/repos/%s/%s/stacks", url.PathEscape(owner), url.PathEscape(repo))
	var stacks []stackSummary
	for page := 1; ; page++ {
		var response []stackSummary
		if err := c.get(ctx, path, url.Values{
			"page":     []string{strconv.Itoa(page)},
			"per_page": []string{strconv.Itoa(stacksPageSize)},
		}, &response); err != nil {
			return nil, err
		}
		stacks = append(stacks, response...)
		if len(response) < stacksPageSize {
			return stacks, nil
		}
	}
}

func (c *Client) branchHeadSHA(ctx context.Context, repoFullName, branch string) (string, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(repoFullName), "/")
	if !ok || owner == "" || repo == "" || strings.TrimSpace(branch) == "" {
		return "", fmt.Errorf("invalid branch identity %q %q", repoFullName, branch)
	}
	path := fmt.Sprintf("/repos/%s/%s/branches/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(branch))
	var response branchResponse
	if err := c.get(ctx, path, nil, &response); err != nil {
		return "", err
	}
	return response.Commit.SHA, nil
}
