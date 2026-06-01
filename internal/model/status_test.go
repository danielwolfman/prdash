package model

import "testing"

func TestNormalizeCheckState(t *testing.T) {
	tests := []struct {
		status     string
		conclusion string
		want       CheckState
	}{
		{"queued", "", CheckWaiting},
		{"requested", "", CheckWaiting},
		{"in_progress", "", CheckRunning},
		{"completed", "success", CheckSuccess},
		{"completed", "failure", CheckFailure},
		{"completed", "startup_failure", CheckFailure},
		{"completed", "timed_out", CheckFailure},
		{"completed", "cancelled", CheckCancelled},
		{"completed", "action_required", CheckActionRequired},
		{"completed", "skipped", CheckNeutral},
		{"completed", "neutral", CheckNeutral},
		{"completed", "something-new", CheckUnknown},
	}

	for _, tt := range tests {
		got := NormalizeCheckState(tt.status, tt.conclusion)
		if got != tt.want {
			t.Fatalf("NormalizeCheckState(%q, %q) = %q, want %q", tt.status, tt.conclusion, got, tt.want)
		}
	}
}

func TestSummarizeJobsPriority(t *testing.T) {
	jobs := []Job{
		{Name: "success", State: CheckSuccess},
		{Name: "running", State: CheckRunning},
		{Name: "cancelled", State: CheckCancelled},
		{Name: "failure", State: CheckFailure},
	}

	summary := SummarizeJobs(jobs)
	if summary.State != CheckFailure {
		t.Fatalf("summary state = %q, want %q", summary.State, CheckFailure)
	}
	if summary.Failure != 1 || summary.Cancelled != 1 || summary.Running != 1 || summary.Success != 1 || summary.Total != 4 {
		t.Fatalf("unexpected summary counts: %+v", summary)
	}
}

func TestSummarizeJobsActionRequiredWins(t *testing.T) {
	summary := SummarizeJobs([]Job{
		{State: CheckFailure},
		{State: CheckActionRequired},
	})
	if summary.State != CheckActionRequired {
		t.Fatalf("summary state = %q, want %q", summary.State, CheckActionRequired)
	}
}
