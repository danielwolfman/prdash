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
	Error           string
	Message         string
	SnapshotAt      time.Time
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
}

type Row struct {
	PR         model.PullRequest
	Runs       []model.WorkflowRun
	FetchError string
	Loading    bool
}
