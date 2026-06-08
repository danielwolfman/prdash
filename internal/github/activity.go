package github

import (
	"context"
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
