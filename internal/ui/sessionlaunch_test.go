package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
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

// pressRune sends one key through Update the way the terminal would.
func (m *Model) pressRune(t *testing.T, r rune) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	*m = *updated.(*Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			updated, _ = m.Update(msg)
			*m = *updated.(*Model)
		}
	}
}

// staleRefreshAfter is the refresh a poll pass that opened before the launch
// would deliver: it carries no sessions and a listing time from before them.
func staleRefreshAfter(m *Model) refreshMsg {
	earliest := time.Now()
	for _, at := range m.launched {
		if at.Before(earliest) {
			earliest = at
		}
	}
	return refreshMsg{listedAt: earliest.Add(-time.Millisecond)}
}

func TestAStaleRefreshDoesNotBringBackASessionJustDeleted(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	if err := m.spawnSession("claude", "doomed", t.TempDir(), "", "", false, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess := m.sessionRows()[0]

	m.selectSessionRow(t, sess.Name)
	m.pressRune(t, 'd')
	m.pressRune(t, 'y')
	if _, err := m.store.Get(sess.ID); err == nil {
		t.Fatal("the delete did not reach the store")
	}

	updated, _ := m.Update(staleRefreshAfter(m))
	*m = *updated.(*Model)

	if !sessionGone(m.sessions, sess.ID) {
		t.Fatalf("a deleted session came back on a stale poll: %v", m.sessionRows())
	}
}

func TestAStaleRefreshDoesNotBringBackASessionJustArchived(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	if err := m.spawnSession("claude", "shelved", t.TempDir(), "", "", false, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess := m.sessionRows()[0]

	m.selectSessionRow(t, sess.Name)
	m.pressRune(t, 'a')
	m.pressRune(t, 'y')

	updated, _ := m.Update(staleRefreshAfter(m))
	*m = *updated.(*Model)

	for _, row := range m.sessionRows() {
		if row.ID == sess.ID {
			t.Fatalf("an archived session came back on the live tree as %q", row.Status)
		}
	}
}

// A poll that listed the store before the delete delivers its full list
// afterwards; that stale copy must not put the row back on screen.
func TestAStalePollCannotResurrectADeletedRow(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "doomed", t.TempDir(), "")
	sess := m.sessionRows()[0]
	listedAt := time.Now()

	deleteSession(t, m, "doomed")

	stale := sess
	stale.Status = status.Dead
	updated, _ := m.Update(refreshMsg{sessions: []store.Session{stale}, listedAt: listedAt})
	*m = *updated.(*Model)

	for _, row := range m.sessionRows() {
		if row.ID == sess.ID {
			t.Fatalf("a stale poll brought the deleted row back as %q", row.Status)
		}
	}
}

func TestAStalePollCannotBringAnArchivedRowBackLive(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "shelved", t.TempDir(), "")
	sess := m.sessionRows()[0]
	listedAt := time.Now()

	m.selectSessionRow(t, "shelved")
	m.archiveSelected()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	stale := sess
	stale.Status = status.Dead
	updated, _ := m.Update(refreshMsg{sessions: []store.Session{stale}, listedAt: listedAt})
	*m = *updated.(*Model)

	for _, row := range m.sessionRows() {
		if row.ID == sess.ID {
			t.Fatalf("a stale poll brought the archived row back on the live tree")
		}
	}
}

func TestAFreshListingRetiresDeletionMarkers(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "doomed", t.TempDir(), "")
	listedAt := time.Now()

	deleteSession(t, m, "doomed")

	fresh := refreshMsg{listedAt: listedAt.Add(time.Second)}
	updated, _ := m.Update(fresh)
	*m = *updated.(*Model)

	if len(m.gone) != 0 {
		t.Fatalf("deletion markers outlived a listing that postdates them: %v", m.gone)
	}
}

func TestAStalePollCannotRestoreADeletedGroupHeader(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("zone", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "in-zone", dir, "zone")
	sess := m.sessionRows()[0]
	listedAt := time.Now()

	m.selectGroupRow(t, "zone")
	m.prepareDelete()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	stale := refreshMsg{
		sessions:   []store.Session{sess},
		listedAt:   listedAt,
		groups:     []string{"zone"},
		groupPaths: map[string]string{"zone": dir},
	}
	updated, _ := m.Update(stale)
	*m = *updated.(*Model)

	for _, r := range m.rows {
		if r.isGroup && r.group == "zone" {
			t.Fatalf("a stale poll brought the deleted group header back")
		}
	}
	for _, g := range m.groups {
		if g == "zone" {
			t.Fatal("a stale poll restored the deleted group path")
		}
	}
}
