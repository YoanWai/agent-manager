package frame

import (
	"strings"
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

func TestDividerFillsItsWidth(t *testing.T) {
	plain := ansi.Strip(Divider("files", 20, lipgloss.NewStyle(), lipgloss.NewStyle()))
	if w := ansi.StringWidth(plain); w != 20 {
		t.Fatalf("divider width = %d, want 20: %q", w, plain)
	}
	if !strings.Contains(plain, "files") {
		t.Fatalf("divider lost its label: %q", plain)
	}
}
