package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/remote/remotetest"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
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
	if !confirmOpen(m) {
		return cmd
	}
	m.runRemoteCmd(t, cmd)
	if calls := fake.taken(); len(calls) != 0 {
		t.Fatalf("calls before the answer = %q", calls)
	}
	_, answer := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return answer
}

func (m *Model) remoteIdle() bool {
	return m.effects.Remote.Active == nil && len(m.effects.Remote.Pending) == 0
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
		ready, ok := cmd().(uilink.AttachReadyMsg)
		if !ok || ready.Err != nil || ready.Host != "box" {
			t.Fatalf("%v: msg %+v", kind, ready)
		}
		args := ready.Cmd.Args
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
	m.quick().Input.SetValue("hello there")
	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.runRemoteCmd(t, cmd)
	want := []string{"type", "--json", "--", "s1", "hello there"}
	if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], want) {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
	if m.quick().Input.Value() != "" || m.errBar.text != "sent to api on box" {
		t.Fatalf("draft %q status %q", m.quick().Input.Value(), m.errBar.text)
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
			m.quick().Input.SetValue("hello there")
			fake.mu.Lock()
			fake.stderr, fake.code = tc.stderr, tc.code
			fake.mu.Unlock()
			_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
			m.runRemoteCmd(t, cmd)
			if kept := m.quick().Input.Value() == "hello there"; kept != tc.kept {
				t.Fatalf("draft %q kept %v, want %v; status %q", m.quick().Input.Value(), kept, tc.kept, m.errBar.text)
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
	m.quick().Input.SetValue("ls")
	if _, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || !m.remoteIdle() || len(fake.taken()) != 0 {
		t.Fatal("a prompt to a remote shell went out")
	}
}

func TestQuickBarSpawnsInARemoteGroup(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxGroup)
	m.openQuickMode()
	m.quick().Input.SetValue("build it")
	m.runQuickRequest(uiquick.Request{Toggle: true})
	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.runRemoteCmd(t, cmd)
	want := []string{"spawn", "--json", "--prompt=build it", "--tool=" + m.quick().Tool(), "--group=web", "--worktree=true"}
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
	if !formOpen(m) || !m.form().Remote.On() || m.form().GroupPath() != "web" || m.form().Dir() != "" {
		t.Fatalf("form = mode %v remote %+v group %q dir %q", m.screenID(), m.form().Remote, m.form().GroupPath(), m.form().Dir())
	}
	if cmd := m.runFormRequest(spawn.FormRequest{Probe: true}, nil); cmd != nil {
		t.Fatal("the form probed a local directory for a remote spawn")
	}
	m.form().SetName("job")
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
	if !m.onList() {
		t.Fatalf("screen = %v, want the form closed", m.screenID())
	}
}

func TestNewGroupFormCreatesOnTheConnection(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxGroup)
	if _, cmd := m.runRailIntent(uirail.Intent{Kind: uirail.NewGroup, Target: boxGroup}); cmd != nil {
		t.Fatal("opening the form queued work")
	}
	if !m.groupFormOpen() || m.groupSurface().RemoteTarget() != (uidialogs.Remote{Host: "box", Group: "web"}) || m.groupSurface().Dir() != "" {
		t.Fatalf("form = mode %v remote %+v dir %q", m.screenID(), m.groupSurface().RemoteTarget(), m.groupSurface().Dir())
	}
	m.groupSurface().SetName("api")
	_, cmd := m.submitGroupForm()
	m.runRemoteCmd(t, cmd)
	calls := fake.taken()
	if len(calls) != 1 || !slices.Equal(calls[0], []string{"create-group", "--json", "--", "web/api"}) {
		t.Fatalf("calls = %q", calls)
	}
	if !m.onList() || !strings.Contains(m.errBar.text, "created group web/api on box") {
		t.Fatalf("mode %v status %q, want the form closed and the group reported", m.screenID(), m.errBar.text)
	}
	if groups, err := m.services.store.Groups(); err != nil || len(groups) != 0 {
		t.Fatalf("local groups = %+v, %v; a remote group was written locally", groups, err)
	}
}

func TestRemoteLifecycleAsksFirstAndNoSendsNothing(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	_, cmd := m.runRailIntent(uirail.Intent{Kind: uirail.Kill, Target: boxSession("s1")})
	if !confirmOpen(m) || pendingConfirm(m).label != "kill api on box? frees its RAM there, v revives it." {
		t.Fatalf("mode %v label %q, want the kill confirm", m.screenID(), pendingConfirm(m).label)
	}
	m.runRemoteCmd(t, m.answerRemote(t, fake, cmd, "n"))
	if calls := fake.taken(); len(calls) != 0 || !m.onList() {
		t.Fatalf("calls = %q, mode %v after n", calls, m.screenID())
	}
	// A dead agent holds no RAM there to free.
	m.runRailIntent(uirail.Intent{Kind: uirail.Kill, Target: boxSession("s2")})
	if pendingConfirm(m).label != "kill old on box?" {
		t.Fatalf("dead kill label %q", pendingConfirm(m).label)
	}
}

func TestQuickBarRefusesWhenARefreshDropsItsRemoteTarget(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("s1"))
	m.openQuickMode()
	m.quick().Input.SetValue("hello there")
	fake.mu.Lock()
	fake.snapshot.Sessions = fake.snapshot.Sessions[1:]
	fake.mu.Unlock()
	m.applyTestMsg(t, m.ssh.PollHost("box")())
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
	for _, lane := range m.effects.Lanes() {
		if lane.Active != nil || len(lane.Pending) != 0 {
			t.Fatal("enter queued an effect for the row the cursor slid to")
		}
	}
	if !strings.Contains(m.errBar.text, "gone from box") || m.quick().Input.Value() != "hello there" {
		t.Fatalf("status %q draft %q", m.errBar.text, m.quick().Input.Value())
	}
}

func TestQuickBarFollowsAMoveTheUserMakes(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("s1"))
	m.openQuickMode()
	m.quick().Input.SetValue("hello there")
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
		m.quick().Input.SetValue("yes")
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
	m.quick().RestartChoice(m.spawnHost())
	ctrl := func(key tea.KeyType) { m.handleQuickKey(tea.KeyMsg{Type: key}) }
	ctrl(tea.KeyCtrlL)
	if m.quick().Picking() != uiquick.PickModel {
		t.Fatalf("ctrl+l on a remote group: picking %d, status %q", m.quick().Picking(), m.errBar.text)
	}
	if sheet := ansi.Strip(m.viewQuickBar(120, uiquick.BarMaxRows)); !strings.Contains(sheet, "model for web on box") {
		t.Fatalf("the model list is not painted:\n%s", sheet)
	}
	for _, r := range "opu" {
		m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	ctrl(tea.KeyEnter)
	ctrl(tea.KeyCtrlX)
	ctrl(tea.KeyCtrlX)
	if target := strings.Split(ansi.Strip(m.viewQuickBar(120, uiquick.BarMaxRows)), "\n")[0]; !strings.Contains(target, "claude · opus · medium") {
		t.Fatalf("target row: %s", target)
	}
	ctrl(tea.KeyCtrlY)
	if m.errBar.text != "profile: claude has none" {
		t.Fatalf("ctrl+y status = %q, want the local answer", m.errBar.text)
	}
	m.quick().Input.SetValue("build it")
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
	m.settings.Cache.ApplyValues([]uisettings.Value{{Key: "choice.claude", Value: kept}})
	m.rail.Focus(boxGroup)
	m.openQuickMode()
	setQuickTool(m, slices.Index(quickTools(m), "claude"))
	if target := strings.Split(ansi.Strip(m.viewQuickBar(120, uiquick.BarMaxRows)), "\n")[0]; strings.Contains(target, "opus") {
		t.Fatalf("a remote bar starts on this machine's choice: %s", target)
	}
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlX})
	if got := m.settings.Cache.Value("choice.claude"); got != kept {
		t.Fatalf("a pick for the host was kept as this machine's: %s", got)
	}
	m.quick().RestartChoice(m.spawnHost())
	m.quick().Input.SetValue("build it")
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
	if got := m.quick().Input.Value(); got != composer.SavedToken("Role")+" " {
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
				m.form().SetName("job")
			} else {
				m.openQuickMode()
				m.quick().Input.SetValue("go on")
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

func TestClicksOnTheConnectionDialog(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 40
	m.ssh = uilink.New(testRemoteClient(t), nil)
	m.openConnectionDialog(remote.Connection{})
	for _, tc := range []struct {
		label string
		field int
	}{{"binary ", uiconnections.BinaryField}, {"ssh to ", uiconnections.DestinationField}, {"name ", uiconnections.NameField}} {
		x, y := bodyCell(t, m, tc.label)
		m = leftClick(m, x, y)
		if !m.connectionDialogOpen() || m.connectionSurface().Dialog().Focus() != tc.field {
			t.Fatalf("a click on %q: open %v", tc.label, m.connectionDialogOpen())
		}
	}
	x, y := paintedCell(t, m, "esc")
	if m = leftClick(m, x, y); m.connectionDialogOpen() {
		t.Fatal("a click on the esc hint left the card open")
	}
}

func (m *Model) connectionSurface() *uiconnections.Surface {
	s, ok := surfaceOf[*uiconnections.Surface](m)
	if !ok {
		panic("the connection card is not open")
	}
	return s
}

func (m *Model) connectionDialogOpen() bool {
	_, ok := surfaceOf[*uiconnections.Surface](m)
	return ok
}

func openConnectionModel(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 40
	m.ssh = uilink.New(testRemoteClient(t), nil)
	m.applyTestMsg(t, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	if !m.connectionDialogOpen() {
		t.Fatal("the new connection key did not open the card")
	}
	return m
}

// typeConnection types a name and a destination into the open dialog.
func typeConnection(m *Model, name, destination string) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(destination)})
}

// esc closes the card and the status line it leaves is clear.
func TestConnectionCardEscCloses(t *testing.T) {
	m := openConnectionModel(t)
	m.reportErr("stale")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.applyTestMsg(t, cmd())
	if m.connectionDialogOpen() || len(m.surfaces) != 0 {
		t.Fatalf("esc left %d surfaces", len(m.surfaces))
	}
	if m.errBar.text != "" {
		t.Fatalf("status = %q, want it cleared", m.errBar.text)
	}
}

// The card holds typed fields, so a stray click keeps the draft.
func TestConnectionCardKeepsItsDraft(t *testing.T) {
	m := openConnectionModel(t)
	if top, _ := m.top(); top.Dismissible() {
		t.Fatal("a card holding fields says a stray click closes it")
	}
	typeConnection(m, "box", "me@box")
	m = clickBackdrop(t, m)
	if !m.connectionDialogOpen() {
		t.Fatal("a click off the card closed it")
	}
}

// A save that completes after its card closed, or after another card
// replaced it, neither closes nor releases the card on screen.
func TestConnectionSaveCompletionIsFenced(t *testing.T) {
	m := openConnectionModel(t)
	stale := uilink.SaveRequest{Op: uilink.SaveAdd, Next: store.Connection{Name: "box", Destination: "me@box"}, Gen: m.connectionSurface().Gen()}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.applyConnectionSave(stale, nil, nil)
	if m.connectionDialogOpen() {
		t.Fatal("a completion reopened a closed card")
	}
	m.openConnectionDialog(remote.Connection{})
	m.applyConnectionSave(stale, nil, nil)
	if !m.connectionDialogOpen() {
		t.Fatal("a stale completion closed the card that replaced it")
	}
}

func TestNewConnectionKeyOpensTheDialogAndSavesToTheStore(t *testing.T) {
	m := buildModel(t)
	m.ssh = uilink.New(testRemoteClient(t), nil)
	m.applyTestMsg(t, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	if !m.connectionDialogOpen() {
		t.Fatal("C did not open the card")
	}
	typeConnection(m, "box", "me@box")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyTestMsg(t, cmd())
	m.drainEffects(t)
	stored, err := m.services.store.Connections()
	if err != nil || len(stored) != 1 || stored[0] != (store.Connection{Name: "box", Destination: "me@box"}) {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if !m.onList() || len(m.ssh.List()) != 1 {
		t.Fatalf("mode %v list %+v", m.screenID(), m.ssh.List())
	}
	if selection, _ := m.rail.Selected(); selection != (uirail.Selection{Kind: uirail.ConnectionRow, Host: "box"}) {
		t.Fatalf("selection = %+v, want the new connection", selection)
	}

	_, cmd = m.runRailIntent(uirail.Intent{Kind: uirail.Delete, Target: uirail.Selection{Kind: uirail.ConnectionRow, Host: "box"}})
	if cmd != nil || !confirmOpen(m) || pendingConfirm(m).label != "remove connection box? Its sessions keep running on the host." {
		t.Fatalf("delete: mode %v label %q", m.screenID(), pendingConfirm(m).label)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyTestMsg(t, cmd())
	m.drainEffects(t)
	if stored, _ := m.services.store.Connections(); len(stored) != 0 || len(m.ssh.List()) != 0 {
		t.Fatalf("stored %+v list %+v after removal", stored, m.ssh.List())
	}
}

func TestAFailedConnectionSaveTakesEnterAgain(t *testing.T) {
	m := buildModel(t)
	m.ssh = uilink.New(testRemoteClient(t), nil)
	if err := m.services.store.AddConnection(store.Connection{Name: "box", Destination: "me@elsewhere"}); err != nil {
		t.Fatal(err)
	}
	m.applyTestMsg(t, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	typeConnection(m, "box", "me@box")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyTestMsg(t, cmd())
	m.drainEffects(t)
	if !m.connectionDialogOpen() {
		t.Fatal("a refused save closed the card")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter after a refused save did nothing")
	}
}

// fakeSSH stands in for ssh: it records the agent-manager words each call
// carried and answers as the remote CLI would.
type fakeSSH struct {
	mu       sync.Mutex
	calls    [][]string
	snapshot sessioncmd.Snapshot
	screen   string
	// stderr and code fail every call the way a remote command does.
	stderr string
	code   int
}

type fakeExit int

func (e fakeExit) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e fakeExit) ExitCode() int { return int(e) }

func (f *fakeSSH) run(ctx context.Context, argv []string) ([]byte, []byte, error) {
	stdout, stderr, err := f.answer(ctx, argv)
	return remotetest.Answer(stdout), stderr, err
}

func (f *fakeSSH) answer(_ context.Context, argv []string) ([]byte, []byte, error) {
	all, _ := remotetest.Words(argv)
	words := all[1:]
	f.mu.Lock()
	f.calls = append(f.calls, words)
	snapshot, screen, stderr, code := f.snapshot, f.screen, f.stderr, f.code
	f.mu.Unlock()
	if code != 0 {
		return nil, []byte(stderr), fakeExit(code)
	}
	var reply any
	switch words[0] {
	case "snapshot":
		reply = snapshot
	case "serve":
		reply = map[string]bool{"started": false}
	case "send":
		reply = sessioncmd.SendResult{MessageID: 7, QueuePosition: 1, ManagerAwake: true}
	case "read":
		reply = sessioncmd.SessionScreen{Session: sessioncmd.Session{ID: words[len(words)-1]}, Output: screen}
	case "terminal":
		switch words[1] {
		case "close":
			return []byte("closed\n"), nil, nil
		case "read":
			reply = sessioncmd.TerminalScreen{Terminal: sessioncmd.Terminal{ID: words[len(words)-1]}, Output: screen}
		case "send":
			reply = sessioncmd.TerminalInput{TerminalID: words[len(words)-1], Sent: "command"}
		default:
			reply = sessioncmd.Terminal{ID: "t9", Name: "shell"}
		}
	default:
		reply = sessioncmd.Session{ID: "s9", Name: "fresh"}
	}
	out, err := json.Marshal(reply)
	return out, nil, err
}

func (f *fakeSSH) taken() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func boxSnapshot() sessioncmd.Snapshot {
	return sessioncmd.Snapshot{
		Version:      sessioncmd.SnapshotVersion,
		ManagerAwake: true,
		Groups:       []sessioncmd.Group{{Path: "web"}},
		Sessions: []sessioncmd.Session{
			{ID: "s1", Name: "api", Tool: "claude", Group: "web", Status: "idle", Running: true},
			{ID: "s2", Name: "old", Tool: "claude", Group: "web", Status: "dead"},
			{ID: "s3", Name: "shelved", Tool: "claude", Group: "web", Status: "dead", Archived: true},
		},
		Terminals: []sessioncmd.Terminal{{ID: "t1", Name: "sh", Group: "web", Status: "idle", Running: true}},
	}
}

// listedModel lists one connection, box, not refreshed yet, with every
// call going to fake.
func listedModel(t *testing.T, fake *fakeSSH) *Model {
	t.Helper()
	m := buildModel(t)
	fake.snapshot = boxSnapshot()
	m.ssh = uilink.New(testRemoteClient(t, remote.WithRunner(fake.run)), []store.Connection{{Name: "box", Destination: "me@box"}})
	m.rebuildRows()
	return m
}

// connectedModel is listedModel with box as its last refresh left it.
func connectedModel(t *testing.T, fake *fakeSSH) *Model {
	t.Helper()
	m := listedModel(t, fake)
	setHost(m, "box", uilink.Facts(remote.HostState{OK: true, Snapshot: fake.snapshot}))
	return m
}

// setHost lands state as host's latest refresh.
func setHost(m *Model, host string, state uiconnections.Host) {
	m.ssh.Refreshed(host, m.ssh.Gen(), state)
	m.rebuildRows()
}

func hostState(m *Model, host string) uiconnections.Host {
	state, _ := m.ssh.Host(host)
	return state
}

// testRemoteClient makes a client on a throwaway profile and removes the
// control directory the first call creates under /tmp.
func testRemoteClient(t *testing.T, opts ...remote.Option) *remote.Client {
	t.Helper()
	profile := t.TempDir()
	t.Cleanup(func() { os.RemoveAll(remote.ControlDir(profile)) })
	return remote.New(profile, opts...)
}

var (
	boxRow   = uirail.Selection{Kind: uirail.ConnectionRow, Host: "box"}
	boxGroup = uirail.Selection{Kind: uirail.GroupRow, Group: "web", Host: "box"}
)

func boxSession(id string) uirail.Selection {
	return uirail.Selection{Kind: uirail.SessionRow, SessionID: id, Group: "web", Host: "box"}
}

func railNames(m *Model) []string {
	var names []string
	for _, row := range m.rail.Rows() {
		names = append(names, row.Name)
	}
	return names
}

func TestPollRefreshesEachHostOnceAtATimeOffTheUpdatePath(t *testing.T) {
	fake := &fakeSSH{}
	m := listedModel(t, fake)

	cmd := m.ssh.PollHost("box")
	if cmd == nil {
		t.Fatal("no refresh started")
	}
	if len(fake.taken()) != 0 {
		t.Fatal("the refresh ran on the update path")
	}
	if again := m.ssh.PollHost("box"); again != nil {
		t.Fatal("a second refresh started while the first was in flight")
	}
	m.applyTestMsg(t, cmd())
	if calls := fake.taken(); len(calls) != 1 || calls[0][0] != "snapshot" {
		t.Fatalf("calls = %q, want one snapshot", calls)
	}
	if state := hostState(m, "box"); !state.OK {
		t.Fatalf("state %+v after the refresh landed", state)
	}
	if names := railNames(m); !slices.Contains(names, "api") || !slices.Contains(names, "box") {
		t.Fatalf("rows = %q, want box and its agents", names)
	}
}

func TestOfflineHostKeepsItsLastRows(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.applyTestMsg(t, m.ssh.PollHost("box")())
	fake.mu.Lock()
	fake.stderr, fake.code = "ssh: connect to host box port 22: Connection refused", 255
	fake.mu.Unlock()
	m.applyTestMsg(t, m.ssh.PollHost("box")())

	state := hostState(m, "box")
	if state.OK || state.Failures != 1 || !strings.Contains(state.Err, "Connection refused") {
		t.Fatalf("state = %+v", state)
	}
	if names := railNames(m); !slices.Contains(names, "api") || !slices.Contains(names, "sh") {
		t.Fatalf("rows = %q, want the last snapshot kept", names)
	}
	if host := m.ssh.RailHosts()[0]; host.State != uirail.HostOffline || host.Trouble != "unreachable" {
		t.Fatalf("rail host = %+v, want offline and unreachable", host)
	}
}

// A host ssh reaches says what is wrong with agent-manager there rather
// than reading as down.
func TestFailedHostNamesItsTrouble(t *testing.T) {
	for _, tc := range []struct {
		version int
		stderr  string
		code    int
		want    string
	}{
		{sessioncmd.SnapshotVersion, "sh: agent-manager: not found", 127, "agent-manager missing"},
		{sessioncmd.SnapshotVersion - 1, "", 0, "agent-manager too old"},
		{sessioncmd.SnapshotVersion + 1, "", 0, "agent-manager newer"},
		{sessioncmd.SnapshotVersion, "agent-manager: database is locked", 1, "error"},
	} {
		fake := &fakeSSH{}
		m := connectedModel(t, fake)
		fake.mu.Lock()
		fake.snapshot.Version, fake.stderr, fake.code = tc.version, tc.stderr, tc.code
		fake.mu.Unlock()
		m.applyTestMsg(t, m.ssh.PollHost("box")())
		if host := m.ssh.RailHosts()[0]; host.Trouble != tc.want {
			t.Errorf("version %d, %q: trouble %q, want %q", tc.version, tc.stderr, host.Trouble, tc.want)
		}
	}
}

// A host that failed its last refresh is not dialled again on the next
// poll; it waits out remote.RetryWait for its failure count first.
func TestUnreachableHostRestsBetweenRetries(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	fake.mu.Lock()
	fake.stderr, fake.code = "ssh: connect to host box port 22: Connection refused", 255
	fake.mu.Unlock()
	m.applyTestMsg(t, m.ssh.PollHost("box")())
	if m.ssh.PollHost("box") != nil {
		t.Fatal("a host that just failed was dialled again at once")
	}
	state := hostState(m, "box")
	state.Attempted = time.Now().Add(-remote.RetryWait(state.Failures))
	setHost(m, "box", state)
	cmd := m.ssh.PollHost("box")
	if cmd == nil {
		t.Fatal("a host past its wait was not dialled")
	}
	m.applyTestMsg(t, cmd())
	if got := hostState(m, "box").Failures; got != 2 {
		t.Fatalf("failures = %d, want 2", got)
	}
}

func TestRefreshOfAReplacedListIsDropped(t *testing.T) {
	fake := &fakeSSH{}
	m := listedModel(t, fake)
	stale := m.ssh.PollHost("box")
	m.ssh.SetList([]store.Connection{{Name: "box", Destination: "me@other"}})
	m.applyTestMsg(t, stale())
	if _, seen := m.ssh.Host("box"); seen {
		t.Fatal("a refresh of the old destination landed on the new one")
	}
}

func TestRemovedConnectionTakesItsRowsWithIt(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.ssh.SetList(nil)
	m.rebuildRows()
	if names := railNames(m); slices.Contains(names, "box") || slices.Contains(names, "api") {
		t.Fatalf("rows = %q after removal", names)
	}
}

func contentText(m *Model) string {
	var lines []string
	for _, line := range m.contentLines(80, 20) {
		lines = append(lines, ansi.Strip(line.text))
	}
	return strings.Join(lines, "\n")
}

func TestRemotePreviewReadsTheSelectedScreenOffTheUpdatePath(t *testing.T) {
	fake := &fakeSSH{screen: "\x1b[1mbuilding\x1b[0m api\n$ "}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("s1"))
	cmd := m.syncRemotePreview()
	if cmd == nil || len(fake.taken()) != 0 {
		t.Fatal("the read was not deferred to a command")
	}
	if text := contentText(m); !strings.Contains(text, "api  on box") || !strings.Contains(text, "reading the screen over SSH") {
		t.Fatalf("content before the read:\n%s", text)
	}
	m.applyTestMsg(t, cmd())
	if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], []string{"read", "--styled", "--json", "--", "s1"}) {
		t.Fatalf("calls = %q", calls)
	}
	if text := contentText(m); !strings.Contains(text, "building api") || strings.Contains(text, "\x1b") {
		t.Fatalf("content after the read:\n%s", text)
	}
}

func TestRemotePreviewReadsATerminalAsOne(t *testing.T) {
	fake := &fakeSSH{screen: "$ ls"}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("t1"))
	m.applyTestMsg(t, m.syncRemotePreview()())
	if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], []string{"terminal", "read", "--styled", "--json", "--", "t1"}) {
		t.Fatalf("calls = %q", calls)
	}
}

func TestRemotePreviewDropsAReadForAnEarlierSelection(t *testing.T) {
	fake := &fakeSSH{screen: "old screen"}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("s1"))
	stale := m.syncRemotePreview()
	m.rail.Focus(boxSession("s2"))
	m.syncRemotePreview()
	m.applyTestMsg(t, stale())
	if text := contentText(m); strings.Contains(text, "old screen") || !strings.Contains(text, "reading the screen over SSH") {
		t.Fatalf("content = %q, want the s1 read dropped while s2 reads", text)
	}
}

func TestConnectionRowShowsItsFacts(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxRow)
	text := contentText(m)
	for _, want := range []string{"box", "online", "me@box", "3 sessions · 1 terminal · manager awake"} {
		if !strings.Contains(text, want) {
			t.Fatalf("content lacks %q:\n%s", want, text)
		}
	}
}

// hostile is what a compromised host could put in any string it returns:
// an OSC 52 clipboard write, a carriage return, a backspace and a bell.
const hostile = "\x1b]52;c;cHduZWQ=\x07\r\b\x07"

// A connection's host is not trusted to drive the terminal: whatever it
// names, reports or prints reaches a frame as text.
func TestHostTextCannotDriveTheTerminal(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.layout.width, m.layout.height = 120, 40
	group := "web" + hostile
	fake.mu.Lock()
	fake.snapshot.Groups = []sessioncmd.Group{{Path: group}}
	fake.snapshot.Sessions = []sessioncmd.Session{{ID: "s1", Name: "api" + hostile, Tool: "claude" + hostile, Group: group, Status: "idle" + hostile}}
	fake.snapshot.Terminals = nil
	fake.screen = "build" + hostile + "ing\x1bc\x0e\x0fdone\n$ "
	raw := fake.snapshot
	fake.mu.Unlock()
	m.applyTestMsg(t, m.ssh.PollHost("box")())
	// internal/remote already cleans what a host answers; the UI escapes on
	// its own too, so this test hands it the host's raw rows.
	setHost(m, "box", uilink.Facts(remote.HostState{OK: true, Snapshot: raw}))

	frame := func(surface string) {
		t.Helper()
		view := preparedView(m)
		if stray := strayControl(view); stray != "" {
			t.Fatalf("%s leaks a control byte near %q", surface, stray)
		}
		if !strings.Contains(view, "52;c;cHduZWQ=") {
			t.Fatalf("%s: the host's text should be on screen to be a real test:\n%s", surface, ansi.Strip(view))
		}
	}
	session := uirail.Selection{Kind: uirail.SessionRow, SessionID: "s1", Group: group, Host: "box"}
	m.rail.Focus(session)
	m.applyTestMsg(t, m.syncRemotePreview()())
	if text := contentText(m); !strings.Contains(text, "building") || !strings.Contains(text, "done") {
		t.Fatalf("the screen lost its text:\n%s", text)
	}
	frame("the rail and the remote screen")

	m.rail.Focus(uirail.Selection{Kind: uirail.GroupRow, Group: group, Host: "box"})
	frame("a remote group")
	m.openForm()
	m.form().FocusField(m.spawnHost(), spawn.FieldGroup)
	frame("the New Session form")
	m.base = baseList
	m.clearSurfaces()

	m.rail.Focus(session)
	m.openQuickMode()
	frame("the quick bar")
	m.closeQuick()

	_, cmd := m.runRailIntent(uirail.Intent{Kind: uirail.Kill, Target: session})
	m.runRemoteCmd(t, cmd)
	if !confirmOpen(m) {
		t.Fatalf("screen = %v, want the kill confirm", m.screenID())
	}
	frame("the confirm")
	m.base = baseList
	m.clearSurfaces()

	fake.mu.Lock()
	fake.stderr, fake.code = "denied"+hostile, 1
	fake.mu.Unlock()
	m.ssh.Preview = uiconnections.Preview{}
	m.applyTestMsg(t, m.syncRemotePreview()())
	frame("a failed read")

	m.applyTestMsg(t, m.ssh.PollHost("box")())
	setHost(m, "box", uilink.Facts(remote.HostState{Snapshot: raw, Failures: 1, Err: errors.New("box: denied" + hostile)}))
	m.rail.Focus(boxRow)
	frame("an offline host")
}
