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

func TestNavigationHelpPreservesLiteralBindingsAndSearch(t *testing.T) {
	for _, tc := range []struct{ key, basic string }{{"left", "left"}, {"shift+up", "shift+up"}, {"←", "←"}, {"↑", "↑"}, {"→", "→"}, {"↕", "↕"}} {
		ctx := testContext(100)
		ctx.ListKeys = ctx.ListKeys.With(keybind.Prompt, bindingOf(t, tc.key))
		for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
			ctx.Glyphs.Mode = mode
			sections := helpSections(ctx)
			want := tc.basic
			if mode == presentation.GlyphModeAdvanced {
				want = ctx.ListKeys.Binding(keybind.Prompt).Glyph(" / ")
			}
			found := false
			for _, section := range sections {
				if section.title == "quick prompt mode ("+want+")" {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s binding %q lost: %+v", mode, tc.key, sections)
			}
		}
	}
	ctx := testContext(84)
	ctx.Glyphs.Mode = presentation.GlyphModeBasic
	s := State{scope: Global, query: "left", searching: true}
	body := strings.Join(s.Content(ctx).Lines, "\n")
	if !strings.Contains(body, "left") || strings.Contains(body, "no matches") || strings.ContainsAny(body, "↑↓←→↕") {
		t.Fatalf("navigation search: %s", body)
	}
}

func TestCompatibleResizeHelpKeepsCancelVisible(t *testing.T) {
	for _, width := range []int{84, 100} {
		ctx := testContext(width)
		ctx.Glyphs.Mode = presentation.GlyphModeBasic
		body := ansi.Strip(strings.Join(New(Global).Content(ctx).Lines, "\n"))
		if !strings.Contains(body, "resize split: left/right, h/l, drag; enter sets, esc cancels") {
			t.Fatalf("width %d clips resize controls: %s", width, body)
		}
	}
}
