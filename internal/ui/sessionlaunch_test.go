package ui

import (
	"testing"
	"time"
)

func TestAStaleRefreshKeepsASessionLaunchedAfterItWasListed(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())

	// What a poll hands the UI: the session list as it stood when the pass
	// opened, delivered once the pass has finished its tmux and ps calls.
	inFlight := m.poller.refreshOnce()

	if err := m.spawnSession("claude", "late-arrival", t.TempDir(), "", "", false, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	launched := m.sessionRows()
	if len(launched) != 1 {
		t.Fatalf("rows after the spawn = %d, want the one just launched", len(launched))
	}

	updated, _ := m.Update(inFlight)
	*m = *updated.(*Model)

	if sessionGone(m.sessions, launched[0].ID) {
		t.Fatalf("the stale poll took the launched session off screen, leaving %v", m.sessionRows())
	}
}

func TestAStaleRefreshKeepsALaunchListedWhileTmuxWasStartingIt(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())

	if err := m.spawnSession("claude", "slow-window", t.TempDir(), "", "", false, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	launched := m.sessionRows()[0]

	// Building the tmux window takes tens of milliseconds, so a poll can
	// list the store after the launch began and still be missing its row.
	updated, _ := m.Update(refreshMsg{listedAt: launched.CreatedAt.Add(time.Millisecond)})
	*m = *updated.(*Model)

	if sessionGone(m.sessions, launched.ID) {
		t.Fatalf("the stale poll took the launched session off screen, leaving %v", m.sessionRows())
	}
}
