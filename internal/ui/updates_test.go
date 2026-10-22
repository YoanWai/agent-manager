package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

// handleMsg routes each message family to the file that owns it. A case
// body written here instead grows the router back into a monolith.
const maxHandleMsgLines = 60

func TestHandleMsgLineCap(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "updates.go", nil, 0)
	if err != nil {
		t.Fatalf("parse updates.go: %v", err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "handleMsg" {
			continue
		}
		lines := fset.Position(fn.Body.Rbrace).Line - fset.Position(fn.Body.Lbrace).Line - 1
		if lines > maxHandleMsgLines {
			t.Fatalf("handleMsg body has %d lines, cap is %d: route the message to its owner's file", lines, maxHandleMsgLines)
		}
		return
	}
	t.Fatal("handleMsg not found in updates.go")
}

func TestSecondQuitBindingAbandonsDrain(t *testing.T) {
	for _, name := range []string{"q", "f12"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := buildModel(t)
			key, err := keybind.Parse(name)
			if err != nil {
				t.Fatal(err)
			}
			m.services.listKeys = m.services.listKeys.With(keybind.Quit, keybind.Keys(key))
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}
			if name == "f12" {
				msg = tea.KeyMsg{Type: tea.KeyF12}
			}
			m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"a"}}}, 0, false)
			m.Update(msg)
			if !m.effects.quitting || m.effects.main.active == nil {
				t.Fatal("first quit did not begin draining")
			}
			if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}); cmd != nil {
				t.Fatal("ordinary input was accepted during drain")
			}
			_, cmd := m.Update(msg)
			if cmd == nil {
				t.Fatal("second quit was discarded")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatal("second quit did not exit")
			}
			if len(m.AbandonedEffects()) != 1 {
				t.Fatalf("abandoned = %v", m.AbandonedEffects())
			}
		})
	}
}
