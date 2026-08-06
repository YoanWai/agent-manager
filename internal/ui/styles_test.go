package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestWashTextFitsContent(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	applyTheme(themes[0])
	inner := keyStyle.Render("focus ") + subtleStyle.Render("hint")
	if !strings.Contains(inner, "\x1b[0m") {
		t.Fatal("styled input must emit ANSI resets for reapply coverage")
	}
	washed := washText(focusWashHex(), inner)
	if lipgloss.Width(washed) != lipgloss.Width(inner) {
		t.Fatalf("wash width %d != content width %d", lipgloss.Width(washed), lipgloss.Width(inner))
	}
	fill := bgSeq(focusWashHex())
	if !strings.Contains(washed, fill) {
		t.Fatal("washText must emit the fill sequence")
	}
	if !strings.Contains(washed, "\x1b[0m"+fill) {
		t.Fatal("washText must re-apply the fill after each ANSI reset")
	}
}
