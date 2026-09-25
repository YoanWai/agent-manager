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
// size; the first refresh after startup must bring a wrong-width window to
// the preview panel so its captures fit without a terminal resize.
func TestFirstRefreshResizesExistingSessions(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "leftover", t.TempDir(), "")
	id := m.sessionRows()[0].ID

	// Drift the window as if an older manager had sized it to the terminal;
	// a fresh manager also starts with no geometry cached for it.
	if _, err := tmuxCmd("resize-window", "-t", "am_"+id, "-x", "191", "-y", "55").CombinedOutput(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}

	m.sessionsSized = false
	m.pane.geom = nil
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

// A revive that runs outside the manager, from the CLI or the MCP tool,
// puts the row on a window this process never sized. The geometry it
// cached for the window that died would otherwise read as already matching
// the box, leaving the replacement at whatever it was born with. The
// replacement is pinned to the exact box, taller or shorter, since a pane
// born a moment ago has no scrollback to keep.
func TestRelaunchedSessionIsRepinnedToThePreviewPanel(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "revived", dir, "")
	id := m.sessionRows()[0].ID
	m.applyCmd(t, m.refreshCmd())
	if w, _ := windowSize(t, id); w != m.previewPaneWidth() {
		t.Fatalf("before the relaunch, window width = %d, want %d", w, m.previewPaneWidth())
	}

	// What a revive with no manager to ask does: the sized window is gone
	// and the replacement carries tmux's own default, or a box some other
	// manager run recorded.
	wantW, wantH := m.previewPaneWidth(), m.previewPaneHeight()
	for _, born := range [][2]int{{0, 0}, {wantW + 40, wantH + 20}} {
		if err := m.tmux.Kill(id); err != nil {
			t.Fatalf("kill: %v", err)
		}
		if err := m.tmux.Create(id, dir, "", nil, born[0], born[1]); err != nil {
			t.Fatalf("create: %v", err)
		}
		if w, h := windowSize(t, id); w == wantW && h == wantH {
			t.Fatalf("relaunched window already at the box %dx%d, nothing to prove", w, h)
		}
		m.applyCmd(t, m.refreshCmd())
		if w, h := windowSize(t, id); w != wantW || h != wantH {
			t.Fatalf("born %dx%d: after the refresh, window = %dx%d, want %dx%d", born[0], born[1], w, h, wantW, wantH)
		}
	}
}

// The launch paths that run without a manager read the box out of the
// store, so the manager has to keep it current there.
func TestRefreshPublishesThePaneSize(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "sized", t.TempDir(), "")
	m.applyCmd(t, m.refreshCmd())
	width, height, err := m.store.PaneSize()
	if err != nil {
		t.Fatalf("pane size: %v", err)
	}
	if width != m.previewPaneWidth() || height != m.previewPaneHeight() {
		t.Fatalf("published %dx%d, want %dx%d", width, height, m.previewPaneWidth(), m.previewPaneHeight())
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	*m = *updated.(*Model)
	width, height, err = m.store.PaneSize()
	if err != nil {
		t.Fatalf("pane size after resize: %v", err)
	}
	if width != m.previewPaneWidth() || height != m.previewPaneHeight() {
		t.Fatalf("after a resize published %dx%d, want %dx%d", width, height, m.previewPaneWidth(), m.previewPaneHeight())
	}
}

// An agent that splits its own window takes room from the pane the preview
// draws, on whichever axis it split, and no manager action precedes it for
// the geometry cache to follow. A later refresh has to notice and pin that
// pane back to the box.
func TestRefreshRepinsAnAgentSplitPane(t *testing.T) {
	for _, split := range []struct {
		axis string
		flag string
	}{{"horizontal", "-h"}, {"vertical", "-v"}} {
		t.Run(split.axis, func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "split", t.TempDir(), "")
			id := m.sessionRows()[0].ID
			m.applyCmd(t, m.refreshCmd())

			if out, err := tmuxCmd("split-window", split.flag, "-t", "am_"+id, "--", "sh", "-c", "sleep 30").CombinedOutput(); err != nil {
				t.Fatalf("split-window: %v: %s", err, out)
			}
			if width, height := agentPaneSize(t, id); width == m.previewPaneWidth() && height == m.previewPaneHeight() {
				t.Fatalf("the split should have taken room from the agent pane, pane = %dx%d", width, height)
			}

			m.applyCmd(t, m.refreshCmd())

			width, height := agentPaneSize(t, id)
			if width != m.previewPaneWidth() || height != m.previewPaneHeight() {
				t.Fatalf("after refresh, agent pane = %dx%d, want the preview box %dx%d",
					width, height, m.previewPaneWidth(), m.previewPaneHeight())
			}
		})
	}
}

func agentPaneSize(t *testing.T, id string) (int, int) {
	t.Helper()
	out, err := tmuxCmd("list-panes", "-t", "am_"+id, "-f", "#{==:#{pane_index},0}", "-F", "#{pane_width} #{pane_height}").CombinedOutput()
	if err != nil {
		t.Fatalf("list-panes: %v: %s", err, out)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		t.Fatalf("pane size %q", out)
	}
	width, widthErr := strconv.Atoi(fields[0])
	height, heightErr := strconv.Atoi(fields[1])
	if widthErr != nil || heightErr != nil {
		t.Fatalf("pane size %q: %v %v", out, widthErr, heightErr)
	}
	return width, height
}

func TestStartupPreservesExistingPaneHeight(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "tall", t.TempDir(), "")
	id := m.sessionRows()[0].ID
	if _, err := tmuxCmd("resize-window", "-t", "am_"+id, "-x", "120", "-y", "80").CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	loaded := New(m.cfg, m.store, m.tmux, m.poller.engine, m.hooks, "dev")
	loaded.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	loaded.applyCmd(t, loaded.refreshCmd())
	if _, h := windowSize(t, id); h < 80 {
		t.Fatalf("reopen shrank an existing pane from 80 to %d rows", h)
	}
}
