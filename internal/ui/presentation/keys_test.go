package presentation

import "testing"

func TestKeyLabel(t *testing.T) {
	for _, tc := range []struct{ label, basic string }{
		{"↵", "enter"}, {"⌫", "backspace"}, {"↵/tab", "enter/tab"},
		{"y/↵", "y/enter"}, {"space/↵", "space/enter"}, {"n / ↵ / d", "n / enter / d"},
		{"ctrl+r", "ctrl+r"}, {"↑↓ 界e\u0301", "↑↓ 界e\u0301"}, {"", ""},
	} {
		for _, mode := range []GlyphMode{GlyphModeBasic, GlyphModeAdvanced, ""} {
			want := tc.label
			if mode.Effective() == GlyphModeBasic {
				want = tc.basic
			}
			if got := KeyLabel(mode, tc.label); got != want {
				t.Errorf("%s/%q = %q, want %q", mode, tc.label, got, want)
			}
		}
	}
}

func TestNavigationLabelFormatsOnlyFixedDirections(t *testing.T) {
	for _, tc := range []struct{ fixed, basic string }{
		{"↑↓", "up/down"}, {"←→", "left/right"}, {"tab/↑↓", "tab/up/down"},
		{"↑↓/jk", "up/down/jk"}, {"shift+↑", "shift+up"}, {"← →", "left right"},
		{"drag / ↑↓ / wheel", "drag / up/down / wheel"}, {"↕", "up/down"},
	} {
		for _, mode := range []GlyphMode{GlyphModeBasic, GlyphModeAdvanced, ""} {
			want := tc.fixed
			if mode.Effective() == GlyphModeBasic {
				want = tc.basic
			}
			if got := NavigationLabel(mode, tc.fixed); got != want {
				t.Fatalf("%s: %q != %q", mode, got, want)
			}
		}
	}
}
