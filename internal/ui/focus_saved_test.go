package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The saved prompt key opens the list over a focused session, a pick is
// pasted into the agent's prompt unsent, and the leave key still leaves.
func TestFocusSavedPromptPastesThePick(t *testing.T) {
	m := buildModel(t)
	savePrompt(t, m, "Role", "line one\nline two")
	createSession(t, m, "paster", t.TempDir(), "")
	m.selectSessionRow(t, "paster")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainEffects(t)
	if m.mode != modeFocus {
		t.Fatalf("enter did not focus, mode = %v", m.mode)
	}
	var pasted []string
	restore := pasteFocused
	pasteFocused = func(_ *tmux.Driver, _, text string) error {
		pasted = append(pasted, text)
		return nil
	}
	t.Cleanup(func() { pasteFocused = restore })

	_, load := m.handleKey(tea.KeyMsg{Type: tea.KeyF4})
	m.runBatch(t, load)
	m.prepareFrame()
	if !strings.Contains(ansi.Strip(m.View()), "Role") || !strings.Contains(ansi.Strip(m.viewFooter()), "paste") {
		t.Fatal("f4 did not show the list")
	}
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.runBatch(t, cmd)
	m.drainEffects(t)
	if len(pasted) != 1 || pasted[0] != "line one\nline two" || m.focus.saved.list.Open() || m.mode != modeFocus {
		t.Fatalf("pasted %q, list open %v, mode %v", pasted, m.focus.saved.list.Open(), m.mode)
	}

	_, load = m.handleKey(tea.KeyMsg{Type: tea.KeyF4})
	m.runBatch(t, load)
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if m.mode != modeList || m.focus.saved.list.Open() {
		t.Fatalf("the leave key with the list up left mode %v, open %v", m.mode, m.focus.saved.list.Open())
	}
}
