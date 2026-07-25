package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// gitRepoWithLongFile is a repo whose single change is far taller than any
// terminal, so the review viewport has to scroll to reach the end.
func gitRepoWithLongFile(t *testing.T, lines int) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-m", "init")

	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "line-%03d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The end of a file has to be reachable in review. A viewport sized larger
// than the rows the frame actually paints leaves a tail of lines the cursor
// can address but the screen never shows, which is how the last screenful
// silently went missing once before.
func TestDiffReviewReachesLastLine(t *testing.T) {
	const lines = 400
	for _, layout := range []struct {
		name  string
		split bool
	}{{"unified", false}, {"side-by-side", true}} {
		t.Run(layout.name, func(t *testing.T) {
			m := buildModel(t)
			dir := gitRepoWithLongFile(t, lines)
			createSession(t, m, "coder", dir, "")
			m.selectSessionRow(t, "coder")
			m.applyCmd(t, m.openDiff())
			if m.diff.loading || len(m.diff.set.Files) == 0 {
				t.Fatalf("diff did not load: %q", m.diff.errText)
			}
			m.diff.sideBySide = layout.split

			last := fmt.Sprintf("line-%03d", lines)
			view := ansi.Strip(m.View())
			if strings.Contains(view, last) {
				t.Fatalf("the file's end should start off screen, got:\n%s", view)
			}

			// G jumps to the end; the final line must be painted, not merely
			// selected, and the cursor must sit on it.
			m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
			view = ansi.Strip(m.View())
			if !strings.Contains(view, last) {
				t.Fatalf("G should paint the last line %q, got:\n%s", last, view)
			}
			// The whole tail has to be on screen, not just the final line:
			// a viewport that outruns its painted rows shows the last line
			// and silently drops the ones just above it.
			for n := lines; n > lines-6; n-- {
				if !strings.Contains(view, fmt.Sprintf("line-%03d", n)) {
					t.Fatalf("line %d missing from the end of the file, got:\n%s", n, view)
				}
			}
		})
	}
}

// Stepping down one line at a time has to arrive at the same place G does:
// if the viewport is taller than the painted rows, the walk stops short and
// the tail of the file becomes unreachable by keyboard.
func TestDiffReviewStepsDownToTheEnd(t *testing.T) {
	const lines = 120
	m := buildModel(t)
	dir := gitRepoWithLongFile(t, lines)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.applyCmd(t, m.openDiff())
	if m.diff.loading || len(m.diff.set.Files) == 0 {
		t.Fatalf("diff did not load: %q", m.diff.errText)
	}

	for i := 0; i < lines*2; i++ {
		m.handleDiffKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	last := fmt.Sprintf("line-%03d", lines)
	if view := ansi.Strip(m.View()); !strings.Contains(view, last) {
		t.Fatalf("stepping down should reach the last line %q, got:\n%s", last, view)
	}
}
