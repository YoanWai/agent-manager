package terminal

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/termseq"
)

// Handing the terminal to another program puts the terminal's own pointer
// back first, so a hand shown over a link never sits over that program.
func TestHandoffsPutThePointerBack(t *testing.T) {
	var sink strings.Builder
	previous := termseq.Out
	termseq.Out = &sink
	t.Cleanup(func() { termseq.Out = previous })
	stubLinkCopy(t, nil)

	handoffs := map[string]func() error{
		"child process": func() error { return childCommand{exec.Command("sh", "-c", "true")}.Run() },
		"link page": func() error {
			runLinkPage(t, "https://example.com/docs", "\n")
			return nil
		},
	}
	for name, run := range handoffs {
		ShowHand(true)
		sink.Reset()
		if err := run(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.HasPrefix(sink.String(), resetPointer) {
			t.Fatalf("%s wrote %q before the pointer reset", name, sink.String())
		}
		sink.Reset()
		ShowHand(false)
		if sink.Len() != 0 {
			t.Fatalf("%s: the pointer was reset twice", name)
		}
	}
}
