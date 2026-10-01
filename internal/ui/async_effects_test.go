package ui

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmDefersLifecycleAndKeepsCapturedTarget(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	alpha := railSelectedSession(m)
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !m.services.tmux.Exists(alpha.ID) {
		t.Fatal("confirmation performed lifecycle effects before command execution")
	}
	m.selectSessionRow(t, "beta")
	beta := railSelectedSession(m)
	m.confirm.sessions = []store.Session{beta}
	m.applyCmd(t, cmd)
	if m.services.tmux.Exists(alpha.ID) || !m.services.tmux.Exists(beta.ID) {
		t.Fatal("completion retargeted the later selection")
	}
}

func TestRailDefersPersistenceAndCopiesCollapsedPaths(t *testing.T) {
	m := buildModel(t)
	paths := []string{"alpha"}
	_, cmd := m.applyRailDecision(uirail.Decision{Mutations: []uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: paths}}})
	raw, err := m.services.store.Setting(collapsedSetting)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "" {
		t.Fatal("Rail persisted on the update path")
	}
	paths[0] = "beta"
	m.applyCmd(t, cmd)
	raw, err = m.services.store.Setting(collapsedSetting)
	if err != nil {
		t.Fatal(err)
	}
	var stored []string
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0] != "alpha" {
		t.Fatalf("stored=%v, want captured alpha", stored)
	}
}

func TestEffectQueueDrainsAcceptedWritesOnQuit(t *testing.T) {
	m := buildModel(t)
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"first"}}, {Kind: uirail.SaveCollapsed, Collapsed: []string{"last"}}}, 0, false)
	_, command := m.requestQuit()
	if _, ok := command().(tea.QuitMsg); ok {
		t.Fatal("quit discarded accepted writes")
	}
	m.drainEffects(t)
	raw, err := m.services.store.Setting(collapsedSetting)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "[\"last\"]" {
		t.Fatalf("FIFO final write=%s", raw)
	}
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"late"}}}, 0, false)
	if len(m.effects.pending) != 0 {
		t.Fatal("quit accepted a new user write")
	}
	if _, ok := m.nextEffectCmd()().(tea.QuitMsg); !ok {
		t.Fatal("drained queue did not quit")
	}
}

func TestEffectCommandExecutesOnceAndCompletionIsIdempotent(t *testing.T) {
	m := buildModel(t)
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"first"}}}, 0, false)
	command := m.nextEffectCmd()
	msg := command()
	if err := m.services.store.SetSetting(collapsedSetting, "external"); err != nil {
		t.Fatal(err)
	}
	command()
	raw, _ := m.services.store.Setting(collapsedSetting)
	if raw != "external" {
		t.Fatal("duplicate command wrote again")
	}
	m.applyTestMsg(t, msg)
	fence := m.effects.latestObservation
	m.applyTestMsg(t, msg)
	if m.effects.latestObservation != fence {
		t.Fatal("duplicate completion changed observation fence")
	}
}

func TestStopEffectsRefusesUnstartedCommand(t *testing.T) {
	m := buildModel(t)
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"late"}}}, 0, false)
	command := m.nextEffectCmd()
	m.StopEffects()
	msg := command().(effectCompletedMsg)
	if msg.err != errEffectsStopped {
		t.Fatalf("late command error=%v", msg.err)
	}
	raw, _ := m.services.store.Setting(collapsedSetting)
	if raw != "" {
		t.Fatal("late command touched the store")
	}
}

func TestDrainAdmitsCausalRailFollowup(t *testing.T) {
	m := buildModel(t)
	m.effects.quitting = true
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"followup"}}}, 7, true)
	m.drainEffects(t)
	raw, _ := m.services.store.Setting(collapsedSetting)
	if raw != "[\"followup\"]" {
		t.Fatalf("causal followup discarded: %s", raw)
	}
}

func TestSlowLifecycleDoesNotBlockWindowUpdateAndKeepsSnapshotWriter(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "slow", t.TempDir(), "")
	m.selectSessionRow(t, "slow")
	started := make(chan struct{})
	release := make(chan struct{})
	original := m.services.setSnapshot
	var signal sync.Once
	m.services.setSnapshot = func(id, pane string) error {
		signal.Do(func() { close(started) })
		<-release
		return original(id, pane)
	}
	m.archiveSelected()
	_, command := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.services.setSnapshot = func(string, string) error { return fmt.Errorf("replacement writer must not run") }
	completed := make(chan tea.Msg, 1)
	go func() { completed <- command() }()
	<-started
	updated := make(chan struct{})
	go func() { m.Update(tea.WindowSizeMsg{Width: 150, Height: 45}); close(updated) }()
	select {
	case <-updated:
	case <-time.After(time.Second):
		close(release)
		<-completed
		t.Fatal("window update waited on running lifecycle")
	}
	close(release)
	msg := <-completed
	m.applyTestMsg(t, msg)
	m.drainEffects(t)
	if m.errBar.text != "" {
		t.Fatalf("captured writer lost: %s", m.errBar.text)
	}
}

func TestLifecycleRequestCopiesNestedSessionValues(t *testing.T) {
	m := &Model{}
	sessions := []store.Session{{ID: "captured", PendingInputs: []string{"first"}, RelaunchSnapshot: map[string]int64{"conversation": 1}}}
	m.enqueueEffect(lifecycleRequest{target: confirmTarget{sessions: sessions}}, 0, false)
	sessions[0].PendingInputs[0] = "changed"
	sessions[0].RelaunchSnapshot["conversation"] = 2
	request := m.effects.pending[0].request.(lifecycleRequest)
	if request.target.sessions[0].PendingInputs[0] != "first" || request.target.sessions[0].RelaunchSnapshot["conversation"] != 1 {
		t.Fatal("request retained mutable session aliases")
	}
}

func TestCollapseSaveQueuesWhileMoveIsPending(t *testing.T) {
	m := buildModel(t)
	for _, sess := range []store.Session{{ID: "one", Name: "one", Tool: "claude", Cwd: "/tmp"}, {ID: "two", Name: "two", Tool: "claude", Cwd: "/tmp"}} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	m.queueRail([]uirail.Mutation{{Kind: uirail.SwapSession, SessionID: "one", TargetID: "two"}}, 0, false)
	_, command := m.applyRailDecision(uirail.Decision{Mutations: []uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"saved"}}}})
	m.applyCmd(t, command)
	raw, err := m.services.store.Setting(collapsedSetting)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "[\"saved\"]" {
		t.Fatalf("collapse save lost behind move: %s", raw)
	}
}

func TestMoveFollowupKeepsLaterCollapsePreference(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "mover", Name: "mover", Tool: "claude", Cwd: "/tmp"}
	if err := m.services.store.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.CreateGroup("alpha", ""); err != nil {
		t.Fatal(err)
	}
	m.rail.SetCollapsed("alpha", true)
	m.queueRail([]uirail.Mutation{{Kind: uirail.PlaceSession, SessionID: sess.ID, Group: "alpha"}}, 0, false)
	m.rail.SetCollapsed("beta", true)
	_, command := m.applyRailDecision(uirail.Decision{Mutations: []uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: m.rail.Collapsed()}}})
	m.applyCmd(t, command)
	raw, err := m.services.store.Setting(collapsedSetting)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "[\"beta\"]" {
		t.Fatalf("later collapse save undid completed move's expansion: %s", raw)
	}
}
