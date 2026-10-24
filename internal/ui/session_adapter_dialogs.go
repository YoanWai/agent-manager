package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	sessiondialogs "github.com/YoanWai/agent-manager/internal/ui/session/dialogs"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
)

// lifecycleView is the loaded list and the pane projection the session keys
// decide against.
func (m *Model) lifecycleView() sessiondialogs.View {
	view := sessiondialogs.View{
		Sessions: m.workspace.sessions,
		Listed:   m.listedSessions(),
		Groups:   m.workspace.groups,
		Archived: m.rail.ShowArchived(),
		Live:     make(map[string]bool, len(m.workspace.panes)),
	}
	for id := range m.workspace.panes {
		view.Live[id] = m.sessionWindowProjectedLive(id)
	}
	for id := range m.ledger.lifecycle.Launched {
		view.Live[id] = true
	}
	return view
}

// selectedLifecycleRow is the selected row as the session keys read it.
func (m *Model) selectedLifecycleRow() (sessiondialogs.Row, bool) {
	entry, ok := m.selectedRow()
	if !ok {
		return sessiondialogs.Row{}, false
	}
	switch {
	case entry.isRoot():
		return sessiondialogs.Row{Kind: sessiondialogs.RootRow}, true
	case entry.isGroup:
		return sessiondialogs.Row{Kind: sessiondialogs.GroupRow, Group: entry.group}, true
	}
	return sessiondialogs.Row{Kind: sessiondialogs.SessionRow, Group: entry.group, Sess: entry.sess}, true
}

// runSessionIntent executes what a session key decided: the card it opens, the
// batch it queues, or the line it reports.
func (m *Model) runSessionIntent(intent sessiondialogs.Intent) (tea.Model, tea.Cmd) {
	if intent.Notice != "" {
		m.reportErr(intent.Notice)
		return m, nil
	}
	if intent.ClearsErr {
		m.clearErr()
	}
	if intent.Run {
		m.queueLifecycle(intent.Card, intent.AllowLive, intent.EmptyNotice)
		return m, m.nextEffectCmd()
	}
	m.openConfirm(intent.Card)
	return m, nil
}

// cardTarget is a decision's card as the confirm surface and its apply read it.
func cardTarget(card sessiondialogs.Card) confirmTarget {
	return confirmTarget{
		isGroup:      card.Group,
		archivedOnly: card.ArchivedOnly,
		path:         card.Path,
		label:        card.Label,
		sessions:     card.Sessions,
		action:       card.Action,
		batch:        card.Batch,
		selection:    cardSelection(card.Selection),
	}
}

// packageTarget is a confirmation as the lifecycle package runs it.
func packageTarget(target confirmTarget) uilifecycle.Target {
	return uilifecycle.Target{
		Action:       uilifecycle.Action(target.action),
		Sessions:     target.sessions,
		Path:         groupPath(target),
		Batch:        target.batch,
		ArchivedOnly: target.archivedOnly,
		Selection:    packageSelection(target.selection),
	}
}

// confirmTargetFor is a lifecycle the package decided on its own, an after-turn
// archive or kill, in the shape the confirmation's apply reads.
func confirmTargetFor(target uilifecycle.Target) confirmTarget {
	return confirmTarget{
		isGroup:      target.Path != "",
		path:         target.Path,
		sessions:     target.Sessions,
		action:       confirmAction(target.Action),
		batch:        target.Batch,
		archivedOnly: target.ArchivedOnly,
	}
}

// confirmAction is the package's action as the card spells it: the card names
// delete with its zero value.
func confirmAction(action uilifecycle.Action) uidialogs.ConfirmAction {
	if action == uilifecycle.Remove {
		return uidialogs.ConfirmDelete
	}
	return uidialogs.ConfirmAction(action)
}

func packageSelection(selection lifecycleSelection) uilifecycle.Selection {
	return uilifecycle.Selection{Kind: uilifecycle.SelectionKind(selection.kind), RootID: selection.rootID, ArchivedOnly: selection.archivedOnly}
}

func cardSelection(selection sessiondialogs.Selection) lifecycleSelection {
	return lifecycleSelection{kind: lifecycleSelectionKind(selection.Kind), rootID: selection.RootID, archivedOnly: selection.ArchivedOnly}
}

var _ = store.Session{}
