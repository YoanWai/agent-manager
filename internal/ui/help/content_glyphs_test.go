package help

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestCompatibleHelpSearchUsesDisplayedKeys(t *testing.T) {
	ctx := testContext(84)
	ctx.Glyphs.Mode = presentation.GlyphModeBasic
	ctx.ListKeys = ctx.ListKeys.With(keybind.Prompt, bindingOf(t, "backspace"))
	for _, scope := range []Scope{Global, Review} {
		s := New(scope)
		body := strings.Join(s.Content(ctx).Lines, "\n")
		if strings.ContainsAny(body, "↵⌫") {
			t.Fatalf("compatible help leaked key glyphs: %s", body)
		}
	}
	for _, query := range []string{"enter", "backspace"} {
		s := State{scope: Global, query: query, searching: true}
		content := s.Content(ctx)
		body := ansi.Strip(strings.Join(content.Lines, "\n"))
		if !strings.Contains(body, query) || strings.Contains(body, "no matches") {
			t.Fatalf("%q search failed: %s", query, body)
		}
	}
	ctx.Glyphs.Mode = presentation.GlyphModeAdvanced
	if body := strings.Join(New(Global).Content(ctx).Lines, "\n"); !strings.Contains(body, "⌫") || !strings.Contains(body, "↵") {
		t.Fatal("Advanced key labels changed")
	}
}
