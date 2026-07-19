package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

func gitRepoWithTwoChangedFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("git", "init")
	write("a.go", "package a\n\nfunc A() int { return 1 }\n")
	write("b.go", "package a\n\nfunc B() int { return 2 }\n")
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	write("a.go", "package a\n\nfunc A() int { return 10 }\n")
	write("b.go", "package a\n\nfunc B() int { return 20 }\n")
	return dir
}

// drainCmds runs a command chain to exhaustion, feeding every message back
// into Update, so async follow-ups (diff loads, highlights) all land.
func (m *Model) drainCmds(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil && i < 20; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		updated, next := m.Update(msg)
		*m = *updated.(*Model)
		cmd = next
	}
}

func (m *Model) pressDiffKey(t *testing.T, key rune) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
}

func openReviewOn(t *testing.T, m *Model, name, dir string) {
	t.Helper()
	createSession(t, m, name, dir, "")
	m.selectSessionRow(t, name)
	m.drainCmds(t, m.openDiff())
	if m.mode != modeDiff {
		t.Fatalf("openDiff should enter review, err = %q", m.err)
	}
}

// Marking a file reviewed advances to the next unreviewed file; the advanced
// file must still get its syntax highlighting.
func TestSpaceAdvanceKeepsHighlight(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
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
	if m.gitDrv == nil {
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

// While a comment is being written or confirmed, background reloads pause,
// and an in-flight reload result is dropped instead of shifting lines under
// the open editor.
func TestNoReloadWhileAnnotating(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "ann", gitRepoWithTwoChangedFiles(t))
	linesBefore := len(m.currentFileDiff().Lines)

	m.openAnnotate()
	if !m.diff.annotating {
		t.Fatal("openAnnotate should enter annotating mode")
	}
	for i := 0; i < 4; i++ {
		if cmd := m.diffRefreshCmd(); cmd != nil {
			t.Fatal("probe must pause while annotating")
		}
	}
	// An in-flight reload from before the comment box opened is dropped.
	stale := diffLoadedMsg{sessID: m.diff.sessID, scope: m.diff.scope, gen: m.diff.gen}
	if cmd := m.handleDiffLoaded(stale); cmd != nil {
		t.Fatal("stale reload should be dropped without follow-up")
	}
	if got := len(m.currentFileDiff().Lines); got != linesBefore {
		t.Errorf("reload replaced the diff under the comment box: %d -> %d lines", linesBefore, got)
	}

	m.diff.annotating = false
	m.diff.sendConfirm = true
	if cmd := m.diffRefreshCmd(); cmd != nil {
		t.Fatal("probe must pause while confirming a send")
	}
}

// A reload that shifts line numbers re-points saved comments at the line
// carrying their excerpt, so the agent receives the location the user meant.
func TestAnnotationsReanchorAfterReload(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "anchor", dir)
	m.pressDiffKey(t, 'n') // jump to the changed line (return 10)
	m.openAnnotate()
	m.diff.annInput.SetValue("note")
	m.saveAnnotation()
	notes := m.diff.annotations[m.diff.sessID]
	if len(notes) != 1 || notes[0].line != 3 {
		t.Fatalf("annotation = %+v, want line 3", notes)
	}

	shifted := "package a\n\n// pushed down\nfunc A() int { return 10 }\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(shifted), 0o644); err != nil {
		t.Fatal(err)
	}
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}
	m.drainCmds(t, m.retargetDiff(sess))
	notes = m.diff.annotations[m.diff.sessID]
	if len(notes) != 1 || notes[0].line != 4 {
		t.Fatalf("annotation after reload = %+v, want line 4", notes)
	}
}

// Ctrl+C quits from review mode like it does from the list.
func TestReviewCtrlCQuits(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "quitter", gitRepoWithTwoChangedFiles(t))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c in review should return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c in review should quit")
	}
}

func TestExcerptKeepsRuneBoundary(t *testing.T) {
	line := "  " + strings.Repeat("ש", 70)
	excerpt := excerptOf(line)
	if !utf8.ValidString(excerpt) {
		t.Fatalf("excerpt split a rune: %q", excerpt)
	}
	if got := len([]rune(excerpt)); got != 60 {
		t.Fatalf("excerpt rune count = %d, want 60", got)
	}
	if short := excerptOf("  short  "); short != "short" {
		t.Fatalf("short excerpt = %q", short)
	}
}
