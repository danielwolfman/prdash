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
        isDraft
        updatedAt
        headRefName
        headRefOid
        baseRefName
        mergeStateStatus
        reviewDecision
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

func (c *Client) SearchAuthoredOpenPRs(ctx context.Context, limit int, includeOwners, includeAuthors []string) ([]model.PullRequest, error) {
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
			prs, err := c.searchOpenPRsByAuthor(ctx, candidate.QueryAuthor, limit, includeOwners)
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

type monitoredAuthor struct {
	Candidates []authorCandidate
}

type authorCandidate struct {
	QueryAuthor string
	Optional    bool
}

func monitoredAuthors(login string, includeAuthors []string) []monitoredAuthor {
	var authors []monitoredAuthor
	seen := make(map[string]bool)
	login = strings.TrimSpace(login)
	if login != "" {
		key := strings.ToLower(login)
		seen[key] = true
		authors = append(authors, monitoredAuthor{Candidates: []authorCandidate{{QueryAuthor: login}}})
	}
	for _, author := range includeAuthors {
		author = strings.TrimSpace(author)
		if author == "" {
			continue
		}
		key := strings.ToLower(author)
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
		authors = append(authors, monitoredAuthor{Candidates: candidates})
	}
	return authors
}

func isUnsearchableAuthorError(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "listed users cannot be searched")
}

func (c *Client) searchOpenPRsByAuthor(ctx context.Context, author string, limit int, includeOwners []string) ([]model.PullRequest, error) {
	queryParts := []string{"is:pr", "is:open", "author:" + author, "archived:false"}
	for _, owner := range includeOwners {
		owner = strings.TrimSpace(owner)
		if owner != "" {
			queryParts = append(queryParts, "org:"+owner)
		}
	}
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
			prs = append(prs, model.PullRequest{
				Owner:            node.Repository.Owner.Login,
				Repo:             node.Repository.Name,
				RepoFullName:     node.Repository.NameWithOwner,
				Number:           node.Number,
				Title:            node.Title,
				URL:              node.URL,
				IsDraft:          node.IsDraft,
				UpdatedAt:        node.UpdatedAt.Time,
				HeadRefName:      node.HeadRefName,
				HeadSHA:          node.HeadRefOid,
				BaseRefName:      node.BaseRefName,
				MergeStateStatus: node.MergeStateStatus,
				ReviewDecision:   node.ReviewDecision,
			})
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

type searchPullRequestsResponse struct {
	Search struct {
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
		Nodes []struct {
			Number           int    `json:"number"`
			Title            string `json:"title"`
			URL              string `json:"url"`
			IsDraft          bool   `json:"isDraft"`
			UpdatedAt        githubTime
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
		} `json:"nodes"`
	} `json:"search"`
}
