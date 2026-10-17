package ui

import (
	"strings"
	"testing"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	"github.com/charmbracelet/x/ansi"
)

func TestSettingsViewportKeepsFocusedFieldAndSaveVisible(t *testing.T) {
	m := buildModel(t)
	for _, size := range [][2]int{{34, 12}, {80, 24}, {160, 42}} {
		m.layout.width, m.layout.height = size[0], size[1]
		m.openSettings()
		for _, field := range []int{uisettings.FieldGlyphs, uisettings.FieldKeybindings, uisettings.FieldUpdate} {
			m.settings.dialog.Field = field
			view := ansi.Strip(m.viewSettings())
			lines := strings.Split(view, "\n")
			if len(lines) > m.layout.height {
				t.Fatalf("Settings exceeds height %d: %d", m.layout.height, len(lines))
			}
			want := map[int]string{uisettings.FieldGlyphs: "symbols", uisettings.FieldKeybindings: "keybindings", uisettings.FieldUpdate: "version"}[field]
			for _, text := range []string{want, "Save and close", "esc"} {
				if !strings.Contains(view, text) {
					t.Fatalf("Settings %dx%d lost %q:\n%s", m.layout.width, m.layout.height, text, view)
				}
			}
			if field != uisettings.FieldGlyphs && m.settings.dialog.GlyphRow < 0 {
				m.handleSettingsClick(m.layout.cardLeft+1+cardPaddingX, m.layout.cardTop+1)
				if m.prefs.glyphMode != "" {
					t.Fatal("hidden symbols row became a click target")
				}
			}
		}
	}
}
