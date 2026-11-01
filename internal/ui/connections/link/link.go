package link

import (
	"context"
	"os/exec"
	"time"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	tea "github.com/charmbracelet/bubbletea"
)

// remoteTerminalTool names a remote terminal's CLI in its row; a host's
// snapshot lists terminals apart from sessions and carries no tool for
// them.
const remoteTerminalTool = "terminal"

// State is the stored connections, each host's last refresh, the refreshes
// in flight and the selected row's screen, next to the client that
// refreshes and calls the hosts.
type State struct {
	Client *remote.Client
	uiconnections.Connections
}

// Asker is the surface a call came from, so a completed one closes it.
type Asker uint8

const (
	FormAsker Asker = iota + 1
	GroupFormAsker
)

type Root interface {
	RebuildRows()
	SyncPreview() tea.Cmd
	RailConnection(change RailChange)
	AnswerSave(gen uint64, failed bool)
	// CloseAsker closes the form or card a completed call came from, when
	// it is still the one that asked.
	CloseAsker(gen int, asker Asker)
	// ClearAccepted clears the quick bar a send came from, when it still
	// holds that draft.
	ClearAccepted(composerGen int, draft string, closeAfterSend bool)
	ReportErr(text string)
	ReportDone(text string)
}

func New(client *remote.Client, stored []store.Connection) State {
	state := State{Client: client}
	state.SetList(stored)
	return state
}

func (s *State) SetList(stored []store.Connection) {
	list := make([]remote.Connection, len(stored))
	for i, conn := range stored {
		list[i] = remote.Connection(conn)
	}
	s.Connections.SetList(list)
	s.Client.SetConnections(list)
}

func (s *State) Row(selection uirail.Selection) (uiconnections.Row, bool) {
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
	return s.Connections.Row(uiconnections.Target{Kind: kind, Host: selection.Host, Group: selection.Group, ID: selection.SessionID})
}

func (s *State) RailHosts() []uirail.Host {
	list := s.List()
	hosts := make([]uirail.Host, 0, len(list))
	for _, conn := range list {
		state, seen := s.Host(conn.Name)
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
				ParentID: term.ParentID, IsTool: true, IsShell: true,
			})
		}
		hosts = append(hosts, host)
	}
	return hosts
}

func Facts(state remote.HostState) uiconnections.Host {
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

// RefreshMsg is one host's refresh, for the list generation that started
// it.
type RefreshMsg struct {
	Host  string
	Gen   uint64
	State uiconnections.Host
}

type PreviewMsg struct {
	Read uiconnections.Read
	Text string
	Err  error
}

// AttachReadyMsg carries the SSH attach command built off the update path.
type AttachReadyMsg struct {
	Host string
	Cmd  *exec.Cmd
	Err  error
}

type AttachDoneMsg struct {
	Host string
	Err  error
}

// PollHost refreshes one connection that has no refresh in flight.
func (s *State) PollHost(host string) tea.Cmd {
	if !s.StartRefresh(host, time.Now()) {
		return nil
	}
	client, gen := s.Client, s.Gen()
	return func() tea.Msg {
		return RefreshMsg{Host: host, Gen: gen, State: Facts(client.Refresh(context.Background(), host))}
	}
}

// Poll refreshes every connection that has no refresh in flight. It rides
// the workspace poll, off the update path.
func (s *State) Poll() tea.Cmd {
	var commands []tea.Cmd
	for _, conn := range s.List() {
		commands = append(commands, s.PollHost(conn.Name))
	}
	return tea.Batch(commands...)
}

// Follow tracks the selected row: a remote session or terminal it has not
// read yet gets a fresh read, anything else clears the preview.
func (s *State) Follow(row uiconnections.Row, ok bool) tea.Cmd {
	if !s.Preview.Follow(row, ok) {
		return nil
	}
	return s.Read()
}

// Tick re-reads the selected screen, following the selection first when it
// has moved to another remote row since.
func (s *State) Tick(row uiconnections.Row, ok bool) tea.Cmd {
	if cmd := s.Follow(row, ok); cmd != nil {
		return cmd
	}
	return s.Read()
}

// Read reads the tracked row's screen once, off the update path.
func (s *State) Read() tea.Cmd {
	read, ok := s.StartRead()
	if !ok {
		return nil
	}
	client := s.Client
	return func() tea.Msg {
		text, err := client.ReadScreen(context.Background(), read.Ref, read.Terminal)
		return PreviewMsg{Read: read, Text: text, Err: err}
	}
}

// Attach builds the SSH attach to the row's tmux session in this terminal,
// off the update path.
func (s *State) Attach(row uiconnections.Row) tea.Cmd {
	client, ref := s.Client, row.Ref
	return func() tea.Msg {
		cmd, err := client.AttachCommand(ref)
		return AttachReadyMsg{Host: ref.Host, Cmd: cmd, Err: err}
	}
}

func (s *State) Route(root Root, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case RefreshMsg:
		if !s.Refreshed(msg.Host, msg.Gen, msg.State) {
			return nil, true
		}
		root.RebuildRows()
		return root.SyncPreview(), true

	case PreviewMsg:
		s.Preview.Apply(msg.Read, msg.Text, msg.Err)
		return nil, true

	case AttachReadyMsg:
		return s.attachReady(root, msg), true

	case AttachDoneMsg:
		return s.attachDone(root, msg), true
	}
	return nil, false
}

// attachReady suspends the TUI and runs the attach on the real terminal, so
// the tmux session fills the screen as it does in a plain shell.
func (s *State) attachReady(root Root, msg AttachReadyMsg) tea.Cmd {
	if msg.Err != nil {
		root.ReportErr(msg.Err.Error())
		return nil
	}
	host := msg.Host
	return uiterminal.ExecProcess(msg.Cmd, func(err error) tea.Msg { return AttachDoneMsg{Host: host, Err: err} })
}

// attachDone reports how the attach ended and picks the host and its screen
// back up.
func (s *State) attachDone(root Root, msg AttachDoneMsg) tea.Cmd {
	key, err := remote.AttachKey(msg.Err)
	switch {
	case err != nil:
		root.ReportErr(msg.Host + ": attach ended: " + err.Error())
	case key == "review":
		root.ReportErr("review opens only for sessions on this machine, not yet for one on " + msg.Host)
	case key == "editor":
		root.ReportErr("the editor opens only for sessions on this machine, not yet for one on " + msg.Host)
	}
	return tea.Batch(s.PollHost(msg.Host), s.Read())
}
