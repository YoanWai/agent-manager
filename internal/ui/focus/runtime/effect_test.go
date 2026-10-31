package runtime

import (
	"os/exec"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// A probe of a window tmux no longer holds refuses the entry, and the refusal
// arrives as the request's own failure so the root reports it.
func TestProbeRequestRefusesADroppedWindow(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath(tmux.Binary); err != nil {
		t.Skip(tmux.Binary + " not installed")
	}
	st, current := storeWith(t)
	driver, err := tmux.NewWithSocket("amfocuseffect")
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	result, err := ProbeRequest{SessionID: current.ID, Generation: 7}.Run(Services{Store: st, Driver: driver})
	if err == nil {
		t.Fatal("a window tmux does not hold passed the probe")
	}
	if result != nil {
		t.Fatalf("result = %T, want nothing", result)
	}
}

// The acknowledgement clears the unread mark the finished status left.
func TestAckRequestClearsTheUnreadMark(t *testing.T) {
	t.Parallel()
	st, current := storeWith(t)
	if err := st.UpdateStatus(current.ID, status.Finished); err != nil {
		t.Fatal(err)
	}
	if _, err := (AckRequest{SessionID: current.ID}).Run(Services{Store: st}); err != nil {
		t.Fatalf("ack: %v", err)
	}
	acked, err := st.Get(current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if acked.Status != status.Idle || !acked.Acked {
		t.Fatalf("after the ack, status = %q acked = %v", acked.Status, acked.Acked)
	}
}

// The copy the lane keeps owns its slices, so a caller that reuses the row it
// built the write from cannot change a queued write.
func TestInputRequestCopiesWhatItCarries(t *testing.T) {
	t.Parallel()
	request := InputRequest{Value: Request{Kind: Keys, Args: []string{"send-keys"}, Command: "x"}}
	copied := request.EffectCopy().(InputRequest)
	copied.Value.Args[0] = "resize-pane"
	if request.Value.Args[0] != "send-keys" {
		t.Fatal("the copy shared the args slice with the request")
	}
	if copied.EffectName() != "input to a session" {
		t.Fatalf("name = %q", copied.EffectName())
	}
}

// A write reports the session it reached and the text it pasted, over the
// package's own result.
func TestInputRequestReportsTheWrite(t *testing.T) {
	t.Parallel()
	st, current := storeWith(t)
	var pasted string
	result, err := InputRequest{Value: Request{Kind: Paste, Session: current, Text: "draft"}}.Run(Services{
		Store: st,
		Paste: func(_ *tmux.Driver, id, text string) error {
			if id != current.ID {
				t.Fatalf("paste target = %q, want %q", id, current.ID)
			}
			pasted = text
			return nil
		},
	})
	if err != nil {
		t.Fatalf("paste: %v", err)
	}
	if pasted != "draft" {
		t.Fatalf("pasted %q, want the draft", pasted)
	}
	input, ok := result.(Result)
	if !ok {
		t.Fatalf("result = %T, want Result", result)
	}
	if input.InputID != current.ID {
		t.Fatalf("input id = %q, want %q", input.InputID, current.ID)
	}
}
