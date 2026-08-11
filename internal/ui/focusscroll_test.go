package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
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
		t.Fatalf("did not enter focus: %q", m.errBar.text)
	}
	// Wait for the control client, which every scroll query rides.
	deadline := time.Now().Add(5 * time.Second)
	for !m.focus.serving(sess.ID) {
		if time.Now().After(deadline) {
			t.Skip("control client never came up on this host")
		}
		time.Sleep(20 * time.Millisecond)
	}

	command := `i=1; while [ "$i" -le 120 ]; do printf 'history-line-%03d\n' "$i"; i=$((i+1)); done`
	if err := m.tmux.SendText(sess.ID, command); err != nil {
		t.Fatalf("SendText: %v", err)
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
	m.pane.history = paneHistorySize(t, m, sess.ID)
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

// A capture scheduled before the preview reflows must not blank the bottom
// of the resized viewport when its reply arrives afterwards.
func TestFocusScrollRecapturesAfterPreviewResize(t *testing.T) {
	m, _ := focusedWithHistory(t, "reflow")
	cmd := m.scrollFocus(-1)
	if cmd == nil {
		t.Fatal("wheel up produced no capture")
	}
	stale := cmd()
	if stale == nil {
		t.Fatal("scroll capture returned nothing")
	}
	oldRows := m.previewPaneHeight()
	m.height += 8
	if m.previewPaneHeight() == oldRows {
		t.Fatal("test setup did not change preview height")
	}
	m.resizeSessions()

	updated, recapture := m.Update(stale)
	m = updated.(*Model)
	if recapture == nil {
		t.Fatal("stale geometry capture was accepted")
	}
	updated, _ = m.Update(recapture())
	m = updated.(*Model)
	if got, want := len(paneExact(m.preview, m.previewPaneHeight())), m.previewPaneHeight(); got != want {
		t.Fatalf("scroll frame has %d rows, want %d", got, want)
	}
	if !strings.Contains(m.preview, "history-line-") {
		t.Fatalf("recaptured frame lost history:\n%s", m.preview)
	}
}

// Deep history must keep the requested pane-sized frame intact. This covers
// the control-pipe capture path beyond the shallow history used by the wheel
// smoke test above.
func TestFocusScrollKeepsDeepHistoryFrame(t *testing.T) {
	m, sessID := focusedWithHistory(t, "deep-history")
	command := `i=121; while [ "$i" -le 1000 ]; do printf 'history-line-%04d\n' "$i"; i=$((i+1)); done`
	if err := m.tmux.SendText(sessID, command); err != nil {
		t.Fatalf("send deep history: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(sessID)
		if err != nil {
			t.Fatalf("capture deep history: %v", err)
		}
		if strings.Contains(pane, "history-line-1000") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane never printed deep history: %q", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}
	m.pane.history = paneHistorySize(t, m, sessID)
	if m.pane.history < 820 {
		t.Skipf("tmux history is only %d lines", m.pane.history)
	}
	m.focusScroll = 807
	cmd := m.focusRegionCmd(sessID, m.focusScroll)
	msg := cmd()
	if msg == nil {
		t.Fatal("deep capture returned nothing")
	}
	updated, _ := m.Update(msg)
	m = updated.(*Model)
	if got, want := len(paneExact(m.preview, m.previewPaneHeight())), m.previewPaneHeight(); got != want {
		t.Fatalf("deep frame has %d rows, want %d", got, want)
	}
	if !strings.Contains(m.preview, "history-line-") {
		t.Fatalf("deep frame lost history:\n%s", m.preview)
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
	limit := m.pane.history
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
	m.pane.cursor = paneCursor{x: 1, y: 0, ok: true}
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

// focusedMouseApp focuses a session whose tool claims the mouse, with the
// watcher's pushes pumped through Update the way the running app does, so
// the pane-state cache the wheel routes on is populated.
func focusedMouseApp(t *testing.T, tool, name string) (*Model, store.Session) {
	t.Helper()
	m := buildModel(t)
	if err := m.spawnSession(tool, name, t.TempDir(), "", "", true, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, name)
	sess := m.rows[m.cursor].sess

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
	for !m.pane.mouse {
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
	return m, sess
}

// Ordinary clicks still select and copy inside agent-manager. Holding Alt is
// the deliberate handoff gesture for an agent that owns the mouse.
func TestAltClickReachesMouseTrackingApp(t *testing.T) {
	m, sess := focusedMouseApp(t, "mouse-tool", "clickapp")

	m.handleFocusMouse(tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Alt: true,
		X: m.pane.box.x + 2, Y: m.pane.box.y + 1,
	})
	if m.sel.active {
		t.Fatal("Alt-click on a mouse-tracking pane started a selection")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if press := strings.Index(pane, "[<0;"); press >= 0 {
			move := strings.Index(pane, "[<35;")
			if move < 0 || move > press {
				t.Fatalf("all-motion pointer move did not lead the press: %q", pane)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Alt-click report never reached the pane: %q", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// An application that turns on mouse tracking owns the wheel: agent CLIs
// run on the alternate screen, where tmux keeps no scrollback at all, and
// scroll themselves when they receive the event.
func TestWheelReachesMouseTrackingApp(t *testing.T) {
	m, sess := focusedMouseApp(t, "mouse-tool", "wheelapp")

	// A wheel notch over the pane now goes to the app, not to tmux history.
	before := m.focusScroll
	m.wheelFocus(true, m.pane.box.x+2, m.pane.box.y+1)
	if m.focusScroll != before {
		t.Fatal("wheel scrolled tmux history while the app owned the mouse")
	}
	if !m.pane.sgr {
		t.Fatal("pane asked for SGR reports but the model did not read it")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		// cat echoes the control bytes, so the wheel report shows up as
		// text with the escape rendered as ^[.
		if strings.Contains(pane, "[<64;") {
			// The app tracks all motion, so the notch has to arrive with
			// the pointer already reported at that cell.
			move := strings.Index(pane, "[<35;")
			if move < 0 {
				t.Fatalf("no pointer move ahead of the wheel report: %q", pane)
			}
			if move > strings.Index(pane, "[<64;") {
				t.Fatalf("pointer move landed after the wheel report: %q", pane)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("wheel report never reached the pane: %q", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A pane whose app owns the wheel must not stay parked on a scrollback
// offset: the wheel goes to the app from then on, so nothing would walk
// the offset back down and the view would hold a stale frame for good.
func TestAppMouseClearsScrollback(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "sticky", t.TempDir(), "")
	m.selectSessionRow(t, "sticky")
	sess := m.rows[m.cursor].sess
	m.mode = modeFocus
	m.focusScroll = 9
	if !m.scrolledBack() {
		t.Fatal("setup did not leave the view scrolled back")
	}

	updated, _ := m.Update(focusPreviewMsg{
		sessID:    sess.ID,
		preview:   "LIVE-FRAME\n",
		paneMouse: true,
	})
	m = updated.(*Model)
	if m.focusScroll != 0 {
		t.Fatalf("focusScroll = %d, want the app-owned pane back at the bottom", m.focusScroll)
	}
	if m.preview != "LIVE-FRAME\n" {
		t.Fatalf("preview = %q, want the live frame", m.preview)
	}
}

// The poll is the only source of frames once a control client dies, so
// it owes a scrolled-back pane the same stillness the pushed frames do.
func TestPolledFrameHoldsScrolledView(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "polled", t.TempDir(), "")
	m.selectSessionRow(t, "polled")
	sess := m.rows[m.cursor].sess
	m.mode = modeFocus
	m.preview = "SCROLLED-FRAME\n"
	m.focusScroll = 6

	m.setPreview(sess.ID, "LIVE-FRAME\n")
	if m.preview != "SCROLLED-FRAME\n" {
		t.Fatalf("preview = %q, want the scrolled frame held", m.preview)
	}

	m.focusScroll = 0
	m.setPreview(sess.ID, "LIVE-FRAME\n")
	if m.preview != "LIVE-FRAME\n" {
		t.Fatalf("preview = %q, want the live frame back at the bottom", m.preview)
	}
}

// An app that claims the mouse without asking for SGR reads the original
// encoding, and reports in the newer one would reach it as text.
func TestWheelFallsBackToX10Reports(t *testing.T) {
	m, sess := focusedMouseApp(t, "x10-tool", "x10app")
	if m.pane.sgr {
		t.Fatal("a pane that never asked for SGR reported it")
	}

	m.wheelFocus(true, m.pane.box.x+2, m.pane.box.y+1)
	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		// cat echoes the bytes back, so an X10 report shows up as [M and
		// its three coordinate characters, with no SGR report anywhere.
		if strings.Contains(pane, "[M") {
			if strings.Contains(pane, "[<") {
				t.Fatalf("SGR report reached a pane that never asked for it: %q", pane)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("X10 wheel report never reached the pane: %q", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestMouseReportEncodings(t *testing.T) {
	if got, want := sgrMouse(motionButton, 0, 0), "\x1b[<35;1;1M"; got != want {
		t.Errorf("motion = %q, want %q", got, want)
	}
	if got, want := sgrMouse(wheelUpButton, 0, 0), "\x1b[<64;1;1M"; got != want {
		t.Errorf("wheel up = %q, want %q", got, want)
	}
	if got, want := sgrMouse(wheelDownButton, 11, 4), "\x1b[<65;12;5M"; got != want {
		t.Errorf("wheel down = %q, want %q", got, want)
	}
	if got, want := hexBytes("\x1b[<64;1;1M"), "1b 5b 3c 36 34 3b 31 3b 31 4d"; got != want {
		t.Errorf("hexBytes = %q, want %q", got, want)
	}

	got, ok := x10Mouse(wheelUpButton, 0, 0)
	if !ok || got != "\x1b[M`!!" {
		t.Errorf("x10 wheel up = %q (ok=%v), want %q", got, ok, "\x1b[M`!!")
	}
	if got, ok := x10Mouse(motionButton, 11, 4); !ok || got != "\x1b[MC,%" {
		t.Errorf("x10 motion = %q (ok=%v), want %q", got, ok, "\x1b[MC,%")
	}
	// Past the cell the encoding can name, a report would land on the
	// wrong column, so there is none to send.
	if _, ok := x10Mouse(wheelUpButton, x10Limit, 4); ok {
		t.Error("x10 named a column the encoding cannot carry")
	}
	if _, ok := x10Mouse(wheelUpButton, 4, x10Limit); ok {
		t.Error("x10 named a row the encoding cannot carry")
	}
}

// Entering focus on a pane that has gone quiet keeps the cached pane
// state. The watcher is already streaming this session and a quiet pane
// pushes no fresh capture, so a reset on entry would route the wheel as
// a plain pane with no history — dead until the agent next paints,
// which is exactly the shape of scrolling a finished agent's pane.
func TestFocusReentryKeepsPaneStateOnQuietPane(t *testing.T) {
	m, sess := focusedMouseApp(t, "mouse-tool", "quietapp")

	// Out to the list and back in, with the pane painting nothing in
	// between — checking on an agent whose turn has ended.
	m.leaveFocus()
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("did not re-enter focus: %q", m.errBar.text)
	}

	if !m.pane.mouse {
		t.Fatal("re-entering focus dropped the pane's mouse claim")
	}
	if !m.pane.sgr {
		t.Fatal("re-entering focus dropped the pane's SGR encoding")
	}
	m.View()

	// The wheel still reaches the app, with no pushed capture in between.
	m.wheelFocus(true, m.pane.box.x+2, m.pane.box.y+1)
	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if strings.Contains(pane, "[<64;") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("wheel report never reached the quiet pane: %q", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// A cache stamped by another session's capture still resets, serving
	// client or not: this session's first capture may not have landed.
	m.leaveFocus()
	m.pane.forID = "someone-else"
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.pane.mouse {
		t.Fatal("another session's cached flags survived focus entry")
	}
}

// The wheel reports the pane's own row, which is the painted row plus
// whatever the panel dropped off the top of a taller capture.
func TestWheelReportUsesPaneRow(t *testing.T) {
	m := paneAt(t, "one", "two")
	m.pane.sgr = true
	m.preview = "a\nb\nc\nd\none\ntwo\n"

	if got, want := m.paneRowOffset(m.pane.box.height), 4; got != want {
		t.Fatalf("row offset = %d, want %d", got, want)
	}
	report, ok := m.wheelReport(true, 3, 1+m.paneRowOffset(m.pane.box.height))
	if !ok {
		t.Fatal("no wheel report for a pane inside the encoding's range")
	}
	if want := "\x1b[<64;4;6M"; report != want {
		t.Fatalf("report = %q, want %q", report, want)
	}
}
