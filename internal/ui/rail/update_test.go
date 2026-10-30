package rail

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

func keyCtx() KeyContext { return KeyContext{ListKeys: keybind.DefaultList(), EnterFocuses: true} }

// A list key the root acts on reaches it: dropping a consumed decision lost
// help, settings and new session.
func TestListKeyIntentReachesTheRoot(t *testing.T) {
	model := New(nil)
	model.Reconcile(Snapshot{Sessions: []Session{{ID: "a", Name: "a"}}})

	decision, consumed := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}, Frame{}, keyCtx())
	if !consumed || decision.Intent.Kind == NoAction {
		t.Fatalf("help key = %+v, consumed %v", decision.Intent, consumed)
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
