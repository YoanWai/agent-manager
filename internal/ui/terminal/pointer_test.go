package terminal

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/termseq"
)

// The pointer is written only when it changes.
func TestShowHandWritesOnlyChanges(t *testing.T) {
	var sink strings.Builder
	previous := termseq.Out
	termseq.Out = &sink
	t.Cleanup(func() { termseq.Out = previous })

	for _, step := range []struct {
		hand bool
		want string
	}{
		{true, handPointer},
		{true, ""},
		{false, resetPointer},
		{false, ""},
	} {
		sink.Reset()
		ShowHand(step.hand)
		if got := sink.String(); got != step.want {
			t.Fatalf("ShowHand(%v) wrote %q, want %q", step.hand, got, step.want)
		}
	}
}
