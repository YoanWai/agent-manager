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
	driver := requireFocusDriver(t)
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
	watch.setFocus(id)
	if watch.watching() == id {
		t.Fatal("failed session reopened without backoff")
	}

	// A deliberate focus lifts the pause immediately.
	watch.retryNow()
	watch.setFocus(id)
	if watch.watching() != id {
		t.Fatal("retryNow did not lift the backoff")
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
