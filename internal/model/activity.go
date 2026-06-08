package model

import "time"

type PullRequestActivityKind string

const (
	ActivityIssueComment      PullRequestActivityKind = "issue_comment"
	ActivityPullRequestReview PullRequestActivityKind = "pull_request_review"
)

type PullRequestActivity struct {
	ID        string
	Kind      PullRequestActivityKind
	Author    string
	URL       string
	BodyText  string
	State     string
	CreatedAt time.Time
	UpdatedAt time.Time
}
