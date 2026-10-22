package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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
	_, answer := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return answer
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
			if err := m.services.store.SetRemoteTerminalOpener("box", "t1", "cafe"); err != nil {
				t.Fatal(err)
			}
			m.rail.Focus(tc.target)
			_, cmd := m.runRailIntent(uirail.Intent{Kind: tc.kind, Target: tc.target})
			m.runRemoteCmd(t, m.answerRemote(t, fake, cmd, "y"))
			if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], tc.want) {
				t.Fatalf("calls = %q, want one %q", calls, tc.want)
			}
			opener, err := m.services.store.RemoteTerminalOpener("box", "t1")
			if closed := tc.target == boxSession("t1"); err != nil || (opener == "") != closed {
				t.Fatalf("opener of t1 after %s = %q, %v", tc.name, opener, err)
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
		uirail.CancelEnd: "cancel end", uirail.KillAll: "kill all",
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
	state := hostState(m, "box")
	state.OK, state.Failures, state.Attempted, state.Err = false, 1, time.Now(), "box: unreachable over SSH: connection refused"
	setHost(m, "box", state)
	_, cmd := m.runRailIntent(uirail.Intent{Kind: uirail.Kill, Target: boxSession("s1")})
	if cmd != nil || !m.remoteIdle() || len(fake.taken()) != 0 {
		t.Fatal("a call to an offline host went out")
	}
	if m.errBar.text != "box: unreachable over SSH: connection refused" {
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
	want := []string{"type", "--json", "--", "s1", "hello there"}
	if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
	if m.quick.input.Value() != "" || m.errBar.text != "sent to api on box" {
		t.Fatalf("draft %q status %q", m.quick.input.Value(), m.errBar.text)
	}
}

// A prompt that may sit in the remote pane leaves the bar, as it does on
// this machine; one the host refused before typing stays for another try.
func TestQuickBarClearsAPromptThatMayHaveReachedARemoteAgent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   int
		stderr string
		kept   bool
	}{
		{"the host saw the paste start", 3, "agent-manager: prompt may have reached api and was not sent again: tmux: exit status 1", false},
		{"ssh dropped after it started", 255, "Connection to box closed by remote host.", false},
		{"the host refused it", 1, "agent-manager: prompt was not sent to api: pane is busy", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSSH{}
			m := connectedModel(t, fake)
			m.rail.Focus(boxSession("s1"))
			m.openQuickMode()
			m.quick.input.SetValue("hello there")
			fake.mu.Lock()
			fake.stderr, fake.code = tc.stderr, tc.code
			fake.mu.Unlock()
			_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
			m.runRemoteCmd(t, cmd)
			if kept := m.quick.input.Value() == "hello there"; kept != tc.kept {
				t.Fatalf("draft %q kept %v, want %v; status %q", m.quick.input.Value(), kept, tc.kept, m.errBar.text)
			}
			if m.errBar.text == "" {
				t.Fatal("the failure was not reported")
			}
		})
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
	m.runQuickRequest(uiquick.Request{Toggle: true})
	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.runRemoteCmd(t, cmd)
	want := []string{"spawn", "--json", "--prompt=build it", "--tool=" + m.quick.Tool(), "--group=web", "--worktree=true"}
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
	if m.mode != modeForm || !m.form.remote.on() || m.form.groupPath() != "web" || m.form.Dir() != "" {
		t.Fatalf("form = mode %v remote %+v group %q dir %q", m.mode, m.form.remote, m.form.groupPath(), m.form.Dir())
	}
	if cmd := m.runFormRequest(spawn.FormRequest{Probe: true}, nil); cmd != nil {
		t.Fatal("the form probed a local directory for a remote spawn")
	}
	m.form.SetName("job")
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

func TestNewGroupFormCreatesOnTheConnection(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxGroup)
	if _, cmd := m.runRailIntent(uirail.Intent{Kind: uirail.NewGroup, Target: boxGroup}); cmd != nil {
		t.Fatal("opening the form queued work")
	}
	if m.mode != modeGroupForm || m.groupForm.remote != (formRemote{host: "box", group: "web"}) || m.groupForm.Dir() != "" {
		t.Fatalf("form = mode %v remote %+v dir %q", m.mode, m.groupForm.remote, m.groupForm.Dir())
	}
	m.groupForm.SetName("api")
	_, cmd := m.submitGroupForm()
	m.runRemoteCmd(t, cmd)
	calls := fake.taken()
	if len(calls) != 1 || !slices.Equal(calls[0], []string{"create-group", "--json", "--", "web/api"}) {
		t.Fatalf("calls = %q", calls)
	}
	if m.mode != modeList || !strings.Contains(m.errBar.text, "created group web/api on box") {
		t.Fatalf("mode %v status %q, want the form closed and the group reported", m.mode, m.errBar.text)
	}
	if groups, err := m.services.store.Groups(); err != nil || len(groups) != 0 {
		t.Fatalf("local groups = %+v, %v; a remote group was written locally", groups, err)
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
	// A dead agent holds no RAM there to free.
	m.runRailIntent(uirail.Intent{Kind: uirail.Kill, Target: boxSession("s2")})
	if m.confirm.label != "kill old on box?" {
		t.Fatalf("dead kill label %q", m.confirm.label)
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
	m.runQuickRequest(uiquick.Request{Move: 1})
	if selection, _ := m.rail.Selected(); selection.SessionID != "s2" {
		t.Fatalf("selection = %+v, want s2 below s1", selection)
	}
	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.runRemoteCmd(t, cmd)
	if calls := fake.taken(); len(calls) != 1 || calls[0][0] != "type" || !slices.Contains(calls[0], "s2") {
		t.Fatalf("calls = %q, want a prompt typed into s2", calls)
	}
}

// An answer to a remote agent is typed into its pane as the user, the way
// the local quick bar types one, so the same answer twice goes in twice.
func TestQuickBarTypesTheSameAnswerTwiceIntoARemoteAgent(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("s1"))
	m.openQuickMode()
	for range 2 {
		m.quick.input.SetValue("yes")
		_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
		m.runRemoteCmd(t, cmd)
		if m.errBar.text != "sent to api on box" {
			t.Fatalf("status = %q", m.errBar.text)
		}
	}
	var typed int
	for _, call := range fake.taken() {
		if slices.Equal(call, []string{"type", "--json", "--", "s1", "yes"}) {
			typed++
		}
	}
	if typed != 2 {
		t.Fatalf("calls = %q, want the answer typed twice", fake.taken())
	}
}

// A remote group takes the model, effort and profile keys the way a local
// one does, and the spawn carries what the bar shows.
func TestQuickBarChoiceReachesARemoteSpawn(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	answered(m, claudeLike, claudeAnswer)
	m.rail.Focus(boxGroup)
	m.openQuickMode()
	setQuickTool(m, slices.Index(quickTools(m), "claude"))
	m.quick.RestartChoice(m.spawnHost())
	ctrl := func(key tea.KeyType) { m.handleQuickKey(tea.KeyMsg{Type: key}) }
	ctrl(tea.KeyCtrlL)
	if m.quick.Picking() != uiquick.PickModel {
		t.Fatalf("ctrl+l on a remote group: picking %d, status %q", m.quick.Picking(), m.errBar.text)
	}
	if sheet := ansi.Strip(m.viewQuickBar(120, quickBarMaxRows)); !strings.Contains(sheet, "model for web on box") {
		t.Fatalf("the model list is not painted:\n%s", sheet)
	}
	for _, r := range "opu" {
		m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	ctrl(tea.KeyEnter)
	ctrl(tea.KeyCtrlX)
	ctrl(tea.KeyCtrlX)
	if target := strings.Split(ansi.Strip(m.viewQuickBar(120, quickBarMaxRows)), "\n")[0]; !strings.Contains(target, "claude · opus · medium") {
		t.Fatalf("target row: %s", target)
	}
	ctrl(tea.KeyCtrlY)
	if m.errBar.text != "profile: claude has none" {
		t.Fatalf("ctrl+y status = %q, want the local answer", m.errBar.text)
	}
	m.quick.input.SetValue("build it")
	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.runRemoteCmd(t, cmd)
	want := []string{"spawn", "--json", "--prompt=build it", "--tool=claude", "--model=opus", "--effort=medium", "--group=web"}
	if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

// The kept choice is this machine's: a remote spawn starts on the host's
// defaults, and what the user picks for the host is not kept here.
func TestRemoteSpawnLeavesThisMachinesChoiceAlone(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	answered(m, claudeLike, claudeAnswer)
	const kept = `{"model":"opus","effort":"high"}`
	m.settings.cache.applyValues([]settingValue{{Key: "choice.claude", Value: kept}})
	m.rail.Focus(boxGroup)
	m.openQuickMode()
	setQuickTool(m, slices.Index(quickTools(m), "claude"))
	if target := strings.Split(ansi.Strip(m.viewQuickBar(120, quickBarMaxRows)), "\n")[0]; strings.Contains(target, "opus") {
		t.Fatalf("a remote bar starts on this machine's choice: %s", target)
	}
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlX})
	if got := m.settings.cache.value("choice.claude"); got != kept {
		t.Fatalf("a pick for the host was kept as this machine's: %s", got)
	}
	m.quick.RestartChoice(m.spawnHost())
	m.quick.input.SetValue("build it")
	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.runRemoteCmd(t, cmd)
	want := []string{"spawn", "--json", "--prompt=build it", "--tool=claude", "--group=web"}
	if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
}

// Enter in the saved prompts list inserts the pick before anything is
// sent, as it does on a local target.
func TestQuickBarSavedPromptsTakeEnterOnARemoteTarget(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	savePrompt(t, m, "Role", "instruct")
	m.rail.Focus(boxSession("s1"))
	m.openQuickMode()
	_, load := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	m.runBatch(t, load)
	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || !m.remoteIdle() || len(fake.taken()) != 0 || m.errBar.text != "" {
		t.Fatalf("enter in the list sent %q, status %q", fake.taken(), m.errBar.text)
	}
	if got := m.quick.input.Value(); got != savedToken("Role")+" " {
		t.Fatalf("chip landed as %q", got)
	}
	_, cmd = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.runRemoteCmd(t, cmd)
	if calls := fake.taken(); len(calls) == 0 || !slices.Equal(calls[0], []string{"type", "--json", "--", "s1", "instruct"}) {
		t.Fatalf("calls = %q, want the saved prompt typed", calls)
	}
}

// A second enter while the first call is on its way is refused with the
// words the local lanes use, rather than reaching the host twice.
func TestRemoteSendAndSpawnRefuseASecondEnterInFlight(t *testing.T) {
	cases := []struct {
		name   string
		target uirail.Selection
		submit func(m *Model) tea.Cmd
		op     string
		want   string
	}{
		{"quick bar answer", boxSession("s1"), func(m *Model) tea.Cmd {
			_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
			return cmd
		}, "type", "this prompt is already being sent"},
		{"quick bar spawn", boxGroup, func(m *Model) tea.Cmd {
			_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
			return cmd
		}, "spawn", "this spawn is already in progress"},
		{"New Session form", boxGroup, func(m *Model) tea.Cmd {
			_, cmd := m.submitForm()
			return cmd
		}, "spawn", "this spawn is already in progress"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSSH{}
			m := connectedModel(t, fake)
			m.rail.Focus(tc.target)
			if tc.name == "New Session form" {
				m.openForm()
				m.form.SetName("job")
			} else {
				m.openQuickMode()
				m.quick.input.SetValue("go on")
			}
			first := tc.submit(m)
			if second := tc.submit(m); second != nil || m.errBar.text != tc.want {
				t.Fatalf("second enter: cmd %v, status %q, want %q", second != nil, m.errBar.text, tc.want)
			}
			m.runRemoteCmd(t, first)
			var calls int
			for _, call := range fake.taken() {
				if call[0] == tc.op {
					calls++
				}
			}
			if calls != 1 {
				t.Fatalf("calls = %q, want one %s", fake.taken(), tc.op)
			}
		})
	}
}

// A dead remote agent has no tmux session left to attach to, and the host
// archives and revives agents only, so these are refused here with what to
// press instead of failing on the host.
func TestRemoteActionsTheHostCannotTakeAreRefusedHere(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	cases := []struct {
		kind   uirail.ActionKind
		target string
		want   string
	}{
		{uirail.Attach, "s2", "old is dead on box - press v to revive it"},
		{uirail.Focus, "s2", "old is dead on box - press v to revive it"},
		{uirail.Archive, "t1", "a terminal on box is not archived or revived - x closes it"},
		{uirail.Revive, "t1", "a terminal on box is not archived or revived - x closes it"},
	}
	for _, tc := range cases {
		m.clearErr()
		_, cmd := m.runRailIntent(uirail.Intent{Kind: tc.kind, Target: boxSession(tc.target)})
		if cmd != nil || !m.remoteIdle() || m.errBar.text != tc.want {
			t.Fatalf("%v on %s: cmd %v, status %q", tc.kind, tc.target, cmd != nil, m.errBar.text)
		}
	}
	if calls := fake.taken(); len(calls) != 0 {
		t.Fatalf("calls = %q, want none", calls)
	}
}
