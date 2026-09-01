package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
)

// A paste is read the way every answer but yes is read: as no.
func TestPasteClosesConfirmWithoutConfirming(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "keeper", t.TempDir(), "")
	m.selectSessionRow(t, "keeper")
	sess, _ := m.selected()
	m.prepareDelete()
	if m.mode != modeConfirmDelete {
		t.Fatalf("mode = %v, want modeConfirmDelete", m.mode)
	}

	updated, _ := m.Update(tea.PasteMsg{Content: "y\n"})
	m = updated.(*Model)

	if m.mode != modeList {
		t.Fatalf("after paste, mode = %v, want modeList", m.mode)
	}
	if !m.tmux.Exists(sess.ID) {
		t.Fatal("a pasted y must not kill the session")
	}
	if _, err := m.store.Get(sess.ID); err != nil {
		t.Fatalf("a pasted y must not delete the session: %v", err)
	}
}

// A destructive answer is asked in a dialog, not on the status line, so a
// busy frame cannot hide the question.
func TestConfirmRendersAsADialog(t *testing.T) {
	m := buildModel(t)
	m.mode = modeConfirmDelete
	m.confirm = confirmTarget{
		action:   actionKill,
		sessions: []store.Session{{ID: "one", Name: "builder"}},
		label:    "kill builder? frees its RAM, v revives it.",
	}

	out := ansi.Strip(m.viewFrame())
	for _, want := range []string{"Kill session", "kill builder?", "frees its RAM", "cancel"} {
		if !strings.Contains(out, want) {
			t.Fatalf("confirm dialog missing %q:\n%s", want, out)
		}
	}
	if status := ansi.Strip(m.statusLine()); strings.Contains(status, "builder") {
		t.Fatalf("the question moved to the dialog, status should not repeat it: %q", status)
	}
}

func TestConfirmTitleNamesTheAct(t *testing.T) {
	m := buildModel(t)
	cases := []struct {
		action  string
		isGroup bool
		want    string
	}{
		{actionKill, false, "Kill session"},
		{actionKill, true, "Kill group"},
		{actionDelete, false, "Delete session"},
		{actionArchive, true, "Archive group"},
		{actionRestore, false, "Restore session"},
	}
	for _, tc := range cases {
		m.confirm = confirmTarget{action: tc.action, isGroup: tc.isGroup}
		if got := m.confirmTitle(); !strings.Contains(got, tc.want) {
			t.Errorf("action %q group=%v titled %q, want it to name %q", tc.action, tc.isGroup, got, tc.want)
		}
	}
}

func TestSplitConfirmLabel(t *testing.T) {
	question, consequence := splitConfirmLabel("kill web? frees its RAM.")
	if question != "kill web?" || consequence != "frees its RAM." {
		t.Fatalf("split gave %q / %q", question, consequence)
	}
	if question, consequence = splitConfirmLabel("delete everything"); question != "delete everything" || consequence != "" {
		t.Fatalf("a label without a question mark stays whole, got %q / %q", question, consequence)
	}
}
