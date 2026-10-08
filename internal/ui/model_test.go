package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"strings"
	"testing"
)

// New hands the stored key table to the tmux driver, so a session the
// manager creates is bound and labelled the same way focus reads its keys.
func TestNewHandsTheKeyTableToTmux(t *testing.T) {
	m := buildModel(t)
	stored := keybind.DefaultSession().With(keybind.Detach, bindingOf(t, "f9")).With(keybind.Review, bindingOf(t, "ctrl+g"))
	if err := m.services.store.SetKeys(stored); err != nil {
		t.Fatalf("SetKeys: %v", err)
	}
	loaded := reloadModel(t, m)
	loaded.width, loaded.height = 120, 40
	t.Cleanup(func() { m.services.tmux.SetSessionKeys(keybind.DefaultSession()) })
	if got := loaded.services.keys.Binding(keybind.Editor).Label(); got != "f3" {
		t.Fatalf("an action nobody moved should keep its default, got %q", got)
	}
	createSession(t, loaded, "tablebound", t.TempDir(), "")
	loaded.selectSessionRow(t, "tablebound")
	sess := railSelectedSession(loaded)
	t.Cleanup(func() { m.services.tmux.Kill(sess.ID) })
	right, err := tmuxCmd("display-message", "-p", "-t", "am_"+sess.ID, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("status-right: %v", err)
	}
	if !strings.Contains(string(right), "Ctrl+g = review") || !strings.Contains(string(right), "F9") {
		t.Fatalf("session footer should carry the stored keys, got %q", right)
	}
}
