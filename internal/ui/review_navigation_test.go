package ui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

// The cursor's line has to be inside the rows review actually paints. A
// viewport taller than its painted area lets the cursor walk past the last
// visible row: the selection is at the end of the file, the screen is not.
func TestDiffCursorStaysOnScreen(t *testing.T) {
	const lines = 400
	m := buildModel(t)
	dir := gitRepoWithLongFile(t, lines)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
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
			m.drainCmds(t, m.openDiff())
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
	m.drainCmds(t, m.openDiff())
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

// Long lines wrap onto several painted rows, but the cursor and scroll
// count logical lines. If the window math ignores the wraps, every wrapped
// line on screen pushes one more line of the file's tail off the bottom:
// the cursor lands on the last line and the screen never shows it.
func TestDiffReviewReachesEndWithWrappedLines(t *testing.T) {
	const lines = 80
	for _, layout := range []struct {
		name  string
		split bool
	}{{"unified", false}, {"side-by-side", true}} {
		t.Run(layout.name, func(t *testing.T) {
			m := buildModel(t)
			dir := gitRepoWithWideFile(t, lines, 220)
			createSession(t, m, "coder", dir, "")
			m.selectSessionRow(t, "coder")
			m.drainCmds(t, m.openDiff())
			if m.diff.loading || len(m.diff.set.Files) == 0 {
				t.Fatalf("diff did not load: %q", m.diff.errText)
			}
			m.width, m.height = 120, 34
			m.diff.sideBySide = layout.split

			m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
			last := fmt.Sprintf("wide-%03d", lines)
			if view := ansi.Strip(m.View()); !strings.Contains(view, last) {
				t.Fatalf("G should paint the last line %q, got:\n%s", last, view)
			}

			// Stepping down must keep the cursor painted the whole way.
			m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
			for i := 0; i < lines+5; i++ {
				m.handleDiffKey(tea.KeyMsg{Type: tea.KeyDown})
				fd := m.currentFileDiff()
				lineIdx := m.cursorDiffLine()
				if fd == nil || lineIdx >= len(fd.Lines) {
					t.Fatal("cursor out of range")
				}
				marker := strings.Fields(fd.Lines[lineIdx].Text)[0]
				if view := ansi.Strip(m.View()); !strings.Contains(view, marker) {
					t.Fatalf("step %d: cursor on %q but the frame never paints it:\n%s", i, marker, view)
				}
			}
		})
	}
}

// Marking a file reviewed advances to the next unreviewed file; the advanced
// file must still get its syntax highlighting.
func TestSpaceAdvanceKeepsHighlight(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "hl", gitRepoWithTwoChangedFiles(t))
	if len(m.diff.set.Files) != 2 {
		t.Fatalf("want 2 files, got %d (err=%q)", len(m.diff.set.Files), m.diff.errText)
	}
	if m.currentHL() == nil {
		t.Fatal("first file should be highlighted after open")
	}
	m.pressDiffKey(t, ' ')
	if m.diff.fileIdx != 1 {
		t.Fatalf("space should advance to the next file, idx = %d", m.diff.fileIdx)
	}
	if m.currentHL() == nil {
		t.Error("advanced file lost its highlight: switch command was dropped")
	}
}

// Scroll positions are per session and per scope; a second session touching
// the same path must open the file at the top.
func TestScrollDoesNotLeakAcrossSessions(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "one", dir)
	firstFile := m.diff.set.Files[0].File.Path
	m.diff.scroll = 2
	m.drainCmds(t, m.switchDiffFile(1)) // persists scroll for file one

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	openReviewOn(t, m, "two", dir)
	m.drainCmds(t, m.switchDiffFile(1))
	m.drainCmds(t, m.switchDiffFile(1)) // wraps back to the first file
	if fd := m.currentFileDiff(); fd == nil || fd.File.Path != firstFile {
		t.Fatalf("expected to land back on %q", firstFile)
	}
	if m.diff.scroll != 0 {
		t.Errorf("session two inherited session one's scroll: %d", m.diff.scroll)
	}
}

// A compiled artifact is dropped on its name alone, which is what a file git
// has not classified yet needs: an untracked one carries no numstat verdict,
// and nothing sniffs its bytes until the cursor reaches it.
func TestNonCodePathNamesCompiledArtifacts(t *testing.T) {
	hidden := []string{
		"build/Main.class", "app/__pycache__/mod.pyc", "assets/logo.PNG",
		"go.sum", "Cargo.lock", "web/package-lock.json", "vendor/lib.so",
	}
	for _, path := range hidden {
		if !nonCodePath(path) {
			t.Errorf("%q should be filtered out of a code-only review", path)
		}
	}
	shown := []string{"main.go", "Main.java", "mod.py", "readme.md", "classy.go"}
	for _, path := range shown {
		if nonCodePath(path) {
			t.Errorf("%q is source and should stay in the review", path)
		}
	}
}

// f drops the files git calls binary out of the review, and neither the
// selection nor a file switch is left on one of them.
func TestReviewCodeOnlyHidesBinaryFiles(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "filter", gitRepoWithBinaryBetweenTextFiles(t))

	binary := -1
	for i := range m.diff.set.Files {
		if m.diff.set.Files[i].File.Path == "b.dat" {
			binary = i
		}
	}
	if binary < 0 {
		t.Fatalf("b.dat missing from the diff set: %+v", m.diff.set.Files)
	}
	if !m.diff.set.Files[binary].Stat.Binary {
		t.Fatal("numstat should mark b.dat binary before its content is read")
	}
	m.diff.fileIdx = binary

	m.pressFilterKey(t)
	if !m.diff.codeOnly {
		t.Fatal("f should turn the code-only filter on")
	}
	if m.diff.fileIdx == binary {
		t.Fatal("the selection should leave a file the filter hides")
	}
	list := ansi.Strip(m.viewDiffFileList(60, 20))
	if strings.Contains(list, "b.dat") {
		t.Fatalf("b.dat should be filtered out of the file list:\n%s", list)
	}
	if !strings.Contains(list, "a.go") || !strings.Contains(list, "c.go") {
		t.Fatalf("the code files should stay listed:\n%s", list)
	}

	for i := range m.diff.set.Files {
		if m.diff.set.Files[i].File.Path == "a.go" {
			m.diff.fileIdx = i
		}
	}
	m.drainCmds(t, m.switchDiffFile(1))
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "c.go" {
		t.Fatalf("the file after a.go = %q, want c.go", got)
	}
	m.drainCmds(t, m.switchDiffFile(-1))
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "a.go" {
		t.Fatalf("the file before c.go = %q, want a.go", got)
	}

	m.pressFilterKey(t)
	if m.diff.codeOnly {
		t.Fatal("f again should show the binary files")
	}
	if list := ansi.Strip(m.viewDiffFileList(60, 20)); !strings.Contains(list, "b.dat") {
		t.Fatalf("b.dat should be back in the file list:\n%s", list)
	}
}

// A newly written image and a regenerated lock file are the common case, and
// neither has a git verdict to go on: the filter has to settle them by name so
// they go the moment the key is pressed and stay gone across a silent reload.
func TestReviewCodeOnlyHidesUntrackedAndLockFiles(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := writeGitRepo(t,
		map[string]string{
			"a.go":   "package a\n\nfunc A() int { return 1 }\n",
			"go.sum": "mod v1.0.0 h1:aaa=\n",
			"src.go": "package a\n\nfunc S() int { return 2 }\n",
		},
		map[string]string{
			"a.go":   "package a\n\nfunc A() int { return 10 }\n",
			"go.sum": "mod v1.0.1 h1:bbb=\n",
			"src.go": "package a\n\nfunc S() int { return 20 }\n",
			"z.png":  "\x89PNG\r\n\x1a\n\x00\x00\x00\x00new",
		})
	openReviewOn(t, m, "blobs", dir)

	untracked := m.fileDiffByPath("z.png")
	if untracked == nil {
		t.Fatalf("z.png missing from the diff set: %+v", m.diff.set.Files)
	}
	if untracked.Loaded() {
		t.Fatal("an untracked file the cursor never reached should stay unloaded")
	}
	if !untracked.StatKnown() || !untracked.Stat.Binary {
		t.Fatal("an untracked image should already count as binary in the file list")
	}

	m.pressFilterKey(t)
	list := ansi.Strip(m.viewDiffFileList(60, 20))
	if strings.Contains(list, "z.png") || strings.Contains(list, "go.sum") {
		t.Fatalf("the image and the lock file should be off the list:\n%s", list)
	}
	if !strings.Contains(list, "a.go") || !strings.Contains(list, "src.go") {
		t.Fatalf("the code files should stay listed:\n%s", list)
	}

	// Reverting the file under the cursor drops it from the reloaded set, so the
	// selection falls on go.sum unless the reload settles it on the spot. The
	// load it schedules is drained afterwards: the list is rendered between the
	// two, and a cursor parked on a hidden row draws no cursor at all.
	writeRepoFile(t, dir, "a.go", "package a\n\nfunc A() int { return 1 }\n")
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}
	m.diff.gen++
	updated, cmd := m.Update(m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, true)())
	*m = *updated.(*Model)
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "src.go" {
		t.Fatalf("the reload should settle the selection itself, landed on %q", got)
	}
	m.drainCmds(t, cmd)
	if list := ansi.Strip(m.viewDiffFileList(60, 20)); strings.Contains(list, "z.png") {
		t.Fatalf("the filter should survive a silent reload:\n%s", list)
	}
}

// A blob whose name gives nothing away is only outed when its load sniffs a
// NUL, so the cursor has to be carried off it the way every other file switch
// moves: with the file it lands on scrolled back where the user left it.
func TestReviewCodeOnlyCarriesCursorOffASniffedBlob(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	var long, edited strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&long, "line %d\n", i)
		fmt.Fprintf(&edited, "line %d\n", i)
	}
	edited.WriteString("tail\n")
	dir := writeGitRepo(t,
		map[string]string{
			"a.txt": long.String(),
			"c.go":  "package a\n\nfunc C() int { return 3 }\n",
		},
		map[string]string{
			"a.txt":    edited.String(),
			"c.go":     "package a\n\nfunc C() int { return 30 }\n",
			"blob.dat": "\x00\x01\x02new",
		})
	openReviewOn(t, m, "sniff", dir)
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "a.txt" {
		t.Fatalf("review should open on a.txt, got %q", got)
	}

	m.pressFilterKey(t)
	m.diff.scroll = 150
	m.diff.cursorLine = 150
	m.drainCmds(t, m.switchDiffFile(1))
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "c.go" {
		t.Fatalf("the file after a.txt = %q, want c.go", got)
	}

	m.pressDiffKey(t, 'J')
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "a.txt" {
		t.Fatalf("the load should carry the cursor off blob.dat, landed on %q", got)
	}
	if m.diff.scroll != 150 {
		t.Fatalf("a.txt scroll = %d, want the 150 it was left at", m.diff.scroll)
	}
}

// space walks to the next file still to review, and a file the filter hides is
// not one the user can review.
func TestReviewCodeOnlySpaceSkipsHiddenFile(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "queue", writeGitRepo(t,
		map[string]string{
			"a.go":  "package a\n\nfunc A() int { return 1 }\n",
			"b.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\x00one",
			"c.go":  "package a\n\nfunc C() int { return 3 }\n",
			"d.go":  "package a\n\nfunc D() int { return 4 }\n",
		},
		map[string]string{
			"a.go":  "package a\n\nfunc A() int { return 10 }\n",
			"b.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\x00two",
			"c.go":  "package a\n\nfunc C() int { return 30 }\n",
			"d.go":  "package a\n\nfunc D() int { return 40 }\n",
		}))
	paths := make([]string, len(m.diff.set.Files))
	for i := range m.diff.set.Files {
		paths[i] = m.diff.set.Files[i].File.Path
	}
	want := []string{"a.go", "b.png", "c.go", "d.go"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("file order = %v, want %v", paths, want)
	}

	m.pressFilterKey(t)
	m.drainCmds(t, m.switchDiffFile(2))
	m.pressDiffKey(t, ' ')
	if !m.fileReviewed("c.go") {
		t.Fatal("space should mark c.go reviewed")
	}
	m.drainCmds(t, m.switchDiffFile(-3))
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "a.go" {
		t.Fatalf("the cursor should be back on a.go, got %q", got)
	}

	m.pressDiffKey(t, ' ')
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "d.go" {
		t.Fatalf("space should walk past the hidden b.png and the reviewed c.go to d.go, landed on %q", got)
	}
}

// Filtering a review whose every change is a blob leaves both panes with
// nothing to show, which has to read as a state rather than as a broken pane,
// and leaves tab with nowhere to go.
func TestReviewCodeOnlyWithNoCodeFiles(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "onlybin", writeGitRepo(t,
		map[string]string{
			"b.dat":    "\x00\x01\x02one",
			"logo.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\x00one",
		},
		map[string]string{
			"b.dat":    "\x00\x01\x02two",
			"logo.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\x00two",
		}))

	m.pressFilterKey(t)
	if list := ansi.Strip(m.viewDiffFileList(30, 20)); !strings.Contains(list, "no code files") {
		t.Fatalf("the file list should say why it is empty:\n%s", list)
	}
	code := ansi.Strip(m.viewDiffCode(60, 20))
	if strings.Contains(code, "b.dat") {
		t.Fatalf("the code pane should not render a hidden file:\n%s", code)
	}
	if !strings.Contains(code, "no code files") {
		t.Fatalf("the code pane should say why it is empty:\n%s", code)
	}

	m.pressDiffKey(t, 'J')
	if m.diff.fileIdx != 0 {
		t.Fatalf("tab should not walk the selection through hidden files, fileIdx = %d", m.diff.fileIdx)
	}

	path := m.diff.set.Files[m.diff.fileIdx].File.Path
	m.pressDiffKey(t, ' ')
	if m.fileReviewed(path) {
		t.Fatal("space should not mark a file the filter is hiding")
	}
	m.pressDiffKey(t, 'c')
	if m.diff.annotating {
		t.Fatal("c should not comment a file the filter is hiding")
	}
}

func TestDiffPageKeysMoveAViewport(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithLongFile(t, 80)
	openReviewOn(t, m, "pager", dir)
	before := m.diff.cursorLine
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.diff.cursorLine <= before {
		t.Fatalf("pgdown left cursor at %d", m.diff.cursorLine)
	}
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.diff.cursorLine != before {
		t.Fatalf("pgup should return to %d, got %d", before, m.diff.cursorLine)
	}
}

func TestNarrowReviewKeepsBothPanesMeasurable(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "narrow", gitTestRepo(t))
	for _, width := range []int{29, 24, 10, 1} {
		m.width = width
		fileWidth, codeWidth := m.diffPaneWidths()
		if fileWidth < 0 || codeWidth < 0 {
			t.Fatalf("width %d gave panes %d and %d", width, fileWidth, codeWidth)
		}
		if fileWidth+codeWidth > width {
			t.Fatalf("width %d gave panes wider than the screen: %d and %d", width, fileWidth, codeWidth)
		}
		if lines := splitLines(m.View()); len(lines) == 0 {
			t.Fatalf("width %d rendered nothing", width)
		}
	}
}
