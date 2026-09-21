package ui

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/federation"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

type nativeFederation struct {
	client      *federation.Client
	hosts       []federation.HostSnapshot
	localName   string
	remoteNames map[string]bool
	refreshing  bool
	inputQueue  []remoteInput
	inputBusy   bool
	promptBusy  bool
}
type remoteInput struct {
	ref           federation.Ref
	text          string
	keys          []string
	paste         bool
	mouse         bool
	width, height int
}
type remoteInputDoneMsg struct{ err error }
type remotePaneMsg struct {
	id    string
	gen   uint64
	state federation.PaneState
	err   error
}
type nativeSnapshotMsg []federation.HostSnapshot
type nativeTickMsg struct{}
type nativeResultMsg struct{ err error }
type remotePromptResultMsg struct {
	text string
	err  error
}

func (m *Model) EnableFederation(configDir string) error {
	client, err := federation.Load(configDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	n := &nativeFederation{client: client, localName: "Mac", remoteNames: map[string]bool{}}
	for _, host := range client.Hosts() {
		if host.SSH == "" {
			n.localName = host.Name
		} else {
			n.remoteNames[host.Name] = true
		}
	}
	groups, err := m.store.Groups()
	if err != nil {
		return err
	}
	for _, group := range groups {
		for host := range n.remoteNames {
			if group.Name == host || strings.HasPrefix(group.Name, host+"/") {
				return fmt.Errorf("remote host %q conflicts with a local group; choose another host name in fleet.json", host)
			}
		}
	}
	m.federation = n
	return nil
}

func (m *Model) remoteRef(id string) (federation.Ref, bool) {
	if m.federation == nil {
		return federation.Ref{}, false
	}
	host, id, ok := strings.Cut(id, "::")
	return federation.Ref{Host: host, ID: id}, ok && m.federation.remoteNames[host]
}

func (m *Model) remoteGroup(group string) (string, string, bool) {
	if m.federation != nil {
		for host := range m.federation.remoteNames {
			if group == host {
				return host, "", true
			}
			if rest, ok := strings.CutPrefix(group, host+"/"); ok {
				return host, rest, true
			}
		}
	}
	return "", "", false
}

func (m *Model) federationRefresh() tea.Cmd {
	if m.federation == nil || m.federation.refreshing {
		return nil
	}
	m.federation.refreshing = true
	client := m.federation.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return nativeSnapshotMsg(client.Snapshot(ctx))
	}
}
func nativeTick() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return nativeTickMsg{} })
}

func (m *Model) mergeFederation(msg *refreshMsg) {
	if m.federation == nil {
		return
	}
	msg.sessions = append([]store.Session(nil), msg.sessions...)
	msg.groups = append([]string(nil), msg.groups...)
	msg.groupPaths = maps.Clone(msg.groupPaths)
	if msg.groupPaths == nil {
		msg.groupPaths = map[string]string{}
	}
	for _, host := range m.federation.hosts {
		if !m.federation.remoteNames[host.Name] {
			continue
		}
		msg.groups = append(msg.groups, host.Name)
		for _, group := range host.Groups {
			msg.groups = append(msg.groups, host.Name+"/"+group)
		}
		for _, row := range host.Rows {
			group := host.Name
			if row.Group != "" {
				group += "/" + row.Group
			}
			parent := ""
			if row.ParentID != "" {
				parent = host.Name + "::" + row.ParentID
			}
			tool := row.Tool
			if row.Terminal {
				tool = "terminal"
			}
			msg.sessions = append(msg.sessions, store.Session{ID: host.Name + "::" + row.Ref.ID, Name: row.Name, Tool: tool, Cwd: row.Directory, Group: group, ParentID: parent, Status: row.Status, Archived: row.Archived})
			if msg.groupPaths[group] == "" {
				msg.groupPaths[group] = row.Directory
			}
		}
		for group, path := range host.GroupPaths {
			key := host.Name
			if group != "" {
				key += "/" + group
			}
			msg.groupPaths[key] = path
		}
	}
}

func (m *Model) remotePreview(sess store.Session, ref federation.Ref, gen uint64) tea.Cmd {
	client := m.federation.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		state, err := client.CaptureState(ctx, ref, 0, 0)
		return remotePaneMsg{id: sess.ID, gen: gen, state: state, err: err}
	}
}

func (m *Model) remoteAttach(sess store.Session, ref federation.Ref) (tea.Model, tea.Cmd) {
	if err := m.remoteAvailable(ref.Host); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	cmd := m.federation.client.Attach(ref)
	if cmd == nil {
		m.errBar.text = "invalid remote session"
		return m, nil
	}
	return m, execTerminalProcess(cmd, func(err error) tea.Msg { return nativeResultMsg{err} })
}

func (m *Model) remoteSend(ref federation.Ref, text string) tea.Cmd {
	m.federation.promptBusy = true
	client := m.federation.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := client.Send(ctx, ref, text)
		return remotePromptResultMsg{text: text, err: err}
	}
}

func (m *Model) remoteSpawn(host, group, tool, name, dir, prompt string, worktree bool) tea.Cmd {
	client := m.federation.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, err := client.Create(ctx, host, sessioncmd.CreateSessionOptions{Tool: tool, Name: name, Directory: dir, Group: &group, Prompt: prompt, Worktree: &worktree})
		return nativeResultMsg{err}
	}
}

func (m *Model) remoteLifecycle(sessions []store.Session, action string) tea.Cmd {
	client := m.federation.client
	refs := make([]federation.Ref, 0, len(sessions))
	for _, sess := range sessions {
		if ref, ok := m.remoteRef(sess.ID); ok {
			refs = append(refs, ref)
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		for _, ref := range refs {
			op, restore := action, false
			if action == actionRestore {
				op, restore = "archive", true
			}
			if _, err := client.Lifecycle(ctx, ref, op, restore); err != nil {
				return nativeResultMsg{err}
			}
		}
		return nativeResultMsg{}
	}
}

func (m *Model) guardRemoteAction(action string) (bool, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return false, nil
	}
	host, _, remote := m.remoteGroup(m.contextGroup())
	if !remote {
		return false, nil
	}
	if err := m.remoteAvailable(host); err != nil {
		switch action {
		case keybind.Up, keybind.Down, keybind.Quit, keybind.Search, keybind.Filter, keybind.FoldAll, keybind.EmptyGroups, keybind.Settings, keybind.Help, keybind.Resize:
		default:
			m.errBar.text = err.Error()
			return true, nil
		}
	}
	switch action {
	case keybind.Open, keybind.Attach, keybind.StepIn, keybind.StepOut, keybind.Up, keybind.Down, keybind.Quit, keybind.Prompt, keybind.NewSession, keybind.NewGroup, keybind.Terminal, keybind.Review, keybind.Search, keybind.Filter, keybind.FoldAll, keybind.EmptyGroups, keybind.Archived, keybind.Settings, keybind.Resize, keybind.Help, keybind.Messages:
		return false, nil
	case keybind.Kill, keybind.Revive, keybind.Archive, keybind.Restore:
		if entry.isGroup {
			m.errBar.text = "Select a session to manage it on its host."
			return true, nil
		}
		op := map[string]string{keybind.Kill: actionKill, keybind.Revive: actionRevive, keybind.Archive: actionArchive, keybind.Restore: actionRestore}[action]
		m.confirm = confirmTarget{action: op, sessions: []store.Session{entry.sess}, label: fmt.Sprintf("%s %s on %s?", op, entry.sess.Name, strings.Split(entry.sess.ID, "::")[0])}
		m.mode = modeConfirmDelete
		return true, nil
	default:
		m.errBar.text = "This action is not yet supported for remote sessions."
		return true, nil
	}
}

func (m *Model) remoteTerminal(host, group string) tea.Cmd {
	client := m.federation.client
	caller, dir, nest := "", m.groupPaths[m.contextGroup()], false
	if sess, ok := m.selected(); ok {
		ref, _ := m.remoteRef(sess.ID)
		caller, dir, nest = ref.ID, sess.Cwd, true
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_, err := client.TerminalCreateFor(ctx, host, caller, sessioncmd.CreateTerminalOptions{Group: &group, Directory: dir, Nest: &nest})
		return nativeResultMsg{err}
	}
}

func (m *Model) remoteCreateGroup(host, group, path string) tea.Cmd {
	client := m.federation.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		args := []string{"create-group", group, "--json"}
		if path != "" {
			args = append(args, "--directory", path)
		}
		_, err := client.Command(ctx, host, args...)
		return nativeResultMsg{err}
	}
}

func (m *Model) remoteAvailable(host string) error {
	for _, snapshot := range m.federation.hosts {
		if snapshot.Name == host && snapshot.Reachability == federation.Unreachable {
			return fmt.Errorf("%s unreachable: %s", host, snapshot.Error)
		}
	}
	return nil
}

func (m *Model) remoteFocusKey(ref federation.Ref, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if err := m.remoteAvailable(ref.Host); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	if m.keys.Binding(keybind.Editor).Has(msg.String()) {
		m.errBar.text = "Remote editor is not configured."
		return m, nil
	}
	if m.keys.Binding(keybind.Review).Has(msg.String()) {
		m.clearSelection()
		cmd := m.openDiff()
		if m.mode == modeDiff {
			m.diff.refocus = true
		}
		return m, cmd
	}
	if msg.Type == tea.KeyLeft && !msg.Alt && m.arrowStep {
		if sess, ok := m.selected(); ok && m.caretAtInputStart(sess.ID, sess.Tool) {
			return m, m.leaveFocus()
		}
	}
	m.cursorOn = true
	if m.scrolledBack() {
		m.focusScroll = 0
		m.focusFetchInFlight = false
	}
	input := remoteInput{ref: ref}
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace || msg.Paste {
		input.text = string(msg.Runes)
		if msg.Type == tea.KeySpace {
			input.text = " "
		}
		input.paste = msg.Paste
		if msg.Alt {
			input.text = "\x1b" + input.text
		}
	} else if key, ok := focusNamedKeys[msg.Type]; ok {
		if msg.Alt {
			key = "M-" + key
		}
		input.keys = []string{key}
	} else {
		return m, nil
	}
	queue := m.federation.inputQueue
	if len(queue) > 0 && !input.paste && len(input.keys) == 0 && !queue[len(queue)-1].paste && !queue[len(queue)-1].mouse && queue[len(queue)-1].width == 0 && len(queue[len(queue)-1].keys) == 0 && queue[len(queue)-1].ref == ref {
		queue[len(queue)-1].text += input.text
		m.federation.inputQueue = queue
	} else {
		m.federation.inputQueue = append(queue, input)
	}
	return m, m.nextRemoteInput()
}

func (m *Model) nextRemoteInput() tea.Cmd {
	n := m.federation
	if n == nil || n.inputBusy || len(n.inputQueue) == 0 {
		return nil
	}
	input := n.inputQueue[0]
	n.inputQueue = n.inputQueue[1:]
	n.inputBusy = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var err error
		if input.width > 0 {
			err = n.client.Resize(ctx, input.ref, input.width, input.height)
		} else if input.mouse {
			err = n.client.SendMouse(ctx, input.ref, input.text)
		} else if len(input.keys) > 0 {
			err = n.client.SendKeys(ctx, input.ref, input.keys)
		} else {
			err = n.client.SendText(ctx, input.ref, input.text, input.paste)
		}
		return remoteInputDoneMsg{err}
	}
}

func (m *Model) queueRemoteResize(id string, state federation.PaneState) {
	ref, ok := m.remoteRef(id)
	if !ok {
		return
	}
	width, height := m.paneTargetSize()
	if m.fullFocus() {
		width, height = m.width, m.listBodyHeight()
	}
	if width <= 0 || height <= 0 {
		return
	}
	if m.pane.geom == nil {
		m.pane.geom = map[string][2]int{}
	}
	last, sized := m.pane.geom[id]
	if !sized {
		last = [2]int{state.Width, state.Height}
	}
	height = max(height, last[1])
	if last == [2]int{width, height} {
		return
	}
	m.pane.geom[id] = [2]int{width, height}
	m.federation.inputQueue = append(m.federation.inputQueue, remoteInput{ref: ref, width: width, height: height})
}

func (m *Model) remoteRegion(id string, ref federation.Ref, offset, rows int) tea.Cmd {
	client := m.federation.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		state, err := client.CaptureState(ctx, ref, offset, rows)
		preview := state.Output
		if offset == 0 {
			preview = bottomWindow(preview, rows, 0)
		}
		return focusScrollMsg{sessID: id, offset: offset, rows: rows, preview: preview, ok: err == nil}
	}
}

func (m *Model) acceptRemoteSnapshots(snapshots []federation.HostSnapshot) {
	// The caller may reuse its message slice in tests or adapters. Keep the
	// accepted health state independent so the next failure can compare with
	// the previous pass instead of mutating it in place.
	snapshots = append([]federation.HostSnapshot(nil), snapshots...)
	for i := range snapshots {
		if snapshots[i].Error == "" {
			snapshots[i].Reachability = federation.Healthy
			snapshots[i].LastSuccess = time.Now()
			continue
		}
		snapshots[i].Reachability = federation.Unreachable
		snapshots[i].Stale = true
		snapshots[i].ConsecutiveFailures = 1
		for _, old := range m.federation.hosts {
			if old.Name != snapshots[i].Name {
				continue
			}
			snapshots[i].Rows = append([]federation.Row(nil), old.Rows...)
			snapshots[i].Groups, snapshots[i].GroupPaths = old.Groups, old.GroupPaths
			snapshots[i].LastSuccess = old.LastSuccess
			snapshots[i].ConsecutiveFailures = old.ConsecutiveFailures + 1
			snapshots[i].Stale = true
			snapshots[i].Reachability = federation.Suspect
			if snapshots[i].ConsecutiveFailures >= 3 {
				snapshots[i].Reachability = federation.Unreachable
			}
		}
	}
	m.federation.hosts = snapshots
}

func (m *Model) arrangeHostRows(rows []treeRow) []treeRow {
	if m.federation == nil {
		return rows
	}
	local, remote := make([]treeRow, 0, len(rows)), make([]treeRow, 0)
	for _, row := range rows {
		group := row.group
		if !row.isGroup {
			group = row.sess.Group
		}
		if _, _, ok := m.remoteGroup(group); ok {
			remote = append(remote, row)
		} else {
			if !row.isRoot() && m.collapsed[""] && strings.TrimSpace(m.search) == "" && !m.statusFilter.active() && !m.showArchived {
				continue
			}
			if !row.isRoot() {
				row.depth++
			}
			local = append(local, row)
		}
	}
	return append(local, remote...)
}
