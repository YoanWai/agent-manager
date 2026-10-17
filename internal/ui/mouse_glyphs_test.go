package ui

import (
	"fmt"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

func TestSettingsWheelReachesSymbolsAndDocsInShortWindows(t *testing.T) {
	for _, size := range [][2]int{{34, 12}, {80, 11}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := buildModel(t)
			m.layout.width, m.layout.height = size[0], size[1]
			m.runBatch(t, m.openSettings())
			m.viewSettings()
			if m.settings.dialog.GlyphRow >= 0 {
				t.Fatal("symbols should start below this viewport")
			}
			wheel := func(button tea.MouseButton) {
				m.handleMouse(tea.MouseMsg{Button: button, Action: tea.MouseActionPress})
				m.viewSettings()
			}
			for range uisettings.FieldGlyphs {
				wheel(tea.MouseButtonWheelDown)
			}
			if m.settings.dialog.Field != uisettings.FieldGlyphs || m.settings.dialog.GlyphRow < 0 {
				t.Fatal("wheel did not reveal symbols")
			}
			x := m.layout.cardLeft + 1 + cardPaddingX
			m.handleMouse(tea.MouseMsg{X: x, Y: m.layout.cardTop + 2 + m.settings.dialog.GlyphRow,
				Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if m.prefs.glyphMode != presentation.GlyphModeBasic {
				t.Fatal("revealed symbols row was not clickable")
			}
			for range uisettings.FieldDocs - uisettings.FieldGlyphs {
				wheel(tea.MouseButtonWheelDown)
			}
			if m.settings.dialog.Field != uisettings.FieldDocs || m.settings.dialog.DocsRow < 0 {
				t.Fatal("wheel did not reveal docs")
			}
			var opened string
			openBrowser = func(url string) error { opened = url; return nil }
			t.Cleanup(func() { openBrowser = defaultOpenBrowser })
			_, docs := m.handleMouse(tea.MouseMsg{
				X: m.layout.cardLeft + 1 + cardPaddingX, Y: m.layout.cardTop + 2 + m.settings.dialog.DocsRow,
				Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
			})
			m.runBatch(t, docs)
			if opened != docsURL {
				t.Fatal("docs click did not follow the scrolled row")
			}
			for range uisettings.FieldDocs - uisettings.FieldGlyphs {
				wheel(tea.MouseButtonWheelUp)
			}
			if m.settings.dialog.Field != uisettings.FieldGlyphs || m.settings.dialog.GlyphRow < 0 {
				t.Fatal("wheel up did not return to symbols")
			}
		})
	}
}

func TestSettingsWheelLeavesSubpanelsAlone(t *testing.T) {
	for _, panel := range []string{"CLIs", "keys", "editor"} {
		t.Run(panel, func(t *testing.T) {
			m := buildModel(t)
			m.runBatch(t, m.openSettings())
			switch panel {
			case "CLIs":
				m.settings.dialog.Field = uisettings.FieldCLIs
				m.handleSettingsKey(key("enter"))
			case "keys":
				m.settings.dialog.Field = uisettings.FieldKeybindings
				m.handleSettingsKey(key("enter"))
			case "editor":
				m.settings.dialog.Field = uisettings.FieldEditor
				m.settings.dialog.Editor = uisettings.NewEditorRow("custom --test", nil)
				m.handleSettingsKey(key("enter"))
			}
			field := m.settings.dialog.Field
			for _, button := range []tea.MouseButton{tea.MouseButtonWheelDown, tea.MouseButtonWheelUp} {
				m.handleMouse(tea.MouseMsg{Button: button, Action: tea.MouseActionPress})
				if m.settings.dialog.Field != field {
					t.Fatal("wheel in a subpanel moved the parent Settings field")
				}
			}
		})
	}
}
