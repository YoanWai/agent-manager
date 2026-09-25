package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/charmbracelet/x/ansi"
)

// A bare Model must default to mouse reporting on: mouseDisabled is named
// for its off polarity precisely so a zero-valued struct, the shape most
// tests build, does not silently disable the mouse everywhere.
func TestSyncMouseCaptureDefaultsOn(t *testing.T) {
	m := &Model{mode: modeList}
	// Nothing has changed from the zero value's implied "captured" state,
	// so there is nothing to tell the terminal.
	if cmd := m.syncMouseCapture(); cmd != nil {
		t.Fatal("default state should not re-announce mouse capture")
	}
	if m.mouseReleased {
		t.Fatal("mouse should stay captured by default")
	}
}

// The mouse-mode setting releases the mouse everywhere except focus mode,
// whose own forwarding predates the setting and stays on regardless.
func TestSyncMouseCaptureRespectsMouseDisabled(t *testing.T) {
	m := &Model{mode: modeList, mouseDisabled: true}
	if cmd := m.syncMouseCapture(); cmd == nil {
		t.Fatal("turning the setting off should release the mouse")
	}
	if !m.mouseReleased {
		t.Fatal("mouseReleased should follow the setting in list mode")
	}
	m.mode = modeFocus
	if cmd := m.syncMouseCapture(); cmd == nil {
		t.Fatal("entering focus mode should re-capture regardless of the setting")
	}
	if m.mouseReleased {
		t.Fatal("focus mode must keep the mouse captured even with the setting off")
	}
}

// The rows are marked against the server the poll read panes from, so the
// refresh has to hand that socket to the model it renders from.
func TestRefreshCarriesTheSocketItReadPanesFrom(t *testing.T) {
	for _, socket := range []string{"/tmp/first/agentmgr", "/tmp/second/agentmgr", ""} {
		m := &Model{collapsed: map[string]bool{}, tmuxSocket: "/tmp/stale/agentmgr"}
		m.Update(refreshMsg{tmuxSocket: socket, leadingManager: true, listedAt: time.Now()})
		if m.tmuxSocket != socket {
			t.Fatalf("model socket = %q, want the poll's %q", m.tmuxSocket, socket)
		}
		if !m.leadingManager {
			t.Fatal("the poll's hold on the store should reach the model")
		}
	}
}

// New hands the config's key table to the tmux driver, so a session the
// manager creates is bound and labelled the same way focus reads its keys.
func TestNewHandsTheKeyTableToTmux(t *testing.T) {
	m := buildModel(t)
	cfg := m.cfg
	cfg.SessionKeys = keybind.DefaultSession().With(keybind.Detach, bindingOf(t, "f9")).With(keybind.Review, bindingOf(t, "ctrl+g"))
	loaded := New(cfg, m.store, m.tmux, m.poller.engine, m.hooks, "dev")
	loaded.width, loaded.height = 120, 40
	t.Cleanup(func() { m.tmux.SetSessionKeys(keybind.DefaultSession()) })
	if got := loaded.keys.Binding(keybind.Editor).Label(); got != "f3" {
		t.Fatalf("editor left out should take the default, got %q", got)
	}
	createSession(t, loaded, "tablebound", t.TempDir(), "")
	loaded.selectSessionRow(t, "tablebound")
	sess := loaded.rows[loaded.cursor].sess
	t.Cleanup(func() { m.tmux.Kill(sess.ID) })
	right, err := tmuxCmd("display-message", "-p", "-t", "am_"+sess.ID, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("status-right: %v", err)
	}
	if !strings.Contains(string(right), "Ctrl+g = review") || !strings.Contains(string(right), "F9") {
		t.Fatalf("session footer should carry the config's keys, got %q", right)
	}
}

func TestStartupErrorStaysVisibleUntilFirstRefresh(t *testing.T) {
	m := buildModel(t)
	m.booting = true
	m.Update(errMsg{errors.New("startup poll failed")})
	if !m.booting {
		t.Fatal("an error before the first refresh must not finish boot")
	}
	if !strings.Contains(ansi.Strip(m.View()), "startup poll failed") {
		t.Fatal("startup error is hidden behind the boot loader")
	}
	m.Update(refreshMsg{listedAt: time.Now()})
	if m.booting {
		t.Fatal("the first successful refresh must finish boot")
	}
}
