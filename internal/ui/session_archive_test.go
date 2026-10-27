package ui

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestArchiveConfirmationUsesLoadedSessionTree(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")
	if err := m.services.store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	m.archiveSelected()
	if m.mode != modeConfirmDelete || len(m.confirm.sessions) != 1 {
		t.Fatalf("archive confirmation depended on the closed store: mode=%v err=%q target=%+v", m.mode, m.errBar.text, m.confirm.sessions)
	}
}

func TestArchiveWorkerRejectsChildAddedAfterConfirmation(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")
	agent, _ := m.selected()
	m.archiveSelected()
	late := store.Session{
		ID: newID(), Name: "late-terminal", Tool: shellToolName, Cwd: dir,
		ParentID: agent.ID, Status: status.Dead,
	}
	if err := m.services.store.CreateSession(late); err != nil {
		t.Fatalf("create late child: %v", err)
	}

	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.applyCmd(t, cmd)
	if !strings.Contains(m.errBar.text, "sessions changed since confirmation") {
		t.Fatalf("membership change error = %q", m.errBar.text)
	}
	stored, err := m.services.store.Get(agent.ID)
	if err != nil || stored.Archived {
		t.Fatalf("stale confirmation archived root: archived=%v err=%v", stored.Archived, err)
	}
	if !m.services.tmux.Exists(agent.ID) {
		t.Fatal("stale confirmation killed the root pane")
	}
}

func TestArchiveSelectedNoopInArchivedView(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()

	createSession(t, m, "alpha", dir, "")
	m.selectSessionRow(t, "alpha")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "alpha")
	m.archiveSelected()
	if m.mode == modeConfirmDelete {
		t.Fatal("archiveSelected should not open a confirm dialog in the archived view")
	}

	if err := m.services.store.CreateGroup("zone", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.rail.SetArchived(false)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "beta", dir, "zone")
	if err := m.services.store.SetArchived(m.sessionRows()[0].ID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "zone")
	m.archiveSelected()
	if m.mode == modeConfirmDelete {
		t.Fatal("archiveSelected should not open a confirm dialog for a group row in the archived view")
	}
}

func TestRestoreSelectedNoopInActiveView(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	seedGroups(t, m, "zone")
	_, sleeper, stash := seedRestoreScenario(t, m, "zone")
	press := func(key string) {
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m.drainEffects(t)
	}

	m.selectGroupRow(t, "zone")
	press("u")
	if m.mode == modeConfirmDelete {
		t.Errorf("u on a group opened %q in the active view", m.confirm.label)
	}
	press("y")
	if m.services.tmux.Exists(sleeper.ID) {
		t.Fatal("sleeper was killed on purpose and came back")
	}
	if got, err := m.services.store.Get(stash.ID); err != nil || !got.Archived || m.services.tmux.Exists(stash.ID) {
		t.Fatalf("stash left the archive: archived=%v err=%v", got.Archived, err)
	}

	m.selectSessionRow(t, "live")
	press("u")
	if m.mode == modeConfirmDelete {
		t.Fatalf("u on a session opened %q in the active view", m.confirm.label)
	}
}

func TestRestoreGroupBringsBackOnlyItsArchivedSessions(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	seedGroups(t, m, "zone")
	live, sleeper, stash := seedRestoreScenario(t, m, "zone")

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "zone")
	m.restoreSelected()
	if len(m.confirm.sessions) != 1 || m.confirm.sessions[0].ID != stash.ID {
		t.Errorf("restore targets %d sessions, want stash alone", len(m.confirm.sessions))
	}
	if want := "restore whole group zone (1 archived sessions)? includes sessions hidden by filters."; m.confirm.label != want {
		t.Errorf("label = %q, want %q", m.confirm.label, want)
	}
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	if m.services.tmux.Exists(sleeper.ID) {
		t.Fatal("the group restore revived sleeper, which was never archived")
	}
	if !m.services.tmux.Exists(live.ID) || !m.services.tmux.Exists(stash.ID) {
		t.Fatal("live and stash should both be running")
	}
	if got, err := m.services.store.Get(stash.ID); err != nil || got.Archived {
		t.Fatalf("stash is still filed as archived: archived=%v err=%v", got.Archived, err)
	}
	m.rail.SetArchived(false)
	m.applyCmd(t, m.refreshCmd())
	if names := strings.Join(sessionNames(m), " "); names != "live sleeper stash" {
		t.Fatalf("active view = %q, want all of zone back", names)
	}
}

func TestArchiveAndRestoreRefuseTheRootRow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		action, key, next string
		archived          bool
	}{
		{"archive", "a", "live", false},
		{"restore", "u", "stash", true},
	} {
		t.Run(tc.action, func(t *testing.T) {
			m := buildModel(t)
			live, sleeper, stash := seedRestoreScenario(t, m, rootGroup)
			m.rail.SetArchived(tc.archived)
			m.applyCmd(t, m.refreshCmd())
			m.selectGroupRow(t, rootGroup)

			m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)})
			m.drainEffects(t)
			if want := "root is the top level, not a group to " + tc.action; m.mode != modeList || m.errBar.text != want {
				t.Errorf("mode = %v, errBar = %q, want the list and %q", m.mode, m.errBar.text, want)
			}
			m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
			m.drainEffects(t)
			if !m.services.tmux.Exists(live.ID) || m.services.tmux.Exists(sleeper.ID) || m.services.tmux.Exists(stash.ID) {
				t.Fatalf("%s on root touched its sessions", tc.action)
			}
			if got, err := m.services.store.Get(stash.ID); err != nil || !got.Archived {
				t.Fatalf("stash left the archive: archived=%v err=%v", got.Archived, err)
			}

			m.selectSessionRow(t, tc.next)
			m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.key)})
			m.drainEffects(t)
			if card := ansi.Strip(preparedView(m)); !strings.Contains(card, tc.action+" "+tc.next+"?") || strings.Contains(card, "root is the top level") {
				t.Errorf("the %s card on %s should drop the root refusal:\n%s", tc.action, tc.next, card)
			}
		})
	}
}

func TestArchiveRestoreClearStaleError(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "alpha", dir, "")

	m.selectSessionRow(t, "alpha")
	m.errBar.text = "stale failure from an earlier action"
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("archive should clear the stale error, err = %q", m.errBar.text)
	}

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "alpha")
	m.errBar.text = "stale failure from an earlier action"
	m.restoreSelected()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("restore should clear the stale error, err = %q", m.errBar.text)
	}
}

func TestRestoreKeepsArchiveWhenReviveFails(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "homeless", dir, "")
	sess := m.sessionRows()[0]

	m.selectSessionRow(t, "homeless")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "homeless")
	m.restoreSelected()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.errBar.text == "" {
		t.Fatal("restore without a working directory should error")
	}
	if m.services.tmux.Exists(sess.ID) {
		t.Fatal("failed restore must not leave a tmux session")
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Archived {
		t.Fatal("failed restore must leave the session archived")
	}
}

func TestArchiveAbortsWhenSnapshotFails(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "alpha", dir, "")
	m.services.setSnapshot = func(id, snapshot string) error {
		return errors.New("disk full")
	}

	m.selectSessionRow(t, "alpha")
	// Archive snapshots only a pane that has drawn.
	waitForPreview(t, m)
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	if m.errBar.text != "disk full" {
		t.Fatalf("snapshot failure should surface, err = %q", m.errBar.text)
	}
	if len(m.sessionRows()) != 1 {
		t.Fatalf("failed snapshot must not archive, active sessions = %d want 1", len(m.sessionRows()))
	}
	if !m.services.tmux.Exists(m.sessionRows()[0].ID) {
		t.Fatal("failed snapshot must not kill the tmux session")
	}
	active, err := m.services.store.ListSessions(false)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(active) != 1 || active[0].Archived {
		t.Fatalf("session should stay unarchived in the store, got %+v", active)
	}
}

func TestArchiveGroupMovesWholeSubtree(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("proj", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := m.services.store.CreateGroup("proj/sub", ""); err != nil {
		t.Fatalf("create subgroup: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "top", dir, "proj")
	createSession(t, m, "deep", dir, "proj/sub")

	m.selectGroupRow(t, "proj")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	if paths := m.groupRowPaths(); len(paths) != 0 {
		t.Fatalf("active view still shows group rows %v", paths)
	}
	if names := sessionNames(m); len(names) != 0 {
		t.Fatalf("active view still shows sessions %v", names)
	}

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	gotGroups := m.groupRowPaths()
	if len(gotGroups) != 2 || gotGroups[0] != "proj" || gotGroups[1] != "proj/sub" {
		t.Fatalf("archived view groups = %v want [proj proj/sub]", gotGroups)
	}
	if names := sessionNames(m); len(names) != 2 {
		t.Fatalf("archived view sessions = %v want 2", names)
	}
	for _, sess := range m.sessionRows() {
		if m.services.tmux.Exists(sess.ID) {
			t.Fatalf("archived session %s should be killed", sess.Name)
		}
	}

	m.selectGroupRow(t, "proj")
	archived := m.sessionRows()
	m.restoreSelected()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	for _, sess := range archived {
		stored, err := m.services.store.Get(sess.ID)
		if err != nil {
			t.Fatalf("get %s: %v", sess.Name, err)
		}
		if stored.Status != status.Starting {
			t.Fatalf("after restore, %s status = %q want %q", sess.Name, stored.Status, status.Starting)
		}
		if stored.LaunchTime().Equal(stored.CreatedAt) {
			t.Fatalf("after restore, %s launch time was not refreshed", sess.Name)
		}
	}
	m.applyCmd(t, cmd)
	m.rail.SetArchived(false)
	m.applyCmd(t, m.refreshCmd())
	if paths := m.groupRowPaths(); len(paths) != 2 {
		t.Fatalf("after restore, active groups = %v want 2", paths)
	}
	if names := sessionNames(m); len(names) != 2 {
		t.Fatalf("after restore, active sessions = %v want 2", names)
	}
	for _, sess := range m.sessionRows() {
		if !m.services.tmux.Exists(sess.ID) {
			t.Fatalf("restore should revive %s", sess.Name)
		}
	}
}

func TestArchiveGroupKeepsEmptyGroupInArchivedView(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("empty", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectGroupRow(t, "empty")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	if paths := m.groupRowPaths(); len(paths) != 0 {
		t.Fatalf("archived empty group still in active view: %v", paths)
	}

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	if paths := m.groupRowPaths(); len(paths) != 1 || paths[0] != "empty" {
		t.Fatalf("archived view groups = %v want [empty]", paths)
	}
}

// Archiving must freeze the pane as a stored snapshot, and the poller must
// keep serving it instead of wiping the preview on the next tick.
func TestArchivedSessionKeepsPaneSnapshot(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "frozen", t.TempDir(), "")
	m.selectSessionRow(t, "frozen")
	sess := m.sessionRows()[0]

	if err := m.services.tmux.SendText(sess.ID, "snapshot-marker"); err != nil {
		t.Fatalf("send text: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := m.services.tmux.CapturePane(sess.ID)
		if err == nil && strings.Contains(pane, "snapshot-marker") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane never showed the marker, last capture: %q", pane)
		}
		time.Sleep(100 * time.Millisecond)
	}

	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	snapshot, err := m.services.store.Snapshot(sess.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !strings.Contains(snapshot, "snapshot-marker") {
		t.Fatalf("archive should persist the pane, snapshot = %q", snapshot)
	}
	if m.services.tmux.Exists(sess.ID) {
		t.Fatal("archive should kill the tmux session")
	}

	m.rail.SetArchived(true)
	m.applyCmd(t, nil)
	m.selectSessionRow(t, "frozen")
	m.applyCmd(t, nil)
	if !strings.Contains(m.workspace.preview, "snapshot-marker") {
		t.Fatalf("archived preview should survive the poll tick, preview = %q", m.workspace.preview)
	}

	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.workspace.preview = ""
	m.applyCmd(t, nil)
	if !strings.Contains(m.workspace.preview, "snapshot-marker") {
		t.Fatalf("archived preview should show the snapshot after tmux is gone, preview = %q", m.workspace.preview)
	}

	m.workspace.preview = ""
	gen := m.focus.pane.MovePreview()
	m.applyCmd(t, m.previewCmd(railSelectedSession(m), gen))
	if !strings.Contains(m.workspace.preview, "snapshot-marker") {
		t.Fatalf("previewCmd should serve the snapshot for an archived session, preview = %q", m.workspace.preview)
	}
}

func TestArchiveAgentPersistsEveryChild(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	child, err := m.services.store.Get(shell.ID)
	if err != nil || !child.Archived {
		t.Fatalf("child archived=%v err=%v", child.Archived, err)
	}
}

func TestRestoreAgentUnarchivesEveryChild(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	agent := m.sessionRows()[0]
	m.selectSessionRow(t, "coder")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "coder")
	m.restoreSelected()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	for _, id := range []string{agent.ID, shell.ID} {
		got, err := m.services.store.Get(id)
		if err != nil || got.Archived {
			t.Fatalf("%s archived=%v err=%v", id, got.Archived, err)
		}
		if !m.services.tmux.Exists(id) {
			t.Fatalf("%s not running", id)
		}
	}
}

func TestRestoreAgentBringsBackOnlyItsArchivedTerminals(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	agent := m.sessionRows()[0]
	m.selectSessionRow(t, "coder")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, shell.Name)
	m.restoreSelected()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if err := m.services.tmux.Kill(shell.ID); err != nil {
		t.Fatalf("kill terminal: %v", err)
	}

	m.selectSessionRow(t, "coder")
	m.restoreSelected()
	if want := "restore coder? brings it back."; m.confirm.label != want {
		t.Errorf("label = %q, want %q", m.confirm.label, want)
	}
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if !m.services.tmux.Exists(agent.ID) || m.services.tmux.Exists(shell.ID) {
		t.Fatalf("coder running=%v, terminal running=%v; want coder back and the terminal left dead",
			m.services.tmux.Exists(agent.ID), m.services.tmux.Exists(shell.ID))
	}
}

func TestConfirmedArchiveLeavesTheActiveViewAtOnce(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "shelved", t.TempDir(), "")
	sess := m.sessionRows()[0]

	m.selectSessionRow(t, "shelved")
	m.archiveSelected()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)

	if _, err := m.services.store.Get(sess.ID); err != nil {
		t.Fatalf("archive did not reach the store: %v", err)
	}
	for _, row := range m.sessionRows() {
		if row.ID == sess.ID {
			t.Fatalf("archived session still on the active tree before the next poll, status %q", row.Status)
		}
	}

	// The archived view still holds it once a fresh listing has run.
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	if len(m.sessionRows()) != 1 || !m.sessionRows()[0].Archived {
		t.Fatalf("archived session should show in the archived view, rows = %v", sessionNames(m))
	}
}

func TestConfirmedRestoreLeavesTheArchivedViewAtOnce(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "returning", t.TempDir(), "")
	sess := m.sessionRows()[0]

	m.selectSessionRow(t, "returning")
	m.archiveSelected()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "returning")
	m.restoreSelected()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)

	for _, row := range m.sessionRows() {
		if row.ID == sess.ID {
			t.Fatalf("restored session stayed in the archived view before the next poll")
		}
	}
	if _, err := m.services.store.Get(sess.ID); err != nil {
		t.Fatalf("restore did not reach the store: %v", err)
	}

	// The active view takes it back without waiting for a poll.
	m.rail.SetArchived(false)
	if got := m.visibleSessions(); len(got) != 1 || got[0].ID != sess.ID {
		t.Fatalf("active view after restore = %v", got)
	}
}

func TestConfirmedGroupRestoreShowsTheSubtreeAtOnce(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("zone", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "in-zone", dir, "zone")
	sess := m.sessionRows()[0]

	m.selectGroupRow(t, "zone")
	m.archiveSelected()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "zone")
	m.restoreSelected()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)

	for _, row := range m.sessionRows() {
		if row.ID == sess.ID {
			t.Fatalf("restored session stayed in the archived view before the next poll")
		}
	}
	m.rail.SetArchived(false)
	if m.groupEffectivelyArchived("zone") {
		t.Fatal("restored group still reads as archived before the next poll")
	}
	got := m.visibleSessions()
	if len(got) != 1 || got[0].ID != sess.ID {
		t.Fatalf("active view after group restore = %v", got)
	}
}

func TestConfirmedGroupArchiveHidesTheSubtreeAtOnce(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("zone", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "in-zone", dir, "zone")
	sess := m.sessionRows()[0]

	m.selectGroupRow(t, "zone")
	m.archiveSelected()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)

	if !m.groupEffectivelyArchived("zone") {
		t.Fatal("archived group still reads as active before the next poll")
	}
	for _, got := range m.visibleSessions() {
		if got.ID == sess.ID {
			t.Fatalf("archived session still on the active tree before the next poll")
		}
	}
}

func TestRestoreArchivedGroupRecoversLegacyHiddenRowsWithoutRevivingDead(t *testing.T) {
	m := buildModel(t)
	seedGroups(t, m, "legacy/deep")
	if err := m.services.store.SetGroupArchived("legacy", true); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	live := store.Session{ID: newID(), Name: "hidden-live", Tool: "claude", Cwd: dir, Group: "legacy/deep", Status: status.Idle}
	dead := store.Session{ID: newID(), Name: "hidden-dead", Tool: "claude", Cwd: dir, Group: "legacy/deep", Status: status.Dead}
	stash := store.Session{ID: newID(), Name: "archived", Tool: "claude", Cwd: dir, Group: "legacy/deep", Status: status.Dead, Archived: true}
	// Legacy rows bypassed active placement validation when they were created.
	for _, sess := range []store.Session{live, dead, stash} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.services.tmux.Create(live.ID, dir, m.services.cfg.Tools[live.Tool].Command, nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	if m.focusSession(live.ID) || m.focusSession(dead.ID) {
		t.Fatal("legacy hidden rows unexpectedly listed in active view")
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "legacy")
	m.restoreSelected()
	if len(m.confirm.sessions) != 1 || m.confirm.sessions[0].ID != stash.ID {
		t.Fatalf("restore targets=%+v", m.confirm.sessions)
	}
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	m.drainEffects(t)
	if !m.services.tmux.Exists(live.ID) || !m.services.tmux.Exists(stash.ID) || m.services.tmux.Exists(dead.ID) {
		t.Fatalf("live=%v stash=%v dead=%v err=%s", m.services.tmux.Exists(live.ID), m.services.tmux.Exists(stash.ID), m.services.tmux.Exists(dead.ID), m.errBar.text)
	}
	m.rail.SetArchived(false)
	m.applyCmd(t, m.refreshCmd())
	for _, sess := range []store.Session{live, dead, stash} {
		if !m.focusSession(sess.ID) {
			t.Fatalf("restored group still hides %s", sess.Name)
		}
	}
}

func TestNestedGroupRestoreIsVisibleAcrossPolls(t *testing.T) {
	m := buildModel(t)
	seedGroups(t, m, "a/b/deep", "a/sibling", "outside")
	restored := store.Session{ID: newID(), Name: "restored", Tool: "claude", Cwd: t.TempDir(), Group: "a/b", Status: status.Dead}
	live := store.Session{ID: newID(), Name: "already-live", Tool: "claude", Cwd: t.TempDir(), Group: "a/b/deep", Status: status.Idle}
	parent := store.Session{ID: newID(), Name: "parent-stash", Tool: "claude", Cwd: t.TempDir(), Group: "a", Status: status.Dead}
	sibling := store.Session{ID: newID(), Name: "sibling-stash", Tool: "claude", Cwd: t.TempDir(), Group: "a/sibling", Status: status.Dead}
	sleeper := store.Session{ID: newID(), Name: "outside-dead", Tool: "claude", Cwd: t.TempDir(), Group: "outside", Status: status.Dead}
	for _, sess := range []store.Session{restored, live, parent, sibling, sleeper} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.services.store.SetGroupArchived("a", true); err != nil {
		t.Fatal(err)
	}
	if err := m.services.tmux.Create(live.ID, live.Cwd, m.services.cfg.Tools[live.Tool].Command, nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	beforePID, err := m.services.tmux.PanePID(live.ID)
	if err != nil {
		t.Fatal(err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "a/b")
	stalePoll := m.refreshCmd()()
	staleAt := time.Now()
	m.restoreSelected()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	if !m.services.tmux.Exists(restored.ID) {
		t.Fatalf("restore did not launch: %s", m.errBar.text)
	}
	if pid, err := m.services.tmux.PanePID(live.ID); err != nil || pid != beforePID {
		t.Fatalf("live restore replaced pane: %d %v", pid, err)
	}
	m.applyTestMsg(t, stalePoll)
	m.rail.SetArchived(false)
	for _, sess := range []store.Session{restored, live} {
		if !m.focusSession(sess.ID) {
			t.Fatalf("restored row invisible before poll: %s", sess.Name)
		}
	}
	stale := refreshMsg{listedAt: staleAt, archivedGroups: map[string]bool{"a": true, "a/b": true, "a/b/deep": true, "a/sibling": true}}
	stripDeletedGroups(&stale, m.ledger.goneGroups)
	if stale.archivedGroups["a"] || stale.archivedGroups["a/b"] || stale.archivedGroups["a/b/deep"] || !stale.archivedGroups["a/sibling"] {
		t.Fatalf("stale poll restored archive flags: %+v", stale.archivedGroups)
	}
	for poll := 0; poll < 3; poll++ {
		m.applyCmd(t, m.refreshCmd())
		for _, sess := range []store.Session{restored, live} {
			if !m.focusSession(sess.ID) {
				t.Fatalf("restored row invisible after poll %d: %s", poll, sess.Name)
			}
		}
	}
	m.rail.SetArchived(true)
	for _, sess := range []store.Session{parent, sibling} {
		if !m.focusSession(sess.ID) || m.services.tmux.Exists(sess.ID) {
			t.Fatalf("unrelated archived row changed: %s", sess.Name)
		}
	}
	if m.services.tmux.Exists(sleeper.ID) {
		t.Fatal("restore revived a deliberately killed active session")
	}
	fresh := refreshMsg{listedAt: time.Now().Add(time.Second)}
	stripDeletedGroups(&fresh, m.ledger.goneGroups)
	if len(m.ledger.goneGroups) != 0 {
		t.Fatalf("new poll did not retire group ledger: %+v", m.ledger.goneGroups)
	}
}

func TestRestoreEmptyNestedGroupClearsAncestors(t *testing.T) {
	m := buildModel(t)
	seedGroups(t, m, "a", "a/b")
	if err := m.services.store.SetGroupArchived("a", true); err != nil {
		t.Fatal(err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "a/b")
	m.restoreSelected()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.rail.SetArchived(false)
	if m.groupEffectivelyArchived("a/b") {
		t.Fatal("empty subgroup restore left archived ancestry")
	}
	m.selectGroupRow(t, "a/b")
}

func TestWholeGroupArchiveRestoreIncludesFilteredAndArchivedRows(t *testing.T) {
	m := buildModel(t)
	seedGroups(t, m, "zone")
	for _, name := range []string{"match", "hidden", "stash"} {
		createSession(t, m, name, t.TempDir(), "zone")
	}
	var rows []store.Session
	for _, sess := range m.workspace.sessions {
		if sess.Group == "zone" {
			rows = append(rows, sess)
			if sess.Name == "stash" {
				if err := m.services.store.SetArchived(sess.ID, true); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	m.applyCmd(t, m.refreshCmd())
	m.rail.SetSearch("match", false)
	m.selectGroupRow(t, "zone")
	m.archiveSelected()
	if len(m.confirm.sessions) != 3 || !strings.Contains(m.confirm.label, "whole group zone (3 sessions, 1 already archived)") {
		t.Fatalf("archive scope: %s %+v", m.confirm.label, m.confirm.sessions)
	}
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	for _, sess := range rows {
		stored, err := m.services.store.Get(sess.ID)
		if err != nil || !stored.Archived || m.services.tmux.Exists(sess.ID) {
			t.Fatalf("archive omitted row: %+v %v", stored, err)
		}
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.rail.SetSearch("match", false)
	m.selectGroupRow(t, "zone")
	m.restoreSelected()
	if len(m.confirm.sessions) != 3 || !strings.Contains(m.confirm.label, "whole group zone (3 archived sessions)") {
		t.Fatalf("restore scope: %s %+v", m.confirm.label, m.confirm.sessions)
	}
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	for _, sess := range rows {
		stored, err := m.services.store.Get(sess.ID)
		if err != nil || stored.Archived || !m.services.tmux.Exists(sess.ID) {
			t.Fatalf("restore omitted row: %+v %v", stored, err)
		}
	}
}

func TestLivePublicArchiveRestoresWithoutRelaunchBeforeKill(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "live", t.TempDir(), "")
	sess := m.workspace.sessions[0]
	if err := m.services.store.SetAgentSessionID(sess.ID, "conversation-proof"); err != nil {
		t.Fatal(err)
	}
	before, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := m.services.tmux.PanePID(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.services.lifecycle.SetArchivedForSession("", sess.ID, true, sessioncmd.Vocabulary{}); err != nil {
		t.Fatal(err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, sess.Name)
	m.killSelected()
	m.killAllLive()
	if m.mode == modeConfirmDelete {
		t.Fatal("archived kill opened a card")
	}
	m.restoreSelected()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	after, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current, err := m.services.tmux.PanePID(sess.ID); err != nil || current != pid || after.AgentSessionID != before.AgentSessionID || !after.AgentLaunchedAt.Equal(before.AgentLaunchedAt) {
		t.Fatalf("restore relaunched archived pane: %+v PID=%d %v", after, current, err)
	}
	m.rail.SetArchived(false)
	m.focusSession(sess.ID)
	m.killSelected()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	if m.services.tmux.Exists(sess.ID) {
		t.Fatal("active Kill did not stop restored pane")
	}
	if stored, err := m.services.store.Get(sess.ID); err != nil || stored.Archived || stored.Status != status.Dead {
		t.Fatalf("Kill removed/restored wrong state: %+v %v", stored, err)
	}
}
