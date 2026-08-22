package ui

import (
	"strings"
	"testing"
)

// A picker row names a repo directory or a git ref, and both reach us as raw
// bytes, so the row has to show a control byte rather than hand it to the
// terminal.
func TestRepoPickRowEscapesControlBytes(t *testing.T) {
	m := &Model{width: 120}
	row := pickRow{label: "br\x1b]0;P\x07anch", root: "/tmp/re\x1b[2Jpo/leaf"}

	for _, selected := range []bool{false, true} {
		out := m.repoPickRow(row, selected)
		if !strings.Contains(out, "br^[]0;P^Ganch") {
			t.Errorf("selected=%v: label should read as caret notation, got %q", selected, out)
		}
		if !strings.Contains(out, "/tmp/re^[[2Jpo") {
			t.Errorf("selected=%v: root should read as caret notation, got %q", selected, out)
		}
		if stray := strayControl(out); stray != "" {
			t.Errorf("selected=%v: picker row leaks a control byte near %q", selected, stray)
		}
	}
}
