package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
)

// maxSenderBytes is the longest name the remote send --from takes.
const maxSenderBytes = 64

// remoteSource is where the server finds the user's SSH connections and
// the client that reaches them.
type remoteSource struct {
	client      *remote.Client
	connections func() ([]store.Connection, error)
}

func storeConnections(configDir string) func() ([]store.Connection, error) {
	return func() ([]store.Connection, error) {
		st, err := store.Open(filepath.Join(configDir, "state.db"))
		if err != nil {
			return nil, err
		}
		defer st.Close()
		return st.Connections()
	}
}

// remoteHosts serves the rows of every SSH connection beside this host's.
type remoteHosts struct {
	remoteSource
	sessionID string
	noCaller  error
	sessions  sessionCommands
	terminals terminalCommands
}

type sessionRow struct {
	sessioncmd.Session
	Host  string `json:"host,omitempty" jsonschema:"SSH connection the session runs behind, whose id is written host::id; empty for this machine"`
	Stale bool   `json:"stale,omitempty" jsonschema:"true when that connection could not be refreshed, so the row is the last one it reported"`
}

type terminalRow struct {
	sessioncmd.Terminal
	Host  string `json:"host,omitempty" jsonschema:"SSH connection the terminal runs behind, whose id is written host::id; empty for this machine"`
	Stale bool   `json:"stale,omitempty" jsonschema:"true when that connection could not be refreshed, so the row is the last one it reported"`
}

type groupRow struct {
	sessioncmd.Group
	Host  string `json:"host,omitempty" jsonschema:"SSH connection the group is on; pass it as host beside this path to create_session, create_terminal or delete_group; empty for this machine"`
	Stale bool   `json:"stale,omitempty" jsonschema:"true when that connection could not be refreshed, so the row is the last one it reported"`
}

type connectionError struct {
	Host  string `json:"host" jsonschema:"SSH connection name"`
	Error string `json:"error" jsonschema:"what went wrong refreshing that connection; its rows here, if any, are the last it reported and carry stale"`
}

type hostRows struct {
	name  string
	state remote.HostState
}

// ready refuses a caller outside Agent Manager, as the local tools do, and
// picks up a connection the user added, edited or removed in the manager
// since the last call.
func (h *remoteHosts) ready() ([]remote.Connection, error) {
	if h.noCaller != nil {
		return nil, h.noCaller
	}
	stored, err := h.connections()
	if err != nil {
		return nil, err
	}
	next := make([]remote.Connection, len(stored))
	for i, conn := range stored {
		next[i] = remote.Connection{Name: conn.Name, Destination: conn.Destination}
	}
	if !slices.Equal(next, h.client.Connections()) {
		h.client.SetConnections(next)
	}
	return next, nil
}

// refresh snapshots every connection at once, so a listing waits for its
// slowest host rather than for all of them in turn.
func (h *remoteHosts) refresh(ctx context.Context) ([]hostRows, error) {
	connections, err := h.ready()
	if err != nil {
		return nil, err
	}
	hosts := make([]hostRows, len(connections))
	var wg sync.WaitGroup
	for i, conn := range connections {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hosts[i] = hostRows{name: conn.Name, state: h.client.Refresh(ctx, conn.Name)}
		}()
	}
	wg.Wait()
	return hosts, nil
}

func (h hostRows) heading() string {
	heading := "\n\nSSH connection " + h.name
	switch {
	case !h.state.OK:
		heading += " (not refreshed: " + hostProblem(h.state.Err) + "; these are the rows it last reported)"
	case h.state.Err != nil:
		heading += " (" + hostProblem(h.state.Err) + ")"
	}
	return heading + ":\n"
}

func (h hostRows) failure() (connectionError, bool) {
	if h.state.Err == nil {
		return connectionError{}, false
	}
	problem := hostProblem(h.state.Err)
	if h.state.OK {
		problem = "starting its manager failed: " + problem
	}
	return connectionError{Host: h.name, Error: problem}, true
}

// hostProblem drops the host the error names, which the heading or the
// row already carries.
func hostProblem(err error) string {
	var remoteErr *remote.Error
	if errors.As(err, &remoteErr) {
		return remoteErr.Err.Error()
	}
	return err.Error()
}

// listing puts every connection's rows after this host's, in connection
// order, each under a heading in the text.
func listing[T, R any](hosts []hostRows, local []T, format func([]T) string, remoteRows func(hostRows) []T, row func(item T, host string, stale bool) R) (string, []R, []connectionError) {
	text := format(local)
	rows := make([]R, 0, len(local))
	for _, item := range local {
		rows = append(rows, row(item, "", false))
	}
	var failures []connectionError
	for _, host := range hosts {
		items := remoteRows(host)
		for _, item := range items {
			rows = append(rows, row(item, host.name, !host.state.OK))
		}
		text += host.heading() + format(items)
		if failure, ok := host.failure(); ok {
			failures = append(failures, failure)
		}
	}
	return text, rows, failures
}

func (h *remoteHosts) listSessions(ctx context.Context, local []sessioncmd.Session) (string, listSessionsOutput, error) {
	hosts, err := h.refresh(ctx)
	if err != nil {
		return "", listSessionsOutput{}, err
	}
	text, rows, failures := listing(hosts, local, sessioncmd.FormatSessionList, func(host hostRows) []sessioncmd.Session {
		qualified := make([]sessioncmd.Session, 0, len(host.state.Snapshot.Sessions))
		for _, sess := range host.state.Snapshot.Sessions {
			qualified = append(qualified, qualifySession(host.name, sess))
		}
		return qualified
	}, func(sess sessioncmd.Session, host string, stale bool) sessionRow {
		return sessionRow{Session: sess, Host: host, Stale: stale}
	})
	return text, listSessionsOutput{Sessions: rows, ConnectionErrors: failures}, nil
}

// listTerminals leaves out a host's archived terminals, as the local list
// does.
func (h *remoteHosts) listTerminals(ctx context.Context, local []sessioncmd.Terminal) (string, listTerminalsOutput, error) {
	hosts, err := h.refresh(ctx)
	if err != nil {
		return "", listTerminalsOutput{}, err
	}
	text, rows, failures := listing(hosts, local, sessioncmd.FormatTerminalList, func(host hostRows) []sessioncmd.Terminal {
		qualified := make([]sessioncmd.Terminal, 0, len(host.state.Snapshot.Terminals))
		for _, terminal := range host.state.Snapshot.Terminals {
			if !terminal.Archived {
				qualified = append(qualified, qualifyTerminal(host.name, terminal))
			}
		}
		return qualified
	}, func(terminal sessioncmd.Terminal, host string, stale bool) terminalRow {
		return terminalRow{Terminal: terminal, Host: host, Stale: stale}
	})
	return text, listTerminalsOutput{Terminals: rows, ConnectionErrors: failures}, nil
}

func (h *remoteHosts) listGroups(ctx context.Context, local []sessioncmd.Group) (string, listGroupsOutput, error) {
	hosts, err := h.refresh(ctx)
	if err != nil {
		return "", listGroupsOutput{}, err
	}
	text, rows, failures := listing(hosts, local, sessioncmd.FormatGroupList, func(host hostRows) []sessioncmd.Group {
		return host.state.Snapshot.Groups
	}, func(group sessioncmd.Group, host string, stale bool) groupRow {
		return groupRow{Group: group, Host: host, Stale: stale}
	})
	return text, listGroupsOutput{Groups: rows, ConnectionErrors: failures}, nil
}

func qualifySession(host string, sess sessioncmd.Session) sessioncmd.Session {
	sess.ID = remote.Ref{Host: host, ID: sess.ID}.String()
	return sess
}

func qualifyTerminal(host string, terminal sessioncmd.Terminal) sessioncmd.Terminal {
	terminal.ID = remote.Ref{Host: host, ID: terminal.ID}.String()
	if terminal.ParentID != "" {
		terminal.ParentID = remote.Ref{Host: host, ID: terminal.ParentID}.String()
	}
	return terminal
}

// localOnly refuses a qualified id before any I/O, for a tool whose state
// lives on this host alone.
func localOnly(tool string, ids ...string) error {
	for _, id := range ids {
		if _, ok := remote.ParseRef(id); ok {
			return fmt.Errorf("%s works on this host's sessions only", tool)
		}
	}
	return nil
}

// cachedSession fills the row a remote read does not return from the
// host's last snapshot.
func (h *remoteHosts) cachedSession(ref remote.Ref) sessioncmd.Session {
	for _, sess := range h.client.State(ref.Host).Snapshot.Sessions {
		if sess.ID == ref.ID {
			return qualifySession(ref.Host, sess)
		}
	}
	return sessioncmd.Session{ID: ref.String()}
}

func (h *remoteHosts) cachedTerminal(ref remote.Ref) sessioncmd.Terminal {
	for _, terminal := range h.client.State(ref.Host).Snapshot.Terminals {
		if terminal.ID == ref.ID {
			return qualifyTerminal(ref.Host, terminal)
		}
	}
	return sessioncmd.Terminal{ID: ref.String()}
}

func (h *remoteHosts) readSession(ctx context.Context, ref remote.Ref) (sessioncmd.SessionScreen, error) {
	if _, err := h.ready(); err != nil {
		return sessioncmd.SessionScreen{}, err
	}
	output, err := h.client.Read(ctx, ref, false)
	if err != nil {
		return sessioncmd.SessionScreen{}, err
	}
	return sessioncmd.SessionScreen{Session: h.cachedSession(ref), Output: output}, nil
}

func (h *remoteHosts) readTerminal(ctx context.Context, ref remote.Ref) (sessioncmd.TerminalScreen, error) {
	if _, err := h.ready(); err != nil {
		return sessioncmd.TerminalScreen{}, err
	}
	output, err := h.client.Read(ctx, ref, true)
	if err != nil {
		return sessioncmd.TerminalScreen{}, err
	}
	return sessioncmd.TerminalScreen{Terminal: h.cachedTerminal(ref), Output: output}, nil
}

func (h *remoteHosts) send(ctx context.Context, ref remote.Ref, message string) (string, sessioncmd.SendResult, error) {
	if _, err := h.ready(); err != nil {
		return "", sessioncmd.SendResult{}, err
	}
	from, err := h.sender()
	if err != nil {
		return "", sessioncmd.SendResult{}, err
	}
	result, err := h.client.Send(ctx, ref, message, from)
	if err != nil {
		return "", sessioncmd.SendResult{}, err
	}
	// The id is the remote host's own; handed to this host's message_status
	// it would name another message, so it never leaves this function.
	result.MessageID = 0
	text := fmt.Sprintf("queued a message for session %s at position %d", ref, result.QueuePosition)
	if !result.ManagerAwake {
		text += "; no manager is running on " + ref.Host + " yet, so it waits until one starts"
	}
	text += "; message_status follows messages to this host's sessions only, so call read_session on " + ref.String() + " to see what the agent did with it"
	return text, result, nil
}

// sender names the caller to the remote host, where it has no session: the
// remote envelope shows this name and says replies cannot reach it.
func (h *remoteHosts) sender() (string, error) {
	name, err := h.callerName()
	if err != nil {
		return "", err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	hostname, _, _ = strings.Cut(hostname, ".")
	return senderName(name, hostname), nil
}

func (h *remoteHosts) callerName() (string, error) {
	listed, err := h.sessions.List(h.sessionID)
	if err != nil {
		return "", err
	}
	for _, sess := range listed {
		if sess.Self {
			return sess.Name, nil
		}
	}
	terminals, err := h.terminals.List(h.sessionID)
	if err != nil {
		return "", err
	}
	for _, terminal := range terminals {
		if terminal.ID == h.sessionID {
			return terminal.Name, nil
		}
	}
	return "", fmt.Errorf("session %s is not in Agent Manager's list", h.sessionID)
}

// senderName shortens the session name, not the host, when the whole does
// not fit, so the remote reader still sees where the message came from.
func senderName(name, hostname string) string {
	suffix := " on " + hostname
	if budget := maxSenderBytes - len(suffix); budget > 0 {
		return truncateUTF8(name, budget) + suffix
	}
	return truncateUTF8(name+suffix, maxSenderBytes)
}

func truncateUTF8(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

func (h *remoteHosts) kill(ctx context.Context, ref remote.Ref) (sessioncmd.Session, error) {
	if _, err := h.ready(); err != nil {
		return sessioncmd.Session{}, err
	}
	killed, err := h.client.Kill(ctx, ref)
	return qualifySession(ref.Host, killed), err
}

func (h *remoteHosts) revive(ctx context.Context, ref remote.Ref) (sessioncmd.Session, error) {
	if _, err := h.ready(); err != nil {
		return sessioncmd.Session{}, err
	}
	revived, err := h.client.Revive(ctx, ref)
	return qualifySession(ref.Host, revived), err
}

func (h *remoteHosts) archive(ctx context.Context, ref remote.Ref, archived bool) (sessioncmd.Session, error) {
	if _, err := h.ready(); err != nil {
		return sessioncmd.Session{}, err
	}
	updated, err := h.client.Archive(ctx, ref, !archived)
	return qualifySession(ref.Host, updated), err
}

func (h *remoteHosts) spawn(ctx context.Context, host string, opts sessioncmd.CreateSessionOptions) (sessioncmd.Session, error) {
	if _, err := h.ready(); err != nil {
		return sessioncmd.Session{}, err
	}
	created, err := h.client.Spawn(ctx, host, opts)
	return qualifySession(host, created), err
}

func (h *remoteHosts) createTerminal(ctx context.Context, host string, opts sessioncmd.CreateTerminalOptions) (sessioncmd.Terminal, error) {
	if _, err := h.ready(); err != nil {
		return sessioncmd.Terminal{}, err
	}
	created, err := h.client.CreateTerminal(ctx, host, opts)
	return qualifyTerminal(host, created), err
}

func (h *remoteHosts) sendTerminal(ctx context.Context, ref remote.Ref, command string, keys []string) (sessioncmd.TerminalInput, error) {
	if _, err := h.ready(); err != nil {
		return sessioncmd.TerminalInput{}, err
	}
	sent, err := h.client.TerminalSend(ctx, ref, command, keys)
	sent.TerminalID = ref.String()
	return sent, err
}

func (h *remoteHosts) closeTerminal(ctx context.Context, ref remote.Ref) error {
	if _, err := h.ready(); err != nil {
		return err
	}
	return h.client.TerminalClose(ctx, ref)
}

func (h *remoteHosts) createGroup(ctx context.Context, host, path, directory string) (sessioncmd.Group, error) {
	if _, err := h.ready(); err != nil {
		return sessioncmd.Group{}, err
	}
	return h.client.CreateGroup(ctx, host, path, directory)
}

func (h *remoteHosts) deleteGroup(ctx context.Context, host, path string) (sessioncmd.GroupRemoval, error) {
	if _, err := h.ready(); err != nil {
		return sessioncmd.GroupRemoval{}, err
	}
	removal, err := h.client.DeleteGroup(ctx, host, path)
	for i, id := range removal.Moved {
		removal.Moved[i] = remote.Ref{Host: host, ID: id}.String()
	}
	return removal, err
}
