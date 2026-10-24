package presentation

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestChromeKeepsOneCellAndCompatibleFontRepertoire(t *testing.T) {
	for _, mode := range []GlyphMode{GlyphModeBasic, GlyphModeAdvanced, ""} {
		chrome := reflect.ValueOf(ChromeFor(mode))
		for i := 0; i < chrome.NumField(); i++ {
			mark := chrome.Field(i).String()
			if ansi.StringWidth(mark) != 1 {
				t.Errorf("%s %s occupies %d cells", mode, chrome.Type().Field(i).Name, ansi.StringWidth(mark))
			}
			if mode == GlyphModeBasic {
				for _, r := range mark {
					if (r < '!' || r > '~') && !strings.ContainsRune("─│█", r) {
						t.Errorf("unverified compatible decoration %q", mark)
					}
				}
			}
		}
	}
	if ChromeFor("") != ChromeFor(DefaultGlyphMode) {
		t.Fatal("unset chrome did not follow the default")
	}
}
