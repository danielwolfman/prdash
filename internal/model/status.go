package model

import "strings"

type CheckState string

const (
	CheckWaiting        CheckState = "waiting"
	CheckRunning        CheckState = "running"
	CheckSuccess        CheckState = "success"
	CheckFailure        CheckState = "failure"
	CheckCancelled      CheckState = "cancelled"
	CheckActionRequired CheckState = "action_required"
	CheckNeutral        CheckState = "neutral"
	CheckStale          CheckState = "stale"
	CheckUnknown        CheckState = "unknown"
)

type CheckSummary struct {
	ActionRequired int
	Failure        int
	Cancelled      int
	Running        int
	Waiting        int
	Unknown        int
	Stale          int
	Success        int
	Neutral        int
	Total          int
	State          CheckState
}

func NormalizeCheckState(status, conclusion string) CheckState {
	status = strings.ToLower(strings.TrimSpace(status))
	conclusion = strings.ToLower(strings.TrimSpace(conclusion))

	switch status {
	case "queued", "requested", "waiting", "pending":
		return CheckWaiting
	case "in_progress":
		return CheckRunning
	}

	switch conclusion {
	case "success":
		return CheckSuccess
	case "failure", "startup_failure", "timed_out":
		return CheckFailure
	case "cancelled":
		return CheckCancelled
	case "action_required":
		return CheckActionRequired
	case "neutral", "skipped":
		return CheckNeutral
	case "":
		if status == "" {
			return CheckUnknown
		}
	}

	return CheckUnknown
}

func SummarizeJobs(jobs []Job) CheckSummary {
	var summary CheckSummary
	for _, job := range jobs {
		summary.Total++
		switch job.State {
		case CheckActionRequired:
			summary.ActionRequired++
		case CheckFailure:
			summary.Failure++
		case CheckCancelled:
			summary.Cancelled++
		case CheckRunning:
			summary.Running++
		case CheckWaiting:
			summary.Waiting++
		case CheckUnknown:
			summary.Unknown++
		case CheckStale:
			summary.Stale++
		case CheckSuccess:
			summary.Success++
		case CheckNeutral:
			summary.Neutral++
		default:
			summary.Unknown++
		}
	}
	summary.State = summaryState(summary)
	return summary
}

func summaryState(summary CheckSummary) CheckState {
	switch {
	case summary.ActionRequired > 0:
		return CheckActionRequired
	case summary.Failure > 0:
		return CheckFailure
	case summary.Cancelled > 0:
		return CheckCancelled
	case summary.Running > 0:
		return CheckRunning
	case summary.Waiting > 0:
		return CheckWaiting
	case summary.Unknown > 0:
		return CheckUnknown
	case summary.Stale > 0:
		return CheckStale
	case summary.Success > 0:
		return CheckSuccess
	case summary.Neutral > 0:
		return CheckNeutral
	default:
		return CheckUnknown
	}
}
