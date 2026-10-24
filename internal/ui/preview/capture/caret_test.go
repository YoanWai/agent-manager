package capture

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type caretRecorder struct {
	focused bool
	blinks  int
}

func (r *caretRecorder) Focused() bool { return r.focused }

func (r *caretRecorder) BlinkCaret() { r.blinks++ }

func TestRouteCaretOnlyRearmsWhileFocused(t *testing.T) {
	focused := &caretRecorder{focused: true}
	cmd, ok := RouteCaret(focused, CaretMsg{})
	if !ok || focused.blinks != 1 {
		t.Fatalf("focused caret: handled=%v blinks=%d", ok, focused.blinks)
	}
	if cmd == nil {
		t.Fatal("a focused blink did not re-arm the timer")
	}

	idle := &caretRecorder{}
	cmd, ok = RouteCaret(idle, CaretMsg{})
	if !ok || idle.blinks != 0 {
		t.Fatalf("unfocused caret: handled=%v blinks=%d", ok, idle.blinks)
	}
	if cmd != nil {
		t.Fatal("an unfocused blink kept the timer running")
	}
}

func TestRouteCaretIgnoresOtherMessages(t *testing.T) {
	root := &caretRecorder{focused: true}
	if cmd, ok := RouteCaret(root, TickMsg{}); ok || cmd != nil {
		t.Fatalf("a capture tick was routed to the caret: handled=%v cmd=%v", ok, cmd)
	}
	if root.blinks != 0 {
		t.Fatalf("an unrelated message blinked the caret %d times", root.blinks)
	}
	if _, ok := RouteCaret(root, tea.KeyMsg{Type: tea.KeyEnter}); ok {
		t.Fatal("a key was claimed by the caret route")
	}
}
