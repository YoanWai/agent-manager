package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSettingsViewportKeepsFocusedFieldAndSaveVisible(t *testing.T) {
	m := buildModel(t)
	for _, size := range [][2]int{{34, 12}, {80, 24}, {160, 42}} {
		m.width, m.height = size[0], size[1]
		m.openSettings()
		for _, field := range []int{settingsFieldGlyphs, settingsFieldKeybindings, settingsFieldUpdate} {
			m.settings.field = field
			view := ansi.Strip(m.viewSettings())
			lines := strings.Split(view, "\n")
			if len(lines) > m.height {
				t.Fatalf("Settings exceeds height %d: %d", m.height, len(lines))
			}
			want := map[int]string{settingsFieldGlyphs: "symbols", settingsFieldKeybindings: "keybindings", settingsFieldUpdate: "version"}[field]
			for _, text := range []string{want, "Save and close", "esc"} {
				if !strings.Contains(view, text) {
					t.Fatalf("Settings %dx%d lost %q:\n%s", m.width, m.height, text, view)
				}
			}
			if field != settingsFieldGlyphs && m.settings.glyphRow < 0 {
				m.handleSettingsClick(m.cardLeft+1+cardPaddingX, m.cardTop+1)
				if m.glyphMode != "" {
					t.Fatal("hidden symbols row became a click target")
				}
			}
		}
	}
}
