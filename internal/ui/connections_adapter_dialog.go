package ui

import (
	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
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
		Input: uiframe.TextInputView,
		Muted: uitheme.MutedStyle(),
	}
}

func (r connectionsRoot) CardWidth(cols int) int { return uiframe.CardWidthFor(cols) }

func (r connectionsRoot) Save(gen uint64, editing string, next remote.Connection) tea.Cmd {
	op := uilink.SaveAdd
	if editing != "" {
		op = uilink.SaveUpdate
	}
	request := uilink.SaveRequest{Op: op, Name: editing, Next: store.Connection(next), Gen: gen}
	return func() tea.Msg { return uilink.SaveMsg{Save: &request} }
}

func (r connectionsRoot) Report(err error) tea.Cmd {
	return func() tea.Msg {
		if err == nil {
			return uilink.SaveMsg{}
		}
		return errMsg{err: err}
	}
}

func (m *Model) openConnectionDialog(editing remote.Connection) {
	m.push(uiconnections.NewSurface(connectionsRoot{m}, editing, m.ssh.List(), uint64(m.nextComposerGen())))
	m.clearErr()
}
