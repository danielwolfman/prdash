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
	HeadRepoFullName string
	HeadRefName      string
	HeadSHA          string
	BaseRefName      string
	BaseSHA          string
	MergeStateStatus string
	ReviewDecision   string
	StackNumber      int
	StackPosition    int
	StackSize        int
	StackNeedsRebase bool
}
