package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// askAfterTurn files the request the way archive_self and kill_self do,
// from a session the pass then reads at rest.
func askAfterTurn(t *testing.T, m *Model, name, action string) store.Session {
	t.Helper()
	m.selectSessionRow(t, name)
	sess, _ := m.selected()
	if err := m.services.store.RequestAfterTurn(sess.ID, action, time.Now()); err != nil {
		t.Fatalf("RequestAfterTurn: %v", err)
	}
	return sess
}

// The archive takes the steps the a key takes: the agent and the shells
// nested under it stop, their last screens stay, and the rows leave the list.
func TestASessionAskingToBeArchivedGoesOnceItsTurnEnds(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	pressTerminalKey(t, m)
	shell := terminalSession(t, m)
	sess := askAfterTurn(t, m, "alpha", store.AfterTurnArchive)
	waitForPane(t, m, sess.ID, "last words")

	m.applyCmd(t, m.refreshCmd())
	for _, id := range []string{sess.ID, shell.ID} {
		got, err := m.services.store.Get(id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if !got.Archived || m.services.tmux.Exists(id) {
			t.Fatalf("%s: archived=%v running=%v, want archived and stopped", got.Name, got.Archived, m.services.tmux.Exists(id))
		}
	}
	got, _ := m.services.store.Get(sess.ID)
	if got.AfterTurn != "" {
		t.Fatalf("the request outlived its archive: %q", got.AfterTurn)
	}
	if snapshot, _ := m.services.store.Snapshot(sess.ID); !strings.Contains(snapshot, "last words") {
		t.Fatalf("the archived row lost its last screen: %q", snapshot)
	}
	if len(m.sessionRows()) != 0 {
		t.Fatalf("the active list still shows %v", m.sessionRows())
	}
}

func TestASessionAskingToBeKilledStaysListedDead(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := askAfterTurn(t, m, "alpha", store.AfterTurnKill)

	m.applyCmd(t, m.refreshCmd())
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if m.services.tmux.Exists(sess.ID) || got.Status != status.Dead || got.Archived || got.AfterTurn != "" {
		t.Fatalf("after its turn: running=%v status=%q archived=%v pending=%q", m.services.tmux.Exists(sess.ID), got.Status, got.Archived, got.AfterTurn)
	}
	if rows := m.sessionRows(); len(rows) != 1 || rows[0].Status != status.Dead {
		t.Fatalf("the killed row should stay in the list, dead: %+v", rows)
	}
}

// A report the Model reads after a new turn began must not end that turn.
func TestAStaleReportLeavesAWorkingSessionAlone(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := askAfterTurn(t, m, "alpha", store.AfterTurnKill)

	m.endAfterTurns([]string{sess.ID})
	m.drainEffects(t)
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !m.services.tmux.Exists(sess.ID) || got.AfterTurn != store.AfterTurnKill {
		t.Fatalf("a working session was ended: running=%v pending=%q", m.services.tmux.Exists(sess.ID), got.AfterTurn)
	}
}

// A restart or revive since the request means the user wants this agent
// running, so the request dies with the run that made it.
func TestARelaunchDropsAnEarlierRequest(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	sess, _ := m.selected()
	if err := m.services.store.RequestAfterTurn(sess.ID, store.AfterTurnArchive, sess.CreatedAt.Add(-time.Second)); err != nil {
		t.Fatalf("RequestAfterTurn: %v", err)
	}

	m.applyCmd(t, m.refreshCmd())
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Archived || !m.services.tmux.Exists(sess.ID) || got.AfterTurn != "" {
		t.Fatalf("a request older than the run: archived=%v running=%v pending=%q", got.Archived, m.services.tmux.Exists(sess.ID), got.AfterTurn)
	}
}

func TestTheCancelKeyKeepsTheSessionPastItsTurn(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := askAfterTurn(t, m, "alpha", store.AfterTurnKill)
	loadStoredRows(t, m)
	m.selectSessionRow(t, "alpha")

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m.drainEffects(t)
	if got, _ := m.services.store.Get(sess.ID); got.AfterTurn != "" {
		t.Fatalf("c left the kill pending: %q", got.AfterTurn)
	}
	if m.errBar.text != "alpha stays once its turn ends" {
		t.Fatalf("cancel notice = %q", m.errBar.text)
	}
	m.applyCmd(t, m.refreshCmd())
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("the session was killed after its kill was canceled")
	}
}

// The cancel runs on the effect lane: Update returns before the row is
// cleared, and the cleared row is the one the rail paints afterwards.
func TestCancelEndClearsTheRowThroughTheEffectLane(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := askAfterTurn(t, m, "alpha", store.AfterTurnArchive)
	loadStoredRows(t, m)
	m.selectSessionRow(t, "alpha")

	m.cancelEndSelected()
	if got, _ := m.services.store.Get(sess.ID); got.AfterTurn != store.AfterTurnArchive {
		t.Fatalf("cancel wrote on the Update path: pending=%q", got.AfterTurn)
	}
	m.drainEffects(t)
	if selected, _ := m.selected(); selected.AfterTurn != "" {
		t.Fatalf("the row still carries the request: %q", selected.AfterTurn)
	}
}
