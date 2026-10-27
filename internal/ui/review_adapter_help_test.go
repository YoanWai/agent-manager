package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestReviewUsesConfiguredHelpBinding(t *testing.T) {
	m := reviewModel(t)
	m.services.listKeys = m.services.listKeys.With(keybind.Help, bindingOf(t, "H"))
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if m.mode != modeDiff {
		t.Fatal("old Help alias still opens Help")
	}
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")})
	if m.mode != modeHelp {
		t.Fatal("configured Help key does not open Help")
	}
	m.handleHelpKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")})
	if m.mode != modeDiff {
		t.Fatal("configured Help key does not return to Review")
	}
	footer := strings.Join(strings.Fields(ansi.Strip(m.viewDiffFooter())), " ")
	if !strings.Contains(footer, "H keys") || strings.Contains(footer, "? keys") {
		t.Fatalf("footer=%q", footer)
	}
	m.services.listKeys = m.services.listKeys.With(keybind.Help, keybind.Binding{})
	for _, key := range []string{"?", "H"} {
		m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if m.mode != modeDiff {
			t.Fatalf("disabled Help opens via %s", key)
		}
	}
	if footer = strings.Join(strings.Fields(ansi.Strip(m.viewDiffFooter())), " "); strings.Contains(footer, " keys") {
		t.Fatalf("disabled Help footer=%q", footer)
	}
}

func TestReviewMixedHelpBindingsMatchCatalogAndModalHints(t *testing.T) {
	m := reviewModel(t)
	keys, err := keybind.ListTable(map[string]keybind.Binding{keybind.Help: bindingOf(t, "b", "H")})
	if err != nil {
		t.Fatal(err)
	}
	m.services.listKeys = keys
	footer := strings.Join(strings.Fields(ansi.Strip(m.viewDiffFooter())), " ")
	if !strings.Contains(footer, "b branch") || !strings.Contains(footer, "H keys") || strings.Contains(footer, "b/H keys") {
		t.Fatalf("Review footer disagrees: %s", footer)
	}
	for _, closeKey := range []string{"b", "H"} {
		m.handleDiffKey(runeKey("H"))
		if m.mode != modeHelp {
			t.Fatal("H did not open Help")
		}
		content := m.help.state.Content(m.helpContext(100))
		body := strings.Join(strings.Fields(ansi.Strip(strings.Join(content.Lines, "\n"))), " ")
		if !strings.Contains(body, "H this key map") || strings.Contains(body, "b / H this key map") {
			t.Fatalf("Review catalog: %s", body)
		}
		foundClose := false
		for _, pair := range content.Hints {
			if pair[1] == "close" {
				foundClose = strings.Contains(pair[0], "b") && strings.Contains(pair[0], "H")
			}
		}
		if !foundClose {
			t.Fatalf("modal close keys not advertised: %+v", content.Hints)
		}
		m.handleHelpKey(runeKey(closeKey))
		if m.mode != modeDiff {
			t.Fatalf("advertised %s did not close Help", closeKey)
		}
	}
	m.services.listKeys = keys.With(keybind.Help, bindingOf(t, "c", "H"))
	m = clickLabel(t, m, "c comment")
	if !m.review.Snapshot().Annotating || m.mode != modeDiff {
		t.Fatal("comment footer opened Help")
	}
	m.handleDiffKey(namedKey(tea.KeyEsc))
	m = clickLabel(t, m, "H keys")
	if m.mode != modeHelp {
		t.Fatal("Help footer did not open Help")
	}
}
