package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// The SSH connections keep their list, host states, rows, preview, detail,
// lifecycle decisions and dialog in internal/ui/connections, and the client
// that refreshes and calls the hosts in internal/ui/connections/link. The
// hosts here answer what those reach on the manager; the adapter seeds calls
// from the rail and the composer.

// linkRoot is the root as a routed connection result reaches it.
type linkRoot struct{ *Model }

func (r linkRoot) RebuildRows() { r.rebuildRows() }

func (r linkRoot) SyncPreview() tea.Cmd { return r.syncRemotePreview() }

func (r linkRoot) RailConnection(change uilink.RailChange) {
	switch {
	case change.Rename != "":
		r.queueRail(r.rail.RenameHost(change.Rename, change.To).Mutations, 0, true)
	case change.Forget != "":
		r.queueRail(r.rail.ForgetHost(change.Forget).Mutations, 0, true)
	}
	if change.Focus != "" {
		r.rail.Focus(uirail.Selection{Kind: uirail.ConnectionRow, Host: change.Focus})
	}
}

func (r linkRoot) AnswerSave(gen uint64, failed bool) {
	s, open := surfaceOf[*uiconnections.Surface](r.Model)
	open = open && s.Gen() == gen
	if !open {
		return
	}
	if failed {
		s.Dialog().SaveFailed(s.Dialog().Gen())
		return
	}
	r.Pop()
}

func (r linkRoot) CloseAsker(gen int, asker uilink.Asker) {
	switch asker {
	case uilink.FormAsker:
		if _, ok := r.formFor(gen); ok {
			r.closeForm()
		}
	case uilink.GroupFormAsker:
		if s, ok := surfaceOf[*uidialogs.GroupFormSurface](r.Model); ok && s.Gen() == gen {
			r.Pop()
		}
	}
}

func (r linkRoot) ClearAccepted(composerGen int, draft string, closeAfterSend bool) {
	r.clearAccepted(composerGen, draft, closeAfterSend)
}

func (r linkRoot) ReportErr(text string) { r.reportErr(text) }

func (r linkRoot) ReportDone(text string) { r.reportDone(text) }

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

var (
	_ uilink.Root        = linkRoot{}
	_ uiconnections.Root = connectionsRoot{}
)

// routeConnectionMsg hands a routed connection result to the package, and
// the card's own answer to this machine.
func (m *Model) routeConnectionMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if save, ok := uilink.Route(msg); ok {
		return routed(m, m.handleConnectionDialog(save))
	}
	if cmd, ok := m.ssh.Route(linkRoot{m}, msg); ok {
		return routed(m, cmd)
	}
	return nil, nil, false
}

// handleConnectionDialog runs what the card asked: a cancel clears the
// status line it leaves, a save queues the store write.
func (m *Model) handleConnectionDialog(save *uilink.SaveRequest) tea.Cmd {
	m.clearErr()
	if save == nil {
		return nil
	}
	m.enqueueEffect(*save, 0, false)
	return m.nextEffectCmd()
}

func (m *Model) openConnectionDialog(editing remote.Connection) {
	m.push(uiconnections.NewSurface(connectionsRoot{m}, editing, m.ssh.List(), uint64(m.nextComposerGen())))
	m.clearErr()
}

// syncRemotePreview follows the selection: a remote session or terminal it
// has not read yet gets a fresh read, anything else clears it.
func (m *Model) syncRemotePreview() tea.Cmd {
	row, ok := m.remoteRow()
	return m.ssh.Follow(row, ok)
}

// tickRemotePreview re-reads the selected remote screen, following the
// cursor first when it has moved to another remote row since.
func (m *Model) tickRemotePreview() tea.Cmd {
	row, ok := m.remoteRow()
	return m.ssh.Tick(row, ok)
}

// remoteRow is the connection row the rail's cursor names.
func (m *Model) remoteRow() (uiconnections.Row, bool) {
	return m.ssh.Selected(m.railSelection())
}

// railSelection is the rail's current selection.
func (m *Model) railSelection() uirail.Selection {
	selection, _ := m.rail.Selected()
	return selection
}

// remoteContentLines fills the content column for a remote selection: a
// connection's facts, a remote group's roster, or a remote screen.
func (m *Model) remoteContentLines(width, height int) ([]contentLine, bool) {
	row, ok := m.remoteRow()
	if !ok {
		return nil, false
	}
	lines, ok := m.ssh.Content(row, width, height, uiconnections.Look{
		StatusGlyph: m.glyphs().StatusGlyph,
		Prompt:      func() string { return m.listGlyph(keybind.Prompt) },
	})
	if !ok {
		return nil, false
	}
	gutter := strings.Repeat(" ", uiconnections.Gutter)
	out := make([]contentLine, len(lines))
	for i, line := range lines {
		switch {
		case line.Rule:
			out[i] = contentLine{rule: true}
		case line.Text == "":
			out[i] = contentLine{}
		default:
			out[i] = contentLine{text: gutter + line.Text}
		}
	}
	return out, true
}

// remoteRowLegend is the footer tier for a connection's rows.
func (m *Model) remoteRowLegend() legendSection {
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.Row(selection)
	if !ok {
		return legendSection{}
	}
	return legendOf(m.rail.RemoteLegend(m.railLegendContext(), uirail.RemoteRow{
		Kind: selection.Kind, Host: row.Host, Terminal: row.Terminal, Dead: row.Status == "dead",
	}, m.rail.SelectionCollapsed(selection)))
}

// applyConnectionSave hands a finished save to the runtime, which adopts the
// list the store returned or leaves the card open with the reason.
func (m *Model) applyConnectionSave(request uilink.SaveRequest, connections []store.Connection, err error) tea.Cmd {
	return m.ssh.ApplySave(linkRoot{m}, request, connections, err)
}

// applyRemoteCall hands what a call left to the runtime, which reports it and
// picks the host up again.
func (m *Model) applyRemoteCall(request uilink.Call, result uilink.Result, err error) tea.Cmd {
	return m.ssh.ApplyCall(linkRoot{m}, request, result, err)
}
