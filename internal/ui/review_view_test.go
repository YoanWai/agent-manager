package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestWrapTintedPreservesText(t *testing.T) {
	rows := wrapTinted("func main() {", nil, bgAdd, bgAddSpan, 40)
	if len(rows) != 1 {
		t.Fatalf("short line should be one row: %d", len(rows))
	}
	// A tinted row pads to width so the background fills the column.
	if got := strings.TrimRight(ansi.Strip(rows[0]), " "); got != "func main() {" {
		t.Fatalf("text mangled: %q", got)
	}
	if !strings.HasPrefix(rows[0], bgAdd) {
		t.Fatalf("background not opened: %q", rows[0])
	}
}

func TestWrapTintedReemitsBackgroundAfterReset(t *testing.T) {
	rows := wrapTinted("\x1b[38;5;197mfunc\x1b[0m main", nil, bgDel, bgDelSpan, 40)
	tinted := rows[0]
	if !strings.HasPrefix(ansi.Strip(tinted), "func main") {
		t.Fatalf("text mangled: %q", ansi.Strip(tinted))
	}
	resetAt := strings.Index(tinted, "\x1b[0m")
	if resetAt < 0 || !strings.HasPrefix(tinted[resetAt+4:], bgDel) {
		t.Fatalf("background must be re-opened after chroma reset: %q", tinted)
	}
}

func TestWrapTintedSpanSwitch(t *testing.T) {
	rows := wrapTinted("if t <= exp", []diff.Span{{Start: 5, End: 7}}, bgAdd, bgAddSpan, 40)
	if !strings.Contains(rows[0], bgAddSpan) {
		t.Fatalf("span background missing: %q", rows[0])
	}
	if !strings.HasPrefix(ansi.Strip(rows[0]), "if t <= exp") {
		t.Fatalf("text mangled: %q", ansi.Strip(rows[0]))
	}
}

func TestWrapTintedWordBoundary(t *testing.T) {
	line := "hello world foo bar"
	rows := wrapTinted(line, nil, "", "", 14)
	stripped := ansi.Strip(strings.Join(rows, ""))
	stripped = strings.TrimSpace(stripped)
	words := strings.Fields(stripped)
	if len(words) != 4 {
		t.Fatalf("expected 4 words, got %d: %q", len(words), words)
	}
	for _, w := range words {
		if !strings.Contains("hello world foo bar", w) {
			t.Fatalf("unexpected word fragment: %q", w)
		}
	}
}

func TestWrapTintedLongWordFallsBack(t *testing.T) {
	word := strings.Repeat("x", 60)
	line := "before " + word + " after"
	rows := wrapTinted(line, nil, "", "", 20)
	stripped := ansi.Strip(strings.Join(rows, ""))
	if !strings.Contains(stripped, "before") {
		t.Fatal("missing before")
	}
	if !strings.Contains(stripped, "after") {
		t.Fatal("missing after")
	}
	if xs := strings.Count(stripped, "x"); xs != 60 {
		t.Fatalf("expected 60 x's, got %d", xs)
	}
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > 20 {
			t.Fatalf("row exceeds width: %d", w)
		}
	}
}

func TestWrapTintedWordWrapWithSpans(t *testing.T) {
	line := "if a > b return c"
	rows := wrapTinted(line, []diff.Span{{Start: 5, End: 6}}, bgAdd, bgAddSpan, 10)
	stripped := ansi.Strip(strings.Join(rows, ""))
	stripped = strings.ReplaceAll(stripped, " ", "")
	if stripped != "ifa>breturnc" {
		t.Fatalf("content lost or reordered: %q", stripped)
	}
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > 10 {
			t.Fatalf("row exceeds width: %d row=%q", w, row)
		}
	}
}

func TestWrapTintedWordWrapNoSpaces(t *testing.T) {
	long := strings.Repeat("x", 100)
	rows := wrapTinted(long, nil, "", "", 20)
	if len(rows) != 5 {
		t.Fatalf("100 cols / 20 = 5 rows, got %d", len(rows))
	}
	var joined string
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > 20 {
			t.Fatalf("row exceeds width: %d", w)
		}
		joined += ansi.Strip(row)
	}
	if joined != long {
		t.Fatalf("wrap lost content: %q", joined)
	}
}

func TestHighlightFileBothSides(t *testing.T) {
	fd := diff.BuildFile(
		[]byte("package a\n\nfunc A() {}\n"),
		[]byte("package a\n\nfunc A() int { return 1 }\n"),
		git.ChangedFile{Path: "a.go", OldPath: "a.go", Status: git.Modified}, git.FileStat{})
	hl := highlightFile(&fd)
	if hl == nil {
		t.Fatal("highlight is nil")
	}
	if first := hl.Line(0, ""); !strings.Contains(first, "\x1b[") {
		t.Fatalf("go source should highlight: %q", first)
	}
	assertHighlightMatchesText(t, &fd, hl)
}

// A hunk-only model skips file lines, so highlighting keyed by line number
// would colour every line after the first gap with another line's tokens.
func TestHighlightFileHunkModel(t *testing.T) {
	fd := bigEditedFile(t)
	if fd.Lines[0].Kind != diff.Gap {
		t.Fatalf("want a hunk model opening with a gap, got %d lines", len(fd.Lines))
	}
	hl := highlightFile(&fd)
	var highlighted strings.Builder
	for i := range fd.Lines {
		highlighted.WriteString(hl.Line(i, ""))
	}
	if !strings.Contains(highlighted.String(), "\x1b[") {
		t.Fatal("go source should highlight")
	}
	assertHighlightMatchesText(t, &fd, hl)
}

// The point of the hunk model: one edit deep inside a file too big for the
// whole-file model still reaches the screen.
func TestReviewRendersHunksForBigFile(t *testing.T) {
	fd := bigEditedFile(t)
	m := &Model{width: 100, height: 30, mode: modeDiff}
	seedReviewForTest(m, uireview.Target{ID: "s"}, git.ScopeUncommitted, "/repo",
		diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{fd}}, true)
	for _, split := range []bool{false, true} {
		setReviewSideBySide(m, split)
		code := ansi.Strip(m.viewDiffCode(160, 20))
		if !strings.Contains(code, `var name11000 = "edited"`) || !strings.Contains(code, "11002") {
			t.Fatalf("split=%v: the edit and its line number should render, got:\n%s", split, code)
		}
		if !strings.Contains(code, "⋯") {
			t.Fatalf("split=%v: skipped lines should be marked, got:\n%s", split, code)
		}
	}
}

// bigEditedFile is one edit at line 11002 of a file past the whole-file
// line threshold, so it can only render through the hunk model.
func bigEditedFile(t *testing.T) diff.FileDiff {
	t.Helper()
	var oldText strings.Builder
	oldText.WriteString("package a\n")
	for i := 0; i < 12000; i++ {
		fmt.Fprintf(&oldText, "var name%d = %q\n", i, fmt.Sprintf("value %d", i))
	}
	newText := strings.Replace(oldText.String(), `var name11000 = "value 11000"`, `var name11000 = "edited"`, 1)
	fd := diff.BuildFile([]byte(oldText.String()), []byte(newText),
		git.ChangedFile{Path: "a.go", OldPath: "a.go", Status: git.Modified}, git.FileStat{Adds: 1, Dels: 1})
	if fd.Err != nil {
		t.Fatal(fd.Err)
	}
	if len(fd.Lines) == 0 {
		t.Fatal("the hunk model is empty")
	}
	return fd
}

func assertHighlightMatchesText(t *testing.T, fd *diff.FileDiff, hl *fileHL) {
	t.Helper()
	for i, line := range fd.Lines {
		if got := ansi.Strip(hl.Line(i, line.Text)); got != line.Text {
			t.Fatalf("line %d highlight drifted: %q vs %q", i, got, line.Text)
		}
	}
}

// The review header always names the repo and its branch; in branch scope it
// shows the base and the branch it diffs into.
func TestReviewHeaderShowsRepoBranchAndBase(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "hdr", dir)

	header := m.viewDiffHeader("hdr")
	if !strings.Contains(header, filepath.Base(dir)) {
		t.Fatalf("header should name the repo, got %q", header)
	}
	if !strings.Contains(header, "main") {
		t.Fatalf("header should show the branch, got %q", header)
	}

	for m.review.Snapshot().Scope != git.ScopeBranch {
		m.drainCmds(t, m.cycleDiffScope())
	}
	header = m.viewDiffHeader("hdr")
	if !strings.Contains(header, "→ main") {
		t.Fatalf("branch scope header should show base → branch, got %q", header)
	}
}

// The target pill in the header is the one place a reviewer figures out
// what they are diffing into, so it has to read cleanly: the @<hash>
// suffix BaseDesc carries internally is dropped, each changeable pill
// wears its key, and an auto-detected target says so out loud while an
// explicitly set one does not.
func TestReviewHeaderTargetLabelCleanAndKeyed(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithSecondBranch(t)
	openReviewOn(t, m, "hdr", dir)
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}
	for m.review.Snapshot().Scope != git.ScopeBranch {
		m.drainCmds(t, m.cycleDiffScope())
	}

	header := ansi.Strip(m.viewDiffHeader("hdr"))
	if !strings.Contains(header, "B ") || !strings.Contains(header, "main") {
		t.Fatalf("target pill should wear its B key, got %q", header)
	}
	if strings.Contains(header, "@") {
		t.Fatalf("header should drop the @hash suffix from the target, got %q", header)
	}
	if !strings.Contains(header, "(auto)") {
		t.Fatalf("auto-detected target should be marked, got %q", header)
	}

	if err := m.services.store.SetReviewBase(sess.ID, m.review.Snapshot().RepoSelected, "feature"); err != nil {
		t.Fatal(err)
	}
	request, accepted := m.review.SelectBase("feature")
	if !accepted {
		t.Fatal("explicit base request was rejected")
	}
	m.drainCmds(t, m.reviewLoadCmd(request))
	header = ansi.Strip(m.viewDiffHeader("hdr"))
	if strings.Contains(header, "(auto)") {
		t.Fatalf("explicit target should not be marked auto, got %q", header)
	}
	if !strings.Contains(header, "feature → main") {
		t.Fatalf("header should show the cleaned target → branch, got %q", header)
	}
}

// The header carries the filter state the way it carries the scope and the
// layout, and its readings follow what the list actually shows. A lock file
// is the case the totals turn on: git hands back real add and delete counts
// for it, so a header that kept counting it would disagree with its own list.
func TestReviewHeaderShowsCodeOnlyFilter(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "hdr", gitRepoWithLockFileBetweenTextFiles(t))

	header := ansi.Strip(m.viewDiffHeader("hdr"))
	if !strings.Contains(header, "4 files") {
		t.Fatalf("header should count every changed file, got %q", header)
	}
	if !strings.Contains(header, "+5") || !strings.Contains(header, "−5") {
		t.Fatalf("header should total every changed file, got %q", header)
	}
	if strings.Contains(header, "code only") {
		t.Fatalf("header should not claim a filter that is off, got %q", header)
	}

	m.drainCmds(t, m.toggleCodeOnly())
	header = ansi.Strip(m.viewDiffHeader("hdr"))
	if !regexp.MustCompile(`f\s+code only`).MatchString(header) {
		t.Fatalf("header should show the filter and its key, got %q", header)
	}
	if !strings.Contains(header, "2 files") {
		t.Fatalf("header should count only the files on show, got %q", header)
	}
	if !strings.Contains(header, "+2") || !strings.Contains(header, "−2") {
		t.Fatalf("header should drop the lock file's totals, got %q", header)
	}
}

func BenchmarkHighlightLargeHunkModel(b *testing.B) {
	fd := diff.BuildFile(nil, []byte(strings.Repeat(strings.Repeat("x", 4000)+"\n", 8000)),
		git.ChangedFile{Path: "large.txt", Status: git.Added}, git.FileStat{})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		highlightFile(&fd)
	}
}

func TestDiffScopeCycleAndLayout(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())

	m.applyCmd(t, m.cycleDiffScope())
	if m.review.Snapshot().Scope.String() != "vs target" {
		t.Fatalf("scope = %q", m.review.Snapshot().Scope)
	}

	setReviewSideBySide(m, true)
	if view := ansi.Strip(m.View()); !strings.Contains(view, "split") {
		t.Fatalf("split pill missing:\n%s", view)
	}
}

// Review has to paint exactly the terminal it was given. A frame with more
// rows than the terminal scrolls the top away; a line wider than the
// terminal wraps and pushes every row below it off the bottom, which is how
// the end of a file ends up selected but off screen.
func TestDiffFrameFitsTerminal(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithLongFile(t, 400)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	if m.review.Snapshot().Loading || len(m.review.Snapshot().Set.Files) == 0 {
		t.Fatalf("diff did not load: %q", m.review.Snapshot().Error)
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
				setReviewSideBySide(m, split)
				if m.review.Snapshot().Annotating {
					m.review.AnnotationKey(tea.KeyMsg{Type: tea.KeyEsc})
				}
				if annotating {
					m.openAnnotate()
					typeReviewAnnotation(m, "note")
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

// A file whose line count is unknown must not be silently summed as zero in
// the header: the totals carry a marker instead of asserting an exact count.
func TestHeaderMarksUncountedFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file regardless of mode")
	}
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "counted", dir)
	if strings.Contains(m.viewDiffHeader("counted"), "?") {
		t.Fatal("header should not flag unknown counts when every file is counted")
	}

	locked := filepath.Join(dir, "locked.go")
	if err := os.WriteFile(locked, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })

	set, err := diff.BuildSet(m.services.gitDrv, dir, git.ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	state := m.review.Snapshot()
	m.review.ApplyLoad(uireview.LoadResult{
		TargetID: state.SessionID, Scope: state.Scope, Generation: state.Generation,
		RepoRoot: state.RepoSelected, RepoRoots: state.RepoRoots, Set: set,
	})
	if !strings.Contains(m.viewDiffHeader("counted"), "+?") {
		t.Fatalf("header should mark the uncounted file, got %q", m.viewDiffHeader("counted"))
	}
}

func TestDiffHeaderKeepsCountsWhenNarrow(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 52, 24
	openReviewOn(t, m, "very-long-session-name", gitTestRepo(t))
	got := ansi.Strip(m.viewDiffHeader("very-long-session-name"))
	if !strings.Contains(got, "+") || !strings.Contains(got, "−") {
		t.Fatalf("narrow header dropped counts:\n%s", got)
	}
}

func TestDiffFileListTruncatesAtSlash(t *testing.T) {
	m := buildModel(t)
	openReviewOn(t, m, "paths", gitTestRepo(t))
	set := m.review.SetCopy()
	set.Files[0].File.Path = "internal/api/handlers/sessions.go"
	state := m.review.Snapshot()
	m.review.ApplyLoad(uireview.LoadResult{
		TargetID: state.SessionID, Scope: state.Scope, Generation: state.Generation,
		RepoRoot: state.RepoSelected, RepoRoots: state.RepoRoots, Set: set,
	})
	list := ansi.Strip(m.viewDiffFileList(28, 8))
	if strings.Contains(list, "ternal") {
		t.Fatalf("file list cut mid-segment:\n%s", list)
	}
	if !strings.Contains(list, "sessions.go") {
		t.Fatalf("file list lost the filename:\n%s", list)
	}
}

// The same rule for review's file list: tabbing to the last file has to
// bring it on screen, not just select it.
func TestDiffFileListCursorAlwaysPainted(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithManyFiles(t, 40)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	if m.review.Snapshot().Loading || len(m.review.Snapshot().Set.Files) < 2 {
		t.Fatalf("diff did not load: %q", m.review.Snapshot().Error)
	}

	last := len(m.review.Snapshot().Set.Files) - 1
	for _, size := range []struct{ w, h int }{{80, 20}, {100, 30}, {140, 44}} {
		m.width, m.height = size.w, size.h
		m.drainCmds(t, selectReviewFile(m, last))
		view := ansi.Strip(m.viewDiffFull())
		name := m.review.Snapshot().Set.Files[last].File.Path
		if !strings.Contains(view, name) {
			t.Errorf("%dx%d: file %q is selected but never painted:\n%s", size.w, size.h, name, view)
		}
	}
}

// gitRepoWithManyFiles is a repo with n changed files, enough to overflow
// review's file list.
func gitRepoWithManyFiles(t *testing.T, n int) string {
	t.Helper()
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
	for i := 0; i < n; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file-%02d.txt", i))
		if err := os.WriteFile(path, []byte("seed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-m", "init")
	for i := 0; i < n; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file-%02d.txt", i))
		if err := os.WriteFile(path, []byte("seed\nchanged\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
