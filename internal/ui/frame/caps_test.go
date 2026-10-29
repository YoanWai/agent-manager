package frame

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestCapsPaintAKeyWithItsAction(t *testing.T) {
	cap := Cap{
		Key:   lipgloss.NewStyle(),
		Label: lipgloss.NewStyle(),
		Badge: lipgloss.NewStyle().Bold(true),
	}
	if got := ansi.Strip(cap.keyCap("r", "rename")); got != "r rename" {
		t.Fatalf("keyCap = %q", got)
	}
	if got := ansi.Strip(cap.hintCap("o", "docs")); got != "o docs" {
		t.Fatalf("the docs launch cap = %q", got)
	}
	if got := ansi.Strip(cap.hintCap("x", "other")); got != "x other" {
		t.Fatalf("another key should not take the badge: %q", got)
	}
}
