package ui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

// focusedWithHistory focuses a session whose pane has more output than
// fits on screen, so scrolling has somewhere to go.
func focusedWithHistory(t *testing.T, name string) (*Model, string) {
	t.Helper()
	m := buildModel(t)
	createSession(t, m, name, t.TempDir(), "")
	m.selectSessionRow(t, name)
	sess := m.rows[m.cursor].sess

	// The watcher is normally created by StartPoller, which tests skip.
	m.focus = newFocusWatch(m.tmux, func(tea.Msg) {})
	t.Cleanup(m.focus.Close)
	m.focus.setFocus(sess.ID)

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("did not enter focus: %q", m.err)
	}
	// Wait for the control client, which every scroll query rides.
	deadline := time.Now().Add(5 * time.Second)
	for !m.focus.serving(sess.ID) {
		if time.Now().After(deadline) {
			t.Skip("control client never came up on this host")
		}
		time.Sleep(20 * time.Millisecond)
	}

	for i := 1; i <= 120; i++ {
		if err := m.tmux.SendText(sess.ID, fmt.Sprintf("echo history-line-%03d", i)); err != nil {
			t.Fatalf("SendText: %v", err)
		}
	}
	// Let the pane finish painting so the history is really there.
	deadline = time.Now().Add(10 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if strings.Contains(pane, "history-line-120") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane never printed the history: %q", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}
	m.View()
	// Tests discard the watcher's pushes, so seed the live frame the same
	// way the scroll path fetches one, and the history depth the wheel
	// clamps against.
	seedLive(t, m, sess.ID)
	m.paneHistory = paneHistorySize(t, m, sess.ID)
	return m, sess.ID
}

// paneHistorySize asks tmux for the pane's history depth over the control
// pipe, standing in for the pushed capture that normally caches it.
func paneHistorySize(t *testing.T, m *Model, sessID string) int {
	t.Helper()
	out, ok := m.focus.query(`display-message -p -t ` + tmux.SessionName(sessID) + ` "#{history_size}"`)
	if !ok {
		t.Fatal("history query failed over the control pipe")
	}
	size, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("history size %q: %v", out, err)
	}
	return size
}

// seedLive pulls the pane's current bottom into the model's preview.
func seedLive(t *testing.T, m *Model, sessID string) {
	t.Helper()
	cmd := m.focusRegionCmd(sessID, 0)
	if cmd == nil {
		t.Fatal("no live region command")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("live region capture returned nothing")
	}
	updated, _ := m.Update(msg)
	*m = *updated.(*Model)
}

// The wheel walks the focused pane back through its scrollback and back
// down again, and the view holds still while scrolled.
func TestFocusWheelScrollsHistory(t *testing.T) {
	m, sessID := focusedWithHistory(t, "scroller")

	live := m.preview
	if !strings.Contains(live, "history-line-120") {
		t.Fatalf("live preview missing the newest line: %q", live)
	}

	// Wheel up until an older line comes into view.
	var scrolled string
	for i := 0; i < 10; i++ {
		cmd := m.scrollFocus(-1)
		if cmd == nil {
			t.Fatalf("wheel up produced no capture at offset %d", m.focusScroll)
		}
		updated, _ := m.Update(cmd())
		*m = *updated.(*Model)
		scrolled = m.preview
		if !strings.Contains(scrolled, "history-line-120") {
			break
		}
	}
	if m.focusScroll == 0 {
		t.Fatal("wheel up never moved the pane back")
	}
	if strings.Contains(scrolled, "history-line-120") {
		t.Fatalf("scrolling never reached older output:\n%s", scrolled)
	}
	if !m.scrolledBack() {
		t.Fatal("pane does not report itself scrolled")
	}

	// A live push must not yank the view back to the bottom.
	updated, _ := m.Update(focusPreviewMsg{sessID: sessID, preview: "LIVE-FRAME\n"})
	*m = *updated.(*Model)
	if m.preview != scrolled {
		t.Fatal("a live frame overwrote the scrolled view")
	}

	// Wheel down all the way returns to the live bottom.
	for i := 0; i < 20 && m.focusScroll > 0; i++ {
		cmd := m.scrollFocus(1)
		if cmd == nil {
			continue
		}
		updated, _ := m.Update(cmd())
		*m = *updated.(*Model)
	}
	if m.scrolledBack() {
		t.Fatalf("wheel down left the pane scrolled at %d", m.focusScroll)
	}
	if !strings.Contains(m.preview, "history-line-120") {
		t.Fatalf("bottom does not show the newest line:\n%s", m.preview)
	}
}

// Typing while scrolled snaps back to the live bottom: keystrokes land
// there, so the view must follow them.
func TestTypingResumesLiveView(t *testing.T) {
	m, _ := focusedWithHistory(t, "typeback")
	for i := 0; i < 5; i++ {
		if cmd := m.scrollFocus(-1); cmd != nil {
			updated, _ := m.Update(cmd())
			*m = *updated.(*Model)
		}
	}
	if !m.scrolledBack() {
		t.Skip("pane had no history to scroll")
	}

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	*m = *updated.(*Model)
	if m.scrolledBack() {
		t.Fatal("typing left the view scrolled back")
	}
	if cmd == nil {
		t.Fatal("typing while scrolled fetched no live frame")
	}
}

// Scrolling stops at the top of the history instead of walking into
// empty regions forever.
func TestScrollStopsAtHistoryTop(t *testing.T) {
	m, _ := focusedWithHistory(t, "topstop")
	limit := m.paneHistory
	if limit == 0 {
		t.Skip("pane reported no history")
	}
	for i := 0; i < 500; i++ {
		if cmd := m.scrollFocus(-1); cmd == nil {
			break
		}
	}
	if m.focusScroll > limit {
		t.Fatalf("scrolled %d lines past a history of %d", m.focusScroll, limit)
	}
	if m.focusScroll != limit {
		t.Fatalf("scrolling stopped at %d, want the history top %d", m.focusScroll, limit)
	}
}

// The caret belongs to the live pane; a scrolled view must not paint it.
func TestNoCaretWhileScrolled(t *testing.T) {
	m := paneAt(t, "one", "two")
	m.paneCursor = paneCursor{x: 1, y: 0, ok: true}
	m.cursorOn = true
	if _, _, ok := m.cursorCell(2); !ok {
		t.Fatal("caret missing on the live view")
	}
	m.focusScroll = 6
	if _, _, ok := m.cursorCell(2); ok {
		t.Fatal("caret drawn on a scrolled-back view")
	}
}

// The preview box changes height for reasons other than a terminal
// resize; a pane left at the old height paints a dead band under its
// output, so a refresh has to re-assert the geometry.
func TestRefreshReassertsPaneHeight(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "sizer", t.TempDir(), "")
	m.applyCmd(t, m.refreshCmd())
	sess := m.rows[m.cursor].sess

	if got, want := windowHeight(t, sess.ID), m.previewPaneHeight(); got != want {
		t.Fatalf("initial pane height = %d, want %d", got, want)
	}

	full := m.previewPaneHeight()
	// Opening the quick bar shrinks the box without any terminal resize.
	m.openQuickMode()
	shrunk := m.previewPaneHeight()
	if shrunk >= full {
		t.Fatal("quick bar did not shrink the box height")
	}
	m.applyCmd(t, m.refreshCmd())
	if got := windowHeight(t, sess.ID); got != shrunk {
		t.Fatalf("pane height after opening the quick bar = %d, want %d", got, shrunk)
	}

	// Closing it grows the box back; the pane has to follow.
	m.quick.active = false
	grown := m.previewPaneHeight()
	m.applyCmd(t, m.refreshCmd())
	if got := windowHeight(t, sess.ID); got != grown {
		t.Fatalf("pane height after closing the quick bar = %d, want %d", got, grown)
	}
}

// windowHeight is the tmux window height a session is currently pinned to.
func windowHeight(t *testing.T, id string) int {
	t.Helper()
	out, err := tmuxCmd("display-message", "-p", "-t", "am_"+id, "#{window_height}").CombinedOutput()
	if err != nil {
		t.Fatalf("display-message: %v: %s", err, out)
	}
	height, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("parse height %q: %v", out, err)
	}
	return height
}

// An application that turns on mouse tracking owns the wheel: agent CLIs
// run on the alternate screen, where tmux keeps no scrollback at all, and
// scroll themselves when they receive the event.
func TestWheelReachesMouseTrackingApp(t *testing.T) {
	m := buildModel(t)
	if err := m.spawnSession("mouse-tool", "wheelapp", t.TempDir(), "", "", true); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "wheelapp")
	sess := m.rows[m.cursor].sess

	// Pump the watcher's pushes through Update, the way the running app
	// does, so the mouse-ownership cache the wheel reads gets populated.
	msgs := make(chan tea.Msg, 64)
	m.focus = newFocusWatch(m.tmux, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(m.focus.Close)
	m.focus.setFocus(sess.ID)
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.View()

	deadline := time.Now().Add(5 * time.Second)
	for !m.focus.serving(sess.ID) {
		if time.Now().After(deadline) {
			t.Skip("control client never came up on this host")
		}
		time.Sleep(20 * time.Millisecond)
	}
	deadline = time.Now().Add(5 * time.Second)
	for !m.paneMouse {
		select {
		case msg := <-msgs:
			updated, _ := m.Update(msg)
			*m = *updated.(*Model)
		default:
			time.Sleep(20 * time.Millisecond)
		}
		if time.Now().After(deadline) {
			t.Skip("pane never reported mouse tracking on this host")
		}
	}

	// A wheel notch over the pane now goes to the app, not to tmux history.
	before := m.focusScroll
	m.wheelFocus(true, m.paneBox.x+2, m.paneBox.y+1)
	if m.focusScroll != before {
		t.Fatal("wheel scrolled tmux history while the app owned the mouse")
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		// cat echoes the control bytes, so the wheel report shows up as
		// text with the escape rendered as ^[.
		if strings.Contains(pane, "[<64;") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("wheel report never reached the pane: %q", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestWheelSGREncoding(t *testing.T) {
	if got, want := wheelSGR(true, 0, 0), "\x1b[<64;1;1M"; got != want {
		t.Errorf("wheel up = %q, want %q", got, want)
	}
	if got, want := wheelSGR(false, 11, 4), "\x1b[<65;12;5M"; got != want {
		t.Errorf("wheel down = %q, want %q", got, want)
	}
	if got, want := hexBytes("\x1b[<64;1;1M"), "1b 5b 3c 36 34 3b 31 3b 31 4d"; got != want {
		t.Errorf("hexBytes = %q, want %q", got, want)
	}
}
