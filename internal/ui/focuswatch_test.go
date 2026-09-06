package ui

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

func requireFocusDriver(t *testing.T) *tmux.Driver {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	driver, err := tmux.NewWithSocket(testSocket)
	if err != nil {
		t.Fatalf("NewWithSocket: %v", err)
	}
	return driver
}

// The watcher pushes a fresh preview when the pane paints, with no tick
// asking for it.
func TestFocusWatchPushesPaneUpdates(t *testing.T) {
	driver := requireFocusDriver(t)
	id := "focus" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	msgs := make(chan tea.Msg, 64)
	watch := newFocusWatch(driver, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(watch.Close)
	watch.setFocus(id)

	// The watcher seeds one capture on open.
	waitFocusPreview(t, msgs, id, "")

	if err := driver.SendText(id, "echo focus-watch-ping"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	waitFocusPreview(t, msgs, id, "focus-watch-ping")
}

// Refocusing another session stops the old watcher and serves the new one.
func TestFocusWatchRefocus(t *testing.T) {
	driver := requireFocusDriver(t)
	stamp := strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	first, second := "fw1"+stamp, "fw2"+stamp
	for _, id := range []string{first, second} {
		if err := driver.Create(id, "/tmp", "", nil, 80, 24); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	t.Cleanup(func() { driver.Kill(first); driver.Kill(second) })

	var mu sync.Mutex
	var got []focusPreviewMsg
	watch := newFocusWatch(driver, func(msg tea.Msg) {
		if preview, ok := msg.(focusPreviewMsg); ok {
			mu.Lock()
			got = append(got, preview)
			mu.Unlock()
		}
	})
	t.Cleanup(watch.Close)

	watch.setFocus(first)
	watch.setFocus(second)
	watch.Close()

	mu.Lock()
	defer mu.Unlock()
	for _, preview := range got {
		if preview.sessID != first && preview.sessID != second {
			t.Fatalf("preview for unknown session %q", preview.sessID)
		}
	}
}

func waitFocusPreview(t *testing.T, msgs <-chan tea.Msg, id, contains string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-msgs:
			preview, ok := msg.(focusPreviewMsg)
			if !ok || preview.sessID != id {
				continue
			}
			if strings.Contains(preview.preview, contains) {
				return
			}
		case <-deadline:
			t.Fatalf("no focusPreviewMsg containing %q", contains)
		}
	}
}

// Regression: the UI loop calls setFocus from Update while the watcher may
// be blocked inside send (bubbletea's Send blocks until the same UI loop
// drains it). Refocusing must not wait on the watcher or the two deadlock.
func TestFocusWatchRefocusWhileSendBlocked(t *testing.T) {
	driver := requireFocusDriver(t)
	id := "fwb" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	// Unbuffered and never drained until after setFocus, exactly like the
	// bubbletea msgs channel while Update is running.
	msgs := make(chan tea.Msg)
	watch := newFocusWatch(driver, func(msg tea.Msg) { msgs <- msg })

	sending := make(chan struct{}, 1)
	go func() {
		// Wait for the watcher's seed capture to block in send.
		time.Sleep(200 * time.Millisecond)
		sending <- struct{}{}
	}()
	watch.setFocus(id)
	<-sending

	done := make(chan struct{})
	go func() {
		watch.setFocus("")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("setFocus deadlocked against a blocked send")
	}
	// Drain so the watcher goroutine can finish its blocked send and exit.
	go func() {
		for range msgs {
		}
	}()
	watch.Close()
}

// A session whose control client cannot open goes into backoff instead of
// costing one tmux fork per poll pass; a deliberate act lifts the pause.
func TestFocusWatchBacksOffAfterFailure(t *testing.T) {
	requireFocusDriver(t)
	// A private socket: the failed attach spins up a transient tmux server
	// that is mid-exit moments later, and on the shared socket that dying
	// server takes the next test's fresh session down with it.
	driver, err := tmux.NewWithSocket(testSocket + "backoff")
	if err != nil {
		t.Fatalf("NewWithSocket: %v", err)
	}
	id := "gone" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")

	watch := newFocusWatch(driver, func(tea.Msg) {})
	t.Cleanup(watch.Close)

	// The session does not exist, so the attach fails and the watcher
	// releases its claim, marking the id failed.
	watch.setFocus(id)
	deadline := time.After(5 * time.Second)
	for watch.watching() == id {
		select {
		case <-deadline:
			t.Fatal("failed watcher never released its claim")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// The next selection sync must not reopen inside the backoff window.
	failedAt := func() time.Time {
		watch.mu.Lock()
		defer watch.mu.Unlock()
		return watch.failedAt
	}
	firstFailure := failedAt()
	watch.setFocus(id)
	if watch.watching() == id {
		t.Fatal("failed session reopened without backoff")
	}

	// A deliberate focus lifts the pause: the retry spawns, fails against
	// the still-missing session, and stamps a fresh failure. The claim
	// itself may already be released again, so the new stamp is the
	// race-free evidence the attach was attempted.
	watch.retryNow()
	watch.setFocus(id)
	deadline = time.After(5 * time.Second)
	for !failedAt().After(firstFailure) {
		select {
		case <-deadline:
			t.Fatal("retryNow did not lift the backoff")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// The watcher must deliver real cursor coordinates, not just a preview:
// an unquoted tmux format silently answers with tmux's default status
// message instead, which is exactly how the cursor went missing.
func TestFocusWatchReportsCursor(t *testing.T) {
	driver := requireFocusDriver(t)
	id := "cur" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	msgs := make(chan tea.Msg, 64)
	watch := newFocusWatch(driver, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(watch.Close)
	watch.setFocus(id)

	// Put the cursor somewhere unambiguous: a prompt line plus typed text
	// moves it off column zero.
	if err := driver.SendText(id, "echo cursor-probe"); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-msgs:
			preview, ok := msg.(focusPreviewMsg)
			if !ok || preview.sessID != id {
				continue
			}
			if !preview.cursorOK {
				continue
			}
			if preview.cursorY < 0 || preview.cursorX < 0 {
				t.Fatalf("nonsense cursor: %d,%d", preview.cursorX, preview.cursorY)
			}
			if preview.cursorY == 0 && preview.cursorX == 0 {
				// Still on the first cell; keep waiting for the shell to draw.
				continue
			}
			return
		case <-deadline:
			t.Fatal("no focusPreviewMsg ever carried a cursor position")
		}
	}
}

func TestFocusWatchHonorsHiddenCursor(t *testing.T) {
	driver := requireFocusDriver(t)
	id := "hidecur" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	command := "printf '\\033[?25lhidden-cursor'; exec sleep 10"
	if err := driver.Create(id, "/tmp", command, nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	msgs := make(chan tea.Msg, 64)
	watch := newFocusWatch(driver, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(watch.Close)
	watch.setFocus(id)

	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-msgs:
			preview, ok := msg.(focusPreviewMsg)
			if !ok || preview.sessID != id || !strings.Contains(preview.preview, "hidden-cursor") {
				continue
			}
			if !preview.paneStateOK || preview.cursorOK {
				continue
			}
			if preview.cursorX != len("hidden-cursor") || preview.cursorY != 0 {
				t.Fatalf("hidden cursor position = %d,%d", preview.cursorX, preview.cursorY)
			}
			return
		case <-deadline:
			t.Fatal("no hidden-cursor preview reported the cursor hidden")
		}
	}
}

func TestApplyPaneState(t *testing.T) {
	var msg focusPreviewMsg
	applyPaneState(&msg, "12,34,1,010,250,0,1\n")
	if !msg.paneStateOK || !msg.cursorOK || msg.cursorX != 12 || msg.cursorY != 34 {
		t.Fatalf("pane state = %+v", msg)
	}
	if !msg.paneMouse {
		t.Fatal("mouse flag 1 not read as pane-owned mouse")
	}
	if msg.historySize != 250 {
		t.Fatalf("historySize = %d, want 250", msg.historySize)
	}
	if msg.paneMotion {
		t.Fatal("a button-tracking pane read as tracking all motion")
	}

	// tmux reports 1003 in mouse_all_flag, the apps that want a pointer
	// move with every event.
	var motion focusPreviewMsg
	applyPaneState(&motion, "0,0,1,100,0,1,1")
	if !motion.paneStateOK || !motion.paneMouse || !motion.paneMotion {
		t.Fatalf("all-motion pane state = %+v", motion)
	}

	var plain focusPreviewMsg
	applyPaneState(&plain, "0,0,1,000,0,0,0")
	if !plain.paneStateOK || plain.paneMouse || plain.paneMotion || plain.historySize != 0 || !plain.cursorOK {
		t.Fatalf("plain pane state = %+v", plain)
	}

	var hidden focusPreviewMsg
	applyPaneState(&hidden, "7,8,0,000,0,0,0")
	if !hidden.paneStateOK || hidden.cursorOK || hidden.cursorX != 7 || hidden.cursorY != 8 {
		t.Fatalf("hidden cursor pane state = %+v, want its coordinates kept", hidden)
	}

	// tmux answers an unquoted format with its default status message;
	// that must never read as pane state.
	var bogus focusPreviewMsg
	applyPaneState(&bogus, "[am_x] 0:zsh, current pane 0 - (00:43)")
	if bogus.paneStateOK || bogus.cursorOK || bogus.paneMouse || bogus.historySize != 0 {
		t.Fatal("applyPaneState accepted tmux's default message")
	}
	applyPaneState(&bogus, "nonsense")
	if bogus.paneStateOK || bogus.cursorOK {
		t.Fatal("applyPaneState accepted junk")
	}

	var badCursor focusPreviewMsg
	applyPaneState(&badCursor, "12,y,1,010,250,0,1")
	if badCursor.paneStateOK || badCursor.cursorOK {
		t.Fatal("applyPaneState accepted a non-numeric cursor")
	}
	var badHistory focusPreviewMsg
	applyPaneState(&badHistory, "12,34,1,010,n,0,1")
	if badHistory.paneStateOK || badHistory.historySize != 0 {
		t.Fatal("applyPaneState accepted a non-numeric history size")
	}
}

// Trailing blank pane rows are content: dropping them shifts every line
// up, which is what made the pushed and polled captures disagree. The
// input is shaped like the control client's reply, a newline JOIN of the
// captured rows with no terminator, so a 3-row pane whose last two rows
// are blank arrives as "top line\n\n"; the old trim ate the final blank
// row, and a caret parked on the pane's bottom row pointed past the rows.
func TestControlCaptureKeepsTrailingBlankRows(t *testing.T) {
	pane := "top line\n\n"
	if got, want := matchExecShape(pane), "top line\n\n\n"; got != want {
		t.Fatalf("matchExecShape(%q) = %q, want %q", pane, got, want)
	}
	if got := len(strings.Split(strings.TrimSuffix(matchExecShape(pane), "\n"), "\n")); got != 3 {
		t.Fatalf("kept %d rows, want 3", got)
	}
	// A pane whose last row carries text gains only the terminator.
	if got, want := matchExecShape("a\nb"), "a\nb\n"; got != want {
		t.Fatalf("matchExecShape(%q) = %q, want %q", "a\nb", got, want)
	}
}

// A client that dies under the watcher is reported, not swallowed: the
// preview silently dropping to the poll cadence reads as the manager
// lagging, with nothing on screen saying why.
func TestFocusWatchReportsALostClient(t *testing.T) {
	driver := requireFocusDriver(t)
	id := "lost" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	msgs := make(chan tea.Msg, 64)
	watch := newFocusWatch(driver, func(msg tea.Msg) { msgs <- msg })
	t.Cleanup(watch.Close)
	watch.setFocus(id)
	waitFocusPreview(t, msgs, id, "")

	// Killing the session detaches its control client out from under the
	// watcher, which is one of the ways the client goes away in use.
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

func TestFocusWatchSkipsMissingSession(t *testing.T) {
	driver := requireFocusDriver(t)
	id := "missing" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	watch := newFocusWatch(driver, func(msg tea.Msg) {
		t.Errorf("missing session produced a preview notification: %#v", msg)
	})
	t.Cleanup(watch.Close)

	watch.watch(id, make(chan struct{}))
}
