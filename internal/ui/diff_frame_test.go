package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Review has to paint exactly the terminal it was given. A frame with more
// rows than the terminal scrolls the top away; a line wider than the
// terminal wraps and pushes every row below it off the bottom, which is how
// the end of a file ends up selected but off screen.
func TestDiffFrameFitsTerminal(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithLongFile(t, 400)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.applyCmd(t, m.openDiff())
	if m.diff.loading || len(m.diff.set.Files) == 0 {
		t.Fatalf("diff did not load: %q", m.diff.errText)
	}

	// Narrow widths matter as much as short ones: the footer wraps onto
	// extra lines there, and every row the footer takes is a row the body
	// must give back.
	sizes := []struct{ w, h int }{
		{60, 16}, {60, 20}, {70, 18}, {80, 24}, {90, 22}, {96, 28}, {100, 30},
		{110, 26}, {120, 40}, {132, 34}, {160, 50}, {200, 60}, {240, 70},
	}
	for _, split := range []bool{false, true} {
		for _, annotating := range []bool{false, true} {
			for _, size := range sizes {
				m.width, m.height = size.w, size.h
				m.diff.sideBySide = split
				m.diff.annotating = false
				if annotating {
					m.openAnnotate()
					m.diff.annInput.SetValue("note")
				}

				raw := strings.Split(m.viewDiffFull(), "\n")
				if len(raw) != size.h {
					t.Errorf("split=%v annotating=%v %dx%d: frame paints %d rows",
						split, annotating, size.w, size.h, len(raw))
				}
				for i, line := range raw {
					if got := ansi.StringWidth(line); got > size.w {
						t.Errorf("split=%v annotating=%v %dx%d: line %d is %d wide: %q",
							split, annotating, size.w, size.h, i, got, ansi.Strip(line))
					}
				}
			}
		}
	}
}

// The cursor's line has to be inside the rows review actually paints. A
// viewport taller than its painted area lets the cursor walk past the last
// visible row: the selection is at the end of the file, the screen is not.
func TestDiffCursorStaysOnScreen(t *testing.T) {
	const lines = 400
	m := buildModel(t)
	dir := gitRepoWithLongFile(t, lines)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.applyCmd(t, m.openDiff())
	if m.diff.loading || len(m.diff.set.Files) == 0 {
		t.Fatalf("diff did not load: %q", m.diff.errText)
	}

	for _, size := range []struct{ w, h int }{{80, 24}, {100, 30}, {120, 40}, {160, 50}} {
		m.width, m.height = size.w, size.h
		m.diff.scroll = 0
		m.diff.cursorLine = 0
		m.moveDiffCursor(lines*2, m.diffCodeHeight())

		fd := m.currentFileDiff()
		if fd == nil {
			t.Fatal("no file diff")
		}
		want := fd.Lines[m.cursorDiffLine()]
		if want.Text == "" {
			continue
		}
		view := ansi.Strip(m.viewDiffFull())
		if !strings.Contains(view, strings.TrimSpace(want.Text)) {
			t.Errorf("%dx%d: cursor sits on %q, which the frame never paints",
				size.w, size.h, strings.TrimSpace(want.Text))
		}
	}
}
