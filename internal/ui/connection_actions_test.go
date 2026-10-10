package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

// runRemoteCmd runs what an action returned and every effect it queued.
func (m *Model) runRemoteCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd != nil {
		m.applyTestMsg(t, cmd())
	}
	m.drainEffects(t)
}

// answerRemote answers the confirm a remote lifecycle call opens, after
// checking that nothing reached the host while it was asked.
func (m *Model) answerRemote(t *testing.T, fake *fakeSSH, cmd tea.Cmd, key string) tea.Cmd {
	t.Helper()
	if m.mode != modeConfirmDelete {
		return cmd
	}
	m.runRemoteCmd(t, cmd)
	if calls := fake.taken(); len(calls) != 0 {
		t.Fatalf("calls before the answer = %q", calls)
	}
	return m.confirm.handleKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
}

func (m *Model) remoteIdle() bool {
	return m.effects.remote.active == nil && len(m.effects.remote.pending) == 0
}

func TestRemoteActionsBuildTheirRemoteCall(t *testing.T) {
	cases := []struct {
		name   string
		target uirail.Selection
		kind   uirail.ActionKind
		want   []string
		status string
	}{
		{"kill an agent", boxSession("s1"), uirail.Kill, []string{"kill", "--json", "--", "s1"}, "killed api on box"},
		{"kill a terminal closes it", boxSession("t1"), uirail.Kill, []string{"terminal", "close", "--", "t1"}, "killed sh on box"},
		{"revive", boxSession("s2"), uirail.Revive, []string{"revive", "--json", "--", "s2"}, "revived old on box"},
		{"archive", boxSession("s1"), uirail.Archive, []string{"archive", "--json", "--", "s1"}, "archived api on box"},
		{"restore", boxSession("s3"), uirail.Restore, []string{"archive", "--restore", "--json", "--", "s3"}, "restored shelved on box"},
		{"terminal on the connection", boxRow, uirail.NewTerminal, []string{"terminal", "create", "--json", "--group="}, "opened terminal shell on box"},
		{"terminal in a remote group", boxGroup, uirail.NewTerminal, []string{"terminal", "create", "--json", "--group=web"}, "opened terminal shell on box"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSSH{}
			m := connectedModel(t, fake)
			m.rail.Focus(tc.target)
			_, cmd := m.runRailIntent(uirail.Intent{Kind: tc.kind, Target: tc.target})
			m.runRemoteCmd(t, m.answerRemote(t, fake, cmd, "y"))
			if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], tc.want) {
				t.Fatalf("calls = %q, want one %q", calls, tc.want)
			}
			if m.errBar.text != tc.status {
				t.Fatalf("status = %q, want %q", m.errBar.text, tc.status)
			}
			if len(m.workspace.sessions) != 0 {
				t.Fatalf("a remote action reached the local list: %+v", m.workspace.sessions)
			}
		})
	}
}

func TestRemoteActionsAlreadyInPlaceDoNothing(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	for _, tc := range []struct {
		kind   uirail.ActionKind
		target uirail.Selection
	}{{uirail.Restore, boxSession("s1")}, {uirail.Archive, boxSession("s3")}} {
		if _, cmd := m.runRailIntent(uirail.Intent{Kind: tc.kind, Target: tc.target}); cmd != nil || !m.remoteIdle() {
			t.Fatalf("%v on %s queued work", tc.kind, tc.target.SessionID)
		}
	}
	if calls := fake.taken(); len(calls) != 0 {
		t.Fatalf("calls = %q, want none", calls)
	}
}

func TestUnsupportedRemoteActionsAreRefusedWithoutIO(t *testing.T) {
	refused := map[uirail.ActionKind]string{
		uirail.RenameAction: "rename", uirail.MoveToGroup: "move", uirail.Delete: "delete",
		uirail.Restart: "restart", uirail.Fork: "fork", uirail.OpenReview: "review",
		uirail.OpenEditor: "the editor", uirail.CopyReply: "copy reply", uirail.MarkIdle: "mark idle",
		uirail.CancelEnd: "cancel end", uirail.NewGroup: "new group", uirail.KillAll: "kill all",
		uirail.ReviveAll: "revive all",
	}
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	for kind, action := range refused {
		m.clearErr()
		_, cmd := m.runRailIntent(uirail.Intent{Kind: kind, Target: boxSession("s1")})
		if cmd != nil || !m.remoteIdle() {
			t.Fatalf("%s queued work", action)
		}
		if want := action + " isn't available on SSH connections yet"; m.errBar.text != want {
			t.Fatalf("status = %q, want %q", m.errBar.text, want)
		}
	}
	for _, target := range []uirail.Selection{boxRow, boxGroup} {
		m.clearErr()
		if _, cmd := m.runRailIntent(uirail.Intent{Kind: uirail.Kill, Target: target}); cmd != nil || m.errBar.text != "kill isn't available on SSH connections yet" {
			t.Fatalf("kill on %+v: status %q", target, m.errBar.text)
		}
	}
	if calls := fake.taken(); len(calls) != 0 {
		t.Fatalf("calls = %q, want none", calls)
	}
}

func TestRemoteCallReportsTheHostsError(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	fake.mu.Lock()
	fake.stderr, fake.code = "agent-manager: no session named s1\n", 1
	fake.mu.Unlock()
	_, cmd := m.runRailIntent(uirail.Intent{Kind: uirail.Kill, Target: boxSession("s1")})
	m.runRemoteCmd(t, m.answerRemote(t, fake, cmd, "y"))
	if m.errBar.text != "box: no session named s1" {
		t.Fatalf("status = %q", m.errBar.text)
	}
}

func TestOfflineHostRefusesCallsWithItsError(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	state := m.ssh.hosts["box"]
	state.OK, state.Err = false, fmt.Errorf("box: unreachable over SSH: connection refused")
	m.ssh.hosts["box"] = state
	_, cmd := m.runRailIntent(uirail.Intent{Kind: uirail.Kill, Target: boxSession("s1")})
	if cmd != nil || !m.remoteIdle() || len(fake.taken()) != 0 {
		t.Fatal("a call to an offline host went out")
	}
	if m.errBar.text != "box is offline: box: unreachable over SSH: connection refused" {
		t.Fatalf("status = %q", m.errBar.text)
	}
}

func TestRemoteAttachBuildsTheSSHCommandOffTheUpdatePath(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	for _, kind := range []uirail.ActionKind{uirail.Focus, uirail.Attach} {
		_, cmd := m.runRailIntent(uirail.Intent{Kind: kind, Target: boxSession("s1")})
		if cmd == nil {
			t.Fatalf("%v built nothing", kind)
		}
		ready, ok := cmd().(remoteAttachReadyMsg)
		if !ok || ready.err != nil || ready.host != "box" {
			t.Fatalf("%v: msg %+v", kind, ready)
		}
		args := ready.cmd.Args
		if args[0] != "ssh" || !slices.Contains(args, "-t") || !slices.Contains(args, "me@box") || !strings.Contains(args[len(args)-1], "am_s1") {
			t.Fatalf("%v: argv %q", kind, args)
		}
	}
	if calls := fake.taken(); len(calls) != 0 {
		t.Fatalf("attach ran calls %q", calls)
	}
}

func TestQuickBarSendsToARemoteAgent(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("s1"))
	m.openQuickMode()
	m.quick.input.SetValue("hello there")
	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.runRemoteCmd(t, cmd)
	want := []string{"send", "--from=" + m.ssh.from, "--json", "--", "s1", "hello there"}
	if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
	if m.quick.input.Value() != "" || m.errBar.text != "sent to api on box" {
		t.Fatalf("draft %q status %q", m.quick.input.Value(), m.errBar.text)
	}
}

func TestQuickBarRefusesAShellOnAConnection(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("t1"))
	m.openQuickMode()
	m.quick.input.SetValue("ls")
	if _, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || !m.remoteIdle() || len(fake.taken()) != 0 {
		t.Fatal("a prompt to a remote shell went out")
	}
}

func TestQuickBarSpawnsInARemoteGroup(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxGroup)
	m.openQuickMode()
	m.quick.input.SetValue("build it")
	m.runQuickRequest(quickRequest{toggle: true})
	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.runRemoteCmd(t, cmd)
	want := []string{"spawn", "--json", "--prompt=build it", "--tool=" + m.quick.tool(), "--group=web", "--worktree=true"}
	if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
	if m.errBar.text != "started fresh on box" {
		t.Fatalf("status = %q", m.errBar.text)
	}
}

func TestNewSessionFormSpawnsOnTheConnection(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxGroup)
	m.openForm()
	if m.mode != modeForm || !m.form.remote.on() || m.form.groupPath() != "web" || m.form.dir.Value() != "" {
		t.Fatalf("form = mode %v remote %+v group %q dir %q", m.mode, m.form.remote, m.form.groupPath(), m.form.dir.Value())
	}
	if cmd := m.runFormRequest(formRequest{probe: true}, nil); cmd != nil {
		t.Fatal("the form probed a local directory for a remote spawn")
	}
	m.form.name.SetValue("job")
	_, cmd := m.submitForm()
	m.runRemoteCmd(t, cmd)
	calls := fake.taken()
	if len(calls) != 1 || calls[0][0] != "spawn" || !slices.Contains(calls[0], "--name=job") || !slices.Contains(calls[0], "--group=web") {
		t.Fatalf("calls = %q", calls)
	}
	for _, word := range calls[0] {
		if strings.HasPrefix(word, "--worktree") || strings.HasPrefix(word, "--directory") {
			t.Fatalf("an untouched form sent %q; the host resolves it", word)
		}
	}
	if m.mode != modeList {
		t.Fatalf("mode = %v, want the form closed", m.mode)
	}
}

func TestRemoteLifecycleAsksFirstAndNoSendsNothing(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	_, cmd := m.runRailIntent(uirail.Intent{Kind: uirail.Kill, Target: boxSession("s1")})
	if m.mode != modeConfirmDelete || m.confirm.label != "kill api on box? frees its RAM there, v revives it." {
		t.Fatalf("mode %v label %q, want the kill confirm", m.mode, m.confirm.label)
	}
	m.runRemoteCmd(t, m.answerRemote(t, fake, cmd, "n"))
	if calls := fake.taken(); len(calls) != 0 || m.mode != modeList {
		t.Fatalf("calls = %q, mode %v after n", calls, m.mode)
	}
}

func TestQuickBarRefusesWhenARefreshDropsItsRemoteTarget(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("s1"))
	m.openQuickMode()
	m.quick.input.SetValue("hello there")
	fake.mu.Lock()
	fake.snapshot.Sessions = fake.snapshot.Sessions[1:]
	fake.mu.Unlock()
	m.applyTestMsg(t, m.pollConnection("box")())
	if selection, _ := m.rail.Selected(); selection.SessionID == "s1" {
		t.Fatal("the refresh should have moved the cursor off the dropped row")
	}

	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		m.runRemoteCmd(t, cmd)
	}
	if calls := fake.taken(); len(calls) != 1 || calls[0][0] != "snapshot" {
		t.Fatalf("calls = %q, want only the refresh", calls)
	}
	for _, lane := range m.lanes() {
		if lane.active != nil || len(lane.pending) != 0 {
			t.Fatal("enter queued an effect for the row the cursor slid to")
		}
	}
	if !strings.Contains(m.errBar.text, "gone from box") || m.quick.input.Value() != "hello there" {
		t.Fatalf("status %q draft %q", m.errBar.text, m.quick.input.Value())
	}
}

func TestQuickBarFollowsAMoveTheUserMakes(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("s1"))
	m.openQuickMode()
	m.quick.input.SetValue("hello there")
	m.runQuickRequest(quickRequest{move: 1})
	if selection, _ := m.rail.Selected(); selection.SessionID != "s2" {
		t.Fatalf("selection = %+v, want s2 below s1", selection)
	}
	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.runRemoteCmd(t, cmd)
	if calls := fake.taken(); len(calls) != 1 || calls[0][0] != "send" || !slices.Contains(calls[0], "s2") {
		t.Fatalf("calls = %q, want a send to s2", calls)
	}
}
