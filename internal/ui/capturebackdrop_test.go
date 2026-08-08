package ui

import (
	"strings"
	"testing"
)

// lightThemeNames mirrors TestLightThemesPresent: the themes whose backdrop
// is light, which the luminance split must classify exactly.
var lightThemeNames = map[string]bool{
	"solarized light":  true,
	"catppuccin latte": true,
	"tokyo night day":  true,
	"gruvbox light":    true,
	"rosé pine dawn":   true,
	"paper":            true,
}

func TestLightBackdropClassification(t *testing.T) {
	for _, theme := range themes {
		if got, want := theme.lightBackdrop(), lightThemeNames[theme.Name]; got != want {
			t.Errorf("%s: lightBackdrop() = %v, want %v (Bg %s)", theme.Name, got, want, theme.Bg)
		}
	}
}

func TestSGRDropsColors(t *testing.T) {
	tests := []struct {
		params string
		want   bool
	}{
		{"", true},
		{"0", true},
		{"39", true},
		{"49", true},
		{"0;33", true},
		{"1;31", false},
		{"7", false},
		{"38;5;0", false},
		{"48;5;249", false},
		{"38;2;0;0;0", false},
		{"48;2;15;17;21", false},
		{"38:2::0:0:0", false},
		{"38;2;0;0;0;49", true},
		{"1;38;5;39", false},
		{"38;5;39", false},
	}
	for _, tt := range tests {
		if got := sgrDropsColors(tt.params); got != tt.want {
			t.Errorf("sgrDropsColors(%q) = %v, want %v", tt.params, got, tt.want)
		}
	}
}

func onLightTheme(t *testing.T) {
	t.Helper()
	applyTheme(themes[themeIndex("solarized light")])
	t.Cleanup(func() { applyTheme(themes[0]) })
	if !captureOnDark {
		t.Fatal("captureOnDark not active on solarized light")
	}
}

func TestReassertCaptureColors(t *testing.T) {
	onLightTheme(t)
	got := reassertCaptureColors("\x1b[31mred\x1b[0m plain")
	if !strings.Contains(got, "\x1b[0m"+captureOpen) {
		t.Errorf("reset not followed by capture colors: %q", got)
	}
	kept := "\x1b[38;2;0;0;0mblack"
	if got := reassertCaptureColors(kept); got != kept {
		t.Errorf("explicit color rewritten: %q", got)
	}
}

func TestPreviewLineCaptureBackdrop(t *testing.T) {
	onLightTheme(t)
	line := previewLine("hi", 10)
	if !strings.HasPrefix(line, captureOpen) {
		t.Errorf("capture line does not open with backdrop colors: %q", line)
	}
	if !strings.Contains(line, captureBgSeq+strings.Repeat(" ", 8)+"\x1b[0m") {
		t.Errorf("padding not painted with the backdrop: %q", line)
	}
}

func TestPreviewLineDarkThemeUnchanged(t *testing.T) {
	applyTheme(themes[0])
	if got := previewLine("hi", 4); got != "hi  " {
		t.Errorf("dark theme preview line rewritten: %q", got)
	}
}

func TestCaptureBlankRow(t *testing.T) {
	onLightTheme(t)
	row := captureBlankRow(6)
	if !strings.HasPrefix(row, captureBgSeq) || !strings.HasSuffix(row, "\x1b[0m") {
		t.Errorf("blank row not wrapped in backdrop: %q", row)
	}
	if !strings.Contains(row, strings.Repeat(" ", 6)) {
		t.Errorf("blank row not full width: %q", row)
	}
	if captureBlankRow(0) != "" {
		t.Error("zero width blank row not empty")
	}
}
