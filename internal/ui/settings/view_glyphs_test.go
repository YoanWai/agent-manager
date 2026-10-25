package settings

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestCompatibleSettingsKeepEditorTextAndFormatActionKeys(t *testing.T) {
	const literal = "editor-↵⌫"
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
