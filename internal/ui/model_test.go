package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"strings"
	"testing"
)

// New hands the config's key table to the tmux driver, so a session the
// manager creates is bound and labelled the same way focus reads its keys.
func TestNewHandsTheKeyTableToTmux(t *testing.T) {
	m := buildModel(t)
	cfg := m.services.cfg
	cfg.SessionKeys = keybind.DefaultSession().With(keybind.Detach, bindingOf(t, "f9")).With(keybind.Review, bindingOf(t, "ctrl+g"))
	loaded := New(cfg, m.services.store, m.services.tmux, m.services.engine, m.services.hooks, "dev")
	loaded.width, loaded.height = 120, 40
	t.Cleanup(func() { m.services.tmux.SetSessionKeys(keybind.DefaultSession()) })
	if got := loaded.services.keys.Binding(keybind.Editor).Label(); got != "f3" {
		t.Fatalf("editor left out should take the default, got %q", got)
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
		t.Fatalf("session footer should carry the config's keys, got %q", right)
	}
}
