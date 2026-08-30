package sessioncmd

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

func TestSnapshotRelaunchClearsUnavailableAndSkipsKnownIDs(t *testing.T) {
	h := newSessionHarness(t)
	tool := config.Tool{SessionStore: "codex"}
	if err := h.store.SetRelaunchSnapshot(h.caller.ID, map[string]int64{"stale": 1}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", t.TempDir())
	if err := SnapshotRelaunch(h.store, h.caller, tool, ""); err != nil {
		t.Fatal(err)
	}
	got, err := h.store.Get(h.caller.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RelaunchSnapshot != nil {
		t.Fatalf("unavailable store left snapshot %v", got.RelaunchSnapshot)
	}

	if err := h.store.SetRelaunchSnapshot(h.caller.ID, map[string]int64{"keep": 2}); err != nil {
		t.Fatal(err)
	}
	if err := SnapshotRelaunch(h.store, h.caller, tool, "known-id"); err != nil {
		t.Fatal(err)
	}
	got, err = h.store.Get(h.caller.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RelaunchSnapshot["keep"] != 2 {
		t.Fatalf("known-id relaunch changed snapshot to %v", got.RelaunchSnapshot)
	}
}

func TestInjectPickerKeysStopsOnInvalidInputAndPaneDisappearance(t *testing.T) {
	h := newSessionHarness(t)
	InjectPickerKeys(h.driver, h.caller.ID, "[", "DO-NOT-SEND")
	time.Sleep(100 * time.Millisecond)
	pane, err := h.driver.CapturePane(h.caller.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pane, "DO-NOT-SEND") {
		t.Fatal("invalid composer pattern still injected keys")
	}

	goneID := "gonepane"
	if err := h.driver.Create(goneID, filepath.Dir(h.caller.Cwd), "cat", nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	InjectPickerKeys(h.driver, goneID, "NEVER-APPEARS", "/sessions")
	time.Sleep(50 * time.Millisecond)
	if err := h.driver.Kill(goneID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond)
	if h.driver.Exists(goneID) {
		t.Fatal("picker injection kept a disappeared pane alive")
	}
}

// waitForAgentGone waits for the pane to hold nothing but its shell, and
// for that to still be true on a second reading: the launch script's own
// exit leaves a process visible for a moment after the agent is done.
func waitForAgentGone(t *testing.T, driver *tmux.Driver, sessID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	quiet := 0
	for time.Now().Before(deadline) {
		running, err := AgentRunning(driver, sessID)
		if err == nil && !running {
			quiet++
			if quiet == 2 {
				return
			}
			time.Sleep(25 * time.Millisecond)
			continue
		}
		quiet = 0
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("session %s never came back to its shell", sessID)
}
