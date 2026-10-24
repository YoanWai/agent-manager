package connections

import (
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

type Op uint8

const (
	OpNone Op = iota
	OpAttach
	OpKill
	OpCloseTerminal
	OpRevive
	OpArchive
	OpRestore
)

// Call is the lifecycle call a rail action makes on a remote session or
// terminal row, or the question the row asks first. Refusal names why a
// call the host cannot take stays on this machine; Answered says the row had
// something to say at all, so the root stops looking for a local one.
type Call struct {
	Op       Op
	Refusal  string
	Answered bool
	// Dead marks a kill of an agent that has already exited there, which
	// holds no RAM to free.
	Dead bool
	// Action, Verb and Consequence word the confirm the call opens, the way
	// the same call on a local row words it.
	Action            uidialogs.ConfirmAction
	Verb, Consequence string
}

// Refusals names the rail actions a connection's rows do not offer, as the
// status line words them.
var Refusals = map[uirail.ActionKind]string{
	uirail.RenameAction: "rename",
	uirail.MoveToGroup:  "move",
	uirail.Delete:       "delete",
	uirail.Restart:      "restart",
	uirail.Fork:         "fork",
	uirail.OpenReview:   "review",
	uirail.OpenEditor:   "the editor",
	uirail.CopyReply:    "copy reply",
	uirail.MarkIdle:     "mark idle",
	uirail.CancelEnd:    "cancel end",
	uirail.KillAll:      "kill all",
	uirail.ReviveAll:    "revive all",
	uirail.Kill:         "kill",
	uirail.Revive:       "revive",
	uirail.Archive:      "archive",
	uirail.Restore:      "restore",
}

// confirm is the act and question a remote lifecycle call asks, the way the
// same call on a local row does.
var confirm = map[Op]struct {
	action            uidialogs.ConfirmAction
	verb, consequence string
}{
	OpKill:          {uidialogs.ConfirmKill, "kill", " frees its RAM there, v revives it."},
	OpCloseTerminal: {uidialogs.ConfirmKill, "close terminal", ""},
	OpRevive:        {uidialogs.ConfirmRevive, "revive", ""},
	OpArchive:       {uidialogs.ConfirmArchive, "archive", ""},
	OpRestore:       {uidialogs.ConfirmRestore, "restore", ""},
}

// SessionCall is the call a rail action makes on a remote session or
// terminal row. The zero Call asks nothing: the action belongs to a local
// row, and the root runs it.
func SessionCall(row Row, kind uirail.ActionKind) Call {
	switch {
	case (kind == uirail.Focus || kind == uirail.Attach) && row.Status == "dead":
		// Its tmux session is gone, so an attach could only fail.
		if row.Terminal {
			return Call{Refusal: "terminal " + row.Name + " has exited on " + row.Host + " - x closes it", Answered: true}
		}
		return Call{Refusal: row.Name + " is dead on " + row.Host + " - press v to revive it", Answered: true}
	case kind == uirail.Focus || kind == uirail.Attach:
		return Call{Op: OpAttach, Answered: true}
	case row.Terminal && (kind == uirail.Revive || kind == uirail.Archive || kind == uirail.Restore):
		return Call{Refusal: "a terminal on " + row.Host + " is not archived or revived - x closes it", Answered: true}
	case kind == uirail.Kill && row.Terminal:
		return asking(OpCloseTerminal, false)
	case kind == uirail.Kill:
		return asking(OpKill, row.Status == "dead")
	case kind == uirail.Revive:
		return asking(OpRevive, false)
	case kind == uirail.Archive || kind == uirail.Restore:
		// Each key is a no-op in the view where its rows are already
		// where it would put them.
		if (kind == uirail.Restore) != row.Archived {
			return Call{Answered: true}
		}
		if row.Archived {
			return asking(OpRestore, false)
		}
		return asking(OpArchive, false)
	}
	return Call{}
}

// asking is the confirm a lifecycle call opens, a dead agent's kill asking
// without the RAM consequence.
func asking(op Op, dead bool) Call {
	ask := confirm[op]
	call := Call{Op: op, Answered: true, Dead: dead, Action: ask.action, Verb: ask.verb, Consequence: ask.consequence}
	if dead {
		call.Consequence = ""
	}
	return call
}

func StatusColor(state string) lipgloss.Color {
	switch state {
	case "working", "starting":
		return uitheme.ColorWorking()
	case "waiting":
		return uitheme.ColorWaiting()
	case "finished":
		return uitheme.ColorFinished()
	case "errored", "dead":
		return uitheme.ColorErrored()
	}
	return uitheme.ColorIdle()
}
