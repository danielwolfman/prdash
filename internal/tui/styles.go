package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/danielwolfman/prdash/internal/model"
)

type styles struct {
	header         lipgloss.Style
	footer         lipgloss.Style
	row            lipgloss.Style
	focused        lipgloss.Style
	muted          lipgloss.Style
	badge          lipgloss.Style
	workflow       lipgloss.Style
	success        lipgloss.Style
	failure        lipgloss.Style
	cancelled      lipgloss.Style
	running        lipgloss.Style
	waiting        lipgloss.Style
	actionRequired lipgloss.Style
	neutral        lipgloss.Style
	unknown        lipgloss.Style
	error          lipgloss.Style
	stale          lipgloss.Style
	confirm        lipgloss.Style
}

func newStyles() styles {
	return styles{
		header:         lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")),
		footer:         lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Background(lipgloss.Color("236")),
		row:            lipgloss.NewStyle().Foreground(lipgloss.Color("252")),
		focused:        lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("238")).Bold(true),
		muted:          lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		badge:          lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("238")).Padding(0, 1),
		workflow:       lipgloss.NewStyle().Foreground(lipgloss.Color("153")).Bold(true),
		success:        lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		failure:        lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
		cancelled:      lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		running:        lipgloss.NewStyle().Foreground(lipgloss.Color("45")).Bold(true),
		waiting:        lipgloss.NewStyle().Foreground(lipgloss.Color("220")),
		actionRequired: lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true),
		neutral:        lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		unknown:        lipgloss.NewStyle().Foreground(lipgloss.Color("201")),
		error:          lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		stale:          lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		confirm:        lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(lipgloss.Color("94")).Bold(true),
	}
}

func (s styles) forState(state model.CheckState) lipgloss.Style {
	switch state {
	case model.CheckSuccess:
		return s.success
	case model.CheckFailure:
		return s.failure
	case model.CheckCancelled:
		return s.cancelled
	case model.CheckRunning:
		return s.running
	case model.CheckWaiting:
		return s.waiting
	case model.CheckActionRequired:
		return s.actionRequired
	case model.CheckNeutral, model.CheckStale:
		return s.neutral
	default:
		return s.unknown
	}
}
