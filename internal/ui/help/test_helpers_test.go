package help

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const testCursorMarker = "<cursor>"

func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func namedKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func testContext(width int) Context {
	return Context{
		SessionKeys: keybind.DefaultSession(),
		ListKeys:    keybind.DefaultList(),
		ArrowStep:   true,
		Width:       width,
		Glyphs: Glyphs{
			RowMenu:          "[…]",
			Reorder:          "⠿",
			AfterTurnArchive: "↓",
			AfterTurnKill:    "■",
		},
		QuickKeys:    QuickKeys{Model: "ctrl+l", Effort: "ctrl+x", Profile: "ctrl+y"},
		CursorMarker: testCursorMarker,
		Styles: Styles{
			Section: lipgloss.NewStyle().Bold(true),
			Rule:    lipgloss.NewStyle(),
			Key:     lipgloss.NewStyle().Bold(true),
			Muted:   lipgloss.NewStyle(),
			Subtle:  lipgloss.NewStyle(),
			Value:   lipgloss.NewStyle(),
			Match:   lipgloss.NewStyle().Bold(true),
			Cursor:  lipgloss.NewStyle(),
		},
	}
}

func bindingOf(t *testing.T, specs ...string) keybind.Binding {
	t.Helper()
	keys := make([]keybind.Key, 0, len(specs))
	for _, spec := range specs {
		key, err := keybind.Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		keys = append(keys, key)
	}
	return keybind.Keys(keys...)
}

func sessionOf(t *testing.T, detach, review, editor []string) keybind.Table {
	t.Helper()
	return keybind.DefaultSession().
		With(keybind.Detach, bindingOf(t, detach...)).
		With(keybind.Review, bindingOf(t, review...)).
		With(keybind.Editor, bindingOf(t, editor...))
}
