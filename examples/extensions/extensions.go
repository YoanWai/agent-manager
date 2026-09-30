package extensions

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

func DeliveryBacklog(view execution.View, minimum int) []string {
	var notices []string
	for session := range view.Sessions() {
		if queued := view.Queued(session.ID); queued >= minimum && queued > 0 {
			notices = append(notices, fmt.Sprintf("%s has %d queued messages", session.Name, queued))
		}
	}
	return notices
}

type ArchiveCommands interface {
	Archive(callerID, targetID string, archived bool) (sessioncmd.Session, error)
}

type ArchiveOutcome struct {
	SessionID string
	Err       error
}

// ArchiveFinished is an explicit action, never an automatic polling hook.
func ArchiveFinished(callerID string, view execution.View, commands ArchiveCommands) []ArchiveOutcome {
	var outcomes []ArchiveOutcome
	for session := range view.Sessions() {
		if session.ID == callerID || session.Archived || session.Status != "finished" {
			continue
		}
		_, err := commands.Archive(callerID, session.ID, true)
		outcomes = append(outcomes, ArchiveOutcome{SessionID: session.ID, Err: err})
	}
	return outcomes
}
