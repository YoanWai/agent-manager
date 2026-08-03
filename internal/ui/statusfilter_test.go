package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestStatusFilterAttentionMatches(t *testing.T) {
	want := map[string]bool{
		status.Waiting:  true,
		status.Finished: true,
		status.Errored:  true,
		status.Working:  false,
		status.Idle:     false,
		status.Dead:     false,
		status.Starting: false,
	}
	for st, keep := range want {
		if got := statusFilterAttention.matches(st); got != keep {
			t.Fatalf("attention.matches(%q) = %v want %v", st, got, keep)
		}
	}
	if !statusFilterAll.matches(status.Idle) {
		t.Fatal("all filter should match every status")
	}
}

func TestStatusFilterCycleStartsAtAttention(t *testing.T) {
	if got := statusFilterAll.next(); got != statusFilterAttention {
		t.Fatalf("first f press = %v want attention", got)
	}
	if got := statusFilterAttention.next(); got != statusFilterAll {
		t.Fatalf("second f press = %v want all (only one mode for now)", got)
	}
	if statusFilterAll.active() {
		t.Fatal("all should not report active")
	}
	if !statusFilterAttention.active() {
		t.Fatal("attention should report active")
	}
	if statusFilterAttention.label() != "attention" {
		t.Fatalf("label = %q", statusFilterAttention.label())
	}
}

func TestStatusFilterKeyKeepsAttentionSessions(t *testing.T) {
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "w", Name: "needs-you", Tool: "claude", Cwd: "/tmp", Status: status.Waiting},
		{ID: "f", Name: "done-turn", Tool: "claude", Cwd: "/tmp", Status: status.Finished},
		{ID: "e", Name: "broke", Tool: "claude", Cwd: "/tmp", Status: status.Errored},
		{ID: "busy", Name: "grinding", Tool: "claude", Cwd: "/tmp", Status: status.Working},
		{ID: "rest", Name: "quiet", Tool: "claude", Cwd: "/tmp", Status: status.Idle},
		{ID: "gone", Name: "killed", Tool: "claude", Cwd: "/tmp", Status: status.Dead},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(*Model)
	if cmd != nil {
		m.applyCmd(t, cmd)
	}
	if m.statusFilter != statusFilterAttention {
		t.Fatalf("statusFilter = %v want attention", m.statusFilter)
	}
	got := sessionNames(m)
	want := []string{"needs-you", "done-turn", "broke"}
	if !slices.Equal(got, want) {
		t.Fatalf("attention list = %v want %v", got, want)
	}
	header := ansi.Strip(strings.Join(m.viewHeaderRows(), "\n"))
	if !strings.Contains(header, "ATTENTION") {
		t.Fatalf("header missing ATTENTION badge:\n%s", header)
	}
	if !strings.Contains(header, "3 session") {
		t.Fatalf("header count should match listed sessions:\n%s", header)
	}
	footer := ansi.Strip(m.viewFooter())
	if !strings.Contains(footer, "show all") {
		t.Fatalf("footer should offer clearing the filter:\n%s", footer)
	}

	updated, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = updated.(*Model)
	if cmd != nil {
		m.applyCmd(t, cmd)
	}
	if m.statusFilter != statusFilterAll {
		t.Fatalf("second f should clear filter, got %v", m.statusFilter)
	}
	if got := sessionNames(m); len(got) != 6 {
		t.Fatalf("cleared filter should show all 6 sessions, got %v", got)
	}
}

func TestStatusFilterIgnoresFolds(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("work", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	for _, sess := range []store.Session{
		{ID: "wait", Name: "blocked", Tool: "claude", Cwd: "/tmp", Group: "work", Status: status.Waiting},
		{ID: "idle", Name: "resting", Tool: "claude", Cwd: "/tmp", Group: "work", Status: status.Idle},
	} {
		if err := m.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)
	m.collapsed["work"] = true
	m.rebuildRows()
	if len(m.sessionRows()) != 0 {
		t.Fatalf("fold should hide sessions before filter, got %d", len(m.sessionRows()))
	}

	m.statusFilter = statusFilterAttention
	m.rebuildRows()
	got := sessionNames(m)
	if !slices.Equal(got, []string{"blocked"}) {
		t.Fatalf("attention filter should reveal the waiting session past the fold, got %v", got)
	}
}

func TestStatusFilterEmptyState(t *testing.T) {
	m := shotModel()
	m.sessions = []store.Session{
		{ID: "idle", Name: "quiet", Tool: "claude", Cwd: "/tmp", Status: status.Idle},
	}
	m.statusFilter = statusFilterAttention
	m.rebuildRows()
	rail := ansi.Strip(strings.Join(splitLines(joinContentText(m.railLines(40, 20))), "\n"))
	if !strings.Contains(rail, "nothing needs attention") {
		t.Fatalf("rail missing empty attention copy:\n%s", rail)
	}
}
