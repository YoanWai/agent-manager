package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

func TestInputDoesNotWaitBehindTheMainLane(t *testing.T) {
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
