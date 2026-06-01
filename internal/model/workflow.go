package model

import "time"

type WorkflowRun struct {
	ID         int64
	Name       string
	WorkflowID int64
	RunNumber  int
	RunAttempt int
	Event      string
	Status     string
	Conclusion string
	URL        string
	HeadSHA    string
	UpdatedAt  time.Time
	Jobs       []Job
}

type Job struct {
	ID          int64
	RunID       int64
	Name        string
	Status      string
	Conclusion  string
	State       CheckState
	URL         string
	StartedAt   time.Time
	CompletedAt time.Time
}
