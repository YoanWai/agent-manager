package runtime

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/focus/watch"
)

// The root routes a watcher frame through FrameMessage and applies it to the
// focused surface, so every pane fact has to survive that hop.
func TestFrameMessageCarriesEveryPaneFact(t *testing.T) {
	t.Parallel()
	msg := FrameMessage(watch.Frame{
		SessionID:   "s1",
		Preview:     "pane\n",
		CursorX:     4,
		CursorY:     2,
		CursorOK:    true,
		PaneStateOK: true,
		Mouse:       true,
		Motion:      true,
		SGR:         true,
		Alt:         true,
		History:     40,
		Writes:      3,
	})
	if msg.SessionID != "s1" || msg.Preview != "pane\n" {
		t.Fatalf("frame = %+v, want the session and preview", msg)
	}
	if !msg.Mouse || !msg.Motion || !msg.SGR || !msg.Alt {
		t.Fatalf("frame = %+v, want the four pointer modes", msg)
	}
	if msg.History != 40 || msg.Writes != 3 {
		t.Fatalf("frame = %+v, want history 40 and writes 3", msg)
	}
	if msg.CursorX != 4 || msg.CursorY != 2 || !msg.CursorOK || !msg.PaneStateOK {
		t.Fatalf("frame = %+v, want the cursor cell", msg)
	}
}

// A run with no watcher serves nothing, counts no write and reads as caught
// up, which is what routes a report over the forked transport.
func TestStateWithoutAWatchIsInert(t *testing.T) {
	t.Parallel()
	state := &State{}
	if state.Ready() || state.Serving("s1") || state.CountWrite("s1") {
		t.Fatalf("a watcherless state claimed to serve a pane: %+v", state)
	}
	if state.Watching() != "" {
		t.Fatalf("watching = %q, want empty", state.Watching())
	}
	if !state.CaughtUp(0) {
		t.Fatal("a watcherless state should read as caught up")
	}
}

// The scroll read echoes its request when there is no watcher, so the surface
// settles the fetch it started instead of waiting for a reply.
func TestScrollEchoesTheRequestWithoutAWatch(t *testing.T) {
	t.Parallel()
	msg := Scroll(nil, "s1", 4, 5)
	if msg.SessionID != "s1" || msg.Offset != 4 || msg.Rows != 5 {
		t.Fatalf("msg = %+v, want the request echoed", msg)
	}
	if msg.OK || msg.Preview != "" {
		t.Fatalf("msg = %+v, want no preview", msg)
	}
}
