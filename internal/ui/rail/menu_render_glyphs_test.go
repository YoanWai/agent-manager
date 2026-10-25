package rail

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

func TestCompatibleRailMeasuresRemappedKeys(t *testing.T) {
	key, err := keybind.Parse("backspace")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		ctx := RenderContext{GlyphMode: mode, ListKeys: keybind.DefaultList().With(keybind.Settings, keybind.Keys(key)), Theme: theme.Palettes[0], TerminalWidth: 80}
		r := renderer{ctx: ctx, palette: newPalette(ctx.Theme)}
		r.model.menu = rowMenu{title: "root", items: []menuItem{{label: "Settings", action: OpenSettings}}}
		rows := r.renderMenu()
		mark := presentation.KeyLabel(mode, "⌫")
		if !strings.Contains(ansi.Strip(strings.Join(rows, "\n")), mark) {
			t.Fatalf("menu missing %s", mark)
		}
		width := ansi.StringWidth(rows[0])
		for _, row := range rows {
			if ansi.StringWidth(row) != width {
				t.Fatalf("menu columns diverged in %s", mode)
			}
		}
		if hint := ansi.Strip(r.listHint(keybind.Settings, "settings")); hint != mark+" settings" {
			t.Fatalf("hint=%q", hint)
		}
	}
}
