package ui

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

// The status bar carries git and filesystem errors, whose text quotes a path
// or a ref that can hold control bytes, on both the failure and the outcome
// branch.
func TestStatusMessageEscapesControlBytes(t *testing.T) {
	payload := "boom \x1b]0;P\x07\x1b[2J"
	const want = "boom ^[]0;P^G^[[2J"

	var m Model
	m.errBar.text = payload
	if got := ansi.Strip(m.statusMessage("✖", "✔", "▲")); !strings.Contains(got, want) {
		t.Errorf("failure branch should read as caret notation, got %q", got)
	}
	if stray := strayControl(m.statusMessage("✖", "✔", "▲")); stray != "" {
		t.Errorf("failure branch leaks a control byte near %q", stray)
	}

	m.errBar.done = payload
	if !m.errBar.worked() {
		t.Fatal("matching text and done should read as an outcome")
	}
	if got := ansi.Strip(m.statusMessage("✖", "✔", "▲")); !strings.Contains(got, want) {
		t.Errorf("outcome branch should read as caret notation, got %q", got)
	}
	if stray := strayControl(m.statusMessage("✖", "✔", "▲")); stray != "" {
		t.Errorf("outcome branch leaks a control byte near %q", stray)
	}
}

// A copy that went through with a caveat reads as neither the green
// outcome nor the red failure: it takes the warning glyph and the tone the
// theme gives work in progress.
func TestStatusMessageWarnsInItsOwnTone(t *testing.T) {
	var m Model
	m.reportWarn("copied the whole pane")

	got := m.statusMessage("✖", "✔", "▲")
	if !strings.Contains(ansi.Strip(got), "▲ copied the whole pane") {
		t.Fatalf("warning should take the warning glyph, got %q", ansi.Strip(got))
	}
	if got != warnStyle.Render("▲ copied the whole pane") {
		t.Fatalf("warning should render in the warning style, got %q", got)
	}
	if got == doneStyle.Render("✔ copied the whole pane") || got == errStyle.Render("✖ copied the whole pane") {
		t.Fatal("warning must not read as an outcome or a failure")
	}
}
