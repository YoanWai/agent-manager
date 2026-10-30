package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
)

func TestInputDoesNotWaitBehindTheMainLane(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"slow"}}}, 0, false)
	if m.nextEffectCmd() == nil || m.effects.Main.Active == nil {
		t.Fatal("main lane did not start")
	}
	m.dispatchInput(focusruntime.Request{Kind: focusruntime.Keys, Session: store.Session{ID: "s"}, Command: "x"})
	if m.nextEffectCmd() == nil {
		t.Fatal("input waited for the busy main lane")
	}
	if _, ok := m.effects.Input.Active.Request.(focusruntime.InputRequest); !ok {
		t.Fatalf("input lane active = %#v", m.effects.Input.Active)
	}
}

func TestKeysNeitherFenceListingsNorAskForAPoll(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.dispatchInput(focusruntime.Request{Kind: focusruntime.Keys, Session: store.Session{ID: "s"}, Command: "x"})
	m.nextEffectCmd()
	id := m.effects.Input.Active.ID
	m.handleEffectCompleted(uieffects.Completed{ID: id, Result: focusruntime.Result{}, FinishedAt: time.Now()})
	if !m.effects.latestObservation.IsZero() {
		t.Fatal("a keystroke fenced later listings")
	}
	m.dispatchInput(focusruntime.Request{Kind: focusruntime.Prompt, Session: store.Session{ID: "s"}, Text: "p"})
	m.nextEffectCmd()
	id = m.effects.Input.Active.ID
	m.handleEffectCompleted(uieffects.Completed{ID: id, Result: focusruntime.Result{}, FinishedAt: time.Now()})
	if m.effects.latestObservation.IsZero() {
		t.Fatal("a recorded prompt did not fence older listings")
	}
}

func TestSecondQuitAbandonsAStuckDrain(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"a"}}, {Kind: uirail.SaveCollapsed, Collapsed: []string{"b"}}}, 0, false)
	if _, command := m.requestQuit(); command == nil || m.effects.Main.Active == nil {
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
	if len(m.effects.Main.Pending) != 0 {
		t.Fatal("abandoned work stayed queued")
	}
	if !m.effects.Lifetime.Begin() {
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
	if !m.effects.Lifetime.Begin() {
		t.Fatal("lifetime refused work")
	}
	m.StopEffects()
	if got := m.AbandonedEffects(); !slices.Equal(got, []string{"rail change was still running"}) {
		t.Fatalf("abandoned = %q", got)
	}
}

// An input re-reads its session before it writes, but a restart that lands
// between that read and the paste would hand the text to the replacement
// agent. Input to the restarting pane waits for the restart, and input to
// any other pane goes ahead.
func TestInputToAPaneWaitsForTheJobRestartingIt(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.queueLifecycle(uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmRestart), Sessions: []store.Session{{ID: "s"}}}, false, "")
	m.nextEffectCmd()
	if m.effects.Main.Active == nil {
		t.Fatal("restart did not start")
	}
	m.dispatchInput(focusruntime.Request{Kind: focusruntime.Paste, Session: store.Session{ID: "s"}, Text: "late"})
	m.dispatchInput(focusruntime.Request{Kind: focusruntime.Keys, Session: store.Session{ID: "other"}, Command: "x"})
	m.nextEffectCmd()
	if id, _ := inputTarget(m.effects.Input.Active); id != "other" {
		t.Fatalf("input lane started %q, want the other pane's keys", id)
	}
	m.handleEffectCompleted(uieffects.Completed{ID: m.effects.Input.Active.ID, Result: focusruntime.Result{}, FinishedAt: time.Now()})
	if m.effects.Input.Active != nil {
		t.Fatal("the paste started under the restart of its own pane")
	}
	m.handleEffectCompleted(uieffects.Completed{ID: m.effects.Main.Active.ID, Result: uilifecycle.Result{}, FinishedAt: time.Now()})
	if id, _ := inputTarget(m.effects.Input.Active); id != "s" {
		t.Fatalf("input lane active = %#v, want the held paste once the restart ended", m.effects.Input.Active)
	}
}

func TestAKillWaitsForTheInputInFlightToItsPane(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.dispatchInput(focusruntime.Request{Kind: focusruntime.Paste, Session: store.Session{ID: "s"}, Text: "typed"})
	m.nextEffectCmd()
	m.queueLifecycle(uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmKill), Sessions: []store.Session{{ID: "s"}}}, false, "")
	m.nextEffectCmd()
	if m.effects.Main.Active != nil {
		t.Fatal("kill started while its pane's paste was in flight")
	}
	m.handleEffectCompleted(uieffects.Completed{ID: m.effects.Input.Active.ID, Result: focusruntime.Result{}, FinishedAt: time.Now()})
	if _, ok := m.effects.Main.Active.Request.(uilifecycle.Request); !ok {
		t.Fatalf("main lane active = %#v, want the kill once the paste landed", m.effects.Main.Active)
	}
}

// A quit that waits on accepted work says so, so a manager that does not
// close at once is not taken for a hang.
func TestQuitNamesTheWorkItWaitsFor(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.queueLifecycle(uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmArchive), Sessions: []store.Session{{ID: "s"}}}, false, "")
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
