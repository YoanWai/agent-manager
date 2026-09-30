package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestKillEndsTheSessionAndKeepsItRevivable(t *testing.T) {
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
	if _, _ = m.reviveSelected(); m.errBar.text != "" {
		t.Fatalf("revive after kill: %q", m.errBar.text)
	}
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("revive should bring a killed session back")
	}
}

func TestKillGroupEndsEverySessionInside(t *testing.T) {
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
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work")
	createSession(t, m, "alpha", dir, "work")
	createSession(t, m, "outside", dir, "")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
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
	m := buildModel(t)
	seedGroups(t, m, "work")
	createSession(t, m, "ghost", t.TempDir(), "work")
	sess := m.sessionRows()[0]
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}

	m.selectSessionRow(t, "ghost")
	if _, _ = m.killSelected(); m.errBar.text == "" {
		t.Fatal("killing a dead session should report it is already dead")
	}
	if m.mode == modeConfirmDelete {
		t.Fatal("a dead session must not open the kill confirm")
	}

	m.selectGroupRow(t, "work")
	m.errBar.text = ""
	if _, _ = m.killSelected(); m.errBar.text == "" {
		t.Fatal("killing a group with nothing live should report it")
	}
	if m.mode == modeConfirmDelete {
		t.Fatal("a group with nothing live must not open the kill confirm")
	}
}

func TestKillAgentIncludesChildren(t *testing.T) {
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
	if !strings.Contains(m.confirm.label, "terminal") {
		t.Fatalf("confirm should name terminals: %q", m.confirm.label)
	}
	ids := map[string]bool{}
	for _, sess := range m.confirm.sessions {
		ids[sess.ID] = true
	}
	if !ids[shell.ID] {
		t.Fatal("kill confirm omitted the child")
	}
}

func TestKillChildIsSingleSession(t *testing.T) {
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
	if len(m.confirm.sessions) != 1 || m.confirm.sessions[0].ID != shell.ID {
		t.Fatalf("child kill = %+v", m.confirm.sessions)
	}
}

func TestKillDeadAgentStillKillsItsLiveChild(t *testing.T) {
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
	if m.mode != modeConfirmDelete {
		t.Fatalf("mode = %v, want the kill confirm (errBar %q)", m.mode, m.errBar.text)
	}
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.applyCmd(t, cmd)
	if m.services.tmux.Exists(shell.ID) {
		t.Fatal("live child survived the kill")
	}
}

func TestKillAgentConfirmNamesExtraTerminals(t *testing.T) {
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
	if m.confirm.label != want {
		t.Fatalf("label = %q, want %q", m.confirm.label, want)
	}
}
