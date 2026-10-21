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
			if paints(m, "symbols") {
				t.Fatal("symbols should start below this viewport")
			}
			for range uisettings.FieldGlyphs {
				m = wheelOnCard(m, tea.MouseButtonWheelDown)
			}
			if m.settings.dialog.Field != uisettings.FieldGlyphs || !paints(m, "symbols") {
				t.Fatal("wheel did not reveal symbols")
			}
			if m, _ = clickBody(t, m, "symbols"); m.prefs.glyphMode != presentation.GlyphModeBasic {
				t.Fatal("revealed symbols row was not clickable")
			}
			for range uisettings.FieldDocs - uisettings.FieldGlyphs {
				m = wheelOnCard(m, tea.MouseButtonWheelDown)
			}
			if m.settings.dialog.Field != uisettings.FieldDocs || !paints(m, "docs") {
				t.Fatal("wheel did not reveal docs")
			}
			var opened string
			openBrowser = func(url string) error { opened = url; return nil }
			t.Cleanup(func() { openBrowser = defaultOpenBrowser })
			m, docs := clickBody(t, m, "docs")
			m.runBatch(t, docs)
			if opened != docsURL {
				t.Fatal("docs click did not follow the scrolled row")
			}
			for range uisettings.FieldDocs - uisettings.FieldGlyphs {
				m = wheelOnCard(m, tea.MouseButtonWheelUp)
			}
			if m.settings.dialog.Field != uisettings.FieldGlyphs || !paints(m, "symbols") {
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
				if m = wheelOnCard(m, button); m.settings.dialog.Field != field {
					t.Fatal("wheel in a subpanel moved the parent Settings field")
				}
			}
		})
	}
}
