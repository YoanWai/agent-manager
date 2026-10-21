package sessioncmd

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
)

// A manager on another machine drives this one's CLI over SSH, with no
// session of its own here. The caller check on these only proved one
// existed, which anyone with a shell on the host could get around.
func TestLifecycleCommandsRunWithNoCallingSession(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create("", CreateSessionOptions{Name: "worker", Prompt: "hold the line"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForSessionOutput(t, h.sessions, "", created.ID, "hold the line")

	killed, err := h.sessions.Kill("", created.ID)
	if err != nil {
		t.Fatalf("Kill with no caller: %v", err)
	}
	if killed.Running || killed.Status != status.Dead || h.driver.Exists(created.ID) {
		t.Fatalf("killed session = %+v", killed)
	}
	revived, err := h.sessions.Revive("", created.ID)
	if err != nil {
		t.Fatalf("Revive with no caller: %v", err)
	}
	if !revived.Running || !h.driver.Exists(created.ID) {
		t.Fatalf("revived session = %+v", revived)
	}
	archived, err := h.sessions.Archive("", created.ID, true)
	if err != nil {
		t.Fatalf("Archive with no caller: %v", err)
	}
	if !archived.Archived {
		t.Fatalf("archived session = %+v", archived)
	}
	restored, err := h.sessions.Archive("", created.ID, false)
	if err != nil {
		t.Fatalf("restore with no caller: %v", err)
	}
	if restored.Archived {
		t.Fatalf("restored session = %+v", restored)
	}
	// Everything else these check still holds.
	terminal, err := h.terminals.Create("", CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	if _, err := h.sessions.Kill("", terminal.ID); err == nil || !strings.Contains(err.Error(), "terminal, not an agent") {
		t.Fatalf("killing a terminal as an agent = %v", err)
	}
	if _, err := h.sessions.Archive("", "nosuch01", true); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("archiving a missing session = %v", err)
	}
}
