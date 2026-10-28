package settings

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestCompatibleSettingsKeepEditorTextAndFormatActionKeys(t *testing.T) {
	const literal = "editor-↵⌫◂▸"
	s := openState(testContext())
	s.Editor = NewEditorRow(literal, nil)
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		ctx := viewContext()
		ctx.GlyphMode = mode
		ctx.InnerWidth = 100
		ctx.Height = 80
		body := ansi.Strip(s.View(ctx).Body)
		if !strings.Contains(body, literal) {
			t.Fatalf("editor changed: %s", body)
		}
		choice := "<"
		if mode == presentation.GlyphModeAdvanced {
			choice = "◂"
		}
		if !strings.Contains(body, choice+" on ") {
			t.Fatalf("settings choice missing: %s", body)
		}
		mark := presentation.KeyLabel(mode, "↵")
		if !strings.Contains(body, mark+" open the docs") || !strings.Contains(body, mark+" focus · A attach") {
			t.Fatalf("settings labels missing in %s: %s", mode, body)
		}
		s.CLIPicker = true
		if got := ansi.Strip(s.View(ctx).Body); !strings.Contains(got, mark) {
			t.Fatalf("CLI request indicator missing: %s", got)
		}
		s.CLIPicker = false
	}
}

func TestNavigationSettingsLabelAndOverflowKeepLiteralNames(t *testing.T) {
	const literal = "prompt ↑↓←→"
	s, _ := openPromptsOn(t, Prompt{Name: literal}, Prompt{Name: "one"}, Prompt{Name: "two"}, Prompt{Name: "three"}, Prompt{Name: "four"})
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		ctx := viewContext()
		ctx.GlyphMode = mode
		ctx.InnerWidth = 70
		ctx.Height = 15
		body := ansi.Strip(s.View(ctx).Body)
		if !strings.Contains(body, literal) {
			t.Fatal("saved prompt name rewritten")
		}
		more := "↓ 2 more"
		if mode == presentation.GlyphModeBasic {
			more = "2 more below"
		}
		if !strings.Contains(body, more) {
			t.Fatalf("overflow missing %q: %s", more, body)
		}
		main := openState(testContext())
		ctx.Height = 100
		text := ansi.Strip(main.View(ctx).Body)
		label := "←→ step in/out"
		if mode == presentation.GlyphModeBasic {
			label = "arrow step in/out"
		}
		if !strings.Contains(text, label) {
			t.Fatalf("settings label missing: %s", text)
		}
	}
}
