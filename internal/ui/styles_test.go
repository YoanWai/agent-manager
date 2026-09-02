package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderSelectedRowReappliesTintAfterLipglossReset(t *testing.T) {
	inner := lipgloss.NewStyle().Bold(true).Render("name") + " rest"
	got := renderSelectedRow(inner)
	reapply := ansi.ResetStyle + bgSeq(current.Surface) + fgSeq(current.Bright)
	if !strings.Contains(got, reapply+" rest") {
		t.Fatalf("tint dropped after the inner reset: %q", got)
	}
}
