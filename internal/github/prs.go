package github

import (
	"context"
	"fmt"
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

func (c *Client) SearchAuthoredOpenPRs(ctx context.Context, limit int, includeOwners []string) ([]model.PullRequest, error) {
	if limit <= 0 {
		return nil, nil
	}
	login, err := c.ViewerLogin(ctx)
	if err != nil {
		return nil, err
	}
	queryParts := []string{"is:pr", "is:open", "author:" + login, "archived:false"}
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
