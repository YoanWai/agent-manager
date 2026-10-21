// Package theme holds the built-in palettes and the color math every UI
// package derives its tones from. It imports nothing from the module, so a
// feature package can read the palette without reaching the root's styles.
package theme

import (
	"fmt"
	"strconv"
	"strings"
)

// Index finds a theme by name, falling back to the default when the
// stored name is unknown (a theme removed between releases).
func Index(name string) int {
	for i, t := range Palettes {
		if t.Name == name {
			return i
		}
	}
	return 0
}

func (t Theme) LightBackdrop() bool {
	r, g, b := HexRGB(t.Bg)
	return 0.2126*float64(r)+0.7152*float64(g)+0.0722*float64(b) > 128
}

// AutoName keeps the stored theme whenever it already sits on the
// wanted side, and otherwise prefers its counterpart, so auto-detect
// corrects polarity without discarding the family the user picked.
func AutoName(stored string, wantLight bool) string {
	theme := Palettes[Index(stored)]
	if theme.LightBackdrop() == wantLight {
		return stored
	}
	if theme.Counterpart != "" {
		return theme.Counterpart
	}
	if wantLight {
		return "solarized light"
	}
	return "classic"
}

// HexRGB parses "#rrggbb"; an unparseable value reads as black, which is
// visible enough in a diff to be caught rather than silently mistinted.
func HexRGB(hex string) (int, int, int) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0
	}
	val, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return int(val>>16) & 0xff, int(val>>8) & 0xff, int(val) & 0xff
}

// Mix blends two hex colors, ratio 0 returning a and 1 returning b. Used
// for derived tones (row gutters, gauge tracks) so a theme only has to
// declare its anchors.
func Mix(a, b string, ratio float64) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	ar, ag, ab := HexRGB(a)
	br, bg, bb := HexRGB(b)
	blend := func(x, y int) int {
		return int(float64(x)*(1-ratio) + float64(y)*ratio + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", blend(ar, br), blend(ag, bg), blend(ab, bb))
}
