package ui

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

func buildModel(t *testing.T) *Model {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	cfg := config.Config{
		DefaultGroup: "default",
		Tools: map[string]config.Tool{
			"claude": {Command: "cat", DefaultStatus: status.Idle},
		},
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	driver, err := tmux.New()
	if err != nil {
		t.Fatalf("tmux: %v", err)
	}
	engine, err := status.NewEngine(cfg)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}

	m := New(cfg, st, driver, engine)
	m.width = 120
	m.height = 40
	t.Cleanup(func() {
		for _, s := range m.sessions {
			driver.Kill(s.ID)
		}
	})
	return m
}

func (m *Model) applyCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	updated, _ := m.Update(msg)
	*m = *updated.(*Model)
}

func createSession(t *testing.T, m *Model, name, dir string) {
	t.Helper()
	m.openForm()
	m.form.name.SetValue(name)
	m.form.dir.SetValue(dir)
	m.form.toolIndex = 0
	_, cmd := m.submitForm()
	if m.mode != modeList {
		t.Fatalf("after submit, mode = %v, err = %q", m.mode, m.err)
	}
	m.applyCmd(t, cmd)
}

func TestCreateArchiveRestoreDelete(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	createSession(t, m, "alpha", dir)
	if len(m.nav) != 1 {
		t.Fatalf("after create, nav = %d want 1 (err=%q)", len(m.nav), m.err)
	}
	sess := m.nav[0]
	if !m.tmux.Exists(sess.ID) {
		t.Fatal("tmux session should exist after create")
	}
	if sess.Name != "alpha" || sess.Tool != "claude" || sess.Group != "default" {
		t.Fatalf("session fields wrong: %+v", sess)
	}

	m.cursor = 0
	_, cmd := m.archiveSelected()
	m.applyCmd(t, cmd)
	if len(m.nav) != 0 {
		t.Fatalf("after archive, active nav = %d want 0", len(m.nav))
	}

	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	if len(m.nav) != 1 || !m.nav[0].Archived {
		t.Fatalf("archived session should show in archived view")
	}

	m.cursor = 0
	_, cmd = m.restoreSelected()
	m.applyCmd(t, cmd)
	m.showArchived = false
	m.applyCmd(t, m.refreshCmd())
	if len(m.nav) != 1 {
		t.Fatalf("after restore, active nav = %d want 1", len(m.nav))
	}

	m.cursor = 0
	m.mode = modeConfirmDelete
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if m.tmux.Exists(sess.ID) {
		t.Fatal("tmux session should be killed after delete")
	}
	m.applyCmd(t, cmd)
	if len(m.nav) != 0 {
		t.Fatalf("after delete, nav = %d want 0", len(m.nav))
	}
}

func TestGroupsAndNav(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	m.openForm()
	m.form.name.SetValue("a")
	m.form.dir.SetValue(dir)
	m.form.group.SetValue("work")
	m.form.toolIndex = 0
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)

	createSession(t, m, "b", dir)

	if len(m.nav) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(m.nav))
	}
	groups := map[string]bool{}
	for _, s := range m.nav {
		groups[s.Group] = true
	}
	if !groups["work"] || !groups["default"] {
		t.Fatalf("expected work and default groups, got %v", groups)
	}

	m.search = "does-not-exist"
	m.rebuildNav()
	if len(m.nav) != 0 {
		t.Fatalf("search should filter to 0, got %d", len(m.nav))
	}
	m.search = ""
	m.rebuildNav()
	if len(m.nav) != 2 {
		t.Fatalf("clearing search should restore 2, got %d", len(m.nav))
	}

	if m.View() == "" {
		t.Fatal("View should render non-empty")
	}
}
