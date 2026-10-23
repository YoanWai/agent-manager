package ui

import (
	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// connectionsRoot is the root as the connection card reads it: the theme's
// field painter, the card width, and the store write and status line.
type connectionsRoot struct{ *Model }

func (r connectionsRoot) Look() uiconnections.DialogView {
	styles := r.spawnStyles()
	return uiconnections.DialogView{
		Field: func(label, value string, focused bool) string {
			return spawn.FormField(styles, label, value, focused)
		},
		Input: textInputView,
		Muted: mutedStyle(),
	}
}

func (r connectionsRoot) CardWidth(cols int) int { return cardWidthFor(cols) }

func (r connectionsRoot) Save(gen uint64, editing string, next remote.Connection) tea.Cmd {
	op := connectionAdd
	if editing != "" {
		op = connectionUpdate
	}
	request := connectionRequest{op: op, name: editing, next: store.Connection(next), gen: gen}
	return func() tea.Msg { return connectionDialogMsg{save: &request} }
}

func (r connectionsRoot) Report(err error) tea.Cmd {
	return func() tea.Msg {
		if err == nil {
			return connectionDialogMsg{}
		}
		return errMsg{err: err}
	}
}

// connectionDialogMsg is what a key on the card asked of the root: save a
// connection, or only clear the status line the card is leaving behind.
type connectionDialogMsg struct {
	save *connectionRequest
}

func (m *Model) openConnectionDialog(editing remote.Connection) {
	m.push(uiconnections.NewSurface(connectionsRoot{m}, editing, m.ssh.List(), uint64(m.nextComposerGen())))
	m.clearErr()
}
