package ui

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/testfixture"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
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
	if !confirmOpen(m) || len(pendingConfirm(m).lifecycle.Sessions) != 1 {
		t.Fatalf("archive confirmation depended on the closed store: screen = %v err=%q target=%+v", m.screenID(), m.errBar.text, pendingConfirm(m).lifecycle.Sessions)
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

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "alpha")
	m.archiveSelected()
	if confirmOpen(m) {
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
	if confirmOpen(m) {
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
	if confirmOpen(m) {
		t.Errorf("u on a group opened %q in the active view", pendingConfirm(m).prompt.Label)
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
	if confirmOpen(m) {
		t.Fatalf("u on a session opened %q in the active view", pendingConfirm(m).prompt.Label)
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
	if len(pendingConfirm(m).lifecycle.Sessions) != 1 || pendingConfirm(m).lifecycle.Sessions[0].ID != stash.ID {
		t.Errorf("restore targets %d sessions, want stash alone", len(pendingConfirm(m).lifecycle.Sessions))
	}
	if want := "restore whole group zone (1 archived sessions)? includes sessions hidden by filters."; pendingConfirm(m).prompt.Label != want {
		t.Errorf("label = %q, want %q", pendingConfirm(m).prompt.Label, want)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
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
			if want := "root is the top level, not a group to " + tc.action; !m.onList() || m.errBar.text != want {
				t.Errorf("screen = %v, errBar = %q, want the list and %q", m.screenID(), m.errBar.text, want)
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
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
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "homeless")
	m.restoreSelected()
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
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
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "coder")
	m.restoreSelected()
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
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
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, shell.Name)
	m.restoreSelected()
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if err := m.services.tmux.Kill(shell.ID); err != nil {
		t.Fatalf("kill terminal: %v", err)
	}

	m.selectSessionRow(t, "coder")
	m.restoreSelected()
	if want := "restore coder? brings it back."; pendingConfirm(m).prompt.Label != want {
		t.Errorf("label = %q, want %q", pendingConfirm(m).prompt.Label, want)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
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
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
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
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "returning")
	m.restoreSelected()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
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
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "zone")
	m.restoreSelected()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
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
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
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

func TestArchiveReconcilesDurableRowsBeforeLaterFailure(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	first := store.Session{ID: "cafe", Name: "first", Tool: "claude", Cwd: t.TempDir(), Status: status.Dead}
	if err := m.services.store.CreateSession(first); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.openConfirm(confirmTarget{prompt: uidialogs.Confirm{Action: uidialogs.ConfirmArchive}, lifecycle: uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmArchive), Sessions: []store.Session{first, {ID: "beef", Status: status.Dead}}}})
	if err := m.archiveConfirmed(); err == nil {
		t.Fatal("expected missing second row failure")
	}
	got, _ := m.sessionByID(first.ID)
	if !got.Archived {
		t.Fatal("completed archive remains in active inventory")
	}
	if mark := m.ledger.lifecycle.Gone[first.ID]; !mark.Archived {
		t.Fatal("completed archive lacks stale-poll guard")
	}
}

func TestRestoreReconcilesDurableRowsBeforeLaterFailure(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	first := store.Session{ID: newID(), Name: "first", Tool: "claude", Cwd: t.TempDir(), Status: status.Dead, Archived: true}
	second := store.Session{ID: newID(), Name: "second", Tool: "claude", Cwd: filepath.Join(t.TempDir(), "missing"), Status: status.Dead, Archived: true}
	for _, sess := range []store.Session{first, second} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	loadStoredRows(t, m)
	m.openConfirm(confirmTarget{prompt: uidialogs.Confirm{Action: uidialogs.ConfirmRestore}, lifecycle: uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmRestore), Sessions: []store.Session{first, second}}})
	err := m.restoreConfirmed()
	if err == nil {
		t.Fatal("expected second revive failure")
	}
	got, _ := m.sessionByID(first.ID)
	if got.Archived {
		t.Fatalf("completed restore remains in archived inventory; restore failed with: %v", err)
	}
	untouched, _ := m.sessionByID(second.ID)
	if !untouched.Archived {
		t.Fatal("failed restore changed untouched row")
	}
}

func TestFailedGroupRestoreDoesNotClearMembership(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dbPath := filepath.Join(t.TempDir(), "state.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	m.services.store = st
	m.services.setSnapshot = st.SetSnapshot
	m.services.lifecycle, err = sessioncmd.NewLifecycle(sessioncmd.Runtime{Config: m.services.cfg, Store: st, Driver: m.services.tmux, Hooks: m.services.hooks, Snapshot: st.SetSnapshot})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.CreateGroup("zone", ""); err != nil {
		t.Fatal(err)
	}
	sess := store.Session{ID: newID(), Name: "first", Group: "zone", Tool: "claude", Cwd: t.TempDir(), Status: status.Dead, Archived: true}
	if err := m.services.store.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetGroupArchived("zone", true); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER refuse_group_restore BEFORE UPDATE OF archived ON groups BEGIN SELECT RAISE(ABORT, 'group write refused'); END`); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.openConfirm(confirmTarget{prompt: uidialogs.Confirm{Action: uidialogs.ConfirmRestore, Group: true}, lifecycle: uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmRestore), Path: "zone", Sessions: []store.Session{sess}}})
	if err := m.restoreConfirmed(); err == nil {
		t.Fatal("expected group write failure")
	}
	got, _ := m.sessionByID(sess.ID)
	if !got.Archived || !m.workspace.archivedGroups["zone"] {
		t.Fatal("failed group write cleared local membership")
	}
	if m.services.tmux.Exists(sess.ID) {
		t.Fatal("failed group write left a newly started archived pane")
	}
	if _, ok := m.ledger.lifecycle.Launched[sess.ID]; ok {
		t.Fatal("failed group write retained a launch receipt")
	}
}

func TestDeleteGroupSubtree(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.services.store.CreateGroup("zone/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "in-zone", dir, "zone")
	createSession(t, m, "in-inner", dir, "zone/inner")
	createSession(t, m, "outside", dir, "")

	archivedID := m.sessionRows()[0].ID
	for _, s := range m.sessionRows() {
		if s.Name == "in-inner" {
			archivedID = s.ID
		}
	}
	if err := m.services.store.SetArchived(archivedID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	for i, r := range railRows(m) {
		if r.isGroup && r.group == "zone" {
			setRailCursor(m, i)
		}
	}
	m.prepareDelete()
	if !pendingConfirm(m).prompt.Group || len(pendingConfirm(m).lifecycle.Sessions) != 2 {
		t.Fatalf("confirm should target 2 subtree sessions (incl. archived), got %+v", pendingConfirm(m))
	}
	tmuxIDs := make([]string, 0, 2)
	for _, s := range pendingConfirm(m).lifecycle.Sessions {
		tmuxIDs = append(tmuxIDs, s.ID)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	for _, id := range tmuxIDs {
		if m.services.tmux.Exists(id) {
			t.Fatalf("tmux session %s should be killed", id)
		}
	}
	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Name != "outside" {
		t.Fatalf("only outside should remain, got %v", sessions)
	}
	all, _ := m.services.store.ListSessions(true)
	if len(all) != 1 {
		t.Fatalf("archived subtree session should be gone from db, got %d rows", len(all))
	}
	groups, _ := m.services.store.Groups()
	for _, g := range groups {
		if g.Name == "zone" || g.Name == "zone/inner" {
			t.Fatalf("group %s should be deleted", g.Name)
		}
	}
}

func TestDeleteGroupInArchivedViewSparesLiveSessions(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.services.store.CreateGroup("bugs", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "old", dir, "bugs")
	createSession(t, m, "live", dir, "bugs")

	m.selectSessionRow(t, "old")
	m.archiveSelected()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "bugs")
	m.prepareDelete()
	if len(pendingConfirm(m).lifecycle.Sessions) != 1 || pendingConfirm(m).lifecycle.Sessions[0].Name != "old" {
		t.Fatalf("confirm should target only the archived session, got %+v", pendingConfirm(m).lifecycle.Sessions)
	}
	archivedID := pendingConfirm(m).lifecycle.Sessions[0].ID
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	m.rail.SetArchived(false)
	m.applyCmd(t, m.refreshCmd())
	if names := sessionNames(m); len(names) != 1 || names[0] != "live" {
		t.Fatalf("active view sessions = %v want [live]", names)
	}
	if paths := m.groupRowPaths(); len(paths) != 1 || paths[0] != "bugs" {
		t.Fatalf("group holding a live session should survive, got %v", paths)
	}
	for _, sess := range m.sessionRows() {
		if !m.services.tmux.Exists(sess.ID) {
			t.Fatalf("live session %s lost its tmux window", sess.Name)
		}
	}
	if m.services.tmux.Exists(archivedID) {
		t.Fatalf("archived session %s should be killed", archivedID)
	}
}

func TestDeleteArchivedGroupInArchivedViewRemovesIt(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("empty", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "empty")
	m.archiveSelected()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "empty")
	m.prepareDelete()
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	if paths := m.groupRowPaths(); len(paths) != 0 {
		t.Fatalf("archived empty group should be gone, got %v", paths)
	}
}

func TestIgnoreDeletedSessionDropsOnlyTheDeleteRace(t *testing.T) {
	if err := ignoreDeletedSession(fmt.Errorf("abc: %w", store.ErrSessionGone)); err != nil {
		t.Fatalf("a session deleted mid-poll should not fail the pass: %v", err)
	}
	if err := ignoreDeletedSession(errors.New("database is locked")); err == nil {
		t.Fatal("a real store failure must still surface")
	}
}

func TestDeleteRemovesCleanWorktree(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	if err := m.spawnSession("claude", "wt-clean", repo, "", "", false, true, config.Choice{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sessions, _ := m.services.store.ListSessions(true)
	worktreePath := sessions[0].Cwd
	m.applyCmd(t, m.refreshCmd())

	deleteSession(t, m, "wt-clean")
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatal("clean worktree should be removed on delete")
	}
}

func TestDeleteKeepsWorktreeUntilLastSharingSession(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	if err := m.spawnSession("claude", "owner", repo, "", "", false, true, config.Choice{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sessions, err := m.services.store.ListSessions(true)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %v, err %v", sessions, err)
	}
	owner := sessions[0]
	forked := store.Session{
		ID:             "shared-fork",
		Name:           "forked",
		Tool:           owner.Tool,
		Cwd:            owner.Cwd,
		Status:         status.Idle,
		WorktreeRepo:   owner.WorktreeRepo,
		WorktreeBranch: owner.WorktreeBranch,
	}
	if err := m.services.tmux.Create(forked.ID, forked.Cwd, "cat", nil, m.previewPaneWidth(), m.previewPaneHeight()); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.CreateSession(forked); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())

	deleteSession(t, m, "owner")
	if _, err := os.Stat(owner.Cwd); err != nil {
		t.Fatalf("shared worktree was removed: %v", err)
	}
	if !strings.Contains(m.errBar.text, "used by another session") {
		t.Fatalf("shared worktree message = %q", m.errBar.text)
	}

	m.applyCmd(t, m.refreshCmd())
	deleteSession(t, m, "forked")
	if _, err := os.Stat(owner.Cwd); !os.IsNotExist(err) {
		t.Fatalf("last sharing session left worktree behind: %v", err)
	}
}

func TestDeleteKeepsDirtyWorktree(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	if err := m.spawnSession("claude", "wt-dirty", repo, "", "", false, true, config.Choice{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sessions, _ := m.services.store.ListSessions(true)
	worktreePath := sessions[0].Cwd
	if err := os.WriteFile(filepath.Join(worktreePath, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())

	deleteSession(t, m, "wt-dirty")
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatal("dirty worktree must survive delete")
	}
	if !strings.Contains(m.errBar.text, worktreePath) {
		t.Fatalf("error bar should name the kept path, got %q", m.errBar.text)
	}
	if remaining, _ := m.services.store.ListSessions(true); len(remaining) != 0 {
		t.Fatal("session record should still be deleted")
	}
}

func TestDeleteRemovesTheSessionSettings(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSessionOn(t, m, "hooked", "claude-hooked", t.TempDir())
	settings := m.services.hooks.SettingsFile(m.sessionRows()[0].ID)
	if _, err := os.Stat(settings); err != nil {
		t.Fatalf("launch should write the session's settings: %v", err)
	}

	deleteSession(t, m, "hooked")
	if _, err := os.Stat(settings); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("delete left the session's settings behind: %v", err)
	}
}

func TestDeleteAgentIncludesChildren(t *testing.T) {
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
	m.prepareDelete()
	ids := map[string]bool{}
	for _, sess := range pendingConfirm(m).lifecycle.Sessions {
		ids[sess.ID] = true
	}
	if !ids[shell.ID] {
		t.Fatal("delete confirm omitted the child")
	}
	agent := m.sessionRows()[0]
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	for _, id := range []string{shell.ID, agent.ID} {
		if _, err := m.services.store.Get(id); err == nil {
			t.Fatalf("%s row survived the delete", id)
		}
		if m.services.tmux.Exists(id) {
			t.Fatalf("%s pane survived the delete", id)
		}
	}
}

func TestConfirmedDeleteDropsTheRowBeforeTheNextPoll(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "doomed", t.TempDir(), "")
	sess := m.sessionRows()[0]

	deleteSession(t, m, "doomed")

	for _, row := range m.sessionRows() {
		if row.ID == sess.ID {
			t.Fatalf("deleted session still on screen before the next poll: %+v", row)
		}
	}
}

func TestConfirmedGroupDeleteDropsTheGroupRowAtOnce(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("zone", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "in-zone", dir, "zone")

	m.selectGroupRow(t, "zone")
	m.prepareDelete()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)

	for _, r := range railRows(m) {
		if r.isGroup && r.group == "zone" {
			t.Fatalf("deleted group still on screen before the next poll")
		}
	}
}

func TestKillEndsTheSessionAndKeepsItRevivable(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "hungry", t.TempDir(), "")
	m.selectSessionRow(t, "hungry")
	sess := m.sessionRows()[0]
	waitForPane(t, m, sess.ID, "kill-marker")

	m.killSelected()
	confirmKill(t, m)

	if m.services.tmux.Exists(sess.ID) {
		t.Fatal("kill should end the tmux session")
	}
	if len(m.sessionRows()) != 1 {
		t.Fatalf("kill must keep the row, rows = %d", len(m.sessionRows()))
	}
	stored, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Status != status.Dead {
		t.Fatalf("after kill, status = %q want %q", stored.Status, status.Dead)
	}

	snapshot, err := m.services.store.Snapshot(sess.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !strings.Contains(snapshot, "kill-marker") {
		t.Fatalf("kill should freeze the pane, snapshot = %q", snapshot)
	}
	m.workspace.preview = ""
	m.applyCmd(t, nil)
	if !strings.Contains(m.workspace.preview, "kill-marker") {
		t.Fatalf("a killed session should still preview its last output, preview = %q", m.workspace.preview)
	}

	m.selectSessionRow(t, "hungry")
	m.reviveSelected()
	m.drainEffects(t)
	if m.errBar.text != "" {
		t.Fatalf("revive after kill: %q", m.errBar.text)
	}
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("revive should bring a killed session back")
	}
}

func TestKillGroupEndsEverySessionInside(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work", "work/api")
	createSession(t, m, "alpha", dir, "work")
	createSession(t, m, "beta", dir, "work/api")
	createSession(t, m, "outside", dir, "")

	m.selectGroupRow(t, "work")
	m.killSelected()
	confirmKill(t, m)

	for _, sess := range m.visibleSessions() {
		alive := m.services.tmux.Exists(sess.ID)
		if sess.Name == "outside" && !alive {
			t.Fatal("a group kill must leave sessions outside the group running")
		}
		if sess.Name != "outside" && alive {
			t.Fatalf("group kill should have ended %s", sess.Name)
		}
	}
}

func TestKillAllEndsEveryLiveSessionInView(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work")
	createSession(t, m, "alpha", dir, "work")
	createSession(t, m, "outside", dir, "")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	m.drainEffects(t)
	m = updated.(*Model)
	confirmKill(t, m)

	for _, sess := range m.visibleSessions() {
		if m.services.tmux.Exists(sess.ID) {
			t.Fatalf("kill all should have ended %s", sess.Name)
		}
	}
	if _, _ = m.killAllLive(); m.errBar.text == "" {
		t.Fatal("kill all with nothing live should report it")
	}
}

func TestKillRefusesWhenNothingIsRunning(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	seedGroups(t, m, "work")
	createSession(t, m, "ghost", t.TempDir(), "work")
	sess := m.sessionRows()[0]
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectSessionRow(t, "ghost")
	if _, _ = m.killSelected(); m.errBar.text == "" {
		t.Fatal("killing a dead session should report it is already dead")
	}
	if confirmOpen(m) {
		t.Fatal("a dead session must not open the kill confirm")
	}

	m.selectGroupRow(t, "work")
	m.errBar.text = ""
	if _, _ = m.killSelected(); m.errBar.text == "" {
		t.Fatal("killing a group with nothing live should report it")
	}
	if confirmOpen(m) {
		t.Fatal("a group with nothing live must not open the kill confirm")
	}
}

func TestKillAgentIncludesChildren(t *testing.T) {
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
	_, cmd := m.killSelected()
	m.applyCmd(t, cmd)
	if !strings.Contains(pendingConfirm(m).prompt.Label, "terminal") {
		t.Fatalf("confirm should name terminals: %q", pendingConfirm(m).prompt.Label)
	}
	ids := map[string]bool{}
	for _, sess := range pendingConfirm(m).lifecycle.Sessions {
		ids[sess.ID] = true
	}
	if !ids[shell.ID] {
		t.Fatal("kill confirm omitted the child")
	}
}

func TestKillChildIsSingleSession(t *testing.T) {
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
	m.selectSessionRow(t, shell.Name)
	m.killSelected()
	if len(pendingConfirm(m).lifecycle.Sessions) != 1 || pendingConfirm(m).lifecycle.Sessions[0].ID != shell.ID {
		t.Fatalf("child kill = %+v", pendingConfirm(m).lifecycle.Sessions)
	}
}

func TestKillDeadAgentStillKillsItsLiveChild(t *testing.T) {
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
	if err := m.services.tmux.Kill(agent.ID); err != nil {
		t.Fatalf("kill agent: %v", err)
	}
	m.selectSessionRow(t, "coder")
	_, cmd := m.killSelected()
	m.applyCmd(t, cmd)
	if !confirmOpen(m) {
		t.Fatalf("screen = %v, want the kill confirm (errBar %q)", m.screenID(), m.errBar.text)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.services.tmux.Exists(shell.ID) {
		t.Fatal("live child survived the kill")
	}
}

func TestKillAgentConfirmNamesExtraTerminals(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	m.killSelected()
	want := "kill coder and 2 terminals? frees their RAM, v revives them."
	if pendingConfirm(m).prompt.Label != want {
		t.Fatalf("label = %q, want %q", pendingConfirm(m).prompt.Label, want)
	}
}

// A restart starts a fresh conversation on the model the session chose.
func TestRestartKeepsTheSessionChoice(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	tool := m.services.cfg.Tools["claude"]
	tool.ModelArgs = "--model {model}"
	tool.EffortArgs = "--effort {effort}"
	m.services.cfg.Tools["claude"] = tool
	if err := m.spawnSession("claude", "phoenix", t.TempDir(), "", "", false, false, config.Choice{Model: "sonnet", Effort: "high"}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess := m.sessionRows()[0]
	argsFile := filepath.Join(t.TempDir(), "launch-args")
	tool.Command = argCaptureCommand(argsFile)
	m.services.cfg.Tools["claude"] = tool
	if err := m.restartSession(sess); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if args := readWhenWritten(t, argsFile); !strings.Contains(args, "--model\nsonnet\n--effort\nhigh") {
		t.Fatalf("restart launch arguments = %q, want the session's model and effort", args)
	}
}

// Restart is revive's opposite number: same row, same directory, same tool,
// but a conversation the agent has never seen. The old one is retired rather
// than resumed, so the resume flags revive would have used stay unused.
func TestRestartLaunchesAFreshConversation(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "phoenix", t.TempDir(), "")
	sess := m.sessionRows()[0]

	argsFile := filepath.Join(t.TempDir(), "launch-args")
	tool := m.services.cfg.Tools[sess.Tool]
	tool.Command = argCaptureCommand(argsFile)
	tool.SessionIDFlag = "--session-id"
	tool.ResumeByIDCommand = "false --resume {id}"
	tool.ReviveCommand = "false --continue"
	m.services.cfg.Tools[sess.Tool] = tool

	if err := m.services.store.SetAgentSessionID(sess.ID, "old-conversation"); err != nil {
		t.Fatal(err)
	}
	m.workspace.sessions[0].AgentSessionID = "old-conversation"
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "phoenix")

	if _, _ = m.restartSelected(); !confirmOpen(m) {
		t.Fatalf("restart should ask first, screen = %v err = %q", m.screenID(), m.errBar.text)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("restart: %q", m.errBar.text)
	}
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("restart should leave the session running")
	}

	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID == "" || got.AgentSessionID == "old-conversation" {
		t.Fatalf("restarted conversation id = %q, want a fresh one", got.AgentSessionID)
	}
	if got.RetiredAgentSessionID != "old-conversation" {
		t.Fatalf("retired conversation = %q, want old-conversation", got.RetiredAgentSessionID)
	}
	if got.AgentLaunchedAt.Before(got.CreatedAt) || got.AgentLaunchedAt.IsZero() {
		t.Fatalf("launch time = %v, created = %v", got.AgentLaunchedAt, got.CreatedAt)
	}
	if got.Status != status.Starting {
		t.Fatalf("after restart, status = %q want %q", got.Status, status.Starting)
	}

	// The fresh conversation id rides the launch; the resume flags revive
	// would have used never appear.
	args := readWhenWritten(t, argsFile)
	if !strings.Contains(args, "--session-id\n"+got.AgentSessionID) {
		t.Fatalf("launch arguments = %q, want the fresh session id", args)
	}
	if strings.Contains(args, "--resume") || strings.Contains(args, "--continue") {
		t.Fatalf("restart must not resume, launch arguments = %q", args)
	}
}

// A tool that mints its own conversation id has nothing to
// hand the launch, so restart clears the binding and leaves the id for the
// poller to capture once the new conversation lands.
func TestRestartClearsCapturedConversationID(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "codexish", t.TempDir(), "")
	sess := m.sessionRows()[0]
	// The precondition under test, spelled out rather than inherited from
	// whatever flags the fake tools happen to carry.
	tool := m.services.cfg.Tools[sess.Tool]
	tool.SessionIDFlag = ""
	tool.SessionStore = "codex"
	m.services.cfg.Tools[sess.Tool] = tool

	if err := m.services.store.SetAgentSessionID(sess.ID, "captured-conversation"); err != nil {
		t.Fatal(err)
	}
	m.workspace.sessions[0].AgentSessionID = "captured-conversation"
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "codexish")

	m.restartSelected()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("restart: %q", m.errBar.text)
	}

	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("conversation id = %q, want it cleared for capture", got.AgentSessionID)
	}
	if got.RetiredAgentSessionID != "captured-conversation" {
		t.Fatalf("retired conversation = %q", got.RetiredAgentSessionID)
	}
}

// Restart also serves the session that is still running: it ends the agent
// holding the context and brings the row back empty-handed.
func TestRestartEndsALiveAgentFirst(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "busy", t.TempDir(), "")
	sess := m.sessionRows()[0]
	tool := m.services.cfg.Tools[sess.Tool]
	tool.Command = argCaptureCommand(filepath.Join(t.TempDir(), "launch-args"))
	m.services.cfg.Tools[sess.Tool] = tool
	m.selectSessionRow(t, "busy")

	if _, _ = m.restartSelected(); !confirmOpen(m) {
		t.Fatalf("restart should ask first, screen = %v err = %q", m.screenID(), m.errBar.text)
	}
	if !strings.Contains(pendingConfirm(m).prompt.Label, "ends the running agent") {
		t.Fatalf("confirm label = %q", pendingConfirm(m).prompt.Label)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("restart: %q", m.errBar.text)
	}
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("restart should leave the session running")
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != status.Starting {
		t.Fatalf("after restart, status = %q want %q", got.Status, status.Starting)
	}
}

func TestRestartRefusesGroupRow(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("work", dir); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "member", dir, "work")
	m.selectGroupRow(t, "work")

	if _, _ = m.restartSelected(); !m.onList() || m.errBar.text == "" {
		t.Fatalf("group restart should refuse, screen = %v err = %q", m.screenID(), m.errBar.text)
	}
}
func TestRestartAgentStaysSingleSession(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	m.restartSelected()
	if len(pendingConfirm(m).lifecycle.Sessions) != 1 {
		t.Fatalf("restart confirm = %+v", pendingConfirm(m).lifecycle.Sessions)
	}
}

func TestReviveRecreatesDeadSession(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "phoenix", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.services.store.SetAgentSessionID(sess.ID, "kept-conversation"); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "phoenix")
	sess = m.sessionRows()[0]
	if sess.AgentSessionID != "kept-conversation" {
		t.Fatalf("loaded session id = %q, want kept-conversation", sess.AgentSessionID)
	}
	previousLaunchTime := sess.LaunchTime()

	argsFile := filepath.Join(t.TempDir(), "launch-args")
	tool := m.services.cfg.Tools[sess.Tool]
	tool.ResumeByIDCommand = argCaptureCommand(argsFile) + " --resume {id}"
	m.services.cfg.Tools[sess.Tool] = tool

	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if m.services.tmux.Exists(sess.ID) {
		t.Fatal("session should be dead before revive")
	}
	m.selectSessionRow(t, "phoenix")

	if err := m.services.store.SetAcked(sess.ID, true); err != nil {
		t.Fatalf("set acked: %v", err)
	}
	m.workspace.preview = "old pane from last life\n"

	m.reviveSelected()
	m.drainEffects(t)
	if m.errBar.text != "" {
		t.Fatalf("revive: %q", m.errBar.text)
	}
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("revive should recreate the tmux session")
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Starting {
		t.Fatalf("after revive, status = %q want %q", got.Status, status.Starting)
	}
	if got.Acked {
		t.Fatal("revive should clear a leftover ack")
	}
	if got.AgentSessionID != "kept-conversation" || got.RetiredAgentSessionID != "" {
		t.Fatalf("conversation = %q retired = %q, want kept-conversation", got.AgentSessionID, got.RetiredAgentSessionID)
	}
	if got.AgentLaunchedAt.IsZero() || !got.LaunchTime().Equal(got.AgentLaunchedAt) {
		t.Fatalf("launch time = %v, created = %v", got.LaunchTime(), got.CreatedAt)
	}
	if !got.AgentLaunchedAt.After(previousLaunchTime) {
		t.Fatalf("launch time = %v, want after %v", got.AgentLaunchedAt, previousLaunchTime)
	}
	row := m.sessionRows()[0]
	if row.Status != status.Starting {
		t.Fatalf("row status = %q want %q", row.Status, status.Starting)
	}
	gotPreview := previewText(m)
	if !strings.Contains(gotPreview, "starting up") {
		t.Fatalf("revived preview should carry the launch loader, got %q", gotPreview)
	}
	if strings.Contains(gotPreview, "old pane from last life") {
		t.Fatalf("stale pane should not survive revive, got %q", gotPreview)
	}
	args := readWhenWritten(t, argsFile)
	if !strings.Contains(args, "--resume") || !strings.Contains(args, "kept-conversation") {
		t.Fatalf("revive launch arguments = %q, want --resume kept-conversation", args)
	}
}

func TestReviveOfADeletedRowLeavesNoPane(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "phoenix", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if err := m.services.store.Delete(sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	err := m.reviveSession(sess)
	if !errors.Is(err, store.ErrSessionGone) {
		t.Fatalf("revive error = %v, want ErrSessionGone", err)
	}
	if m.services.tmux.Exists(sess.ID) {
		t.Fatal("a failed revive must leave no tmux session behind")
	}
}

func TestRevivedLaunchTimeSitsInsideStartingGrace(t *testing.T) {
	created := time.Now().Add(-5 * 24 * time.Hour)
	revived := store.Session{CreatedAt: created, AgentLaunchedAt: time.Now()}
	if time.Since(revived.LaunchTime()) >= 30*time.Second {
		t.Fatal("a revive that stamps launch time must still be inside the grace")
	}
	stale := store.Session{CreatedAt: created}
	if time.Since(stale.LaunchTime()) < 30*time.Second {
		t.Fatal("a 5-day-old row with no relaunch is outside the grace")
	}
}

// degradedResumeNotice exists for the revive that came back blind: a tool
// whose picker opened, or a session whose own conversation id was captured,
// resumes the right conversation, and only the blind revive_command
// fallback warrants the warning.
func TestDegradedResumeNoticeWarnsOnlyForBlindFallbacks(t *testing.T) {
	t.Parallel()
	base := config.Tool{
		Command:           "claude",
		ReviveCommand:     "claude --continue",
		ResumeByIDCommand: "claude --resume {id}",
	}
	picker := base
	picker.ResumePickerCommand = "claude --resume"

	for _, tc := range []struct {
		name string
		tool config.Tool
		id   string
		want string
	}{
		{"a picker revive is not degraded", picker, "", ""},
		{"a captured id is not degraded", base, "abc-123", ""},
		{"a blind continue fallback warns", base, "", "revived reviveme with --continue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			m.services.cfg.Tools["claude"] = tc.tool
			got := uilifecycle.DegradedResume(tc.tool, tc.id, "reviveme")
			if tc.want == "" {
				if got != "" {
					t.Fatalf("degradedResumeNotice = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("degradedResumeNotice = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestReviveAllRecreatesEveryDeadSession(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "alpha", dir, "")
	createSession(t, m, "beta", dir, "")

	for _, sess := range m.visibleSessions() {
		if err := m.services.tmux.Kill(sess.ID); err != nil {
			t.Fatalf("kill %s: %v", sess.Name, err)
		}
	}
	// A refresh marks the pane-less sessions dead so revive-all picks them up.
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "running", dir, "")

	if _, _ = m.reviveAllDead(); !confirmOpen(m) {
		t.Fatalf("reviving two dead sessions should ask first, screen = %v err = %q", m.screenID(), m.errBar.text)
	}
	if want := "revive every dead session (2)? brings them back."; pendingConfirm(m).prompt.Label != want {
		t.Fatalf("label = %q, want %q", pendingConfirm(m).prompt.Label, want)
	}
	if len(pendingConfirm(m).lifecycle.Sessions) != 2 {
		t.Fatalf("confirm targets = %+v, want the two dead sessions", pendingConfirm(m).lifecycle.Sessions)
	}
	_, cmd := m.Update(namedKey(tea.KeyEnter))
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("revive all: %q", m.errBar.text)
	}
	for _, sess := range m.visibleSessions() {
		if !m.services.tmux.Exists(sess.ID) {
			t.Fatalf("revive all should recreate %s", sess.Name)
		}
	}
}

func TestReviveRefusesLiveSession(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	// A tool whose process stays up: revive leaves a pane alone only while
	// something is running in it.
	createSessionOn(t, m, "alive", "quietchat", t.TempDir())
	waitForAgent(t, m, m.sessionRows()[0].ID, true)
	m.selectSessionRow(t, "alive")

	_, cmd := m.reviveSelected()
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if !strings.Contains(m.errBar.text, "still running") {
		t.Fatalf("revive said %q, want it to refuse a pane that still holds its agent", m.errBar.text)
	}
	if !m.services.tmux.Exists(m.sessionRows()[0].ID) {
		t.Fatal("live session must keep running")
	}
}

func TestReviveRefusesMissingDir(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "homeless", dir, "")

	sess := m.sessionRows()[0]
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	m.selectSessionRow(t, "homeless")

	m.reviveSelected()
	m.drainEffects(t)
	if m.errBar.text == "" {
		t.Fatal("revive without a working directory should error")
	}
}

func TestReviveGroupBringsBackEverySessionInside(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work", "work/api")
	createSession(t, m, "alpha", dir, "work")
	createSession(t, m, "beta", dir, "work/api")
	createSession(t, m, "outside", dir, "")

	m.selectGroupRow(t, "work")
	m.killSelected()
	confirmKill(t, m)
	createSession(t, m, "running", dir, "work")

	m.selectGroupRow(t, "work")
	m.reviveSelected()
	m.drainEffects(t)
	if !confirmOpen(m) {
		t.Fatalf("reviving a group of two dead sessions should ask first, screen = %v err = %q", m.screenID(), m.errBar.text)
	}
	if title := mustTop(t, m).(*uidialogs.ConfirmSurface).Prompt().Title(m.prefs.glyphMode); title != "◆ Revive group" {
		t.Fatalf("title = %q, want ◆ Revive group", title)
	}
	if want := "revive group work (2 dead sessions)? brings them back."; pendingConfirm(m).prompt.Label != want {
		t.Fatalf("label = %q, want %q", pendingConfirm(m).prompt.Label, want)
	}
	if len(pendingConfirm(m).lifecycle.Sessions) != 2 {
		t.Fatalf("confirm targets = %+v, want the two dead sessions", pendingConfirm(m).lifecycle.Sessions)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("revive group: %q", m.errBar.text)
	}
	for _, sess := range m.visibleSessions() {
		if !m.services.tmux.Exists(sess.ID) {
			t.Fatalf("revive group should have brought back %s", sess.Name)
		}
	}
}

func TestReviveGroupConfirmedRevivesWhatItCan(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	gone, kept := t.TempDir(), t.TempDir()
	seedGroups(t, m, "work")
	createSession(t, m, "homeless", gone, "work")
	createSession(t, m, "housed", kept, "work")
	m.selectGroupRow(t, "work")
	m.killSelected()
	confirmKill(t, m)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("remove dir: %v", err)
	}

	m.selectGroupRow(t, "work")
	m.reviveSelected()
	m.drainEffects(t)
	if !confirmOpen(m) {
		t.Fatalf("screen = %v, want the revive card (err %q)", m.screenID(), m.errBar.text)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	if !strings.HasPrefix(m.errBar.text, "revived 1, first error: working directory no longer exists") {
		t.Fatalf("status = %q, want the count revived and the first failure", m.errBar.text)
	}
	if !m.onList() {
		t.Fatalf("screen = %v, want the list", m.screenID())
	}
	if !m.services.tmux.Exists(sessionRow(t, m, "housed").sess.ID) {
		t.Fatal("a failed revive must not keep the rest of the group dead")
	}
}

func TestReviveBatchCancelLeavesEverySessionDead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		key  tea.KeyMsg
	}{
		{"n", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}},
		{"esc", namedKey(tea.KeyEsc)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			dir := t.TempDir()
			seedGroups(t, m, "work")
			createSession(t, m, "alpha", dir, "work")
			createSession(t, m, "beta", dir, "work")
			m.selectGroupRow(t, "work")
			m.killSelected()
			confirmKill(t, m)

			for _, key := range []string{"v", "V"} {
				m.selectGroupRow(t, "work")
				m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
				m.drainEffects(t)
				if !confirmOpen(m) {
					t.Fatalf("%s: screen = %v, want the revive card (err %q)", key, m.screenID(), m.errBar.text)
				}
				m.handleKey(tc.key)
				m.drainEffects(t)
				if !m.onList() {
					t.Fatalf("%s: screen = %v, want the list after %s", key, m.screenID(), tc.name)
				}
				for _, sess := range m.visibleSessions() {
					if m.services.tmux.Exists(sess.ID) {
						t.Fatalf("%s then %s brought back %s", key, tc.name, sess.Name)
					}
				}
			}
		})
	}
}

func TestReviveBatchSkipsASessionBackBeforeTheAnswer(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work")
	createSession(t, m, "alpha", dir, "work")
	createSession(t, m, "beta", dir, "work")
	m.selectGroupRow(t, "work")
	m.killSelected()
	confirmKill(t, m)

	m.selectGroupRow(t, "work")
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m.drainEffects(t)
	if !confirmOpen(m) {
		t.Fatalf("screen = %v, want the revive card (err %q)", m.screenID(), m.errBar.text)
	}
	alpha := sessionRow(t, m, "alpha").sess
	if err := m.reviveSession(alpha); err != nil {
		t.Fatalf("revive alpha while the card is open: %v", err)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)

	if m.errBar.text != "" {
		t.Fatalf("status = %q, want no error for a session that came back on its own", m.errBar.text)
	}
	for _, name := range []string{"alpha", "beta"} {
		if !m.services.tmux.Exists(sessionRow(t, m, name).sess.ID) {
			t.Fatalf("%s should be running", name)
		}
	}
}

func TestReviveOneDeadSessionSkipsTheCard(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work")
	createSession(t, m, "alpha", dir, "work")
	// beta stays live, leaving two rows in scope and only one of them dead.
	createSession(t, m, "beta", dir, "work")
	alpha := sessionRow(t, m, "alpha").sess

	for _, key := range []string{"v", "V"} {
		if err := m.services.tmux.Kill(alpha.ID); err != nil {
			t.Fatalf("kill alpha: %v", err)
		}
		m.applyCmd(t, m.refreshCmd())
		m.selectGroupRow(t, "work")
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m.drainEffects(t)
		if !m.onList() {
			t.Fatalf("%s with one dead session: screen = %v, want it revived without a card", key, m.screenID())
		}
		if !m.services.tmux.Exists(alpha.ID) {
			t.Fatalf("%s should revive the one dead session at once, err = %q", key, m.errBar.text)
		}
	}
}

func TestReviveRunningAgentRevivesDeadChild(t *testing.T) {
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
	if err := m.services.tmux.Kill(shell.ID); err != nil {
		t.Fatalf("kill child: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "coder")
	parent, _ := m.selected()
	parentPID := m.workspace.panes[parent.ID].PID
	_, cmd := m.reviveSelected()
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if !confirmOpen(m) {
		t.Fatalf("screen = %v, want the revive confirm", m.screenID())
	}
	pending := pendingConfirm(m)
	if len(pending.lifecycle.Sessions) != 1 || pending.lifecycle.Sessions[0].ID != shell.ID {
		t.Fatalf("revive targets = %+v", pending.lifecycle.Sessions)
	}
	if want := "revive 1 terminal of coder? brings it back."; pending.prompt.Label != want {
		t.Fatalf("label=%q want=%q", pending.prompt.Label, want)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if !m.services.tmux.Exists(shell.ID) {
		t.Fatal("child still dead")
	}
	m.applyCmd(t, m.refreshCmd())
	after, err := m.services.store.Get(parent.ID)
	if err != nil || m.workspace.panes[parent.ID].PID != parentPID || after.AgentSessionID != parent.AgentSessionID || !after.AgentLaunchedAt.Equal(parent.AgentLaunchedAt) {
		t.Fatalf("parent changed: before=%+v after=%+v err=%v", parent, after, err)
	}
}

func TestReviveAgentIncludesDeadChildren(t *testing.T) {
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
	coder, ok := m.selected()
	if !ok {
		t.Fatal("coder row missing")
	}
	if err := m.services.tmux.Kill(shell.ID); err != nil {
		t.Fatalf("kill child: %v", err)
	}
	if err := m.services.tmux.Kill(coder.ID); err != nil {
		t.Fatalf("kill agent: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "coder")
	m.reviveSelected()
	m.drainEffects(t)
	ids := map[string]bool{}
	for _, sess := range pendingConfirm(m).lifecycle.Sessions {
		ids[sess.ID] = true
	}
	if !ids[shell.ID] {
		t.Fatal("revive confirm omitted the child")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if !m.services.tmux.Exists(coder.ID) || !m.services.tmux.Exists(shell.ID) {
		t.Fatal("confirm should revive the agent and its dead children")
	}
}

// Quitting the agent leaves the window alive on a shell, and v is what
// brings the agent back there: launched by the manager, so it carries the
// session identity, the MCP registration and the hook settings that a CLI
// started by hand from that shell has no way to pick up.
func TestReviveStartsTheAgentAgainInALivePane(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSessionOn(t, m, "quit-and-back", "quietchat", t.TempDir())
	sess := m.sessionRows()[0]
	// Hooks give the session a status file beside its id, so the relaunch
	// has both to carry. The command runs through a shell, which is what
	// survives the settings flag a hooked tool launches with.
	hooked := m.services.cfg.Tools[sess.Tool]
	hooked.Command = "sh -c 'cat'"
	if runtime.GOOS == "windows" {
		hooked.Command = testfixture.ModeCommand("cat-stdin")
	}
	hooked.StatusSource = "claude-hooks"
	m.services.cfg.Tools[sess.Tool] = hooked
	quitAgent(t, m, sess.ID)
	if err := m.services.store.SetAcked(sess.ID, true); err != nil {
		t.Fatalf("set acked: %v", err)
	}
	m.selectSessionRow(t, "quit-and-back")

	_, cmd := m.reviveSelected()
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("revive: %q", m.errBar.text)
	}
	waitForAgent(t, m, sess.ID, true)
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("revive must keep the window it relaunched in")
	}

	// The relaunch hands its environment over in a file the shell sources,
	// so only that file's path is typed, never a value.
	if pane, _ := m.services.tmux.CapturePane(sess.ID); strings.Contains(strings.ReplaceAll(ansi.Strip(pane), "\n", ""), "AGENT_MANAGER_SESSION_ID") {
		t.Fatalf("relaunch typed the session identity into the pane:\n%s", pane)
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Starting {
		t.Fatalf("status after revive = %q, want %q", got.Status, status.Starting)
	}
	if got.Acked {
		t.Fatal("revive must clear the ack of the agent that exited")
	}

	// The relaunch exports rather than prefixes, so the shell it lands in
	// keeps the session identity once this agent exits as well.
	if err := m.services.tmux.SendKeys(sess.ID, "C-d"); err != nil {
		t.Fatalf("send ctrl-d: %v", err)
	}
	waitForAgent(t, m, sess.ID, false)
	marker := filepath.Join(t.TempDir(), "env")
	report := `printf '%s %s\n' "$AGENT_MANAGER_SESSION_ID" "$AGENT_MANAGER_STATUS_FILE" > ` + marker + `.part && mv ` + marker + `.part ` + marker
	if runtime.GOOS == "windows" {
		report = `Set-Content -LiteralPath ` + tmux.ShellQuote(marker+".part") + ` -Value "$env:AGENT_MANAGER_SESSION_ID $env:AGENT_MANAGER_STATUS_FILE"; ` +
			`Move-Item -LiteralPath ` + tmux.ShellQuote(marker+".part") + ` -Destination ` + tmux.ShellQuote(marker)
	}
	if err := m.services.tmux.SendKeys(sess.ID, report, "Enter"); err != nil {
		t.Fatalf("read the shell environment: %v", err)
	}
	want := sess.ID + " " + m.services.hooks.StatusFile(sess.ID)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if data, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(data)) == want {
			break
		}
		if time.Now().After(deadline) {
			pane, _ := m.services.tmux.CapturePane(sess.ID)
			t.Fatalf("the shell lost the session environment after the relaunched agent exited; pane:\n%s", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func launchCaptureCommand(argsFile string) string {
	if runtime.GOOS == "windows" {
		return argCaptureCommand(argsFile) + " $env:" + hooks.EnvLaunch
	}
	script := `printf '%s\n' "$` + hooks.EnvLaunch + `" "$@" > ` + tmux.ShellQuote(argsFile) + `; cat`
	return "sh -c " + tmux.ShellQuote(script) + " sh"
}

func TestReviveResumesTheStoredConversationOverAStaleListedRow(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "phoenix", t.TempDir(), "")
	listed := m.sessionRows()[0]
	argsFile := filepath.Join(t.TempDir(), "launch-args")
	tool := m.services.cfg.Tools[listed.Tool]
	tool.ResumeByIDCommand = launchCaptureCommand(argsFile) + " --resume {id}"
	m.services.cfg.Tools[listed.Tool] = tool
	if err := m.services.tmux.Kill(listed.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if result, err := m.services.store.ReportAgentSessionID(listed.ID, listed.Tool, "switched", 0); err != nil || result != store.ReportAdopted {
		t.Fatalf("report: %v %v", result, err)
	}

	if err := m.reviveSession(listed); err != nil {
		t.Fatalf("revive: %v", err)
	}
	stored, err := m.services.store.Get(listed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.AgentSessionID != "switched" {
		t.Fatalf("stored %q, want switched", stored.AgentSessionID)
	}
	want := fmt.Sprintf("%d\n--resume\nswitched\n", store.LaunchStamp(stored.AgentLaunchedAt))
	if args := readWhenWritten(t, argsFile); !strings.HasPrefix(args, want) || stored.AgentLaunchedAt.IsZero() {
		t.Fatalf("launch = %q, want it to open with %q", args, want)
	}
}

func TestRestartStampsTheLaunchItExported(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "phoenix", t.TempDir(), "")
	sess := m.sessionRows()[0]
	argsFile := filepath.Join(t.TempDir(), "launch-args")
	tool := m.services.cfg.Tools[sess.Tool]
	tool.Command = launchCaptureCommand(argsFile)
	tool.SessionIDFlag = "--session-id"
	m.services.cfg.Tools[sess.Tool] = tool
	if err := m.restartSession(sess); err != nil {
		t.Fatalf("restart: %v", err)
	}
	stored, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%d\n--session-id\n%s\n", store.LaunchStamp(stored.AgentLaunchedAt), stored.AgentSessionID)
	if args := readWhenWritten(t, argsFile); !strings.HasPrefix(args, want) || stored.AgentLaunchedAt.IsZero() {
		t.Fatalf("launch = %q, want it to open with %q", args, want)
	}
}

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
	t.Parallel()
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
	if snapshot, _ := m.services.store.Snapshot(sess.ID); !strings.Contains(ansi.Strip(snapshot), "last words") {
		t.Fatalf("the archived row lost its last screen: %q", snapshot)
	}
	if len(m.sessionRows()) != 0 {
		t.Fatalf("the active list still shows %v", m.sessionRows())
	}
}

func TestASessionAskingToBeKilledStaysListedDead(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
