package ui

import (
	"os/exec"
	"strings"

	"github.com/YoanWai/agent-manager/internal/catalog"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	if row.Kind == uiconnections.ConnectionRow {
		switch intent.Kind {
		case uirail.RenameAction:
			conn, _ := m.ssh.Connection(row.Host)
			m.openConnectionDialog(conn)
			return nil, true
		case uirail.Delete:
			m.confirmRemoveConnection(row.Host)
			return nil, true
		}
	}
	if intent.Kind == uirail.NewGroup {
		m.openGroupForm()
		return nil, true
	}
	if intent.Kind == uirail.NewTerminal {
		group := row.Group
		return m.queueRemote(remoteRequest{op: remoteNewTerminal, host: row.Host,
			terminal: sessioncmd.CreateTerminalOptions{Group: &group}}), true
	}
	if row.Kind == uiconnections.SessionRow {
		if cmd, ok := m.remoteSessionIntent(intent.Kind, row); ok {
			return cmd, true
		}
	}
	if action, refused := remoteRefusals[intent.Kind]; refused {
		m.reportErr(uirail.RemoteRefusal(action))
	}
	return nil, true
}

func (m *Model) remoteSessionIntent(kind uirail.ActionKind, row uiconnections.Row) (tea.Cmd, bool) {
	request := remoteRequest{host: row.Host, ref: row.Ref, name: row.Name}
	switch {
	case (kind == uirail.Focus || kind == uirail.Attach) && row.Status == status.Dead:
		// Its tmux session is gone, so an attach could only fail.
		if row.Terminal {
			m.reportErr("terminal " + row.Name + " has exited on " + row.Host + " - x closes it")
		} else {
			m.reportErr(row.Name + " is dead on " + row.Host + " - press v to revive it")
		}
		return nil, true
	case kind == uirail.Focus || kind == uirail.Attach:
		return m.attachRemote(row), true
	case row.Terminal && (kind == uirail.Revive || kind == uirail.Archive || kind == uirail.Restore):
		m.reportErr("a terminal on " + row.Host + " is not archived or revived - x closes it")
		return nil, true
	case kind == uirail.Kill && row.Terminal:
		request.op = remoteCloseTerminal
	case kind == uirail.Kill:
		request.op, request.dead = remoteKill, row.Status == status.Dead
	case kind == uirail.Revive:
		request.op = remoteRevive
	case kind == uirail.Archive || kind == uirail.Restore:
		// Each key is a no-op in the view where its rows are already
		// where it would put them.
		if (kind == uirail.Restore) != row.Archived {
			return nil, true
		}
		request.op = remoteArchive
		if row.Archived {
			request.op = remoteRestore
		}
	default:
		return nil, false
	}
	return m.confirmRemote(request), true
}

// remoteConfirm is the act and question a remote lifecycle call asks, the
// way the same call on a local row does.
var remoteConfirm = map[remoteOp]struct {
	action            uidialogs.ConfirmAction
	verb, consequence string
}{
	remoteKill:          {actionKill, "kill", " frees its RAM there, v revives it."},
	remoteCloseTerminal: {actionKill, "close terminal", ""},
	remoteRevive:        {actionRevive, "revive", ""},
	remoteArchive:       {actionArchive, "archive", ""},
	remoteRestore:       {actionRestore, "restore", ""},
}

// confirmRemote asks before a lifecycle call goes to the host; an offline
// host refuses before the question.
func (m *Model) confirmRemote(request remoteRequest) tea.Cmd {
	if refusal, offline := m.ssh.Offline(request.host); offline {
		m.reportErr(refusal)
		return nil
	}
	ask := remoteConfirm[request.op]
	if request.dead {
		// A dead agent holds no RAM there to free.
		ask.consequence = ""
	}
	m.confirm.confirmTarget = confirmTarget{
		action: ask.action,
		label:  ask.verb + " " + escapeControlsInline(request.name) + " on " + request.host + "?" + ask.consequence,
		remote: &request,
	}
	m.mode = modeConfirmDelete
	return nil
}

// runRemote is the confirmed remote call.
func (m *Model) runRemote(request remoteRequest) tea.Cmd { return m.queueRemote(request) }

// queueRemote puts a call on the remote lane, unless the host's last
// refresh could not reach it.
func (m *Model) queueRemote(request remoteRequest) tea.Cmd {
	if refusal, offline := m.ssh.Offline(request.host); offline {
		m.reportErr(refusal)
		return nil
	}
	if refusal, repeated := m.remoteRepeat(request); repeated {
		m.reportErr(refusal)
		return nil
	}
	m.clearErr()
	m.enqueueEffect(request, 0, false)
	return m.nextEffectCmd()
}

// remoteRepeat refuses a second enter on a composer whose prompt or spawn
// is still on its way to the host, as the local lane refuses one.
func (m *Model) remoteRepeat(request remoteRequest) (string, bool) {
	refusal, guarded := map[remoteOp]string{
		remoteType:  "this prompt is already being sent",
		remoteSpawn: "this spawn is already in progress",
	}[request.op]
	if !guarded {
		return "", false
	}
	for _, job := range append([]*effectJob{m.effects.remote.active}, m.effects.remote.pending...) {
		if job == nil {
			continue
		}
		if pending, ok := job.request.(remoteRequest); ok && pending.op == request.op && pending.composerGen == request.composerGen {
			return refusal, true
		}
	}
	return "", false
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
func (m *Model) attachRemote(row uiconnections.Row) tea.Cmd {
	if refusal, offline := m.ssh.Offline(row.Host); offline {
		m.reportErr(refusal)
		return nil
	}
	client, ref := m.ssh.client, row.Ref
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

// remoteQuickFacts aims the quick bar's target row at a connection: a
// remote session it answers, or the connection or remote group it spawns
// into, with the worktree left to the host until picked.
func (m *Model) remoteQuickFacts(facts *uiquick.Facts) {
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.row(selection)
	if !ok {
		return
	}
	host := subtleStyle().Render(" on ") + lipgloss.NewStyle().Foreground(colorRemote()).Render(row.Host)
	if row.Kind == uiconnections.SessionRow {
		facts.Remote = lipgloss.NewStyle().Foreground(colorBright()).Bold(true).Render(escapeControlsInline(row.Name)) + host
		return
	}
	facts.Remote = lipgloss.NewStyle().Foreground(colorAccent2()).Render(escapeControlsInline(displayGroup(row.Group))) + host
	facts.RemoteSpawn = true
	facts.WorktreeKnown, facts.WorktreeCapable = true, true
	facts.WorktreeOn = m.quick.WorktreeTouched() && m.quick.Worktree()
	facts.WorktreeInherit = !m.quick.WorktreeTouched()
}

// remoteQuickTarget is the connection row the quick bar is pointed at.
func (m *Model) remoteQuickTarget() (uiconnections.Row, bool) {
	selection, _ := m.rail.Selected()
	return m.ssh.row(selection)
}

// remoteQuickRow is the connection row a send or spawn the quick bar
// accepted goes to. taken says either end of the bar's aim is on a
// connection, and refused that the call cannot go, with the reason shown.
func (m *Model) remoteQuickRow() (row uiconnections.Row, taken, refused bool) {
	selection, _ := m.rail.Selected()
	if refusal, lost := m.quickAimLost(selection); lost {
		m.reportErr(refusal)
		return uiconnections.Row{}, true, true
	}
	row, ok := m.ssh.row(selection)
	if !ok {
		return uiconnections.Row{}, false, false
	}
	if len(m.quick.attachments) > 0 {
		m.reportErr(uirail.RemoteRefusal("pasted images"))
		return row, true, true
	}
	return row, true, false
}

// remoteQuickSend types the prompt into a remote agent's pane the way a
// send does into a local one.
func (m *Model) remoteQuickSend(text string) (tea.Cmd, bool) {
	row, taken, refused := m.remoteQuickRow()
	if !taken || refused {
		return nil, taken
	}
	if row.Terminal {
		m.reportErr(shellPromptHint(row.Name))
		return nil, true
	}
	request := m.quickRemoteRequest(row.Host, text)
	request.op, request.ref, request.name = remoteType, row.Ref, row.Name
	return m.queueRemote(request), true
}

// remoteQuickSpawn starts the bar's spawn in a remote group, with the
// tool, model, effort and profile it shows.
func (m *Model) remoteQuickSpawn(spawned spawn.Request) (tea.Cmd, bool) {
	row, taken, refused := m.remoteQuickRow()
	if !taken || refused {
		return nil, taken
	}
	if strings.HasPrefix(spawned.Prompt, "-") {
		m.reportErr(`prompt cannot start with "-": the tool would read it as a flag`)
		return nil, true
	}
	group := row.Group
	request := m.quickRemoteRequest(row.Host, spawned.Prompt)
	request.op = remoteSpawn
	request.spawn = sessioncmd.CreateSessionOptions{Tool: spawned.Tool, Group: &group, Prompt: spawned.Prompt}
	withRemoteChoice(&request.spawn, config.Choice(spawned.Pick))
	if spawned.WorktreeSet {
		worktree := spawned.Worktree
		request.spawn.Worktree = &worktree
	}
	return m.queueRemote(request), true
}

func (m *Model) quickRemoteRequest(host, text string) remoteRequest {
	q := &m.quick
	return remoteRequest{host: host, text: text, draft: q.input.Value(), composerGen: q.gen, closeAfterSend: q.closeAfterSend}
}

// withRemoteChoice passes the choice as the host's spawn takes it: the
// model by its key, which names the provider where the CLI picks per
// provider.
func withRemoteChoice(opts *sessioncmd.CreateSessionOptions, choice config.Choice) {
	if choice.Model != "" {
		opts.Model = catalog.Model{ID: choice.Model, Provider: choice.Provider}.Key()
	}
	opts.Effort, opts.Profile = choice.Effort, choice.Profile
}

// quickAimLost refuses an enter whose target a refresh moved when either
// end is on a connection: the remote row the bar was aimed at is gone, or
// the cursor slid to another row without the user moving it.
func (m *Model) quickAimLost(selection uirail.Selection) (string, bool) {
	aim := m.quick.aim
	if !aim.Remote() && !selection.Remote() {
		return "", false
	}
	if _, ok := m.ssh.row(aim); aim.Remote() && !ok {
		return "the agent this prompt was aimed at is gone from " + aim.Host + "; nothing was sent", true
	}
	if !sameRow(aim, selection) {
		return "the selection moved since this prompt was aimed; nothing was sent", true
	}
	return "", false
}

// sameRow compares rows by identity; a session that changed group is
// still the same session.
func sameRow(a, b uirail.Selection) bool {
	if a.Kind == uirail.SessionRow && b.Kind == uirail.SessionRow {
		return a.Host == b.Host && a.SessionID == b.SessionID
	}
	return a == b
}

// remoteTarget is the connection and group a spawn from the selected row
// lands in, when that row is on a connection.
func (m *Model) remoteTarget() (formRemote, bool) {
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.row(selection)
	if !ok {
		return formRemote{}, false
	}
	return formRemote{host: row.Host, group: row.Group}, true
}

// remoteFormSpawn turns the New Session form's spawn into the remote
// call: the tool, name, directory, prompt, choice and an explicit worktree
// pick go to the host, which resolves the rest.
func (m *Model) remoteFormSpawn(req spawnRequest) tea.Cmd {
	if len(req.images) > 0 {
		m.reportErr(uirail.RemoteRefusal("pasted images"))
		return nil
	}
	target := m.form.remote
	opts := sessioncmd.CreateSessionOptions{
		Tool: req.toolName, Group: &target.group, Directory: strings.TrimSpace(req.rawDir), Prompt: req.prompt,
	}
	withRemoteChoice(&opts, req.choice)
	if !req.autoNamed {
		opts.Name = req.name
	}
	if !m.form.WorktreeAuto() {
		worktree := req.wantWorktree
		opts.Worktree = &worktree
	}
	return m.queueRemote(remoteRequest{op: remoteSpawn, host: target.host, spawn: opts, composerGen: req.composerGen, fromForm: true})
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
	d.TakePath("")
	d.SetDirPlaceholder("the group's directory on " + target.host)
	d.SeedWorktree(false)
}

// remoteFormRequest takes the form requests that differ on a connection:
// the group is fixed, nothing is probed locally, and the spawn goes to the
// host.
func (m *Model) remoteFormRequest(request spawn.FormRequest) (tea.Cmd, bool) {
	if !m.form.remote.on() {
		return nil, false
	}
	switch {
	case request.Group != 0, request.PickGroup, request.Probe:
		return nil, true
	case request.Toggle:
		m.form.SetWorktree(m.form.WorktreeAuto() || !m.form.Worktree())
		return nil, true
	case request.Spawn != nil:
		m.clearErr()
		return m.remoteFormSpawn(m.formSpawnRequest(*request.Spawn)), true
	}
	return nil, false
}
