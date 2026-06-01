package tui

import (
	"context"
	"time"

	"github.com/danielwolfman/prdash/internal/model"
)

type Loader func(context.Context, chan<- LoadEvent)

type ActionExecutor func(context.Context, ActionRequest) ActionResult

type ActionKind string

const (
	ActionRerunFailedJobs ActionKind = "rerun_failed_jobs"
)

type ActionRequest struct {
	Kind          ActionKind
	Owner         string
	Repo          string
	PRNumber      int
	PRTitle       string
	RunIDs        []int64
	JobCount      int
	WorkflowCount int
}

type ActionResult struct {
	Request ActionRequest
	Message string
	Error   string
}

type LoadEvent struct {
	User            string
	TotalDiscovered int
	ExcludedCount   int
	Row             *Row
	Done            bool
	Closed          bool
	Error           string
	Message         string
	SnapshotAt      time.Time
	RefreshInterval time.Duration
}

type Dashboard struct {
	User            string
	SnapshotAt      time.Time
	Rows            []Row
	TotalDiscovered int
	ExcludedCount   int
	Symbols         string
	Animations      bool
	AnimationFPS    int
	Loader          Loader
	ActionExecutor  ActionExecutor
	ActionsEnabled  bool
	RefreshInterval time.Duration
	StaleAfter      time.Duration
}

type Row struct {
	PR           model.PullRequest
	Runs         []model.WorkflowRun
	FetchError   string
	Loading      bool
	LastFetched  time.Time
	ChangedUntil time.Time
	ChangeState  model.CheckState
}
