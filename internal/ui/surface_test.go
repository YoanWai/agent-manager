package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPaintReappliesFillAfterLipglossReset(t *testing.T) {
	inner := "a" + lipgloss.NewStyle().Bold(true).Render("b") + "c"
	if !strings.Contains(inner, ansi.ResetStyle) {
		t.Fatalf("lipgloss reset form changed: %q", inner)
	}
	fill := bgSeq("#1a1e24")
	got := paint(inner, 6, "#1a1e24")
	if !strings.Contains(got, ansi.ResetStyle+fill+"c") {
		t.Fatalf("fill dropped after the inner reset: %q", got)
	}
}

func TestRenderSelectedRowReappliesTintAfterLipglossReset(t *testing.T) {
	inner := lipgloss.NewStyle().Bold(true).Render("name") + " rest"
	got := renderSelectedRow(inner)
	reapply := ansi.ResetStyle + bgSeq(current.Surface) + fgSeq(current.Bright)
	if !strings.Contains(got, reapply+" rest") {
		t.Fatalf("tint dropped after the inner reset: %q", got)
	}
}
