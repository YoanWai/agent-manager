package sessioncmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

func TestSessionsKillKeepsTheScreenAndReviveBringsItBack(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker", Prompt: "hold the line"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "hold the line")

	killed, err := h.sessions.Kill(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if killed.Running || killed.Status != status.Dead {
		t.Fatalf("killed session = %+v", killed)
	}
	if h.driver.Exists(created.ID) {
		t.Fatal("killed session still has a pane")
	}
	screen, err := h.sessions.Read(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Read after kill: %v", err)
	}
	if !strings.Contains(screen.Output, "hold the line") {
		t.Fatalf("a killed session should keep its last screen, got %q", screen.Output)
	}

	revived, err := h.sessions.Revive(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Revive: %v", err)
	}
	if !revived.Running || !h.driver.Exists(created.ID) {
		t.Fatalf("revived session = %+v", revived)
	}
	if _, err := h.sessions.Kill(h.caller.ID, h.caller.ID); err == nil || !strings.Contains(err.Error(), "kill_self") {
		t.Fatalf("killing itself = %v, want kill_self named", err)
	}
}

func TestSessionsArchiveHidesAndRestores(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	archived, err := h.sessions.Archive(h.caller.ID, created.ID, true)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if !archived.Archived || !archived.Running {
		t.Fatalf("archiving must not stop the pane: %+v", archived)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil || !stored.Archived {
		t.Fatalf("stored archived = %+v err=%v", stored, err)
	}
	restored, err := h.sessions.Archive(h.caller.ID, created.ID, false)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Archived {
		t.Fatalf("restored session = %+v", restored)
	}
	if _, err := h.sessions.Archive(h.caller.ID, h.caller.ID, true); err == nil || !strings.Contains(err.Error(), "archive_self") {
		t.Fatalf("archiving itself = %v, want archive_self named", err)
	}
}

// An agent that exits leaves its window open on the shell it was launched
// from. Revive puts the tool back inside that pane instead of refusing the
// row as still running, which is what the manager's own revive key does.
func TestReviveRestartsTheAgentInsideItsLivePane(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker", Prompt: "hold the line"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "hold the line")
	waitForAgentGone(t, h.driver, created.ID)

	revived, err := h.sessions.Revive(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Revive: %v", err)
	}
	if !revived.Running || revived.Status != status.Starting {
		t.Fatalf("revived session = %+v", revived)
	}
	screen := waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "resumed")
	if !strings.Contains(screen.Output, "hold the line") {
		t.Fatalf("an in-pane revive keeps what the pane already held, got %q", screen.Output)
	}
	if !h.driver.Exists(created.ID) {
		t.Fatal("revive should have left the window running")
	}
}

func TestRevivePickerRecoveryCoversBothPanePaths(t *testing.T) {
	tests := []struct {
		name string
		tool string
		kill bool
	}{
		{"create pane", "picker", true},
		{"surviving pane", "picker-exit", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newSessionHarness(t)
			codexHome := t.TempDir()
			t.Setenv("CODEX_HOME", codexHome)
			if err := os.MkdirAll(filepath.Join(codexHome, "sessions"), 0o755); err != nil {
				t.Fatal(err)
			}
			created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "picker-worker", Tool: tc.tool})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if tc.kill {
				if _, err := h.sessions.Kill(h.caller.ID, created.ID); err != nil {
					t.Fatalf("Kill: %v", err)
				}
			} else {
				waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "initial")
				waitForAgentGone(t, h.driver, created.ID)
			}

			if _, err := h.sessions.Revive(h.caller.ID, created.ID); err != nil {
				t.Fatalf("Revive: %v", err)
			}
			waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "/sessions")
			stored, err := h.store.Get(created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.AgentLaunchedAt.IsZero() {
				t.Fatal("revive did not stamp its launch")
			}
			if stored.RelaunchSnapshot == nil || len(stored.RelaunchSnapshot) != 0 {
				t.Fatalf("relaunch snapshot = %v, want non-nil empty", stored.RelaunchSnapshot)
			}
		})
	}
}

func TestReviveRefusesWhileTheAgentIsStillRunning(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "chatty", Tool: "resting"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "❯")

	if _, err := h.sessions.Revive(h.caller.ID, created.ID); err == nil ||
		!strings.Contains(err.Error(), "still running") {
		t.Fatalf("reviving a session whose agent is up = %v", err)
	}
}

// The request arrives mid-turn, while the stored status can still be the
// last turn's rest; the row reads working with it, so only a rest the poll
// derives after this call can count as the turn ending.
func TestASessionAsksToEndItselfOnceItsTurnEnds(t *testing.T) {
	h := newSessionHarness(t)
	if err := h.store.UpdateStatus(h.caller.ID, status.Finished); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	asked, err := h.sessions.EndAfterTurn(h.caller.ID, store.AfterTurnArchive)
	if err != nil {
		t.Fatalf("EndAfterTurn: %v", err)
	}
	if asked.Pending != store.AfterTurnArchive || asked.Replaced != "" {
		t.Fatalf("asked = %+v", asked)
	}
	stored, err := h.store.Get(h.caller.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.AfterTurn != store.AfterTurnArchive || stored.AfterTurnAt.IsZero() || stored.Status != status.Working {
		t.Fatalf("stored = after %q at %v, status %q", stored.AfterTurn, stored.AfterTurnAt, stored.Status)
	}

	switched, err := h.sessions.EndAfterTurn(h.caller.ID, store.AfterTurnKill)
	if err != nil || switched.Pending != store.AfterTurnKill || switched.Replaced != store.AfterTurnArchive {
		t.Fatalf("switching to a kill = %+v, %v", switched, err)
	}

	canceled, err := h.sessions.CancelAfterTurn(h.caller.ID)
	if err != nil || canceled.Canceled != store.AfterTurnKill || canceled.Pending != "" {
		t.Fatalf("cancel = %+v, %v", canceled, err)
	}
	if stored, err := h.store.Get(h.caller.ID); err != nil || stored.AfterTurn != "" || !stored.AfterTurnAt.IsZero() {
		t.Fatalf("after cancel the row holds %q at %v, err %v", stored.AfterTurn, stored.AfterTurnAt, err)
	}
	if again, err := h.sessions.CancelAfterTurn(h.caller.ID); err != nil || again.Canceled != "" {
		t.Fatalf("canceling nothing = %+v, %v", again, err)
	}
	if FormatAfterTurn(AfterTurn{ManagerAwake: true}) != "no archive or kill was pending" {
		t.Fatalf("nothing pending reads %q", FormatAfterTurn(AfterTurn{ManagerAwake: true}))
	}
}

func TestATerminalHasNoTurnToEndItselfAfter(t *testing.T) {
	h := newSessionHarness(t)
	terminal, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	if _, err := h.sessions.EndAfterTurn(terminal.ID, store.AfterTurnKill); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("a terminal asking to be killed after its turn = %v", err)
	}
	if _, err := h.sessions.EndAfterTurn("", store.AfterTurnArchive); err == nil || !strings.Contains(err.Error(), "not inside an Agent Manager session") {
		t.Fatalf("no caller = %v, want the missing caller named", err)
	}
}
