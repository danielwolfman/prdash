package github

import (
	"context"
	"fmt"
	"sort"

	"github.com/danielwolfman/prdash/internal/model"
)

const pullRequestActivitiesQuery = `
query PullRequestActivities($owner: String!, $repo: String!, $number: Int!, $last: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      comments(last: $last) {
        nodes {
          id
          author {
            login
          }
          bodyText
          url
          createdAt
          updatedAt
        }
      }
      reviews(last: $last) {
        nodes {
          id
          author {
            login
          }
          bodyText
          url
          state
          createdAt
          updatedAt
        }
      }
    }
  }
}`

const pullRequestReviewThreadsQuery = `
query PullRequestReviewThreads($owner: String!, $repo: String!, $number: Int!, $first: Int!, $after: String, $commentsFirst: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      reviewThreads(first: $first, after: $after) {
        pageInfo {
          hasNextPage
          endCursor
        }
        nodes {
          id
          isResolved
          isOutdated
          path
          line
          startLine
          originalLine
          originalStartLine
          diffSide
          startDiffSide
          comments(first: $commentsFirst) {
            pageInfo {
              hasNextPage
              endCursor
            }
            nodes {
              id
              author {
                login
              }
              bodyText
              url
              createdAt
              updatedAt
            }
          }
        }
      }
    }
  }
}`

const pullRequestReviewThreadCommentsQuery = `
query PullRequestReviewThreadComments($id: ID!, $first: Int!, $after: String) {
  node(id: $id) {
    ... on PullRequestReviewThread {
      comments(first: $first, after: $after) {
        pageInfo {
          hasNextPage
          endCursor
        }
        nodes {
          id
          author {
            login
          }
          bodyText
          url
          createdAt
          updatedAt
        }
      }
    }
  }
}`

func (c *Client) PullRequestActivities(ctx context.Context, pr model.PullRequest, last int) ([]model.PullRequestActivity, error) {
	if last <= 0 {
		last = 20
	}
	var response pullRequestActivitiesResponse
	if err := c.graphql(ctx, pullRequestActivitiesQuery, map[string]any{
		"owner":  pr.Owner,
		"repo":   pr.Repo,
		"number": pr.Number,
		"last":   last,
	}, &response); err != nil {
		return nil, err
	}
	pullRequest := response.Repository.PullRequest
	activities := make([]model.PullRequestActivity, 0, len(pullRequest.Comments.Nodes)+len(pullRequest.Reviews.Nodes))
	for _, comment := range pullRequest.Comments.Nodes {
		activities = append(activities, model.PullRequestActivity{
			ID:        comment.ID,
			Kind:      model.ActivityIssueComment,
			Author:    comment.Author.Login,
			URL:       comment.URL,
			BodyText:  comment.BodyText,
			CreatedAt: comment.CreatedAt.Time,
			UpdatedAt: comment.UpdatedAt.Time,
		})
	}
	for _, review := range pullRequest.Reviews.Nodes {
		activities = append(activities, model.PullRequestActivity{
			ID:        review.ID,
			Kind:      model.ActivityPullRequestReview,
			Author:    review.Author.Login,
			URL:       review.URL,
			BodyText:  review.BodyText,
			State:     review.State,
			CreatedAt: review.CreatedAt.Time,
			UpdatedAt: review.UpdatedAt.Time,
		})
	}
	sort.Slice(activities, func(i, j int) bool {
		if activities[i].CreatedAt.Equal(activities[j].CreatedAt) {
			return activities[i].ID < activities[j].ID
		}
		return activities[i].CreatedAt.Before(activities[j].CreatedAt)
	})
	return activities, nil
}

func (c *Client) PullRequestReviewThreads(ctx context.Context, pr model.PullRequest, pageSize int) ([]model.PullRequestReviewThread, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 100
	}

	var threads []model.PullRequestReviewThread
	var after *string
	for {
		var response pullRequestReviewThreadsResponse
		if err := c.graphql(ctx, pullRequestReviewThreadsQuery, map[string]any{
			"owner":         pr.Owner,
			"repo":          pr.Repo,
			"number":        pr.Number,
			"first":         pageSize,
			"after":         after,
			"commentsFirst": pageSize,
		}, &response); err != nil {
			return nil, err
		}

		connection := response.Repository.PullRequest.ReviewThreads
		for _, node := range connection.Nodes {
			thread := reviewThreadFromNode(node)
			if node.Comments.PageInfo.HasNextPage {
				if node.Comments.PageInfo.EndCursor == "" {
					return nil, fmt.Errorf("review thread %s comments have another page without a cursor", node.ID)
				}
				comments, err := c.pullRequestReviewThreadComments(ctx, node.ID, pageSize, node.Comments.PageInfo.EndCursor)
				if err != nil {
					return nil, err
				}
				thread.Comments = append(thread.Comments, comments...)
			}
			sortReviewComments(thread.Comments)
			threads = append(threads, thread)
		}

		if !connection.PageInfo.HasNextPage {
			break
		}
		if connection.PageInfo.EndCursor == "" {
			return nil, fmt.Errorf("review threads have another page without a cursor")
		}
		after = &connection.PageInfo.EndCursor
	}
	sort.Slice(threads, func(i, j int) bool {
		return threads[i].ID < threads[j].ID
	})
	return threads, nil
}

func (c *Client) pullRequestReviewThreadComments(ctx context.Context, threadID string, pageSize int, cursor string) ([]model.PullRequestReviewComment, error) {
	var comments []model.PullRequestReviewComment
	after := &cursor
	for {
		var response pullRequestReviewThreadCommentsResponse
		if err := c.graphql(ctx, pullRequestReviewThreadCommentsQuery, map[string]any{
			"id":    threadID,
			"first": pageSize,
			"after": after,
		}, &response); err != nil {
			return nil, err
		}
		for _, node := range response.Node.Comments.Nodes {
			comments = append(comments, reviewCommentFromNode(node))
		}
		if !response.Node.Comments.PageInfo.HasNextPage {
			return comments, nil
		}
		if response.Node.Comments.PageInfo.EndCursor == "" {
			return nil, fmt.Errorf("review thread %s comments have another page without a cursor", threadID)
		}
		after = &response.Node.Comments.PageInfo.EndCursor
	}
}

func reviewThreadFromNode(node reviewThreadNode) model.PullRequestReviewThread {
	comments := make([]model.PullRequestReviewComment, 0, len(node.Comments.Nodes))
	for _, comment := range node.Comments.Nodes {
		comments = append(comments, reviewCommentFromNode(comment))
	}
	return model.PullRequestReviewThread{
		ID:                node.ID,
		IsResolved:        node.IsResolved,
		IsOutdated:        node.IsOutdated,
		Path:              node.Path,
		Line:              node.Line,
		StartLine:         node.StartLine,
		OriginalLine:      node.OriginalLine,
		OriginalStartLine: node.OriginalStartLine,
		DiffSide:          node.DiffSide,
		StartDiffSide:     node.StartDiffSide,
		Comments:          comments,
	}
}

func reviewCommentFromNode(node reviewCommentNode) model.PullRequestReviewComment {
	return model.PullRequestReviewComment{
		ID:        node.ID,
		Author:    node.Author.Login,
		URL:       node.URL,
		BodyText:  node.BodyText,
		CreatedAt: node.CreatedAt.Time,
		UpdatedAt: node.UpdatedAt.Time,
	}
}

func sortReviewComments(comments []model.PullRequestReviewComment) {
	sort.Slice(comments, func(i, j int) bool {
		if comments[i].CreatedAt.Equal(comments[j].CreatedAt) {
			return comments[i].ID < comments[j].ID
		}
		return comments[i].CreatedAt.Before(comments[j].CreatedAt)
	})
}

type pullRequestActivitiesResponse struct {
	Repository struct {
		PullRequest struct {
			Comments struct {
				Nodes []struct {
					ID     string `json:"id"`
					Author struct {
						Login string `json:"login"`
					} `json:"author"`
					BodyText  string     `json:"bodyText"`
					URL       string     `json:"url"`
					CreatedAt githubTime `json:"createdAt"`
					UpdatedAt githubTime `json:"updatedAt"`
				} `json:"nodes"`
			} `json:"comments"`
			Reviews struct {
				Nodes []struct {
					ID     string `json:"id"`
					Author struct {
						Login string `json:"login"`
					} `json:"author"`
					BodyText  string     `json:"bodyText"`
					URL       string     `json:"url"`
					State     string     `json:"state"`
					CreatedAt githubTime `json:"createdAt"`
					UpdatedAt githubTime `json:"updatedAt"`
				} `json:"nodes"`
			} `json:"reviews"`
		} `json:"pullRequest"`
	} `json:"repository"`
}

type pullRequestReviewThreadsResponse struct {
	Repository struct {
		PullRequest struct {
			ReviewThreads struct {
				PageInfo graphQLPageInfo    `json:"pageInfo"`
				Nodes    []reviewThreadNode `json:"nodes"`
			} `json:"reviewThreads"`
		} `json:"pullRequest"`
	} `json:"repository"`
}

type pullRequestReviewThreadCommentsResponse struct {
	Node struct {
		Comments reviewCommentsConnection `json:"comments"`
	} `json:"node"`
}

type graphQLPageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type reviewThreadNode struct {
	ID                string                   `json:"id"`
	IsResolved        bool                     `json:"isResolved"`
	IsOutdated        bool                     `json:"isOutdated"`
	Path              string                   `json:"path"`
	Line              *int                     `json:"line"`
	StartLine         *int                     `json:"startLine"`
	OriginalLine      *int                     `json:"originalLine"`
	OriginalStartLine *int                     `json:"originalStartLine"`
	DiffSide          string                   `json:"diffSide"`
	StartDiffSide     string                   `json:"startDiffSide"`
	Comments          reviewCommentsConnection `json:"comments"`
}

type reviewCommentsConnection struct {
	PageInfo graphQLPageInfo     `json:"pageInfo"`
	Nodes    []reviewCommentNode `json:"nodes"`
}

type reviewCommentNode struct {
	ID     string `json:"id"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	BodyText  string     `json:"bodyText"`
	URL       string     `json:"url"`
	CreatedAt githubTime `json:"createdAt"`
	UpdatedAt githubTime `json:"updatedAt"`
}
