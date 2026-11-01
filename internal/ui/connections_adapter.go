package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/catalog"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
	row, ok := m.ssh.Row(intent.Target)
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
		return m.queueRemote(newTerminalCall(row.Host, group)), true
	}
	if row.Kind == uiconnections.SessionRow {
		call := uiconnections.SessionCall(row, intent.Kind, m.listKeyHint)
		if cmd, ok := m.remoteSessionIntent(row, call); ok {
			return cmd, true
		}
	}
	if action, refused := uiconnections.Refusals[intent.Kind]; refused {
		m.reportErr(uirail.RemoteRefusal(action))
	}
	return nil, true
}

// remoteSessionIntent runs the call a remote session row took: an attach, a
// lifecycle call its confirm asks about first, or a refusal it names.
func (m *Model) remoteSessionIntent(row uiconnections.Row, call uiconnections.Call) (tea.Cmd, bool) {
	if !call.Answered {
		return nil, false
	}
	if call.Refusal != "" {
		m.reportErr(call.Refusal)
		return nil, true
	}
	if call.Op == uiconnections.OpAttach {
		return m.attachRemote(row), true
	}
	if call.Op == uiconnections.OpNone {
		return nil, true
	}
	request := remoteCall(row.Host, row.Ref, row.Name)
	request.Op = remoteOp[call.Op]
	request.Dead = call.Dead
	return m.confirmRemote(request, call), true
}

// remoteOp is the call a remote row's lifecycle op makes.
var remoteOp = map[uiconnections.Op]uilink.Op{
	uiconnections.OpKill:          uilink.Kill,
	uiconnections.OpCloseTerminal: uilink.CloseTerminal,
	uiconnections.OpRevive:        uilink.Revive,
	uiconnections.OpArchive:       uilink.Archive,
	uiconnections.OpRestore:       uilink.Restore,
}

// confirmRemote asks before a lifecycle call goes to the host; an offline
// host refuses before the question.
func (m *Model) confirmRemote(request uilink.Call, call uiconnections.Call) tea.Cmd {
	if refusal, offline := m.ssh.Offline(request.Host); offline {
		m.reportErr(refusal)
		return nil
	}
	m.openConfirm(confirmTarget{
		prompt: uidialogs.Confirm{Action: call.Action, Label: call.Verb + " " + presentation.EscapeControlsInline(request.Name+" on "+request.Host+"?") + call.Consequence},
		remote: &request,
	})
	return nil
}

// queueRemote puts a call on the remote lane, unless the host's last
// refresh could not reach it.
func (m *Model) queueRemote(request uilink.Call) tea.Cmd {
	if refusal, offline := m.ssh.Offline(request.Host); offline {
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
func (m *Model) remoteRepeat(request uilink.Call) (string, bool) {
	refusal, guarded := map[uilink.Op]string{
		uilink.Type:  "this prompt is already being sent",
		uilink.Spawn: "this spawn is already in progress",
	}[request.Op]
	if !guarded {
		return "", false
	}
	for _, job := range append([]*uieffects.Job{m.effects.Remote.Active}, m.effects.Remote.Pending...) {
		if job == nil {
			continue
		}
		if pending, ok := job.Request.(uilink.Call); ok && pending.Op == request.Op && pending.ComposerGen == request.ComposerGen {
			return refusal, true
		}
	}
	return "", false
}

func (m *Model) confirmRemoveConnection(name string) {
	m.openConfirm(confirmTarget{
		prompt:     uidialogs.Confirm{Action: uidialogs.ConfirmRemoveConnection, Label: "remove connection " + name + "? Its sessions keep running on the host."},
		connection: name,
	})
}

// removeConnection is the confirmed removal; the connection's rows leave
// with it, and the host is never told.
func (m *Model) removeConnection(name string) {
	m.clearErr()
	m.enqueueEffect(uilink.SaveRequest{Op: uilink.SaveRemove, Name: name}, 0, false)
}

// attachRemote opens an SSH attach to the row's tmux session in this
// terminal; focus does the same, since a remote pane cannot be embedded.
func (m *Model) attachRemote(row uiconnections.Row) tea.Cmd {
	if refusal, offline := m.ssh.Offline(row.Host); offline {
		m.reportErr(refusal)
		return nil
	}
	return m.ssh.Attach(row)
}

// remoteQuickFacts aims the quick bar's target row at a connection: a
// remote session it answers, or the connection or remote group it spawns
// into, with the worktree left to the host until picked.
func (m *Model) remoteQuickFacts(facts *uiquick.Facts) {
	row, ok := m.remoteQuickTarget()
	if !ok {
		return
	}
	host := uitheme.SubtleStyle().Render(" on ") + lipgloss.NewStyle().Foreground(uitheme.ColorRemote()).Render(row.Host)
	if row.Kind == uiconnections.SessionRow {
		facts.Remote = lipgloss.NewStyle().Foreground(uitheme.ColorBright()).Bold(true).Render(presentation.EscapeControlsInline(row.Name)) + host
		return
	}
	facts.Remote = lipgloss.NewStyle().Foreground(uitheme.ColorAccent2()).Render(presentation.EscapeControlsInline(uiframe.DisplayGroup(row.Group))) + host
	facts.RemoteSpawn = true
	facts.WorktreeKnown, facts.WorktreeCapable = true, true
	q := m.quick()
	facts.WorktreeOn = q.WorktreeTouched() && q.Worktree()
	facts.WorktreeInherit = !q.WorktreeTouched()
}

// remoteQuickTarget is the connection row the quick bar is pointed at.
func (m *Model) remoteQuickTarget() (uiconnections.Row, bool) {
	return m.remoteRow()
}

// remoteQuickRow is the connection row a send or spawn the quick bar
// accepted goes to. taken says either end of the bar's aim is on a
// connection, and refused that the call cannot go, with the reason shown.
func (m *Model) remoteQuickRow() (row uiconnections.Row, taken, refused bool) {
	if refusal, lost := m.quickAimLost(); lost {
		m.reportErr(refusal)
		return uiconnections.Row{}, true, true
	}
	row, ok := m.remoteRow()
	if !ok {
		return uiconnections.Row{}, false, false
	}
	if len(m.quick().Attachments) > 0 {
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
		m.reportErr(uisession.ShellPromptHint(row.Name))
		return nil, true
	}
	request := m.quickRemoteRequest(row.Host, text)
	request.Op, request.Ref, request.Name = uilink.Type, row.Ref, row.Name
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
		m.reportErr(`prompt cannot start with "-": the CLI would read it as a flag`)
		return nil, true
	}
	group := row.Group
	request := m.quickRemoteRequest(row.Host, spawned.Prompt)
	request.Op = uilink.Spawn
	request.Spawn = sessioncmd.CreateSessionOptions{Tool: spawned.Tool, Group: &group, Prompt: spawned.Prompt}
	withRemoteChoice(&request.Spawn, config.Choice(spawned.Pick))
	if spawned.WorktreeSet {
		worktree := spawned.Worktree
		request.Spawn.Worktree = &worktree
	}
	return m.queueRemote(request), true
}

func (m *Model) quickRemoteRequest(host, text string) uilink.Call {
	q := m.quick()
	return uilink.Call{Host: host, Text: text, Draft: q.Input.Value(), ComposerGen: q.Gen, CloseAfterSend: q.CloseAfterSend}
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

// quickAimLost refuses an enter whose target a refresh moved, when either
// end of the bar's aim is on a connection.
func (m *Model) quickAimLost() (string, bool) {
	selection, _ := m.rail.Selected()
	return m.ssh.AimLost(railSelection(m.quick().Aim), selection)
}

// remoteTarget is the connection and group a spawn from the selected row
// lands in, when that row is on a connection.
func (m *Model) remoteTarget() (formRemote, bool) {
	row, ok := m.remoteRow()
	if !ok {
		return formRemote{}, false
	}
	return formRemote{host: row.Host, group: row.Group}, true
}

// remoteFormSpawn turns the New Session form's spawn into the remote
// call: the tool, name, directory, prompt, choice and an explicit worktree
// pick go to the host, which resolves the rest.
func (m *Model) remoteFormSpawn(req uilaunch.Request) tea.Cmd {
	if len(req.Images) > 0 {
		m.reportErr(uirail.RemoteRefusal("pasted images"))
		return nil
	}
	target := m.form().Remote
	group := target.Group
	opts := sessioncmd.CreateSessionOptions{
		Tool: req.ToolName, Group: &group, Directory: strings.TrimSpace(req.RawDir), Prompt: req.Prompt,
	}
	withRemoteChoice(&opts, req.Choice)
	if !req.AutoNamed {
		opts.Name = req.Name
	}
	if !m.form().WorktreeAuto() {
		worktree := req.WantWorktree
		opts.Worktree = &worktree
	}
	return m.queueRemote(uilink.Call{Op: uilink.Spawn, Host: target.Host, Spawn: opts, ComposerGen: req.ComposerGen, FromForm: true})
}

// formRemote is the connection and group a New Session form spawns into,
// as the root's own connection rows name them.
type formRemote struct{ host, group string }

// remoteFormRequest takes the form requests that differ on a connection:
// the group is fixed, nothing is probed locally, and the spawn goes to the
// host.
func (m *Model) remoteFormRequest(request spawn.FormRequest) (tea.Cmd, bool) {
	f := m.form()
	if !f.Remote.On() {
		return nil, false
	}
	switch {
	case request.Group != 0, request.PickGroup, request.Probe:
		return nil, true
	case request.Toggle:
		f.SetWorktree(f.WorktreeAuto() || !f.Worktree())
		return nil, true
	case request.Spawn != nil:
		m.clearErr()
		return m.remoteFormSpawn(m.formSpawnRequest(*request.Spawn)), true
	}
	return nil, false
}

// remoteCall is one call to a connection's CLI, as the local sites build it:
// the host and the row it names, and no operation yet.
func remoteCall(host string, ref remote.Ref, name string) uilink.Call {
	return uilink.Call{Host: host, Ref: ref, Name: name}
}

// newTerminalCall opens a terminal in a connection's group.
func newTerminalCall(host, group string) uilink.Call {
	return uilink.Call{Op: uilink.NewTerminal, Host: host, Terminal: sessioncmd.CreateTerminalOptions{Group: &group}}
}
