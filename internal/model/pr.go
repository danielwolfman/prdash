package model

import "time"

type PullRequest struct {
	Owner            string
	Repo             string
	RepoFullName     string
	Number           int
	Title            string
	URL              string
	Author           string
	State            string
	Merged           bool
	IsDraft          bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ClosedAt         time.Time
	MergedAt         time.Time
	HeadRefName      string
	HeadSHA          string
	BaseRefName      string
	MergeStateStatus string
	ReviewDecision   string
}
