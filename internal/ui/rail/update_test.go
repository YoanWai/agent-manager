package rail

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

func keyCtx() KeyContext { return KeyContext{ListKeys: keybind.DefaultList(), EnterFocuses: true} }

// A key that only moves the cursor is the Rail's alone: no selection side
// effect, no mutation and no intent leaves the package for the root to run.
func TestCursorMoveStaysInTheRail(t *testing.T) {
	model := New(nil)
	model.Reconcile(Snapshot{Sessions: []Session{{ID: "a", Name: "a"}, {ID: "b", Name: "b"}}})

	decision, consumed := model.Update(tea.KeyMsg{Type: tea.KeyDown}, Frame{}, keyCtx())
	if !consumed {
		t.Fatal("a consumed key left the root thinking it was free")
	}
	if queued(decision) {
		t.Fatalf("a cursor move asked the root to run %+v", decision)
	}
}

// A row action names what the root should open, and the key was consumed.
func TestRowActionLeavesAnIntent(t *testing.T) {
	model := New(nil)
	model.Reconcile(Snapshot{Groups: []string{"work"}, Sessions: []Session{{ID: "a", Name: "a", Group: "work"}}})

	decision, consumed := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}, Frame{}, keyCtx())
	if !consumed || decision.Intent.Kind != RenameAction {
		t.Fatalf("rename key = %+v, consumed %v", decision.Intent, consumed)
	}
}
