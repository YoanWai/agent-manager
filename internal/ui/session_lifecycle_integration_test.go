package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCreateArchiveRestoreDelete(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	createSession(t, m, "alpha", dir, "")
	if len(m.sessionRows()) != 1 {
		t.Fatalf("after create, sessions = %d want 1 (err=%q)", len(m.sessionRows()), m.errBar.text)
	}
	sess := m.sessionRows()[0]
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("tmux session should exist after create")
	}
	if sess.Name != "alpha" || sess.Tool != "claude" || sess.Group != "" {
		t.Fatalf("session fields wrong: %+v", sess)
	}

	m.selectSessionRow(t, "alpha")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if len(m.sessionRows()) != 0 {
		t.Fatalf("after archive, active sessions = %d want 0", len(m.sessionRows()))
	}
	if m.services.tmux.Exists(sess.ID) {
		t.Fatal("archive should kill the tmux session")
	}

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	if len(m.sessionRows()) != 1 || !m.sessionRows()[0].Archived {
		t.Fatalf("archived session should show in archived view")
	}

	m.selectSessionRow(t, "alpha")
	m.restoreSelected()
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Starting {
		t.Fatalf("after restore, status = %q want %q", got.Status, status.Starting)
	}
	if got.LaunchTime().Equal(got.CreatedAt) {
		t.Fatal("restore should stamp a new launch time")
	}
	m.applyCmd(t, cmd)
	m.rail.SetArchived(false)
	m.applyCmd(t, m.refreshCmd())
	if len(m.sessionRows()) != 1 {
		t.Fatalf("after restore, active sessions = %d want 1", len(m.sessionRows()))
	}
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("restore should revive the tmux session")
	}

	m.selectSessionRow(t, "alpha")
	m.prepareDelete()
	if m.mode != modeConfirmDelete {
		t.Fatal("prepareDelete should enter confirm mode")
	}
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	if m.services.tmux.Exists(sess.ID) {
		t.Fatal("tmux session should be killed after delete")
	}
	m.applyCmd(t, cmd)
	if len(m.sessionRows()) != 0 {
		t.Fatalf("after delete, sessions = %d want 0", len(m.sessionRows()))
	}
}

func seedRestoreScenario(t *testing.T, m *Model, group string) (live, sleeper, stash store.Session) {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"live", "sleeper", "stash"} {
		createSession(t, m, name, dir, group)
	}
	live, sleeper, stash = sessionRow(t, m, "live").sess, sessionRow(t, m, "sleeper").sess, sessionRow(t, m, "stash").sess
	m.selectSessionRow(t, "sleeper")
	m.killSelected()
	confirmKill(t, m)
	m.selectSessionRow(t, "stash")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	return live, sleeper, stash
}

// argCaptureCommand builds a launch command that records the arguments the
// manager appended to it and then holds the pane open, so a test can prove
// which flags a launch carried.
func argCaptureCommand(argsFile string) string {
	script := `printf '%s\n' "$@" > ` + tmux.ShellQuote(argsFile) + `; cat`
	return "sh -c " + tmux.ShellQuote(script) + " sh"
}

// readWhenWritten waits for content, not merely for the file: the launching
// shell truncates it before printf runs, so a read that lands between the two
// comes back empty and would fail the assertion it was fetched for.
func readWhenWritten(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
			return string(raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no launch arguments written to %s", path)
	return ""
}

func TestNewSessionShowsStartingImmediately(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	m.form.name.SetValue("boot")
	m.form.dir.SetValue(t.TempDir())
	m.form.toolIndex = 0
	pickGroup(t, m, "")
	// submitForm without the follow-up refresh: the row must already show the
	// launch state from the optimistic insert alone.
	if _, _ = m.submitForm(); m.errBar.text != "" {
		t.Fatalf("submit: %q", m.errBar.text)
	}
	rows := m.sessionRows()
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].Status != status.Starting {
		t.Fatalf("new row status = %q, want %q", rows[0].Status, status.Starting)
	}
	t.Cleanup(func() { m.services.tmux.Kill(rows[0].ID) })
}

// waitForPane blocks until a session's pane shows the marker, so a test can
// act on a pane that has actually painted.
func waitForPane(t *testing.T, m *Model, id, marker string) {
	t.Helper()
	if err := m.services.tmux.SendText(id, marker); err != nil {
		t.Fatalf("send text: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := m.services.tmux.CapturePane(id)
		if err == nil && strings.Contains(pane, marker) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane never showed %q, last capture: %q", marker, pane)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// confirmKill answers the pending confirm modal with yes.
func confirmKill(t *testing.T, m *Model) {
	t.Helper()
	if m.mode != modeConfirmDelete {
		t.Fatalf("kill should ask before acting, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("kill: %q", m.errBar.text)
	}
}

// seedGroups creates group rows so the new-session picker offers them.
func seedGroups(t *testing.T, m *Model, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if err := m.services.store.CreateGroup(path, ""); err != nil {
			t.Fatalf("create group %s: %v", path, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
}

func deleteSession(t *testing.T, m *Model, name string) {
	t.Helper()
	m.selectSessionRow(t, name)
	m.prepareDelete()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
}
