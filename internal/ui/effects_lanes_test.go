package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

func TestInputDoesNotWaitBehindTheMainLane(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"slow"}}}, 0, false)
	if m.nextEffectCmd() == nil || m.effects.main.active == nil {
		t.Fatal("main lane did not start")
	}
	m.dispatchInput(inputRequest{kind: inputKeys, session: store.Session{ID: "s"}, command: "x"})
	if m.nextEffectCmd() == nil {
		t.Fatal("input waited for the busy main lane")
	}
	if _, ok := m.effects.input.active.request.(inputRequest); !ok {
		t.Fatalf("input lane active = %#v", m.effects.input.active)
	}
}

func TestKeysNeitherFenceListingsNorAskForAPoll(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.dispatchInput(inputRequest{kind: inputKeys, session: store.Session{ID: "s"}, command: "x"})
	m.nextEffectCmd()
	id := m.effects.input.active.id
	m.handleEffectCompleted(effectCompletedMsg{id: id, result: inputEffectResult{}, finishedAt: time.Now()})
	if !m.effects.latestObservation.IsZero() {
		t.Fatal("a keystroke fenced later listings")
	}
	m.dispatchInput(inputRequest{kind: inputPrompt, session: store.Session{ID: "s"}, text: "p"})
	m.nextEffectCmd()
	id = m.effects.input.active.id
	m.handleEffectCompleted(effectCompletedMsg{id: id, result: inputEffectResult{}, finishedAt: time.Now()})
	if m.effects.latestObservation.IsZero() {
		t.Fatal("a recorded prompt did not fence older listings")
	}
}

func TestSecondQuitAbandonsAStuckDrain(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"a"}}, {Kind: uirail.SaveCollapsed, Collapsed: []string{"b"}}}, 0, false)
	if _, command := m.requestQuit(); command == nil || m.effects.main.active == nil {
		t.Fatal("first quit did not start draining")
	}
	_, command := m.requestQuit()
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatal("second quit kept waiting on the drain")
	}
	want := []string{"rail change was still running", "rail change never started"}
	if got := m.AbandonedEffects(); !slices.Equal(got, want) {
		t.Fatalf("abandoned = %q, want %q", got, want)
	}
	if len(m.effects.main.pending) != 0 {
		t.Fatal("abandoned work stayed queued")
	}
	if !m.effects.lifetime.begin() {
		t.Fatal("lifetime closed early")
	}
	started := time.Now()
	m.StopEffects()
	if time.Since(started) > time.Second {
		t.Fatal("stop waited on work the user abandoned")
	}
}

func TestStopEffectsGivesUpOnStuckWork(t *testing.T) {
	m := buildModel(t)
	defer func(wait time.Duration) { effectStopWait = wait }(effectStopWait)
	effectStopWait = 10 * time.Millisecond
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"a"}}}, 0, false)
	m.nextEffectCmd()
	if !m.effects.lifetime.begin() {
		t.Fatal("lifetime refused work")
	}
	m.StopEffects()
	if got := m.AbandonedEffects(); !slices.Equal(got, []string{"rail change was still running"}) {
		t.Fatalf("abandoned = %q", got)
	}
}

func TestEffectLifetimeWaitIsBounded(t *testing.T) {
	lifetime := &effectLifetime{}
	lifetime.begin()
	if lifetime.closeAndWait(10 * time.Millisecond) {
		t.Fatal("reported running work as finished")
	}
}

// An input re-reads its session before it writes, but a restart that lands
// between that read and the paste would hand the text to the replacement
// agent. Input to the restarting pane waits for the restart, and input to
// any other pane goes ahead.
func TestInputToAPaneWaitsForTheJobRestartingIt(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.queueLifecycle(confirmTarget{action: actionRestart, sessions: []store.Session{{ID: "s"}}}, false, "")
	m.nextEffectCmd()
	if m.effects.main.active == nil {
		t.Fatal("restart did not start")
	}
	m.dispatchInput(inputRequest{kind: inputPaste, session: store.Session{ID: "s"}, text: "late"})
	m.dispatchInput(inputRequest{kind: inputKeys, session: store.Session{ID: "other"}, command: "x"})
	m.nextEffectCmd()
	if id, _ := inputTarget(m.effects.input.active); id != "other" {
		t.Fatalf("input lane started %q, want the other pane's keys", id)
	}
	m.handleEffectCompleted(effectCompletedMsg{id: m.effects.input.active.id, result: inputEffectResult{}, finishedAt: time.Now()})
	if m.effects.input.active != nil {
		t.Fatal("the paste started under the restart of its own pane")
	}
	m.handleEffectCompleted(effectCompletedMsg{id: m.effects.main.active.id, result: lifecycleEffectResult{}, finishedAt: time.Now()})
	if id, _ := inputTarget(m.effects.input.active); id != "s" {
		t.Fatalf("input lane active = %#v, want the held paste once the restart ended", m.effects.input.active)
	}
}

func TestAKillWaitsForTheInputInFlightToItsPane(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.dispatchInput(inputRequest{kind: inputPaste, session: store.Session{ID: "s"}, text: "typed"})
	m.nextEffectCmd()
	m.queueLifecycle(confirmTarget{action: actionKill, sessions: []store.Session{{ID: "s"}}}, false, "")
	m.nextEffectCmd()
	if m.effects.main.active != nil {
		t.Fatal("kill started while its pane's paste was in flight")
	}
	m.handleEffectCompleted(effectCompletedMsg{id: m.effects.input.active.id, result: inputEffectResult{}, finishedAt: time.Now()})
	if _, ok := m.effects.main.active.request.(lifecycleRequest); !ok {
		t.Fatalf("main lane active = %#v, want the kill once the paste landed", m.effects.main.active)
	}
}

func TestAnAfterTurnEndHoldsTheSessionAndItsTerminals(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.workspace.sessions = []store.Session{{ID: "agent"}, {ID: "shell", ParentID: "agent"}, {ID: "other"}}
	if got := m.heldSessions(afterTurnRequest{id: "agent"}); !slices.Equal(got, []string{"agent", "shell"}) {
		t.Fatalf("held = %v, want the agent and its terminal", got)
	}
	if got := m.heldSessions(afterTurnRequest{id: "agent", cancel: true}); got != nil {
		t.Fatalf("a cancelled end holds %v, want nothing", got)
	}
}

func TestEffectNameSaysWhichLifecycleActionRuns(t *testing.T) {
	t.Parallel()
	for action, want := range map[uidialogs.ConfirmAction]string{actionDelete: "session delete", actionArchive: "session archive", actionRestart: "session restart"} {
		if got := effectName(lifecycleRequest{target: confirmTarget{action: action}}); got != want {
			t.Errorf("effectName(%q) = %q, want %q", action, got, want)
		}
	}
}

// A quit that waits on accepted work says so, so a manager that does not
// close at once is not taken for a hang.
func TestQuitNamesTheWorkItWaitsFor(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.queueLifecycle(confirmTarget{action: actionArchive, sessions: []store.Session{{ID: "s"}}}, false, "")
	m.nextEffectCmd()
	m.requestQuit()
	if !strings.Contains(m.errBar.text, "finishing session archive") || !strings.Contains(m.errBar.text, "again") {
		t.Fatalf("status = %q, want the awaited archive and the second quit key named", m.errBar.text)
	}
	idle := buildModel(t)
	idle.requestQuit()
	if idle.errBar.text != "" {
		t.Fatalf("a quit with nothing pending reported %q", idle.errBar.text)
	}
}
