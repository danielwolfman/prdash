package model

import "time"

type PullRequest struct {
	Owner            string
	Repo             string
	RepoFullName     string
	Number           int
	Title            string
	URL              string
	IsDraft          bool
	UpdatedAt        time.Time
	HeadRefName      string
	HeadSHA          string
	BaseRefName      string
	MergeStateStatus string
	ReviewDecision   string
}
