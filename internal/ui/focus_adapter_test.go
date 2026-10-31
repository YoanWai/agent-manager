package ui

import (
	"errors"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/termseq"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Enter focuses the selected session by mounting the surface, which hands
// the keyboard to the agent; the detach key pops it.
func TestFocusIsASurface(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "focus-surface", t.TempDir(), "")
	m.selectSessionRow(t, "focus-surface")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainEffects(t)
	if !m.focused() || !m.agentOwnsKeyboard() {
		t.Fatalf("enter did not focus: focused=%t agent=%t", m.focused(), m.agentOwnsKeyboard())
	}
	if m.cardOpen() || mustTop(t, m).Dismissible() {
		t.Fatal("focus is no card and holds the agent's keys")
	}
	m.leaveFocus()
	if m.focused() || len(m.surfaces) != 0 {
		t.Fatal("leaving focus left the surface mounted")
	}
}

// A session open full screen owns the whole body, so the frame paints no
// rail line and leaves no hit behind for a click to resolve against.
func TestFullFocusFrameRecordsNoRailHits(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	preparedView(m)
	if len(m.layout.displayedRail.Hits()) == 0 {
		t.Fatal("test setup: the list frame should record the rail's hits")
	}

	m.prefs.fullLayout = true
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.fullFocus() {
		t.Fatalf("test setup: focus alpha full screen, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	preparedView(m)
	if len(m.layout.displayedRail.Hits()) != 0 {
		t.Fatalf("full focus paints no rail, got %d hits", len(m.layout.displayedRail.Hits()))
	}
}

// A lone click on a link opens it in both kinds of pane: held back and
// released in a mouse-tracking pane, resolved at release in a plain one.
func TestFocusClickOpensTheLink(t *testing.T) {
	opened := ""
	prev := openURL
	openURL = func(url string) error { opened = url; return nil }
	t.Cleanup(func() { openURL = prev })

	press := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 8, Y: 0}
	release := tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 8, Y: 0}

	for _, tracking := range []bool{true, false} {
		opened = ""
		m, id := focusedShotPane(t, 80, 0, 0, "read https://example.com/docs now")
		updateFocusPane(m, id, func(update *uifocus.PaneUpdate) { update.Mouse = tracking })
		m.handleFocusMouse(press)
		_, cmd := m.handleFocusMouse(release)
		if cmd == nil {
			t.Fatalf("tracking=%v: click on a link returned no command", tracking)
		}
		if msg := cmd(); msg != nil {
			t.Fatalf("tracking=%v: opener errored: %v", tracking, msg)
		}
		if opened != "https://example.com/docs" {
			t.Fatalf("tracking=%v: opened %q", tracking, opened)
		}
	}

	// A click away from any link keeps its old meaning.
	opened = ""
	m, _ := focusedShotPane(t, 80, 0, 0, "read https://example.com/docs now")
	m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 1, Y: 0})
	_, cmd := m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 1, Y: 0})
	if cmd != nil {
		t.Fatal("a linkless click returned a command")
	}
	if opened != "" {
		t.Fatalf("a linkless click opened %q", opened)
	}
}

// Hovering a link in a focused pane turns the pointer into a hand, named
// for both kinds of terminal; leaving the link, or the pane, gives the
// terminal its own pointer back.
func TestFocusHoverShowsTheHandOverALink(t *testing.T) {
	const hand = "\x1b]22;pointer\x1b\\\x1b]22;hand2\x1b\\"
	const reset = "\x1b]22;\x1b\\"
	var sink strings.Builder
	previous := termseq.Out
	termseq.Out = &sink
	t.Cleanup(func() { termseq.Out = previous })

	m, _ := focusedShotPane(t, 80, 0, 0, "read https://example.com/docs now")
	hover := func(x int) string {
		sink.Reset()
		m.handleMouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonNone, X: x, Y: 0})
		m.syncPointerShape()
		return sink.String()
	}
	if got := hover(8); got != hand {
		t.Fatalf("hovering the link wrote %q, want %q", got, hand)
	}
	if got := hover(12); got != "" {
		t.Fatalf("moving along the link wrote %q again", got)
	}
	if got := hover(1); got != reset {
		t.Fatalf("leaving the link wrote %q, want %q", got, reset)
	}

	hover(8)
	sink.Reset()
	m.openFocusSaved()
	m.syncPointerShape()
	if got := sink.String(); got != reset {
		t.Fatalf("a list over the pane wrote %q, want %q", got, reset)
	}
	hover(1)
	sink.Reset()
	m.Pop()
	m.syncPointerShape()
	if got := sink.String(); got != "" {
		t.Fatalf("closing the list over plain text wrote %q", got)
	}

	m, _ = focusedShotPane(t, 80, 0, 0, "read https://example.com/docs now")
	hover(8)
	sink.Reset()
	m.leaveFocus()
	m.syncPointerShape()
	if got := sink.String(); got != reset {
		t.Fatalf("leaving focus wrote %q, want %q", got, reset)
	}
}

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
	if !m.focused() {
		t.Fatalf("enter did not focus, screen = %v", m.screenID())
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
	if len(pasted) != 1 || pasted[0] != "line one\nline two" || m.savedListOpen() || !m.focused() {
		t.Fatalf("pasted %q, list open %v, mode %v", pasted, m.savedListOpen(), m.screenID())
	}

	_, load = m.handleKey(tea.KeyMsg{Type: tea.KeyF4})
	m.runBatch(t, load)
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if !m.onList() || m.savedListOpen() {
		t.Fatalf("the leave key with the list up left mode %v, open %v", m.screenID(), m.savedListOpen())
	}
}

func requireFocusDriver(t *testing.T) *tmux.Driver {
	t.Helper()
	if _, err := exec.LookPath(tmux.Binary); err != nil {
		t.Skip(tmux.Binary + " not installed")
	}
	driver, err := tmux.NewWithSocket(testSocket)
	if err != nil {
		t.Fatalf("NewWithSocket: %v", err)
	}
	return driver
}

// skipControlMode skips a test that needs the watcher's control client up:
// on Windows OpenControl refuses by design and previews poll instead.
func skipControlMode(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the manager does not use psmux control mode")
	}
}

// The watcher pushes a fresh preview when the pane paints, with no tick
// asking for it: pushed over the control client, or polled where there is
// none.
func TestFocusWatchPushesPaneUpdates(t *testing.T) {
	driver := requireFocusDriver(t)
	id := "focus" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, os.TempDir(), "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	msgs := make(chan tea.Msg, 64)
	watch := newFocusWatch(driver, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(watch.Close)
	watch.SetFocus(id)

	// The watcher seeds one capture on open.
	waitFocusPreview(t, msgs, id, "")

	if err := driver.SendText(id, "echo focus-watch-ping"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	waitFocusPreview(t, msgs, id, "focus-watch-ping")
}

func waitFocusPreview(t *testing.T, msgs <-chan tea.Msg, id, contains string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-msgs:
			preview, ok := msg.(focusruntime.FrameMsg)
			if !ok || preview.SessionID != id {
				continue
			}
			if strings.Contains(preview.Preview, contains) {
				return
			}
		case <-deadline:
			t.Fatalf("no focusruntime.FrameMsg containing %q", contains)
		}
	}
}

// A client that dies under the watcher is reported, not swallowed: the
// preview silently dropping to the poll cadence reads as the manager
// lagging, with nothing on screen saying why. A polled watcher's capture
// failing is the same loss.
func TestFocusWatchReportsALostClient(t *testing.T) {
	driver := requireFocusDriver(t)
	id := "lost" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, os.TempDir(), "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	msgs := make(chan tea.Msg, 64)
	watch := newFocusWatch(driver, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(watch.Close)
	watch.SetFocus(id)
	waitFocusPreview(t, msgs, id, "")

	// Killing the session pulls the pane out from under the watcher, which
	// is one of the ways the client goes away in use.
	driver.Kill(id)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-msgs:
			if failure, ok := msg.(errMsg); ok {
				if !strings.Contains(failure.err.Error(), "preview") {
					t.Fatalf("lost client reported as %q", failure.err)
				}
				return
			}
		case <-deadline:
			t.Fatal("lost control client was never reported")
		}
	}
}

func seedLifecycleWatcher(t *testing.T, m *Model, id string) {
	t.Helper()
	m.focus.runtime.Watch = newFocusWatch(m.services.tmux, func(tea.Msg) {})
	t.Cleanup(m.focus.runtime.Watch.Close)
	m.focus.runtime.Watch.SetFocus(id)
}

func TestFailedArchiveRestoresSurvivingSelectionWatcher(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	// Archive snapshots only a pane that has drawn; a fresh psmux pane can
	// still be blank, and then no write fails.
	waitForPreview(t, m)
	sess := railSelectedSession(m)
	seedLifecycleWatcher(t, m, sess.ID)
	m.services.setSnapshot = func(string, string) error { return errors.New("disk full") }
	m.archiveSelected()
	if err := m.archiveConfirmed(); err == nil {
		t.Fatal("expected snapshot failure")
	}
	if got := m.focus.runtime.Watch.Watching(); got != sess.ID {
		t.Fatalf("watching=%q, want surviving %q", got, sess.ID)
	}
}

func TestFailedChildDeleteRestoresSurvivingParentWatcher(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "parent", t.TempDir(), "")
	m.selectSessionRow(t, "parent")
	parent := railSelectedSession(m)
	seedLifecycleWatcher(t, m, parent.ID)
	child := store.Session{ID: "cafe", ParentID: parent.ID}
	path := m.services.hooks.StatusFile(child.ID)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "block"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	m.openConfirm(confirmTarget{prompt: uidialogs.Confirm{Action: uidialogs.ConfirmDelete}, lifecycle: uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmDelete), Sessions: []store.Session{parent, child}}})
	if _, err := m.deleteConfirmed(); err == nil {
		t.Fatal("expected child cleanup failure")
	}
	if got := m.focus.runtime.Watch.Watching(); got != parent.ID {
		t.Fatalf("watching=%q, want surviving %q", got, parent.ID)
	}
}
