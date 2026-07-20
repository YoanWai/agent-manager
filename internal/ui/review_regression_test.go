package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
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

// umbrellaWithTwoRepos makes a dir that is not itself a repo but holds two
// nested repos, the second one dirty so it ranks first.
func umbrellaWithTwoRepos(t *testing.T) (umbrella, dirtyName string) {
	t.Helper()
	umbrella = t.TempDir()
	run := func(dir string, args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	for _, name := range []string{"alpha", "bravo"} {
		dir := filepath.Join(umbrella, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() int { return 1 }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		run(dir, "git", "init")
		run(dir, "git", "add", ".")
		run(dir, "git", "commit", "-m", "init")
	}
	dirty := filepath.Join(umbrella, "bravo")
	if err := os.WriteFile(filepath.Join(dirty, "a.go"), []byte("package a\n\nfunc A() int { return 99 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return umbrella, "bravo"
}

// A session whose cwd is an umbrella of several repos opens review on the
// most-active repo, shows the repo in the header, and the r key picks another.
func TestReviewPicksRepoUnderUmbrella(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "umbrella", umbrella)

	if len(m.diff.repoRoots) != 2 {
		t.Fatalf("want 2 repos resolved, got %v (err=%q)", m.diff.repoRoots, m.diff.errText)
	}
	if got := filepath.Base(m.diff.repoSel); got != dirtyName {
		t.Fatalf("want dirty repo %q selected first, got %q", dirtyName, got)
	}
	if !strings.Contains(m.viewDiffHeader("umbrella"), dirtyName) {
		t.Fatalf("header should name the selected repo %q", dirtyName)
	}

	m.pickRepo(t, "alpha")
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("picker should select the other repo, got %q", got)
	}
	if !strings.Contains(m.viewDiffHeader("umbrella"), "alpha") {
		t.Fatal("header should follow the picked repo")
	}
	m.pickRepo(t, dirtyName)
	if got := filepath.Base(m.diff.repoSel); got != dirtyName {
		t.Fatalf("picker should select back, got %q", got)
	}
}

func TestRepoPickerFiltersAndSelects(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "picker", umbrella)
	if filepath.Base(m.diff.repoSel) != dirtyName {
		t.Fatalf("expected to start on %q", dirtyName)
	}

	m.pressDiffKey(t, 'r')
	if m.mode != modeRepoPick {
		t.Fatalf("r should open the repo picker, mode = %v", m.mode)
	}
	for _, r := range "alph" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.filteredRows(); len(got) != 1 || got[0].label != "alpha" {
		t.Fatalf("filter should narrow to alpha, got %v", got)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.mode != modeDiff {
		t.Fatalf("enter should return to review, mode = %v", m.mode)
	}
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("enter should select alpha, got %q", got)
	}
}

func TestRepoPickerEscapeKeepsRepo(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "escpick", umbrella)
	before := m.diff.repoSel

	m.pressDiffKey(t, 'r')
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if m.mode != modeDiff {
		t.Fatalf("esc should return to review, mode = %v", m.mode)
	}
	if m.diff.repoSel != before || filepath.Base(m.diff.repoSel) != dirtyName {
		t.Fatalf("esc should not change the repo, got %q", m.diff.repoSel)
	}
}

func TestBranchPickerListsWorktreesAndSwitches(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	alpha := filepath.Join(umbrella, "alpha")
	outside := filepath.Join(t.TempDir(), "wt-feature")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/pick-me", outside)
	cmd.Dir = alpha
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v: %s", err, out)
	}
	openReviewOn(t, m, "branches", umbrella)
	m.drainCmds(t, m.selectRepo(alpha))

	m.pressDiffKey(t, 'b')
	if m.mode != modeRepoPick {
		t.Fatalf("b should open the branch picker, mode = %v (err=%q)", m.mode, m.err)
	}
	rendered := m.viewRepoPick()
	if !strings.Contains(rendered, "feature/pick-me") {
		t.Fatalf("picker should show the worktree branch, got:\n%s", rendered)
	}
	for _, r := range "pick-me" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	updated, cmdSel := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmdSel)
	resolved, _ := filepath.EvalSymlinks(outside)
	sel, _ := filepath.EvalSymlinks(m.diff.repoSel)
	if sel != resolved {
		t.Fatalf("enter should switch to the worktree, got %q", m.diff.repoSel)
	}
}

// The b picker must seed its cursor on the worktree under review even when
// that worktree was declared via a /tmp path that git resolves to
// /private/tmp, since /tmp is a symlink on macOS.
func TestBranchPickerSeedsCursorForSymlinkedWorktree(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	alpha := filepath.Join(umbrella, "alpha")

	linkedParent, err := os.MkdirTemp("/tmp", "am-p2-symlink-seed-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(linkedParent) })
	if resolved, _ := filepath.EvalSymlinks(linkedParent); resolved == linkedParent {
		t.Skip("/tmp does not resolve to a different path on this system")
	}
	rawWorktree := filepath.Join(linkedParent, "wt-declared")

	runGit := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit(alpha, "worktree", "add", "-b", "feature/declared-symlinked", rawWorktree)

	createSession(t, m, "symseed", umbrella, "")
	m.selectSessionRow(t, "symseed")
	sess, _ := m.selected()
	if err := m.store.SetReviewRepo(sess.ID, rawWorktree); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	if m.err != "" {
		t.Fatalf("declared worktree must not be reported missing, err = %q", m.err)
	}
	if m.diff.repoSel != rawWorktree {
		t.Fatalf("repoSel should stay the raw declared path, got %q", m.diff.repoSel)
	}

	m.pressDiffKey(t, 'b')
	if m.mode != modeRepoPick {
		t.Fatalf("b should open the branch picker, mode = %v (err=%q)", m.mode, m.err)
	}
	resolvedWorktree, _ := filepath.EvalSymlinks(rawWorktree)
	rows := m.filteredRows()
	wantCursor := -1
	for i, row := range rows {
		if resolved, _ := filepath.EvalSymlinks(row.root); resolved == resolvedWorktree {
			wantCursor = i
			break
		}
	}
	if wantCursor == -1 {
		t.Fatalf("declared worktree should appear among picker rows, got %v", rows)
	}
	if wantCursor == 0 {
		t.Fatal("test setup invalid: declared worktree must not already be row 0")
	}
	if m.repoPick.cursor != wantCursor {
		t.Fatalf("cursor should seed on the declared worktree row %d, got %d", wantCursor, m.repoPick.cursor)
	}
}

// A reviewed mark placed on a path in one repo must not bleed onto a
// same-named path in a sibling repo when cycling with r.
func TestReviewMarksIsolatedPerRepo(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "umbrella", umbrella)
	if got := filepath.Base(m.diff.repoSel); got != dirtyName {
		t.Fatalf("want %q selected, got %q", dirtyName, got)
	}
	if fd := m.currentFileDiff(); fd == nil || fd.File.Path != "a.go" {
		t.Fatalf("want a.go under review in the dirty repo, got %v", fd)
	}
	m.drainCmds(t, m.toggleReviewed())
	if !m.fileReviewed("a.go") {
		t.Fatal("a.go should be reviewed in the dirty repo")
	}

	m.pickRepo(t, "alpha")
	if filepath.Base(m.diff.repoSel) != "alpha" {
		t.Fatalf("picker should select alpha, got %q", m.diff.repoSel)
	}
	if m.fileReviewed("a.go") {
		t.Fatal("a.go reviewed mark leaked into the sibling repo")
	}

	m.pickRepo(t, dirtyName)
	if !m.fileReviewed("a.go") {
		t.Fatal("picking back should restore the dirty repo's reviewed mark")
	}
}

// The selected repo is pinned by path, so a reload whose fresh ranking would
// put a different repo first keeps the user on the repo they chose.
func TestRepoSelectionSurvivesReload(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "umbrella", umbrella)

	m.pickRepo(t, "alpha")
	if filepath.Base(m.diff.repoSel) != "alpha" {
		t.Fatalf("want alpha selected, got %q", m.diff.repoSel)
	}
	// A scope cycle reloads through ResolveRepos, which ranks the dirty repo
	// first; the path pin must keep alpha selected regardless.
	m.pressDiffKey(t, 's')
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("reload should keep alpha pinned, got %q", got)
	}
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("repoSel should track the pinned repo after re-rank, got %q", got)
	}
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

// pickRepo drives the repo picker the way a human would: r, type the repo
// name, enter.
func (m *Model) pickRepo(t *testing.T, name string) {
	t.Helper()
	m.pressDiffKey(t, 'r')
	if m.mode != modeRepoPick {
		t.Fatalf("r should open the repo picker, mode = %v", m.mode)
	}
	for _, r := range name {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		*m = *updated.(*Model)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.mode != modeDiff {
		t.Fatalf("enter should return to review, mode = %v", m.mode)
	}
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

// refreshDiff drives the silent same-scope reload path (the probe piggyback),
// the only reload that re-anchors comments.
func (m *Model) refreshDiff(t *testing.T) {
	t.Helper()
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}
	m.diff.gen++
	m.drainCmds(t, m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, true))
}

// A silent same-scope reload that shifts line numbers re-points saved comments
// at the line carrying their excerpt, so the agent gets the location meant.
func TestAnnotationsReanchorAfterRefresh(t *testing.T) {
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
	notes := m.diff.annotations[m.reviewKey()]
	if len(notes) != 1 || notes[0].line != 3 {
		t.Fatalf("annotation = %+v, want line 3", notes)
	}

	shifted := "package a\n\n// pushed down\nfunc A() int { return 10 }\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(shifted), 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshDiff(t)
	if notes = m.diff.annotations[m.reviewKey()]; len(notes) != 1 || notes[0].line != 4 {
		t.Fatalf("annotation after refresh = %+v, want line 4", notes)
	}
}

// A scope cycle loads a different file set; it must not re-anchor a comment's
// stored line against content it was never made against.
func TestScopeCycleDoesNotReanchor(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "scoped", gitRepoWithTwoChangedFiles(t))
	m.pressDiffKey(t, 'n')
	m.openAnnotate()
	m.diff.annInput.SetValue("note")
	m.saveAnnotation()
	before := m.diff.annotations[m.reviewKey()][0].line

	m.drainCmds(t, m.cycleDiffScope())
	if got := m.diff.annotations[m.reviewKey()][0].line; got != before {
		t.Fatalf("scope cycle rewrote the comment line: %d -> %d", before, got)
	}
}

// An ambiguous excerpt (blank line, or several identical lines) never moves the
// comment, and re-anchoring never stacks two comments onto one line.
func TestReanchorKeepsAmbiguousAndAvoidsCollapse(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	m.diff.sessID = "s1"
	m.diff.annotations = map[string][]annotation{m.reviewKey(): {
		{file: "f.go", line: 2, excerpt: "", text: "blank"},
		{file: "f.go", line: 5, excerpt: "}", text: "first brace"},
		{file: "f.go", line: 9, excerpt: "}", text: "second brace"},
		{file: "f.go", line: 12, excerpt: "unique()", text: "moves"},
	}}
	lineOf := func(kind diff.LineKind, num int, text string) diff.Line {
		return diff.Line{Kind: kind, NewNum: num, Text: text}
	}
	m.diff.set = diff.Set{Files: []diff.FileDiff{{
		File: git.ChangedFile{Path: "f.go"},
		Lines: []diff.Line{
			lineOf(diff.Same, 1, ""),
			lineOf(diff.Same, 2, "}"), // one of the two braces survived
			lineOf(diff.Same, 3, "unique()"),
		},
	}}}
	m.reanchorAnnotations()
	notes := m.diff.annotations[m.reviewKey()]
	if notes[0].line != 2 {
		t.Errorf("blank excerpt should not move: line=%d", notes[0].line)
	}
	// Two '}' notes, one surviving brace: unique match, but the second must not
	// collapse onto the first's new anchor.
	if notes[1].line == notes[2].line {
		t.Errorf("two comments collapsed onto line %d", notes[1].line)
	}
	if notes[3].line != 3 {
		t.Errorf("unique excerpt should move to line 3: line=%d", notes[3].line)
	}
}

// Ctrl+C quits from the comment editor and the send-confirm prompt, not just
// the base review keymap.
func TestReviewCtrlCQuitsFromSubmodes(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "subquit", gitRepoWithTwoChangedFiles(t))
	m.openAnnotate()
	if _, cmd := m.handleDiffKey(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit while annotating")
	}
	m.diff.annotating = false
	m.diff.sendConfirm = true
	if _, cmd := m.handleDiffKey(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit from the send-confirm prompt")
	}
	m.diff.sendConfirm = false
	m.diff.repoRoots = []string{"/tmp/one", "/tmp/two"}
	m.openRepoPick()
	if _, cmd := m.handleRepoPickKey(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit while the repo picker is open")
	}
}

// A load in flight when the comment box opens (e.g. a scope cycle) must not
// swap the set under the editor, even though m.diff.loading is still true.
func TestInFlightLoadDroppedWhileAnnotating(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "inflight", gitRepoWithTwoChangedFiles(t))
	linesBefore := len(m.currentFileDiff().Lines)
	m.openAnnotate()
	m.diff.loading = true // simulate a user-initiated load still running
	stale := diffLoadedMsg{sessID: m.diff.sessID, scope: m.diff.scope, gen: m.diff.gen}
	if cmd := m.handleDiffLoaded(stale); cmd != nil {
		t.Fatal("load must be dropped while annotating")
	}
	if m.diff.loading {
		t.Fatal("in-flight flag must clear so probes resume")
	}
	if got := len(m.currentFileDiff().Lines); got != linesBefore {
		t.Errorf("set swapped under the comment box: %d -> %d", linesBefore, got)
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

func TestBinaryFileShowsBinaryNotZeroCounts(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x00binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	openReviewOn(t, m, "binary", dir)

	rendered := m.viewDiffFileList(60, 20)
	row := ""
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "logo.png") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("logo.png missing from the file list:\n%s", rendered)
	}
	if !strings.Contains(row, "binary") {
		t.Errorf("logo.png row should be labelled binary, got: %q", row)
	}
	if strings.Contains(row, "+0") || strings.Contains(row, "−0") {
		t.Errorf("logo.png row still shows zero counts: %q", row)
	}
}

// Rows past the eager-load cap are rendered before their content is read, so
// the binary label has to come from numstat rather than the loaded file.
func TestTrackedBinaryPastEagerCapShowsBinary(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
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
	const filler = 250
	for i := 0; i < filler; i++ {
		write(fmt.Sprintf("f%03d.txt", i), "one\n")
	}
	write("zz.bin", "\x00\x01\x02initial")
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	for i := 0; i < filler; i++ {
		write(fmt.Sprintf("f%03d.txt", i), "two\n")
	}
	write("zz.bin", "\x00\x01\x02changed")
	openReviewOn(t, m, "bigbin", dir)

	files := m.diff.set.Files
	index := -1
	for i := range files {
		if files[i].File.Path == "zz.bin" {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("zz.bin missing from the diff set")
	}
	if files[index].Lines != nil || files[index].Binary {
		t.Fatalf("zz.bin at index %d was loaded; the test needs an unloaded row", index)
	}

	rendered := m.viewDiffFileList(60, len(files)+2)
	row := ""
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "zz.bin") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("zz.bin missing from the file list:\n%s", rendered)
	}
	if !strings.Contains(row, "binary") {
		t.Errorf("zz.bin row should be labelled binary, got: %q", row)
	}
	if strings.Contains(row, "+0") || strings.Contains(row, "−0") {
		t.Errorf("zz.bin row still shows zero counts: %q", row)
	}
}

// A file whose line count is unknown must not be silently summed as zero in
// the header: the totals carry a marker instead of asserting an exact count.
func TestHeaderMarksUncountedFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file regardless of mode")
	}
	m := buildModel(t)
	if m.gitDrv == nil {
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

	set, err := diff.BuildSet(m.gitDrv, dir, git.ScopeUncommitted)
	if err != nil {
		t.Fatal(err)
	}
	m.diff.set = set
	if !strings.Contains(m.viewDiffHeader("counted"), "+?") {
		t.Fatalf("header should mark the uncounted file, got %q", m.viewDiffHeader("counted"))
	}
}

func TestReviewOpensOnDeclaredRepo(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	createSession(t, m, "declared", umbrella, "")
	m.selectSessionRow(t, "declared")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no selected session")
	}
	if err := m.store.SetReviewRepo(sess.ID, filepath.Join(umbrella, "alpha")); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("review should open on the declared repo, got %q (ranking prefers %q)", got, dirtyName)
	}
}

// A repo picked by hand outranks the agent's declaration, and keeps doing so
// after review is closed and reopened.
func TestHandPickedRepoOutlivesReopen(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	createSession(t, m, "picked", umbrella, "")
	m.selectSessionRow(t, "picked")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no selected session")
	}
	if err := m.store.SetReviewRepo(sess.ID, filepath.Join(umbrella, "alpha")); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("review should open on the declared repo, got %q", got)
	}

	m.pickRepo(t, "bravo")
	if got := filepath.Base(m.diff.repoSel); got != "bravo" {
		t.Fatalf("picking bravo should load it, got %q", got)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.mode != modeList {
		t.Fatalf("esc should leave review, mode = %v", m.mode)
	}
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.diff.repoSel); got != "bravo" {
		t.Fatalf("the hand-picked repo should win over the declared one on reopen, got %q", got)
	}
}

// A hand-picked repo that disappears must be reported and forgotten, so the
// agent's declaration takes over instead of a dead path shadowing it forever.
func TestVanishedHandPickedRepoIsReportedAndForgotten(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	addDirtyRepo(t, umbrella, "charlie")
	createSession(t, m, "vanish", umbrella, "")
	m.selectSessionRow(t, "vanish")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no selected session")
	}
	if err := m.store.SetReviewRepo(sess.ID, filepath.Join(umbrella, "alpha")); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	m.pickRepo(t, "bravo")
	if got := filepath.Base(m.diff.repoSel); got != "bravo" {
		t.Fatalf("picking bravo should load it, got %q", got)
	}

	if err := os.RemoveAll(filepath.Join(umbrella, "bravo")); err != nil {
		t.Fatal(err)
	}
	m.err = ""
	m.diff.gen++
	m.drainCmds(t, m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false))

	if !strings.Contains(m.err, "bravo") {
		t.Fatalf("a vanished hand-picked repo must be surfaced, got err %q", m.err)
	}
	if !strings.Contains(m.viewDiffStatus(), m.err) {
		t.Fatalf("review status should show %q", m.err)
	}
	if _, still := m.pickedRepos[sess.ID]; still {
		t.Fatal("the dead pick must be forgotten so the declaration can take over")
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("reopening should land on the declared repo, got %q", got)
	}
}

func TestDeclaredWorktreeOutsideCwdIsAccepted(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	outside := filepath.Join(t.TempDir(), "wt-out")
	runGit := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit(filepath.Join(umbrella, "alpha"), "worktree", "add", "-b", "feature/wt", outside)

	createSession(t, m, "wtdecl", umbrella, "")
	m.selectSessionRow(t, "wtdecl")
	sess, _ := m.selected()
	if err := m.store.SetReviewRepo(sess.ID, outside); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	if m.err != "" {
		t.Fatalf("declared worktree must not be reported missing, err = %q", m.err)
	}
	resolved, _ := filepath.EvalSymlinks(outside)
	sel, _ := filepath.EvalSymlinks(m.diff.repoSel)
	if sel != resolved {
		t.Fatalf("review should open on the declared worktree, got %q", m.diff.repoSel)
	}
	found := false
	for _, root := range m.diff.repoRoots {
		if r, _ := filepath.EvalSymlinks(root); r == resolved {
			found = true
		}
	}
	if !found {
		t.Fatal("the declared worktree should appear in the picker roots")
	}
}

// addDirtyRepo adds a committed repo with an uncommitted edit, so it ranks
// ahead of the clean ones.
func addDirtyRepo(t *testing.T, umbrella, name string) {
	t.Helper()
	dir := filepath.Join(umbrella, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
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
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "init")
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() int { return 77 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A declared repo the session cwd does not contain must be reported, not
// silently swapped for whatever the ranking put on top.
func TestDeclaredRepoOutsideCwdIsReported(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	createSession(t, m, "elsewhere", umbrella, "")
	m.selectSessionRow(t, "elsewhere")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no selected session")
	}
	if err := m.store.SetReviewRepo(sess.ID, filepath.Join(t.TempDir(), "somewhere-else")); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())

	if m.err == "" {
		t.Fatal("a declared repo outside the session cwd must be surfaced")
	}
	if !strings.Contains(m.viewDiffStatus(), m.err) {
		t.Fatalf("review status should show %q", m.err)
	}
	if len(m.diff.repoRoots) < 2 {
		t.Fatal("the picker must stay usable so the user can recover")
	}
}

// Picking a repo after the session has left m.sessions must say so instead of
// dropping the user back into review with the old repo and no explanation.
func TestRepoPickerReportsMissingSession(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "gone", umbrella)
	before := m.diff.repoSel

	m.pressDiffKey(t, 'r')
	if m.mode != modeRepoPick {
		t.Fatalf("r should open the repo picker, mode = %v", m.mode)
	}
	for _, r := range "alph" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		*m = *updated.(*Model)
	}
	m.sessions = nil

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if cmd != nil {
		t.Fatal("a missing session must not kick off a diff load")
	}
	if m.err == "" {
		t.Fatal("picking a repo for a missing session must surface an error")
	}
	if m.diff.repoSel != before {
		t.Fatalf("repo should not change when the session is gone, got %q", m.diff.repoSel)
	}
	if !strings.Contains(m.viewDiffStatus(), m.err) {
		t.Fatalf("review status should show the error %q", m.err)
	}
}

// The poller reloads repoRoots while the picker is open, so the live list can
// shrink and, because rankRepos is dirty-first, reorder under a parked cursor.
// The picker works off a snapshot, so Enter must load the repo whose row was on
// screen and must never index past the list.
func TestRepoPickerSurvivesShrinkingRootList(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "shrink", umbrella)

	realRoots := append([]string(nil), m.diff.repoRoots...)
	if len(realRoots) != 2 {
		t.Fatalf("want 2 real repos, got %v", realRoots)
	}
	for i := len(realRoots); i < 20; i++ {
		m.diff.repoRoots = append(m.diff.repoRoots, filepath.Join(umbrella, fmt.Sprintf("repo-%02d", i)))
	}

	m.pressDiffKey(t, 'r')
	if m.mode != modeRepoPick {
		t.Fatalf("r should open the repo picker, mode = %v", m.mode)
	}
	for m.repoPick.cursor != 1 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		*m = *updated.(*Model)
	}
	onScreen := m.filteredRows()[m.repoPick.cursor].root

	// A reload lands carrying only the repos that still exist, re-ranked.
	m.diff.repoRoots = []string{realRoots[1], realRoots[0]}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)

	if m.mode != modeDiff {
		t.Fatalf("enter should return to review, mode = %v", m.mode)
	}
	if m.repoPick.cursor >= len(m.repoPick.rows) {
		t.Fatalf("cursor should stay inside the snapshot, got %d", m.repoPick.cursor)
	}
	if m.diff.repoSel != onScreen {
		t.Fatalf("enter should load the repo on the cursor row %q, got %q", onScreen, m.diff.repoSel)
	}
}

func TestRepoPickerFitsTerminalHeight(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 80, 24
	for i := 0; i < 20; i++ {
		m.diff.repoRoots = append(m.diff.repoRoots,
			fmt.Sprintf("/home/someone/very/long/parent/path/for/wrapping/umbrella/repo-%02d", i))
	}
	m.diff.repoSel = m.diff.repoRoots[0]
	m.openRepoPick()

	view := m.viewRepoPick()
	if lines := len(strings.Split(view, "\n")); lines > m.height {
		t.Fatalf("picker rendered %d lines, terminal is %d", lines, m.height)
	}
	if !strings.Contains(view, "repo-00") {
		t.Fatal("the cursor row should be visible at the top of the list")
	}
	shown := strings.Count(view, "repo-")
	if shown == 0 || shown >= len(m.diff.repoRoots) {
		t.Fatalf("expected a windowed subset of the repos, %d of %d rendered", shown, len(m.diff.repoRoots))
	}
	if want := fmt.Sprintf("+%d more", len(m.diff.repoRoots)-shown); !strings.Contains(view, want) {
		t.Fatalf("hidden count should match the %d rows actually rendered, want %q in view", shown, want)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	*m = *updated.(*Model)
	if m.repoPick.cursor != len(m.diff.repoRoots)-1 {
		t.Fatalf("up from the top should wrap to the last repo, cursor = %d", m.repoPick.cursor)
	}
	view = m.viewRepoPick()
	if lines := len(strings.Split(view, "\n")); lines > m.height {
		t.Fatalf("picker rendered %d lines at the list end, terminal is %d", lines, m.height)
	}
	if !strings.Contains(view, "repo-19") {
		t.Fatal("the cursor must stay visible after moving to the end of the list")
	}
}

func TestCtrlRFromListOpensReview(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "ctrlr", gitRepoWithTwoChangedFiles(t), "")
	m.selectSessionRow(t, "ctrlr")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.mode != modeDiff {
		t.Fatalf("ctrl+r from the list should open review, mode = %v (err=%q)", m.mode, m.err)
	}
	if m.diff.reattachID != "" {
		t.Fatal("review opened from the list should return to the list, not re-attach")
	}
}
