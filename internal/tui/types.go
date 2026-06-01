package tui

import (
	"time"

	"github.com/danielwolfman/prdash/internal/model"
)

type Dashboard struct {
	User            string
	SnapshotAt      time.Time
	Rows            []Row
	TotalDiscovered int
	ExcludedCount   int
	Symbols         string
	Animations      bool
	AnimationFPS    int
}

type Row struct {
	PR         model.PullRequest
	Runs       []model.WorkflowRun
	FetchError string
}
