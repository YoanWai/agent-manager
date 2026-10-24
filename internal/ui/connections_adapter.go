package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uipreview "github.com/YoanWai/agent-manager/internal/ui/preview"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The SSH connections keep their list, host states, rows, preview and
// dialog in internal/ui/connections, and the client that refreshes and
// calls the hosts in internal/ui/connections/link. These adapters answer
// what the package asks of the manager and paint its rows into the content
// column.

// linkRoot is the root as a routed connection result reaches it.
type linkRoot struct{ *Model }

func (r linkRoot) RebuildRows() { r.rebuildRows() }

func (r linkRoot) SyncPreview() tea.Cmd { return r.syncRemotePreview() }

func (r linkRoot) RailConnection(change uilink.RailChange) {
	switch {
	case change.Rename != "":
		r.applyRailStateDecision(r.rail.RenameHost(change.Rename, change.To))
	case change.Forget != "":
		r.applyRailStateDecision(r.rail.ForgetHost(change.Forget))
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
		if s, ok := r.groupFormSurface(); ok && s.Gen() == gen {
			r.Pop()
		}
	}
}

func (r linkRoot) ClearAccepted(composerGen int, draft string, closeAfterSend bool) {
	r.clearAccepted(composerGen, draft, closeAfterSend)
}

func (r linkRoot) ReportErr(text string) { r.reportErr(text) }

func (r linkRoot) ReportDone(text string) { r.reportDone(text) }

// pollConnections refreshes every connection that has no refresh in
// flight. It rides the workspace poll, off the update path.
func (m *Model) pollConnections() tea.Cmd { return m.ssh.Poll() }

func (m *Model) pollConnection(host string) tea.Cmd { return m.ssh.PollHost(host) }

// routeConnectionMsg hands a routed connection result to the package, and
// the card's own answer to this machine.
func (m *Model) routeConnectionMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if msg, ok := msg.(connectionDialogMsg); ok {
		return routed(m, m.handleConnectionDialog(msg))
	}
	if cmd, ok := m.ssh.Route(linkRoot{m}, msg); ok {
		return routed(m, cmd)
	}
	return nil, nil, false
}

// handleConnectionDialog runs what the card asked: a cancel clears the
// status line it leaves, a save queues the store write.
func (m *Model) handleConnectionDialog(msg connectionDialogMsg) tea.Cmd {
	m.clearErr()
	if msg.save == nil {
		return nil
	}
	m.enqueueEffect(*msg.save, 0, false)
	return m.nextEffectCmd()
}

// syncRemotePreview follows the selection: a remote session or terminal it
// has not read yet gets a fresh read, anything else clears it.
func (m *Model) syncRemotePreview() tea.Cmd {
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.Row(selection)
	return m.ssh.Follow(row, ok)
}

// tickRemotePreview re-reads the selected remote screen, following the
// cursor first when it has moved to another remote row since.
func (m *Model) tickRemotePreview() tea.Cmd {
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.Row(selection)
	return m.ssh.Tick(row, ok)
}

// remoteContentLines fills the content column for a remote selection: a
// connection's facts, a remote group's roster, or a remote screen.
func (m *Model) remoteContentLines(width, height int) ([]contentLine, bool) {
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.Row(selection)
	if !ok {
		return nil, false
	}
	gutter := strings.Repeat(" ", contentGutter)
	inner := width - 2*contentGutter
	var head []string
	var body []string
	switch row.Kind {
	case uiconnections.ConnectionRow:
		head = m.connectionDetail(row.Host, inner)
	case uiconnections.GroupRow:
		head = []string{lipgloss.NewStyle().Foreground(colorAccent2()).Bold(true).Render(escapeControlsInline(displayGroup(row.Group))) +
			subtleStyle().Render("  on ") + lipgloss.NewStyle().Foreground(colorRemote()).Render(row.Host)}
		body = []string{mutedStyle().Render("press " + m.listGlyph(keybind.Prompt) + " to start an agent here")}
	default:
		head = m.remoteSessionDetail(row, inner)
		body = m.ssh.Preview.Screen(inner, height-len(head)-1, uiconnections.ScreenView{Err: errStyle(), Muted: mutedStyle(), ExpandTabs: uipreview.ExpandTabs})
	}
	var lines []contentLine
	for _, line := range head {
		lines = append(lines, contentLine{text: gutter + line})
	}
	if body != nil {
		lines = append(lines, contentLine{rule: true})
		for _, line := range body {
			lines = append(lines, contentLine{text: gutter + line})
		}
	}
	for len(lines) < height {
		lines = append(lines, contentLine{})
	}
	return lines[:max(height, 0)], true
}

func (m *Model) connectionDetail(host string, width int) []string {
	conn, _ := m.ssh.Connection(host)
	state, seen := m.ssh.Host(host)
	title := lipgloss.NewStyle().Foreground(colorRemote()).Bold(true).Render(host) + "  " + chipStyle().Render("ssh")
	reading := subtleStyle().Render("connecting…")
	switch {
	case seen && state.OK:
		reading = lipgloss.NewStyle().Foreground(colorFinished()).Render("online")
	case seen:
		reading = errStyle().Render(fmt.Sprintf("%s · %d failed", uiconnections.TroubleLabel(state.Trouble), state.Failures))
	}
	lines := []string{
		uipreview.FitColumns([]string{title}, []string{reading}, width),
		factRow("ssh to", uipreview.TrimmedValue(conn.Destination), "", width),
	}
	if conn.Binary != "" {
		lines = append(lines, factRow("binary", uipreview.TrimmedValue(conn.Binary), "", width))
	}
	if seen && state.OK {
		manager := "manager awake"
		if !state.ManagerAwake {
			manager = "no manager running"
		}
		lines = append(lines, factRow("host", uipreview.PlainValue(mutedStyle().Render(countNoun(len(state.Sessions), "session")+" · "+
			countNoun(len(state.Terminals), "terminal")+" · "+manager)), "", width))
	}
	if seen && state.Err != "" {
		for _, line := range strings.Split(ansi.Wordwrap(escapeControls(state.Err), max(width-uipreview.DetailLabelWidth, 8), " "), "\n") {
			lines = append(lines, labelStyle().Render(padRight("error", uipreview.DetailLabelWidth))+errStyle().Render(line))
		}
	}
	return lines
}

func (m *Model) remoteSessionDetail(row uiconnections.Row, width int) []string {
	status := row.Status
	if row.Archived {
		status = "archived"
	}
	state := lipgloss.NewStyle().Foreground(statusColor(row.Status)).Render(m.glyphs().StatusGlyph(row.Status) + " " + escapeControlsInline(statusLabel(status)))
	name := lipgloss.NewStyle().Foreground(colorBright()).Bold(true).Render(escapeControlsInline(row.Name))
	host := lipgloss.NewStyle().Foreground(colorRemote()).Render(row.Host)
	group := lipgloss.NewStyle().Foreground(colorAccent2()).Render(escapeControlsInline(displayGroup(row.Group)))
	return []string{
		uipreview.FitColumns([]string{name + subtleStyle().Render("  on ") + host, name}, []string{state}, width),
		factRow("group", uipreview.PlainValue(group), "", width),
	}
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

func countNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
