package tui

import "github.com/danielwolfman/prdash/internal/model"

type symbols struct {
	ascii   bool
	focus   string
	success string
	failure string
	cancel  string
	waiting []string
	running []string
	neutral string
	action  string
	unknown string
	stale   string
}

func chooseSymbols(mode string) symbols {
	if mode == "ascii" {
		return symbols{
			ascii:   true,
			focus:   ">",
			success: "v",
			failure: "x",
			cancel:  "c",
			waiting: []string{".", "o", "O", "o"},
			running: []string{">", "=", "<", "="},
			neutral: "-",
			action:  "!",
			unknown: "?",
			stale:   "~",
		}
	}
	return symbols{
		focus:   "▸",
		success: "✓",
		failure: "✗",
		cancel:  "×",
		waiting: []string{"·", "∙", "•", "∙"},
		running: []string{"◐", "◓", "◑", "◒"},
		neutral: "-",
		action:  "!",
		unknown: "?",
		stale:   "~",
	}
}

func (s symbols) forState(state model.CheckState, frame int) string {
	switch state {
	case model.CheckSuccess:
		return s.success
	case model.CheckFailure:
		return s.failure
	case model.CheckCancelled:
		return s.cancel
	case model.CheckWaiting:
		return s.waiting[frame%len(s.waiting)]
	case model.CheckRunning:
		return s.running[frame%len(s.running)]
	case model.CheckNeutral:
		return s.neutral
	case model.CheckActionRequired:
		return s.action
	case model.CheckStale:
		return s.stale
	default:
		return s.unknown
	}
}
