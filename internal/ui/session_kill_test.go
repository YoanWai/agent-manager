package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

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
	if len(m.confirm.sessions) != 1 || m.confirm.sessions[0].ID != shell.ID {
		t.Fatalf("child kill = %+v", m.confirm.sessions)
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
	if m.mode != modeConfirmDelete {
		t.Fatalf("mode = %v, want the kill confirm (errBar %q)", m.mode, m.errBar.text)
	}
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
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
	if m.confirm.label != want {
		t.Fatalf("label = %q, want %q", m.confirm.label, want)
	}
}

func TestFilteredKillTargetsAndEffects(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprint(all), func(t *testing.T) {
			m := buildModel(t)
			seedGroups(t, m, "work", "hidden/deep")
			for _, name := range []string{"needle-one", "needle-two", "other"} {
				createSession(t, m, name, t.TempDir(), "work")
			}
			createSession(t, m, "needle-hidden", t.TempDir(), "hidden/deep")
			inventory := append([]store.Session(nil), m.workspace.sessions...)
			if err := m.services.store.SetGroupArchived("hidden", true); err != nil {
				t.Fatal(err)
			}
			m.applyCmd(t, m.refreshCmd())
			m.rail.SetSearch("needle", false)
			m.selectGroupRow(t, "work")
			if all {
				m.killAllLive()
			} else {
				m.killSelected()
			}
			if len(m.confirm.sessions) != 2 || !strings.Contains(m.confirm.label, "(2") {
				t.Fatalf("targets=%+v label=%q", m.confirm.sessions, m.confirm.label)
			}
			confirmKill(t, m)
			for _, sess := range inventory {
				want := sess.Name == "other" || sess.Name == "needle-hidden"
				if got := m.services.tmux.Exists(sess.ID); got != want {
					t.Errorf("%s live=%v want=%v", sess.Name, got, want)
				}
			}
		})
	}
}

func TestKillRootLabelAndEffects(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "root-agent", t.TempDir(), "")
	m.selectGroupRow(t, "")
	m.killSelected()
	if !strings.Contains(m.confirm.label, "group root (1 live sessions)") {
		t.Fatalf("label=%q", m.confirm.label)
	}
	confirmKill(t, m)
	m.selectGroupRow(t, "")
	m.killSelected()
	if m.errBar.text != "no live sessions to kill in root" {
		t.Fatalf("notice=%q", m.errBar.text)
	}
}

func TestArchivedLiveRowsRefuseUIKill(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "archived-live", t.TempDir(), "")
	sess := m.workspace.sessions[0]
	if err := m.services.store.SetArchived(sess.ID, true); err != nil {
		t.Fatal(err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, sess.Name)
	for _, action := range []string{"selected", "all", "x", "X"} {
		m.mode = modeList
		var cmd tea.Cmd
		switch action {
		case "selected":
			_, cmd = m.killSelected()
		case "all":
			_, cmd = m.killAllLive()
		default:
			_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(action)})
		}
		m.applyCmd(t, cmd)
		m.drainEffects(t)
		if m.mode != modeList || !m.services.tmux.Exists(sess.ID) {
			t.Errorf("%s: mode=%v live=%v", action, m.mode, m.services.tmux.Exists(sess.ID))
		}
	}
}
