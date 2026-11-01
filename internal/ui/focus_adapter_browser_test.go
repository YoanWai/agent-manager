package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
)

func focusBrowserRow(t *testing.T) *Model {
	t.Helper()
	m := browserRowModel(t)
	m.push(newFocusSurface(m))
	m.focus.pane.Enter(uifocus.EnterContext{SessionID: "docs"})
	setFocusPaneID(m, "docs")
	return m
}

func TestFocusedBrowserRowDropsTypedKeys(t *testing.T) {
	m := focusBrowserRow(t)
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("x")},
		{Type: tea.KeyRunes, Runes: []rune("hello"), Paste: true},
		{Type: tea.KeyEnter},
		{Type: tea.KeyCtrlC},
	} {
		m.handleFocusKey(msg)
		if focusInputQueued(m) {
			t.Fatalf("%q reached the browser pane", msg.String())
		}
	}
	m.handleFocusMouse(tea.MouseMsg{X: 60, Y: 20, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if focusInputQueued(m) {
		t.Fatal("a click reached the browser pane")
	}
	if !m.focused() {
		t.Fatal("typing on a browser row should stay focused")
	}
	m.handleFocusKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if m.focused() {
		t.Fatal("the detach key should leave a focused browser row")
	}
}

// The card stands where the pane would, so nothing may spend a control client
// or a capture tick on a pane nobody sees.
func TestFocusingABrowserRowStartsNoWatch(t *testing.T) {
	fakeTerminalBrowser(t)
	m := buildModel(t)
	createSession(t, m, "coder", t.TempDir(), "")
	m.selectSessionRow(t, "coder")
	browser := openBrowserRow(t, m)
	m.focus.runtime.Watch = newFocusWatch(m.services.tmux, func(tea.Msg) {})
	t.Cleanup(m.focus.runtime.Watch.Close)

	m.watchSelection()
	if watching := m.focus.runtime.Watch.Watching(); watching != "" {
		t.Fatalf("a browser row at rest is watched: %q", watching)
	}
	if (previewCaptures{m}).CaptureWanted(browser) {
		t.Fatal("a browser row's pane should not be captured for the preview")
	}

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	for _, job := range m.effects.Main.Pending {
		if _, probe := job.Request.(focusruntime.ProbeRequest); probe {
			t.Fatal("focusing a browser row queued the pane probe")
		}
	}
	if active := m.effects.Main.Active; active != nil {
		if _, probe := active.Request.(focusruntime.ProbeRequest); probe {
			t.Fatal("focusing a browser row runs the pane probe")
		}
	}
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("enter should focus the browser row, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	if watching := m.focus.runtime.Watch.Watching(); watching != "" {
		t.Fatalf("a focused browser row is watched: %q", watching)
	}
}

func TestFocusedBrowserRowAttachesOnA(t *testing.T) {
	m := focusBrowserRow(t)
	m.handleFocusKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	if m.focused() {
		t.Fatal("A should leave focus for the attach")
	}
	jobs := m.effects.Main.Pending
	if m.effects.Main.Active != nil {
		jobs = append(jobs, m.effects.Main.Active)
	}
	for _, job := range jobs {
		if request, ok := job.Request.(uilifecycle.AttachRequest); ok && request.ID == "docs" {
			return
		}
	}
	t.Fatalf("A on a focused browser row queued no attach: %+v", jobs)
}

// The card on a dead browser row says the revive key brings it back, so
// that key works from focus too.
func TestFocusedDeadBrowserRowRevivesOnV(t *testing.T) {
	m := focusBrowserRow(t)
	m.workspace.sessions[1].Status = status.Dead
	m.rail = railModelFromRows([]treeRow{{sess: m.workspace.sessions[0]}, {sess: m.workspace.sessions[1]}}, 1)
	m.handleFocusKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	jobs := m.effects.Main.Pending
	if m.effects.Main.Active != nil {
		jobs = append(jobs, m.effects.Main.Active)
	}
	for _, job := range jobs {
		request, ok := job.Request.(uilifecycle.Request)
		if ok && request.Target.Action == uilifecycle.Revive && len(request.Target.Sessions) == 1 && request.Target.Sessions[0].ID == "docs" {
			return
		}
	}
	t.Fatalf("v on a focused dead browser row queued no revive: %+v", jobs)
}
