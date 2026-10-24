package runtime

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/tmux"
)

// A row whose creation, launch or socket moved since the write was queued
// refuses input, so a relaunched pane never receives text meant for the one it
// replaced.
func TestWriterRefusesAStaleTarget(t *testing.T) {
	t.Parallel()
	st, current := storeWith(t)
	stale := current
	stale.CreatedAt = stale.CreatedAt.Add(-time.Second)
	called := false
	writer := Writer{Store: st, Paste: func(*tmux.Driver, string, string) error { called = true; return nil }}
	if _, err := writer.Run(Request{Kind: Paste, Session: stale, Text: "draft"}); err == nil {
		t.Fatal("a changed creation accepted input")
	}
	if called {
		t.Fatal("a stale target received the paste")
	}
}

// A launch time alone is enough: the same window can carry a new agent process.
func TestWriterRefusesARelaunchedTarget(t *testing.T) {
	t.Parallel()
	st, current := storeWith(t)
	stale := current
	stale.AgentLaunchedAt = stale.LaunchTime().Add(-time.Second)
	writer := Writer{Store: st, Paste: func(*tmux.Driver, string, string) error { return nil }}
	if _, err := writer.Run(Request{Kind: Paste, Session: stale, Text: "draft"}); err == nil {
		t.Fatal("a relaunched target accepted input")
	}
}

// A prompt receipt records the draft on the launch it was typed in, and reports
// the row it reached so the list updates without a second read.
func TestWriterRecordsAPromptOnTheCurrentLaunch(t *testing.T) {
	t.Parallel()
	st, current := storeWith(t)
	writer := Writer{Store: st}
	result, err := writer.Run(Request{Kind: Prompt, Session: current, Text: "hello"})
	if err != nil {
		t.Fatalf("prompt receipt: %v", err)
	}
	if result.PromptID != current.ID || result.Prompt != "hello" {
		t.Fatalf("result = %+v, want the prompt recorded on %q", result, current.ID)
	}
	updated, err := st.Get(current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastPrompt != "hello" {
		t.Fatalf("LastPrompt = %q, want the receipt", updated.LastPrompt)
	}
}

// A receipt for a launch the row left does not overwrite the newer prompt.
func TestWriterRefusesAStalePromptReceipt(t *testing.T) {
	t.Parallel()
	st, current := storeWith(t)
	stale := current
	stale.AgentLaunchedAt = stale.LaunchTime().Add(-time.Second)
	writer := Writer{Store: st}
	if _, err := writer.Run(Request{Kind: Prompt, Session: stale, Text: "old draft"}); err == nil {
		t.Fatal("a stale prompt receipt was accepted")
	}
	updated, err := st.Get(current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastPrompt != "" {
		t.Fatalf("LastPrompt = %q, want it untouched", updated.LastPrompt)
	}
}

// A paste goes over its own transport, and nothing counts a write with no
// watcher to hear it land.
func TestWriterPastesThroughItsOwnTransport(t *testing.T) {
	t.Parallel()
	st, current := storeWith(t)
	var got string
	writer := Writer{Store: st, Paste: func(_ *tmux.Driver, id, text string) error {
		if id != current.ID {
			t.Fatalf("paste target = %q, want %q", id, current.ID)
		}
		got = text
		return nil
	}}
	result, err := writer.Run(Request{Kind: Paste, Session: current, Text: "draft", Echo: true})
	if err != nil {
		t.Fatalf("paste: %v", err)
	}
	if got != "draft" {
		t.Fatalf("pasted %q, want the draft", got)
	}
	if result.InputID != current.ID {
		t.Fatalf("input id = %q, want %q", result.InputID, current.ID)
	}
}
