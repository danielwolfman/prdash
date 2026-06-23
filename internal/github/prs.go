package github

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/danielwolfman/prdash/internal/model"
)

const viewerLoginQuery = `
query ViewerLogin {
  viewer {
    login
  }
}`

const searchPullRequestsQuery = `
query SearchPullRequests($query: String!, $first: Int!, $after: String) {
  search(type: ISSUE, query: $query, first: $first, after: $after) {
    pageInfo {
      hasNextPage
      endCursor
    }
    nodes {
      ... on PullRequest {
        number
        title
        url
        state
        merged
        isDraft
        createdAt
        updatedAt
        closedAt
        mergedAt
        headRefName
        headRefOid
        baseRefName
        mergeStateStatus
        reviewDecision
        author {
          login
        }
        repository {
          name
          nameWithOwner
          isArchived
          owner {
            login
          }
        }
      }
    }
  }
}`

const pullRequestQuery = `
query PullRequest($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      number
      title
      url
      state
      merged
      isDraft
      createdAt
      updatedAt
      closedAt
      mergedAt
      headRefName
      headRefOid
      baseRefName
      mergeStateStatus
      reviewDecision
      author {
        login
      }
      repository {
        name
        nameWithOwner
        owner {
          login
        }
      }
    }
  }
}`

func (c *Client) ViewerLogin(ctx context.Context) (string, error) {
	var response struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	if err := c.graphql(ctx, viewerLoginQuery, nil, &response); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.Viewer.Login) == "" {
		return "", fmt.Errorf("github viewer login was empty")
	}
	return response.Viewer.Login, nil
}

type AuthorFilter struct {
	Author string
	Repos  []string
}

func (c *Client) SearchAuthoredOpenPRs(ctx context.Context, limit int, includeOwners []string, includeAuthors []AuthorFilter) ([]model.PullRequest, error) {
	if limit <= 0 {
		return nil, nil
	}
	login, err := c.ViewerLogin(ctx)
	if err != nil {
		return nil, err
	}
	authors := monitoredAuthors(login, includeAuthors)
	seen := make(map[string]bool)
	var merged []model.PullRequest
	for _, author := range authors {
		for _, candidate := range author.Candidates {
			prs, err := c.searchOpenPRsByAuthor(ctx, candidate.QueryAuthor, limit, includeOwners, author.Repos)
			if err != nil {
				if candidate.Optional && isUnsearchableAuthorError(err) {
					continue
				}
				return nil, err
			}
			for _, pr := range prs {
				key := strings.ToLower(pr.RepoFullName) + "#" + fmt.Sprint(pr.Number)
				if seen[key] {
					continue
				}
				seen[key] = true
				merged = append(merged, pr)
			}
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].UpdatedAt.After(merged[j].UpdatedAt)
	})
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

func (c *Client) PullRequest(ctx context.Context, repoFullName string, number int) (model.PullRequest, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(repoFullName), "/")
	if !ok || owner == "" || repo == "" || number <= 0 {
		return model.PullRequest{}, fmt.Errorf("invalid pull request identity %q#%d", repoFullName, number)
	}
	var response struct {
		Repository struct {
			PullRequest pullRequestNode `json:"pullRequest"`
		} `json:"repository"`
	}
	if err := c.graphql(ctx, pullRequestQuery, map[string]any{
		"owner":  owner,
		"repo":   repo,
		"number": number,
	}, &response); err != nil {
		return model.PullRequest{}, err
	}
	if response.Repository.PullRequest.Number == 0 {
		return model.PullRequest{}, fmt.Errorf("pull request %s#%d not found", repoFullName, number)
	}
	return pullRequestFromNode(response.Repository.PullRequest), nil
}

type monitoredAuthor struct {
	Candidates []authorCandidate
	Repos      []string
}

type authorCandidate struct {
	QueryAuthor string
	Optional    bool
}

func monitoredAuthors(login string, includeAuthors []AuthorFilter) []monitoredAuthor {
	var authors []monitoredAuthor
	seen := make(map[string]bool)
	login = strings.TrimSpace(login)
	if login != "" {
		key := strings.ToLower(login) + "\x00"
		seen[key] = true
		authors = append(authors, monitoredAuthor{Candidates: []authorCandidate{{QueryAuthor: login}}})
	}
	for _, filter := range includeAuthors {
		author := strings.TrimSpace(filter.Author)
		if author == "" {
			continue
		}
		repos := normalizeSearchValues(filter.Repos)
		key := strings.ToLower(author) + "\x00" + strings.ToLower(strings.Join(repos, "\x00"))
		if seen[key] {
			continue
		}
		seen[key] = true
		candidates := []authorCandidate{{QueryAuthor: author}}
		if !strings.Contains(author, "/") && !strings.Contains(author, "[") {
			candidates = []authorCandidate{
				{QueryAuthor: "app/" + author, Optional: true},
				{QueryAuthor: author, Optional: true},
			}
		}
		authors = append(authors, monitoredAuthor{Candidates: candidates, Repos: repos})
	}
	return authors
}

func isUnsearchableAuthorError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "listed users cannot be searched")
}

func (c *Client) searchOpenPRsByAuthor(ctx context.Context, author string, limit int, includeOwners, includeRepos []string) ([]model.PullRequest, error) {
	if len(includeRepos) > 0 {
		seen := make(map[string]bool)
		var merged []model.PullRequest
		for _, repo := range includeRepos {
			prs, err := c.searchOpenPRsByAuthorQuery(ctx, author, limit, []string{"repo:" + repo})
			if err != nil {
				return nil, err
			}
			for _, pr := range prs {
				key := strings.ToLower(pr.RepoFullName) + "#" + fmt.Sprint(pr.Number)
				if seen[key] {
					continue
				}
				seen[key] = true
				merged = append(merged, pr)
			}
		}
		sort.SliceStable(merged, func(i, j int) bool {
			return merged[i].UpdatedAt.After(merged[j].UpdatedAt)
		})
		if len(merged) > limit {
			merged = merged[:limit]
		}
		return merged, nil
	}
	var qualifiers []string
	for _, owner := range includeOwners {
		owner = strings.TrimSpace(owner)
		if owner != "" {
			qualifiers = append(qualifiers, "org:"+owner)
		}
	}
	return c.searchOpenPRsByAuthorQuery(ctx, author, limit, qualifiers)
}

func (c *Client) searchOpenPRsByAuthorQuery(ctx context.Context, author string, limit int, qualifiers []string) ([]model.PullRequest, error) {
	queryParts := []string{"is:pr", "is:open", "author:" + author, "archived:false"}
	queryParts = append(queryParts, qualifiers...)
	queryParts = append(queryParts, "sort:updated-desc")
	query := strings.Join(queryParts, " ")

	var prs []model.PullRequest
	var after *string
	for len(prs) < limit {
		first := min(limit-len(prs), 100)
		var response searchPullRequestsResponse
		err := c.graphql(ctx, searchPullRequestsQuery, map[string]any{
			"query": query,
			"first": first,
			"after": after,
		}, &response)
		if err != nil {
			return nil, err
		}

		for _, node := range response.Search.Nodes {
			if node.Repository.IsArchived {
				continue
			}
			prs = append(prs, pullRequestFromNode(node))
			if len(prs) >= limit {
				break
			}
		}

		if !response.Search.PageInfo.HasNextPage || response.Search.PageInfo.EndCursor == "" {
			break
		}
		after = &response.Search.PageInfo.EndCursor
	}

	return prs, nil
}

func pullRequestFromNode(node pullRequestNode) model.PullRequest {
	return model.PullRequest{
		Owner:            node.Repository.Owner.Login,
		Repo:             node.Repository.Name,
		RepoFullName:     node.Repository.NameWithOwner,
		Number:           node.Number,
		Title:            node.Title,
		URL:              node.URL,
		Author:           node.Author.Login,
		State:            node.State,
		Merged:           node.Merged,
		IsDraft:          node.IsDraft,
		CreatedAt:        node.CreatedAt.Time,
		UpdatedAt:        node.UpdatedAt.Time,
		ClosedAt:         node.ClosedAt.Time,
		MergedAt:         node.MergedAt.Time,
		HeadRefName:      node.HeadRefName,
		HeadSHA:          node.HeadRefOid,
		BaseRefName:      node.BaseRefName,
		MergeStateStatus: node.MergeStateStatus,
		ReviewDecision:   node.ReviewDecision,
	}
}

func normalizeSearchValues(values []string) []string {
	var normalized []string
	seen := make(map[string]bool)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		normalized = append(normalized, value)
	}
	return normalized
}

type searchPullRequestsResponse struct {
	Search struct {
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
		Nodes []pullRequestNode `json:"nodes"`
	} `json:"search"`
}

type pullRequestNode struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	State            string `json:"state"`
	Merged           bool   `json:"merged"`
	IsDraft          bool   `json:"isDraft"`
	CreatedAt        githubTime
	UpdatedAt        githubTime
	ClosedAt         githubTime
	MergedAt         githubTime
	HeadRefName      string `json:"headRefName"`
	HeadRefOid       string `json:"headRefOid"`
	BaseRefName      string `json:"baseRefName"`
	MergeStateStatus string `json:"mergeStateStatus"`
	ReviewDecision   string `json:"reviewDecision"`
	Repository       struct {
		Name          string `json:"name"`
		NameWithOwner string `json:"nameWithOwner"`
		IsArchived    bool   `json:"isArchived"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}
