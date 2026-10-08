package ui

import (
	"os"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

// connectionsFeature is the SSH connections: the stored list, what each
// host's last refresh left, the refreshes in flight, the remote preview and
// the add and edit dialog. Remote rows live only here, never in the store.
type connectionsFeature struct {
	client *remote.Client
	list   []remote.Connection
	// gen counts list changes, so a refresh of a connection since removed
	// or repointed cannot land on the list that replaced it.
	gen     uint64
	hosts   map[string]remote.HostState
	polling map[string]bool
	preview remotePreview
	dialog  connectionDialog
	// from names the user on this machine to the agent a remote send
	// reaches.
	from string
}

// maxSenderBytes is what send --from accepts on the remote host.
const maxSenderBytes = 64

func newConnections(client *remote.Client, stored []store.Connection) connectionsFeature {
	hostname, _ := os.Hostname()
	c := connectionsFeature{client: client, from: senderName(hostname)}
	c.setList(stored)
	return c
}

// senderName is "<short hostname>: you", cut to what send --from takes.
func senderName(hostname string) string {
	short, _, _ := strings.Cut(hostname, ".")
	short = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, short)
	const suffix = ": you"
	if short == "" {
		return "you"
	}
	for len(short)+len(suffix) > maxSenderBytes {
		_, size := utf8.DecodeLastRuneInString(short)
		short = short[:len(short)-size]
	}
	return short + suffix
}

// setList adopts the stored connections and hands them to the client; a
// host no longer listed loses its last refresh.
func (c *connectionsFeature) setList(stored []store.Connection) {
	c.gen++
	previous := c.list
	c.list = make([]remote.Connection, len(stored))
	kept := map[string]remote.HostState{}
	for i, conn := range stored {
		c.list[i] = remote.Connection{Name: conn.Name, Destination: conn.Destination}
		if state, ok := c.hosts[conn.Name]; ok && slices.Contains(previous, c.list[i]) {
			kept[conn.Name] = state
		}
	}
	c.hosts = kept
	c.polling = map[string]bool{}
	c.client.SetConnections(c.list)
}

func (c *connectionsFeature) connection(name string) (remote.Connection, bool) {
	for _, conn := range c.list {
		if conn.Name == name {
			return conn, true
		}
	}
	return remote.Connection{}, false
}

// railHosts copies each connection's last good snapshot into rail rows.
func (c *connectionsFeature) railHosts() []uirail.Host {
	hosts := make([]uirail.Host, 0, len(c.list))
	for _, conn := range c.list {
		state, seen := c.hosts[conn.Name]
		host := uirail.Host{Name: conn.Name, ArchivedGroups: map[string]bool{}}
		switch {
		case !seen:
			host.State = uirail.HostConnecting
		case state.OK:
			host.State = uirail.HostOnline
		default:
			host.State = uirail.HostOffline
		}
		if state.Err != nil {
			host.Err = state.Err.Error()
		}
		for _, group := range state.Snapshot.Groups {
			host.Groups = append(host.Groups, group.Path)
			if group.Archived {
				host.ArchivedGroups[group.Path] = true
			}
		}
		for _, sess := range state.Snapshot.Sessions {
			host.Sessions = append(host.Sessions, uirail.Session{
				ID: sess.ID, Name: sess.Name, Tool: sess.Tool, Cwd: sess.Directory,
				Group: sess.Group, Status: sess.Status, Archived: sess.Archived,
			})
		}
		for _, term := range state.Snapshot.Terminals {
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

// remoteRow is the remote row a selection names, as the last snapshot of
// its host has it.
type remoteRow struct {
	kind     uirail.RowKind
	host     string
	group    string
	ref      remote.Ref
	name     string
	status   string
	terminal bool
	archived bool
}

func (c *connectionsFeature) row(selection uirail.Selection) (remoteRow, bool) {
	if !selection.Remote() {
		return remoteRow{}, false
	}
	row := remoteRow{kind: selection.Kind, host: selection.Host, group: selection.Group}
	if selection.Kind != uirail.SessionRow {
		return row, true
	}
	snapshot := c.hosts[selection.Host].Snapshot
	for _, sess := range snapshot.Sessions {
		if sess.ID == selection.SessionID {
			return remoteSessionRow(row, sess), true
		}
	}
	for _, term := range snapshot.Terminals {
		if term.ID == selection.SessionID {
			row.ref = remote.Ref{Host: selection.Host, ID: term.ID}
			row.name, row.status, row.group = term.Name, term.Status, term.Group
			row.terminal, row.archived = true, term.Archived
			return row, true
		}
	}
	return remoteRow{}, false
}

func remoteSessionRow(row remoteRow, sess sessioncmd.Session) remoteRow {
	row.ref = remote.Ref{Host: row.host, ID: sess.ID}
	row.name, row.status, row.group, row.archived = sess.Name, sess.Status, sess.Group, sess.Archived
	return row
}

// offline is the refusal for a call to a host the last refresh could not
// reach; a host never reached yet is still tried.
func (c *connectionsFeature) offline(host string) (string, bool) {
	state, seen := c.hosts[host]
	if !seen || state.OK {
		return "", false
	}
	return host + " is offline: " + state.Err.Error(), true
}
