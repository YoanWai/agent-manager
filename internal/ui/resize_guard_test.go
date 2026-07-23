package ui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func windowSize(t *testing.T, id string) (int, int) {
	t.Helper()
	out, err := tmuxCmd("display-message", "-p", "-t", "am_"+id,
		"#{window_width} #{window_height}").CombinedOutput()
	if err != nil {
		t.Fatalf("display-message: %v: %s", err, out)
	}
	parts := strings.Fields(string(out))
	if len(parts) != 2 {
		t.Fatalf("unexpected size output %q", out)
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	return w, h
}

// Sessions left over from a previous manager run keep that run's window
// size; the first refresh after startup must shrink them to the preview
// panel so their captures fit without a terminal resize.
func TestFirstRefreshResizesExistingSessions(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "leftover", t.TempDir(), "")
	id := m.sessionRows()[0].ID

	// Drift the window as if an older manager had sized it to the terminal.
	if _, err := tmuxCmd("resize-window", "-t", "am_"+id, "-x", "191", "-y", "55").CombinedOutput(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}

	m.sessionsSized = false
	m.applyCmd(t, m.refreshCmd())
	if w, _ := windowSize(t, id); w != m.previewPaneWidth() {
		t.Fatalf("after first refresh, window width = %d, want %d", w, m.previewPaneWidth())
	}

	// Later refreshes leave sizes alone (attach keeps its own resync path).
	if _, err := tmuxCmd("resize-window", "-t", "am_"+id, "-x", "100", "-y", "30").CombinedOutput(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	if w, _ := windowSize(t, id); w != 100 {
		t.Fatalf("later refresh should not resize, window width = %d, want 100", w)
	}
}

// A WindowSizeMsg carrying the current size (as a tmux-attach resume does)
// skips the per-session tmux resize; a real size change still applies it.
func TestUnchangedWindowSizeSkipsResize(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "sized", t.TempDir(), "")
	id := m.sessionRows()[0].ID

	// Drift the session window away from the manager size behind its back.
	if _, err := tmuxCmd("resize-window", "-t", "am_"+id, "-x", "100", "-y", "30").CombinedOutput(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}

	// Same size as the model: the resume case, which must not touch sessions.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	*m = *updated.(*Model)
	if w, h := windowSize(t, id); w != 100 || h != 30 {
		t.Fatalf("unchanged size should skip resize, session is %dx%d, want 100x30", w, h)
	}

	// A real resize propagates the preview panel box to the session.
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	*m = *updated.(*Model)
	wantW, wantH := m.previewPaneWidth(), m.previewPaneHeight()
	if w, h := windowSize(t, id); w != wantW || h != wantH {
		t.Fatalf("changed size should resize session, got %dx%d, want %dx%d", w, h, wantW, wantH)
	}
}
