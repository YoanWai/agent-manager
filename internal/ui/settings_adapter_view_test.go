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
			if field != uisettings.FieldGlyphs && !strings.Contains(view, "symbols") {
				m = leftClick(m, m.layout.card.bodyLeft, m.layout.card.bodyTop-1)
				if m.prefs.glyphMode != "" {
					t.Fatal("hidden symbols row became a click target")
				}
			}
		}
	}
}

// The save button and the field scroll belong to the main rows; the key
// picker scrolls its own list and saves on esc.
func TestKeyPickerCardCarriesNoSaveButton(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 40
	m.openSettings()
	m.settings.dialog.Field = uisettings.FieldKeybindings
	m.handleSettingsKey(key("enter"))
	if !m.settings.dialog.KeyPicker {
		t.Fatal("test setup: enter on keybindings opens the picker")
	}
	view := ansi.Strip(m.viewSettings())
	if strings.Contains(view, "Save and close") {
		t.Fatalf("the key picker painted the main dialog's save button:\n%s", view)
	}
	if !strings.Contains(view, "detach") {
		t.Fatalf("the key picker lost its first row:\n%s", view)
	}
}

// A flex card is wider than the default, so its hint can sit on fewer rows;
// the room left for the body is counted at the card's own width.
func TestFlexChromeRowsMatchThePaintedCard(t *testing.T) {
	m := buildModel(t)
	hint := [][2]string{{"↑↓", "move"}, {"↵", "set a key"}, {"a", "add one"}, {"d", "off"}, {"r", "defaults"}, {"esc", "back"}}
	body := strings.Repeat("a row wide enough to stretch the card past its default width\n", 5) + "last"
	for _, width := range []int{40, 64, 90, 160} {
		m.layout.width, m.layout.height = width, 60
		m.cardFlex("title", body, hint)
		painted := m.layout.card.bottom - m.layout.card.top - (strings.Count(body, "\n") + 1)
		if got := m.flexChromeRows(hint); got != painted {
			t.Fatalf("width %d: flexChromeRows = %d, the card painted %d", width, got, painted)
		}
	}
}

// The key picker spends every row the terminal has on its list, short of
// the one a table heading can push past the window.
func TestKeyPickerFillsTheTerminal(t *testing.T) {
	m := buildModel(t)
	m.layout.width = 160
	for height := 16; height <= 40; height++ {
		m.layout.height = height
		m.openSettings()
		m.settings.dialog.Field = uisettings.FieldKeybindings
		m.handleSettingsKey(key("enter"))
		m.viewSettings()
		if rows := m.layout.card.bottom - m.layout.card.top; rows > height || rows < height-1 {
			t.Fatalf("height %d: the key picker card is %d rows tall", height, rows)
		}
	}
}
