package ui

import (
	"strings"
	"testing"
	"time"

	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestFocusDropsTheLastSessionsCaret(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	setFocusPaneFacts(m, "another-session", false, false, false, 0, paneCursor{x: 0, y: 25, ok: true})
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if m.mode != modeFocus {
		t.Fatalf("test setup: focus alpha, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	m.workspace.preview = "one\ntwo" + strings.Repeat("\n", 38)
	rows := paneExact(m.workspace.preview, 10, 40, m.paneCaretRow())
	if len(rows) != 2 || rows[0] != "one" {
		t.Fatalf("focused pane = %q, want the painted rows", rows)
	}
}

func TestMouseBackLeavesFocus(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "mouse-back", t.TempDir(), "")
	m.selectSessionRow(t, "mouse-back")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	updated, _ = m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonBackward})
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("mouse back should leave focus, mode = %v", m.mode)
	}
}

func TestPaneBoxMatchesPaintedFrame(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "boxed", t.TempDir(), "")
	m.selectSessionRow(t, "boxed")
	marker := "PANE-BOX-MARKER"
	m.workspace.preview = marker + "\nsecond row\n"

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainEffects(t)
	frame := splitLines(preparedView(m))
	box := m.focus.pane.FrameBox()
	if !box.Valid {
		t.Fatal("pane box never recorded")
	}
	if box.Y >= len(frame) {
		t.Fatalf("pane box row %d past frame of %d rows", box.Y, len(frame))
	}
	row := []rune(ansi.Strip(frame[box.Y]))
	if box.X+len(marker) > len(row) {
		t.Fatalf("pane box x %d past row width %d", box.X, len(row))
	}
	if got := string(row[box.X : box.X+len(marker)]); got != marker {
		t.Fatalf("row %d at column %d = %q, want %q", box.Y, box.X, got, marker)
	}
}

// A triple click on a painted pane row copies that row and nothing from
// the rail beside it.
func TestTripleClickOnRealFrameTakesPaneRowOnly(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "railmate", t.TempDir(), "")
	m.selectSessionRow(t, "railmate")
	m.workspace.preview = "pane row one\npane row two\n"

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainEffects(t)
	preparedView(m)

	box := m.focus.pane.FrameBox()
	if !box.Valid {
		t.Fatal("pane box never recorded")
	}
	x, y := box.X+3, box.Y+1
	for i := 0; i < 3; i++ {
		m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	}
	result := m.focus.pane.Mouse(
		tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: x, Y: y},
		uifocus.MouseContext{SessionID: railSelectedSession(m).ID, Rows: m.focusPaneRows()},
	)
	got := ""
	if result.Copy != nil {
		got = result.Copy.Text
	}
	if got != "pane row two" {
		t.Fatalf("triple click copied %q, want the pane row alone", got)
	}
	if strings.Contains(got, "railmate") {
		t.Fatalf("selection leaked rail content: %q", got)
	}
}

// The clipboard writer runs off the update loop, so its confirmation can
// land after a click elsewhere has already dropped the highlight it counted.
// Re-arming the banner there would put "copied N chars" under no selection.
func TestLateCopyConfirmationIsDroppedAfterTheSelectionGoes(t *testing.T) {
	dragAlph := func(m *Model, id string) uifocus.CopyRequest {
		t.Helper()
		m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 10, Y: 5})
		m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 14, Y: 5})
		result := m.focus.pane.Mouse(
			tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 14, Y: 5},
			uifocus.MouseContext{SessionID: id},
		)
		if result.Copy == nil || result.Copy.Text != "alph" {
			t.Fatalf("test setup: drag copy = %+v, want alph", result.Copy)
		}
		return *result.Copy
	}

	m, id := focusedShotPane(t, 40, 10, 5, "alpha beta", "gamma delta")
	request := dragAlph(m, id)
	inFlight := focusCopiedMsg{chars: 4, gen: request.Generation}
	m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 12, Y: 6})
	m.Update(inFlight)
	if got := m.focus.pane.Status().CopiedChars; got != 0 {
		t.Fatalf("a write that landed after the click re-armed the count: %d", got)
	}

	// The same confirmation still counts while its own selection stands.
	m2, id2 := focusedShotPane(t, 40, 10, 5, "alpha beta", "gamma delta")
	request = dragAlph(m2, id2)
	m2.Update(focusCopiedMsg{chars: 4, gen: request.Generation})
	if got := m2.focus.pane.Status().CopiedChars; got != 4 {
		t.Fatalf("the write for the standing selection was dropped: %d", got)
	}
}

func TestPushedPreviewWinsOverStalePoll(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "typing", t.TempDir(), "")
	m.selectSessionRow(t, "typing")
	sess := railSelectedSession(m)

	m.focus.runtime.watch = newFocusWatch(m.services.tmux, func(tea.Msg) {})
	m.focus.runtime.watch.setFocus(sess.ID)
	t.Cleanup(m.focus.runtime.watch.Close)
	deadline := time.Now().Add(5 * time.Second)
	for !m.focus.runtime.watch.serving(sess.ID) {
		if time.Now().After(deadline) {
			t.Skip("control client never came up on this host")
		}
		time.Sleep(20 * time.Millisecond)
	}

	fresh := "typed-just-now"
	updated, _ := m.Update(focusPreviewMsg{sessID: sess.ID, preview: fresh})
	*m = *updated.(*Model)
	updated, _ = m.Update(previewMsg{sessID: sess.ID, preview: "stale-capture"})
	*m = *updated.(*Model)
	if m.workspace.preview != fresh {
		t.Fatalf("stale tick capture overwrote the pushed frame: %q", m.workspace.preview)
	}
	updated, _ = m.Update(refreshMsg{sessions: m.workspace.sessions, procFor: sess.ID, preview: "stale-poll"})
	*m = *updated.(*Model)
	if m.workspace.preview != fresh {
		t.Fatalf("stale poll capture overwrote the pushed frame: %q", m.workspace.preview)
	}
}

func TestPollPreviewResumesAfterWatcherStops(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "released", t.TempDir(), "")
	m.selectSessionRow(t, "released")
	sess := railSelectedSession(m)
	m.focus.runtime.watch = newFocusWatch(m.services.tmux, func(tea.Msg) {})
	m.focus.runtime.watch.setFocus(sess.ID)
	m.focus.runtime.watch.Close()

	updated, _ := m.Update(previewMsg{sessID: sess.ID, preview: "polled"})
	*m = *updated.(*Model)
	if m.workspace.preview != "polled" {
		t.Fatalf("preview after watcher stop = %q, want the polled capture", m.workspace.preview)
	}
}

func TestBottomParkedCaretSurvivesControlCapture(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "ccpark", t.TempDir(), "")
	m.selectSessionRow(t, "ccpark")
	setRailSessionTool(m, railSelectedSession(m).ID, "command-code")
	rows := make([]string, 47)
	rows[36] = " TODOS  [4 items · 2 done] Sending Tier 3 messages… (paused) [ctrl+x to expand]"
	rows[38] = strings.Repeat("─", 60)
	rows[39] = "❯ Ask your question..."
	rows[40] = strings.Repeat("─", 60)
	rows[41] = "  » permission bypass on [shift+tab]"
	rows[42] = "  ? for shortcuts · PR #390 · taste on"
	m.mode = modeFocus
	m.workspace.preview = matchExecShape(strings.Join(rows, "\n"))
	setFocusPaneFacts(m, "s1", false, false, false, 0, paneCursor{x: 0, y: 46, ok: true})
	if !m.caretAtInputStart("s1", "command-code") {
		t.Fatal("the bottom-parked caret over an empty composer was not recognised")
	}
	if got := m.paneCaretRow(); got != -1 {
		t.Fatalf("paneCaretRow = %d for a parked caret, want -1", got)
	}
	window, start := paneWindow(m.workspace.preview, 30, m.paneCaretRow())
	if last := window[len(window)-1]; strings.TrimSpace(last) != "? for shortcuts · PR #390 · taste on" {
		t.Fatalf("crop bottom = %q, want the footer, not blank fill (start=%d)", last, start)
	}
	setFocusCursor(m, paneCursor{x: 2, y: 39, ok: true})
	if got := m.paneCaretRow(); got != 39 {
		t.Fatalf("paneCaretRow = %d for an in-content caret, want 39", got)
	}
}
