package sessioncmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
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
	InjectPickerKeys(h.driver, h.caller.ID, config.Tool{InputPrefix: "[", ResumePickerKeys: "DO-NOT-SEND"})
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
	InjectPickerKeys(h.driver, goneID, config.Tool{InputPrefix: "NEVER-APPEARS", ResumePickerKeys: "/sessions"})
	time.Sleep(50 * time.Millisecond)
	if err := h.driver.Kill(goneID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(350 * time.Millisecond)
	if h.driver.Exists(goneID) {
		t.Fatal("picker injection kept a disappeared pane alive")
	}
}

// startPickerPane runs a stand-in for a TUI whose picker lives behind a
// composer command, and returns the pane's id.
func startPickerPane(t *testing.T, h *sessionHarness, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tui.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()[:8]
	if err := h.driver.Create(id, h.caller.Cwd, "bash "+path, nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.driver.Kill(id) })
	return id
}

func waitForPaneText(t *testing.T, driver *tmux.Driver, id, text string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var pane string
	for time.Now().Before(deadline) {
		captured, err := driver.CapturePane(id)
		if err != nil {
			t.Fatal(err)
		}
		if pane = ansi.Strip(captured); strings.Contains(pane, text) {
			return pane
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("pane never showed %q:\n%s", text, pane)
	return ""
}

// agy leaves its composer on screen after it exits, so a revive into that
// pane must wait for the caret, not the marker, and its picker opens on the
// first Enter, where a second one would resume its top row.
func TestInjectPickerKeysWaitsForTheCaretAndStopsOnceThePickerOpens(t *testing.T) {
	h := newSessionHarness(t)
	id := startPickerPane(t, h, `printf '>\n----\n'
IFS= read -r -t 2 early && printf 'EARLY %s\n' "$early"
printf '> '
IFS= read -r keys
printf '\npicker for %s\n' "$keys"
IFS= read -r -t 2 picked && printf 'PICKED TOP ROW\n'
printf 'picker still open\n'
sleep 30
`)
	InjectPickerKeys(h.driver, id, config.Tool{ActivityCutoff: "(?m)^[>!]", ResumePickerKeys: "/resume"})
	pane := waitForPaneText(t, h.driver, id, "picker still open")
	if strings.Contains(pane, "EARLY") {
		t.Fatalf("keys went to a stale composer:\n%s", pane)
	}
	if !strings.Contains(pane, "picker for /resume") {
		t.Fatalf("keys never reached the live composer:\n%s", pane)
	}
	if strings.Contains(pane, "PICKED TOP ROW") {
		t.Fatalf("an extra Enter picked the picker's top row:\n%s", pane)
	}
}

func TestInjectPickerKeysWaitsOutAPickerSlowToDraw(t *testing.T) {
	h := newSessionHarness(t)
	id := startPickerPane(t, h, `printf '> '
IFS= read -r keys
printf '> %s' "$keys"
sleep 0.8
printf '\npicker for %s\n' "$keys"
IFS= read -r -t 3 picked && printf 'PICKED TOP ROW\n'
printf 'picker still open\n'
sleep 30
`)
	InjectPickerKeys(h.driver, id, config.Tool{ActivityCutoff: "(?m)^[>!]", ResumePickerKeys: "/resume"})
	if pane := waitForPaneText(t, h.driver, id, "picker still open"); strings.Contains(pane, "PICKED TOP ROW") {
		t.Fatalf("an extra Enter picked the top row of a picker that drew late:\n%s", pane)
	}
}

func TestInjectPickerKeysConfirmsACompletionTheComposerKept(t *testing.T) {
	h := newSessionHarness(t)
	id := startPickerPane(t, h, `printf '> '
IFS= read -r keys
printf '> %s' "$keys"
IFS= read -r -t 3 again && printf '\nOPENED %s\n' "$keys"
sleep 30
`)
	InjectPickerKeys(h.driver, id, config.Tool{InputPrefix: "^>", ResumePickerKeys: "/sessions"})
	waitForPaneText(t, h.driver, id, "OPENED /sessions")
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

// The caller's copy of the row can predate a switch the agent reported
// before it quit, so the relaunch reads the row itself.
func TestRelaunchInPaneResumesTheStoredConversationUnderItsStampedLaunch(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "switcher", Tool: "reporter"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "launched launch=0")
	waitForAgentGone(t, h.driver, created.ID)
	if result, err := h.store.ReportAgentSessionID(created.ID, "reporter", "conv-switched", 0); err != nil || result != store.ReportAdopted {
		t.Fatalf("report: %v %v", result, err)
	}
	tool := testConfigTool(t, "reporter")
	relaunched, err := RelaunchInPane(h.driver, h.store, hooks.NewManager(t.TempDir()), created.ID, tool)
	if err != nil {
		t.Fatalf("RelaunchInPane: %v", err)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if relaunched.AgentSessionID != "conv-switched" || !relaunched.AgentLaunchedAt.Equal(stored.AgentLaunchedAt) || stored.AgentLaunchedAt.IsZero() {
		t.Fatalf("relaunched %+v, stored %+v", relaunched, stored)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, fmt.Sprintf("resumed conv-switched launch=%d", store.LaunchStamp(stored.AgentLaunchedAt)))
}

func testConfigTool(t *testing.T, name string) config.Tool {
	t.Helper()
	cfg, err := testConfigLoader(t, sessionConfig)()
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Tools[name]
}
