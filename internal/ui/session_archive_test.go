package ui

import (
	"errors"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"os"
	"strings"
	"testing"
	"time"
)

func TestArchiveConfirmationUsesLoadedSessionTree(t *testing.T) {
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
	if want := "restore group zone (1 archived sessions)? brings them back."; m.confirm.label != want {
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
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "alpha", dir, "")
	m.services.setSnapshot = func(id, snapshot string) error {
		return errors.New("disk full")
	}

	m.selectSessionRow(t, "alpha")
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
	gen := m.focusPane.MovePreview()
	m.applyCmd(t, m.previewCmd(railSelectedSession(m), gen))
	if !strings.Contains(m.workspace.preview, "snapshot-marker") {
		t.Fatalf("previewCmd should serve the snapshot for an archived session, preview = %q", m.workspace.preview)
	}
}

func TestArchiveAgentPersistsEveryChild(t *testing.T) {
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
