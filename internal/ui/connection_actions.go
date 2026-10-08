package ui

import (
	"os/exec"
	"strings"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

// remoteRefusals names the rail actions a connection's rows do not offer,
// as the status line words them.
var remoteRefusals = map[uirail.ActionKind]string{
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
// local handlers; New Session and quick prompt mode open as usual and
// find their remote target when they submit.
func (m *Model) connectionIntent(intent uirail.Intent) (tea.Cmd, bool) {
	if intent.Kind == uirail.NewConnection {
		m.openConnectionDialog(remote.Connection{})
		return nil, true
	}
	if !intent.Target.Remote() {
		return nil, false
	}
	switch intent.Kind {
	case uirail.Quit, uirail.OpenSettings, uirail.Resize, uirail.OpenMessages, uirail.OpenHelp, uirail.Prompt, uirail.NewSession:
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
	if intent.Kind == uirail.NewTerminal {
		group := row.group
		return m.queueRemote(remoteRequest{op: remoteNewTerminal, host: row.host,
			terminal: sessioncmd.CreateTerminalOptions{Group: &group}}), true
	}
	if row.kind == uirail.SessionRow {
		if cmd, ok := m.remoteSessionIntent(intent.Kind, row); ok {
			return cmd, true
		}
	}
	if action, refused := remoteRefusals[intent.Kind]; refused {
		m.reportErr(uirail.RemoteRefusal(action))
	}
	return nil, true
}

func (m *Model) remoteSessionIntent(kind uirail.ActionKind, row remoteRow) (tea.Cmd, bool) {
	request := remoteRequest{host: row.host, ref: row.ref, name: row.name}
	switch {
	case kind == uirail.Focus || kind == uirail.Attach:
		return m.attachRemote(row), true
	case kind == uirail.Kill && row.terminal:
		request.op = remoteCloseTerminal
	case kind == uirail.Kill:
		request.op = remoteKill
	case kind == uirail.Revive:
		request.op = remoteRevive
	case kind == uirail.Archive || kind == uirail.Restore:
		// Each key is a no-op in the view where its rows are already
		// where it would put them.
		if (kind == uirail.Restore) != row.archived {
			return nil, true
		}
		request.op = remoteArchive
		if row.archived {
			request.op = remoteRestore
		}
	default:
		return nil, false
	}
	return m.queueRemote(request), true
}

// queueRemote puts a call on the remote lane, unless the host's last
// refresh could not reach it.
func (m *Model) queueRemote(request remoteRequest) tea.Cmd {
	if refusal, offline := m.ssh.offline(request.host); offline {
		m.reportErr(refusal)
		return nil
	}
	m.clearErr()
	m.enqueueEffect(request, 0, false)
	return m.nextEffectCmd()
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

type remoteAttachReadyMsg struct {
	host string
	cmd  *exec.Cmd
	err  error
}

type remoteAttachDoneMsg struct {
	host string
	err  error
}

// attachRemote opens an SSH attach to the row's tmux session in this
// terminal; focus does the same, since a remote pane cannot be embedded.
func (m *Model) attachRemote(row remoteRow) tea.Cmd {
	if refusal, offline := m.ssh.offline(row.host); offline {
		m.reportErr(refusal)
		return nil
	}
	client, ref := m.ssh.client, row.ref
	return func() tea.Msg {
		cmd, err := client.AttachCommand(ref)
		return remoteAttachReadyMsg{host: ref.Host, cmd: cmd, err: err}
	}
}

func (m *Model) runRemoteAttach(msg remoteAttachReadyMsg) tea.Cmd {
	if msg.err != nil {
		m.reportErr(msg.err.Error())
		return nil
	}
	host := msg.host
	return execTerminalProcess(msg.cmd, func(err error) tea.Msg { return remoteAttachDoneMsg{host: host, err: err} })
}

// remoteQuickKey answers enter in the quick bar when the target is on a
// connection: a remote agent gets the prompt as a message, a connection or
// remote group gets a new agent.
func (m *Model) remoteQuickKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if msg.Type != tea.KeyEnter || m.quick.picking != pickNone {
		return nil, false
	}
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.row(selection)
	if !ok {
		return nil, false
	}
	q := &m.quick
	switch {
	case q.pasting():
		m.reportErr("still reading the pasted image - try again in a moment")
		return nil, true
	case len(q.attachments) > 0:
		m.reportErr(uirail.RemoteRefusal("pasted images"))
		return nil, true
	}
	text := q.message()
	if text == "" {
		m.reportErr("prompt cannot be empty")
		return nil, true
	}
	request := remoteRequest{host: row.host, text: text, draft: q.input.Value(), composerGen: q.gen, closeAfterSend: q.closeAfterSend}
	if row.kind == uirail.SessionRow {
		if row.terminal {
			m.reportErr(shellPromptHint(row.name))
			return nil, true
		}
		request.op, request.ref, request.name, request.from = remoteSend, row.ref, row.name, m.ssh.from
		return m.queueRemote(request), true
	}
	if strings.HasPrefix(text, "-") {
		m.reportErr(`prompt cannot start with "-": the tool would read it as a flag`)
		return nil, true
	}
	group := row.group
	request.op = remoteSpawn
	request.spawn = sessioncmd.CreateSessionOptions{Tool: q.tool(), Group: &group, Prompt: text}
	if q.worktreeTouched {
		worktree := q.worktree
		request.spawn.Worktree = &worktree
	}
	return m.queueRemote(request), true
}

// remoteTarget is the connection and group a spawn from the selected row
// lands in, when that row is on a connection.
func (m *Model) remoteTarget() (formRemote, bool) {
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.row(selection)
	if !ok {
		return formRemote{}, false
	}
	return formRemote{host: row.host, group: row.group}, true
}

// remoteFormSpawn turns the New Session form's spawn into the remote
// call: the tool, name, directory, prompt, choice and an explicit worktree
// pick go to the host, which resolves the rest.
func (m *Model) remoteFormSpawn(spawn spawnRequest) tea.Cmd {
	if len(spawn.images) > 0 {
		m.reportErr(uirail.RemoteRefusal("pasted images"))
		return nil
	}
	target := m.form.remote
	opts := sessioncmd.CreateSessionOptions{
		Tool: spawn.toolName, Group: &target.group, Directory: strings.TrimSpace(spawn.rawDir), Prompt: spawn.prompt,
		Model: spawn.choice.Model, Effort: spawn.choice.Effort, Profile: spawn.choice.Profile,
	}
	if !spawn.autoNamed {
		opts.Name = spawn.name
	}
	if !m.form.worktreeAuto {
		worktree := spawn.wantWorktree
		opts.Worktree = &worktree
	}
	return m.queueRemote(remoteRequest{op: remoteSpawn, host: target.host, spawn: opts, composerGen: spawn.composerGen, fromForm: true})
}

// formRemote is the connection and group a New Session form spawns into.
type formRemote struct{ host, group string }

func (r formRemote) on() bool { return r.host != "" }

// aimRemote points an opened form at a connection: the group is fixed, the
// directory is the host's to resolve, and the worktree inherits its default
// until picked.
func (d *formDialog) aimRemote(target formRemote) {
	d.remote = target
	d.groups = []groupOption{{path: target.group}}
	d.groupIndex = 0
	d.dir.SetValue("")
	d.dir.Placeholder = "the group's directory on " + target.host
	d.dirAuto = false
	d.worktree = false
}

// remoteFormRequest takes the form requests that differ on a connection:
// the group is fixed, nothing is probed locally, and the spawn goes to the
// host.
func (m *Model) remoteFormRequest(request formRequest) (tea.Cmd, bool) {
	if !m.form.remote.on() {
		return nil, false
	}
	switch {
	case request.group != 0, request.probe:
		return nil, true
	case request.toggle:
		m.form.setWorktree(m.form.worktreeAuto || !m.form.worktree)
		return nil, true
	case request.spawn != nil:
		m.clearErr()
		return m.remoteFormSpawn(*request.spawn), true
	}
	return nil, false
}
