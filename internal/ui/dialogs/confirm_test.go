package dialogs

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestConfirmTitleNamesTheAct(t *testing.T) {
	cases := []struct {
		action ConfirmAction
		group  bool
		want   string
	}{
		{ConfirmKill, false, "Kill session"},
		{ConfirmKill, true, "Kill group"},
		{ConfirmDelete, false, "Delete session"},
		{ConfirmArchive, true, "Archive group"},
		{ConfirmRestore, false, "Restore session"},
	}
	for _, tc := range cases {
		if got := (Confirm{Action: tc.action, Group: tc.group}).Title(); !strings.Contains(got, tc.want) {
			t.Errorf("action %q group=%v titled %q, want it to name %q", tc.action, tc.group, got, tc.want)
		}
	}
}

func TestSplitConfirmLabel(t *testing.T) {
	question, consequence := SplitLabel("kill web? frees its RAM.")
	if question != "kill web?" || consequence != "frees its RAM." {
		t.Fatalf("split gave %q / %q", question, consequence)
	}
	if question, consequence = SplitLabel("delete everything"); question != "delete everything" || consequence != "" {
		t.Fatalf("a label without a question mark stays whole, got %q / %q", question, consequence)
	}
}

func TestConfirmKeys(t *testing.T) {
	c := Confirm{Action: ConfirmKill, Label: "kill a? frees its RAM."}
	cases := []struct {
		key  tea.KeyMsg
		want ConfirmOutcome
	}{
		{runes("j"), ConfirmStay},
		{runes("q"), ConfirmStay},
		{runes("Y"), ConfirmStay},
		{tea.KeyMsg{Type: tea.KeyDown}, ConfirmStay},
		{runes("n"), ConfirmCancel},
		{tea.KeyMsg{Type: tea.KeyEsc}, ConfirmCancel},
		{runes("y"), ConfirmAccept},
		{tea.KeyMsg{Type: tea.KeyEnter}, ConfirmAccept},
		{tea.KeyMsg{Type: tea.KeyCtrlC}, ConfirmQuit},
	}
	for _, tc := range cases {
		if got := c.Key(tc.key); got != tc.want {
			t.Errorf("%q = %v, want %v", tc.key.String(), got, tc.want)
		}
	}
}

func TestConfirmAnswerAndTone(t *testing.T) {
	if c := (Confirm{Action: ConfirmKill}); c.Answer() != "kill" || !c.Destructive() {
		t.Fatalf("kill answers %q destructive=%v", c.Answer(), c.Destructive())
	}
	if c := (Confirm{Action: ConfirmRevive}); c.Answer() != "revive" || c.Destructive() {
		t.Fatalf("revive answers %q destructive=%v", c.Answer(), c.Destructive())
	}
	if c := (Confirm{}); c.Answer() != "delete" || !c.Destructive() {
		t.Fatalf("delete answers %q destructive=%v", c.Answer(), c.Destructive())
	}
}

func TestConfirmBodySetsTheConsequenceBeneath(t *testing.T) {
	plain := lipgloss.NewStyle()
	if got := ConfirmBody("kill a?", "frees its RAM.", 40, plain, plain); got != "kill a?\n\nfrees its RAM." {
		t.Fatalf("body = %q", got)
	}
	if got := ConfirmBody("kill a?", "", 40, plain, plain); got != "kill a?" {
		t.Fatalf("body without a consequence = %q", got)
	}
}
