package rail

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSelectionEffectsPreserveRootSideEffectPolicy(t *testing.T) {
	model := New(nil)
	model.Reconcile(Snapshot{Groups: []string{"empty"}, Sessions: []Session{
		{ID: "one", Name: "one", Status: "working"},
		{ID: "two", Name: "two", Status: "waiting"},
	}})

	if decision := model.Move(1, false); decision.SelectionEffect != SelectionEffectPreview {
		t.Fatalf("move effect = %v, want preview", decision.SelectionEffect)
	}

	model.SetSearch("one", true)
	if !model.Focus(Selection{Kind: SessionRow, SessionID: "one"}) {
		t.Fatal("test setup: filtered session one is missing")
	}
	decision := model.searchKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if !decision.SelectionChanged || decision.SelectionEffect != SelectionEffectNone {
		t.Fatalf("search edit decision = %+v, want changed identity with no root effect", decision)
	}

	decision = model.clearSearch()
	if decision.SelectionChanged || decision.SelectionEffect != SelectionEffectNone {
		t.Fatalf("stable clear search decision = %+v, want no root effect", decision)
	}

	if !model.Focus(Selection{Kind: GroupRow, Group: "empty"}) {
		t.Fatal("test setup: empty group is missing")
	}
	decision = model.toggleEmptyGroups()
	if !decision.SelectionChanged || decision.SelectionEffect != SelectionEffectFilter {
		t.Fatalf("filter decision = %+v, want changed filter effect", decision)
	}
}
