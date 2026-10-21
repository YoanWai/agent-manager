package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The SSH connections keep their list, host states, rows, preview and
// dialog in internal/ui/connections. These adapters hold the SSH client,
// copy each refresh into the package, paint its rows into the rail and the
// content column, and run its requests on the effect lane.

// connectionsFeature is the package's state and the client that refreshes
// and calls the hosts.
type connectionsFeature struct {
	client *remote.Client
	uiconnections.Connections
}

func newConnections(client *remote.Client, stored []store.Connection) connectionsFeature {
	c := connectionsFeature{client: client}
	c.setList(stored)
	return c
}

// setList adopts the stored connections and hands them to the client.
func (c *connectionsFeature) setList(stored []store.Connection) {
	list := make([]remote.Connection, len(stored))
	for i, conn := range stored {
		list[i] = remote.Connection(conn)
	}
	c.SetList(list)
	c.client.SetConnections(list)
}

// row is the remote row a rail selection names.
func (c *connectionsFeature) row(selection uirail.Selection) (uiconnections.Row, bool) {
	if !selection.Remote() {
		return uiconnections.Row{}, false
	}
	kind := uiconnections.SessionRow
	switch selection.Kind {
	case uirail.ConnectionRow:
		kind = uiconnections.ConnectionRow
	case uirail.GroupRow:
		kind = uiconnections.GroupRow
	}
	return c.Row(uiconnections.Target{Kind: kind, Host: selection.Host, Group: selection.Group, ID: selection.SessionID})
}

// railHosts copies each connection's last good snapshot into rail rows.
func (c *connectionsFeature) railHosts() []uirail.Host {
	list := c.List()
	hosts := make([]uirail.Host, 0, len(list))
	for _, conn := range list {
		state, seen := c.Host(conn.Name)
		host := uirail.Host{Name: conn.Name, Err: state.Err, ArchivedGroups: map[string]bool{}}
		switch {
		case !seen:
			host.State = uirail.HostConnecting
		case state.OK:
			host.State = uirail.HostOnline
		default:
			host.State = uirail.HostOffline
			host.Trouble = uiconnections.TroubleLabel(state.Trouble)
		}
		for _, group := range state.Groups {
			host.Groups = append(host.Groups, group.Path)
			if group.Archived {
				host.ArchivedGroups[group.Path] = true
			}
		}
		for _, sess := range state.Sessions {
			host.Sessions = append(host.Sessions, uirail.Session{
				ID: sess.ID, Name: sess.Name, Tool: sess.Tool, Cwd: sess.Directory,
				Group: sess.Group, Status: sess.Status, Archived: sess.Archived,
			})
		}
		for _, term := range state.Terminals {
			host.Sessions = append(host.Sessions, uirail.Session{
				ID: term.ID, Name: term.Name, Tool: remoteTerminalTool, Cwd: term.Directory,
				Group: term.Group, Status: term.Status, Archived: term.Archived,
				ParentID: term.ParentID, IsShell: true,
			})
		}
		hosts = append(hosts, host)
	}
	return hosts
}

// remoteTerminalTool names a remote terminal's CLI in its row; the snapshot
// lists terminals apart from sessions and carries no tool for them.
const remoteTerminalTool = "terminal"

// hostFacts copies a refresh into the package's values.
func hostFacts(state remote.HostState) uiconnections.Host {
	host := uiconnections.Host{
		OK: state.OK, Failures: state.Failures, Attempted: state.Attempted,
		ManagerAwake: state.Snapshot.ManagerAwake,
	}
	if state.Err != nil {
		host.Err, host.Trouble = state.Err.Error(), remote.TroubleOf(state.Err)
	}
	for _, group := range state.Snapshot.Groups {
		host.Groups = append(host.Groups, uiconnections.Group{Path: group.Path, Archived: group.Archived})
	}
	for _, sess := range state.Snapshot.Sessions {
		host.Sessions = append(host.Sessions, uiconnections.Session{
			ID: sess.ID, Name: sess.Name, Tool: sess.Tool, Directory: sess.Directory,
			Group: sess.Group, Status: sess.Status, Archived: sess.Archived,
		})
	}
	for _, term := range state.Snapshot.Terminals {
		host.Terminals = append(host.Terminals, uiconnections.Session{
			ID: term.ID, Name: term.Name, Directory: term.Directory, Group: term.Group,
			Status: term.Status, Archived: term.Archived, ParentID: term.ParentID,
		})
	}
	return host
}

// connectionRefreshMsg is one host's refresh, for the list generation that
// started it.
type connectionRefreshMsg struct {
	host  string
	gen   uint64
	state uiconnections.Host
}

// pollConnections refreshes every connection that has no refresh in
// flight. It rides the workspace poll, off the update path.
func (m *Model) pollConnections() tea.Cmd {
	var commands []tea.Cmd
	for _, conn := range m.ssh.List() {
		commands = append(commands, m.pollConnection(conn.Name))
	}
	return tea.Batch(commands...)
}

func (m *Model) pollConnection(host string) tea.Cmd {
	if !m.ssh.StartRefresh(host, time.Now()) {
		return nil
	}
	client, gen := m.ssh.client, m.ssh.Gen()
	return func() tea.Msg {
		return connectionRefreshMsg{host: host, gen: gen, state: hostFacts(client.Refresh(context.Background(), host))}
	}
}

func (m *Model) applyConnectionRefresh(msg connectionRefreshMsg) tea.Cmd {
	if !m.ssh.Refreshed(msg.host, msg.gen, msg.state) {
		return nil
	}
	m.rebuildRows()
	return m.syncRemotePreview()
}

func (m *Model) routeConnectionMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case connectionRefreshMsg:
		return routed(m, m.applyConnectionRefresh(msg))
	case remotePreviewMsg:
		m.ssh.Preview.Apply(msg.read, msg.text, msg.err)
		return routed(m, nil)
	case remoteAttachReadyMsg:
		return routed(m, m.runRemoteAttach(msg))
	case remoteAttachDoneMsg:
		key, err := remote.AttachKey(msg.err)
		switch {
		case err != nil:
			m.reportErr(msg.host + ": attach ended: " + err.Error())
		case key == "review":
			m.reportErr("review opens only for sessions on this machine, not yet for one on " + msg.host)
		case key == "editor":
			m.reportErr("the editor opens only for sessions on this machine, not yet for one on " + msg.host)
		}
		return routed(m, tea.Batch(m.pollConnection(msg.host), m.readRemotePreview()))
	}
	return nil, nil, false
}

func (m *Model) openConnectionDialog(editing remote.Connection) {
	m.ssh.Dialog.Open(editing)
	m.clearErr()
	m.mode = modeConnection
}

func (m *Model) handleConnectionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := m.ssh.Dialog.Key(typedText(msg), m.ssh.List())
	switch key.Action {
	case uiconnections.DialogCancel:
		m.clearErr()
		m.mode = modeList
	case uiconnections.DialogRefused:
		m.reportErr(key.Refusal)
	case uiconnections.DialogSave:
		op := connectionAdd
		if key.Editing != "" {
			op = connectionUpdate
		}
		m.clearErr()
		m.enqueueEffect(connectionRequest{op: op, name: key.Editing, next: store.Connection(key.Next), gen: key.Gen}, 0, false)
		return m, tea.Batch(key.Cmd, m.nextEffectCmd())
	}
	return m, key.Cmd
}

func (m *Model) viewConnectionDialog() string {
	return m.drawCard(m.ssh.Dialog.View(uiconnections.DialogView{Field: m.formField, Input: textInputView, Muted: mutedStyle()}))
}

// remotePreviewMsg is a read of the selected remote row's screen.
type remotePreviewMsg struct {
	read uiconnections.Read
	text string
	err  error
}

// syncRemotePreview follows the selection: a remote session or terminal it
// has not read yet gets a fresh read, anything else clears it.
func (m *Model) syncRemotePreview() tea.Cmd {
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.row(selection)
	if !m.ssh.Preview.Follow(row, ok) {
		return nil
	}
	return m.readRemotePreview()
}

// tickRemotePreview re-reads the selected remote screen, following the
// cursor first when it has moved to another remote row since.
func (m *Model) tickRemotePreview() tea.Cmd {
	if cmd := m.syncRemotePreview(); cmd != nil {
		return cmd
	}
	return m.readRemotePreview()
}

// readRemotePreview reads the tracked row's screen once, off the update
// path.
func (m *Model) readRemotePreview() tea.Cmd {
	read, ok := m.ssh.StartRead()
	if !ok {
		return nil
	}
	client := m.ssh.client
	return func() tea.Msg {
		text, err := readRemoteScreen(client, read)
		return remotePreviewMsg{read: read, text: text, err: err}
	}
}

func readRemoteScreen(client *remote.Client, read uiconnections.Read) (string, error) {
	return client.ReadScreen(context.Background(), read.Ref, read.Terminal)
}

// remoteContentLines fills the content column for a remote selection: a
// connection's facts, a remote group's roster, or a remote screen.
func (m *Model) remoteContentLines(width, height int) ([]contentLine, bool) {
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.row(selection)
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
		body = m.ssh.Preview.Screen(inner, height-len(head)-1, uiconnections.ScreenView{Err: errStyle(), Muted: mutedStyle(), ExpandTabs: expandPaneTabs})
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
		fitColumns([]string{title}, []string{reading}, width),
		factRow("ssh to", trimmedValue(conn.Destination), "", width),
	}
	if conn.Binary != "" {
		lines = append(lines, factRow("binary", trimmedValue(conn.Binary), "", width))
	}
	if seen && state.OK {
		manager := "manager awake"
		if !state.ManagerAwake {
			manager = "no manager running"
		}
		lines = append(lines, factRow("host", plainValue(mutedStyle().Render(countNoun(len(state.Sessions), "session")+" · "+
			countNoun(len(state.Terminals), "terminal")+" · "+manager)), "", width))
	}
	if seen && state.Err != "" {
		for _, line := range strings.Split(ansi.Wordwrap(escapeControls(state.Err), max(width-detailLabelWidth, 8), " "), "\n") {
			lines = append(lines, labelStyle().Render(padRight("error", detailLabelWidth))+errStyle().Render(line))
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
		fitColumns([]string{name + subtleStyle().Render("  on ") + host, name}, []string{state}, width),
		factRow("group", plainValue(group), "", width),
	}
}

// remoteRowLegend is the footer tier for a connection's rows.
func (m *Model) remoteRowLegend() legendSection {
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.row(selection)
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
