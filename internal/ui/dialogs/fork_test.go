package dialogs

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestForkOpensNamedAfterItsSource(t *testing.T) {
	var d Fork
	d.Open("alpha", "")
	if d.Name() != "alpha-fork" || d.Gen() != 1 {
		t.Fatalf("name %q gen %d", d.Name(), d.Gen())
	}
	d.Open("alpha", "")
	if d.Gen() != 2 {
		t.Fatalf("reopening kept gen %d, want a new one", d.Gen())
	}
}

func TestForkSubmitCleansTheName(t *testing.T) {
	var d Fork
	d.Open("alpha", "work")
	d.name.SetValue(" a/b ")
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEnter}); got.Action != ForkSubmit || got.Name != "a-b" || got.Gen != 1 {
		t.Fatalf("enter = %+v, want the cleaned name submitted", got)
	}
	d.name.SetValue("  ")
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEnter}); got.Action != ForkInvalid || got.Err != "name cannot be empty" {
		t.Fatalf("enter on a blank name = %+v", got)
	}
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEsc}); got.Action != ForkCancel {
		t.Fatalf("esc = %+v", got)
	}
	d.name.SetValue("")
	if got := d.Key(runes("x")); got.Action != ForkStay || d.Name() != "x" {
		t.Fatalf("typing = %+v name %q", got, d.Name())
	}
}

func TestForkViewShowsSourceAndGroup(t *testing.T) {
	var d Fork
	d.Open("alpha", "")
	plain := lipgloss.NewStyle()
	card := d.View(ForkStyles{
		BranchGlyph: "⑂",
		Value:       plain,
		Group:       plain,
		Field:       func(label, value string, focused bool) string { return label + "=" + value },
		Input:       func(textinput.Model) string { return "input" },
	})
	if card.Title != "⑂ Fork Session" || card.Body != "  source  alpha\n  group   root\nname=input" {
		t.Fatalf("card = %+v", card)
	}
}
