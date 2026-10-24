package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiapply "github.com/YoanWai/agent-manager/internal/ui/dialogs/apply"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	tea "github.com/charmbracelet/bubbletea"
)

// forkRequest freezes a fork dialog submission; uiapply.ForkRequest carries
// it and the package worker runs it.
type forkRequest struct{ uiapply.ForkRequest }

func (request forkRequest) EffectRequest() {}

func (request forkRequest) effectName() string { return "fork" }

func (value forkRequest) copied() effectRequest {
	value.ForkRequest = value.Copy()
	return value
}

func (request forkRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runFork(request)
}

// forkEffectResult is the durable outcome of a fork.
type forkEffectResult struct{ uiapply.ForkOutcome }

func (forkEffectResult) EffectResult() {}

func (result forkEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applyForkEffect(job.Request.(forkRequest), result, err)
}

// queueFork admits the fork dialog's request onto the effect lane once per
// dialog generation, minting the child id and the pane geometry it launches
// at.
func (m *Model) queueFork(request forkRequest) {
	for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
		if job == nil {
			continue
		}
		if pending, ok := job.Request.(forkRequest); ok && pending.Gen == request.Gen {
			m.reportErr("this fork is already in progress")
			return
		}
	}
	w, h := m.paneTargetSize()
	request.MintChildID()
	request.Pane = sessioncmd.PaneSize{Width: w, Height: h}
	m.enqueueEffect(request, 0, false)
}

func (s effectServices) runFork(request forkRequest) (uieffects.Result, error) {
	result, err := uiapply.Fork(s.dialogSources(), s.dialogPathProbe(), request.ForkRequest)
	return forkEffectResult{result}, err
}

func (m *Model) applyForkEffect(request forkRequest, result forkEffectResult, err error) tea.Cmd {
	launched := result.Launched
	if launched.ID != "" {
		m.markFreshPane(launched.ID)
		if m.ledger.launched == nil {
			m.ledger.launched = map[string]time.Time{}
		}
		m.ledger.launched[launched.ID] = time.Now()
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

func validateForkSource(toolName string, tool config.Tool, source store.Session) error {
	return uiapply.ValidateForkSource(toolName, tool, source)
}

func expandForkCommand(template, sourceID, newID, name, sessionFile string) string {
	return uiapply.ExpandCommand(template, sourceID, newID, name, sessionFile)
}
