package ui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Bubbletea's RestoreTerminal (v1.3.10) re-enables altscreen, bracketed
// paste, and focus reporting after tea.ExecProcess but not mouse mode, so
// every tmux attach/detach silently kills wheel capture and the host
// scrolls the manager off-screen. Detaching must re-arm mouse cell motion.
func TestDetachReArmsMouseCapture(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { m.tmux.ClearReviewRequest() })

	_, cmd := m.Update(attachDoneMsg{})
	if cmd == nil {
		t.Fatal("detach should re-enable mouse capture, got nil command")
	}
	if reflect.TypeOf(cmd()) != reflect.TypeOf(tea.EnableMouseCellMotion()) {
		t.Fatalf("detach command = %T, want the tea.EnableMouseCellMotion message", cmd())
	}
}
