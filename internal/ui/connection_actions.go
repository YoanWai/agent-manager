package ui

import (
	"github.com/YoanWai/agent-manager/internal/remote"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

// remoteRefusals names the rail actions a connection's rows do not offer,
// as the status line words them.
var remoteRefusals = map[uirail.ActionKind]string{
	uirail.Prompt:       "quick prompt",
	uirail.NewSession:   "new session",
	uirail.Focus:        "focus",
	uirail.Attach:       "attach",
	uirail.NewTerminal:  "new terminal",
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
	uirail.NewGroup:     "new group",
	uirail.KillAll:      "kill all",
	uirail.ReviveAll:    "revive all",
	uirail.Kill:         "kill",
	uirail.Revive:       "revive",
	uirail.Archive:      "archive",
	uirail.Restore:      "restore",
}

// connectionIntent runs a rail intent aimed at a connection or one of its
// rows, and reports whether it took it. Everything else stays with the
// local handlers.
func (m *Model) connectionIntent(intent uirail.Intent) (tea.Cmd, bool) {
	if intent.Kind == uirail.NewConnection {
		m.openConnectionDialog(remote.Connection{})
		return nil, true
	}
	if !intent.Target.Remote() {
		return nil, false
	}
	switch intent.Kind {
	case uirail.Quit, uirail.OpenSettings, uirail.Resize, uirail.OpenMessages, uirail.OpenHelp:
		return nil, false
	}
	row, ok := m.ssh.row(intent.Target)
	if !ok {
		return nil, true
	}
	if row.kind == uirail.ConnectionRow {
		switch intent.Kind {
		case uirail.RenameAction:
			conn, _ := m.ssh.connection(row.host)
			m.openConnectionDialog(conn)
			return nil, true
		case uirail.Delete:
			m.confirmRemoveConnection(row.host)
			return nil, true
		}
	}
	if action, refused := remoteRefusals[intent.Kind]; refused {
		m.reportErr(uirail.RemoteRefusal(action))
	}
	return nil, true
}

func (m *Model) confirmRemoveConnection(name string) {
	m.confirm.confirmTarget = confirmTarget{
		action:     actionRemoveConnection,
		connection: name,
		label:      "remove connection " + name + "? Its sessions keep running on the host.",
	}
	m.mode = modeConfirmDelete
}

// removeConnection is the confirmed removal; the connection's rows leave
// with it, and the host is never told.
func (m *Model) removeConnection(name string) {
	m.clearErr()
	m.enqueueEffect(connectionRequest{op: connectionRemove, name: name}, 0, false)
}
