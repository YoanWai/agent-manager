package ui

import (
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
)

// lifecycleServices is the effect services as a lifecycle run reaches them.
func (s effectServices) lifecycleServices() uilifecycle.Services {
	services := uilifecycle.Services{
		Lifecycle: s.lifecycle,
		Store:     s.store,
		Driver:    s.driver,
		Reflow:    s.reflowSessions,
	}
	// A nil watcher stored in the interface would read as present.
	if s.watch != nil {
		services.Watch = s.watch
	}
	return services
}

// reflowSessions holds the panes a lifecycle run touches, the lane contract
// that keeps an input job from writing into a session it is killing.
func (s effectServices) reflowSessions(ids []string, work func()) error { return s.reflow(ids, work) }

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

// confirmAction is the package's action as the card spells it: the card
// names delete with its zero value.
func confirmAction(action uilifecycle.Action) uidialogs.ConfirmAction {
	if action == uilifecycle.Remove {
		return uidialogs.ConfirmDelete
	}
	return uidialogs.ConfirmAction(action)
}

func packageSelection(selection lifecycleSelection) uilifecycle.Selection {
	return uilifecycle.Selection{Kind: uilifecycle.SelectionKind(selection.kind), RootID: selection.rootID, ArchivedOnly: selection.archivedOnly}
}
