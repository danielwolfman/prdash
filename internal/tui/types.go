package tui

import (
	"context"
	"time"

	"github.com/danielwolfman/prdash/internal/model"
)

type Loader func(context.Context, chan<- LoadEvent)

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
