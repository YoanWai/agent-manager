package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiapply "github.com/YoanWai/agent-manager/internal/ui/dialogs/apply"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	tea "github.com/charmbracelet/bubbletea"
)

// queueFork admits the fork dialog's request onto the effect lane once per
// dialog generation, minting the child id and the pane geometry it launches
// at.
func (m *Model) queueFork(request uiapply.ForkEffect) {
	for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
		if job == nil {
			continue
		}
		if pending, ok := job.Request.(uiapply.ForkEffect); ok && pending.Gen == request.Gen {
			m.reportErr("this fork is already in progress")
			return
		}
	}
	w, h := m.paneTargetSize()
	request.MintChildID()
	request.Pane = sessioncmd.PaneSize{Width: w, Height: h}
	m.enqueueEffect(request, 0, false)
}

// applyFork records the durable fork row the worker committed, then answers
// the card that asked: an open dialog closes, a stale one keeps its edits.
func (m *Model) applyFork(request uiapply.ForkEffect, result uiapply.ForkEffectResult, err error) tea.Cmd {
	launched := result.Launched
	if launched.ID != "" {
		m.markFreshPane(launched.ID)
		m.ledger.lifecycle.RecordLaunch(launched.ID, time.Now())
		m.workspace.sessions = append(m.workspace.sessions, launched)
		// Forks start as starting, which attention excludes; clear so the
		// row the fork just created is on screen.
		m.rail.ClearStatusFilter()
		m.rebuildRows()
	}
	// A completion must not replace a newer dialog or hand its keyboard
	// outcome to a dialog the user has since opened, including one reopened
	// on the same source.
	card, stillOpen := surfaceOf[*uidialogs.ForkSurface](m)
	stillOpen = stillOpen && card.Gen() == request.Gen && card.Name() == request.Name
	if err == nil {
		if stillOpen {
			m.Pop()
			m.clearErr()
			m.focusSession(launched.ID)
		}
		return m.refreshCmd()
	}
	if !stillOpen {
		m.reportErr(err.Error())
		return nil
	}
	request.ChildID = result.ChildID
	if result.RecordedForkID != "" {
		request.KnownForkID = result.RecordedForkID
	}
	m.reportLaunchError(err)
	if fix, ok := m.launchHintFix(); ok {
		fix.Retry = request
	}
	return nil
}
