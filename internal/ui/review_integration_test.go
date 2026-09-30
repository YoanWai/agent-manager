package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/git"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { println(1) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

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

// gitRepoWithWideFile is a repo whose changed lines are far wider than any
// pane, so every line soft-wraps onto several painted rows.
func gitRepoWithWideFile(t *testing.T, lines, lineWidth int) string {
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
	if err := os.WriteFile(filepath.Join(dir, "wide.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-m", "init")

	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "wide-%03d %s\n", i, strings.Repeat("x", lineWidth))
	}
	if err := os.WriteFile(filepath.Join(dir, "wide.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

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
	run("git", "init", "-b", "main")
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

// drainCmds runs a command chain to exhaustion, feeding every message back
// into Update, so async follow-ups (diff loads, highlights) all land.
func (m *Model) drainCmds(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil && i < 20; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				m.drainCmds(t, child)
			}
			return
		}
		// The startup tick reschedules itself, so following its command
		// would spin here rather than drain what the batch already holds.
		if _, ok := msg.(startupTickMsg); ok {
			updated, _ := m.Update(msg)
			*m = *updated.(*Model)
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

func (m *Model) pressFilterKey(t *testing.T) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
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
		t.Fatalf("openDiff should enter review, err = %q", m.errBar.text)
	}
}

// While a comment is being written or confirmed, background reloads pause,
// and an in-flight reload result is dropped instead of shifting lines under
// the open editor.
func TestNoReloadWhileAnnotating(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
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

// A scope cycle loads a different file set; it must not re-anchor a comment's
// stored line against content it was never made against.
func TestScopeCycleDoesNotReanchor(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "scoped", gitRepoWithTwoChangedFiles(t))
	m.pressDiffKey(t, 'n')
	m.openAnnotate()
	m.diff.annInput.SetValue("note")
	m.applyCmd(t, m.saveAnnotation())
	before := m.diff.annotations[m.reviewKey()][0].line

	m.drainCmds(t, m.cycleDiffScope())
	if got := m.diff.annotations[m.reviewKey()][0].line; got != before {
		t.Fatalf("scope cycle rewrote the comment line: %d -> %d", before, got)
	}
}

// writeGitRepo commits a first version of every file, then lays down the
// second one, leaving a working tree whose changes a review can open.
func writeGitRepo(t *testing.T, committed, working map[string]string) string {
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
	run("git", "init", "-b", "main")
	for name, content := range committed {
		writeRepoFile(t, dir, name, content)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	for name, content := range working {
		writeRepoFile(t, dir, name, content)
	}
	return dir
}

func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitRepoWithBinaryBetweenTextFiles changes a tracked binary file sitting
// between two text files. Its name says nothing, so git's numstat verdict is
// the only thing that can call it binary.
func gitRepoWithBinaryBetweenTextFiles(t *testing.T) string {
	t.Helper()
	return writeGitRepo(t,
		map[string]string{
			"a.go":  "package a\n\nfunc A() int { return 1 }\n",
			"b.dat": "\x00\x01\x02one",
			"c.go":  "package a\n\nfunc C() int { return 3 }\n",
		},
		map[string]string{
			"a.go":  "package a\n\nfunc A() int { return 10 }\n",
			"b.dat": "\x00\x01\x02two",
			"c.go":  "package a\n\nfunc C() int { return 30 }\n",
		})
}

// gitRepoWithLockFileBetweenTextFiles carries a lock file git counts in full,
// so a header reading that follows the list can be told apart from one that
// followed every changed file.
func gitRepoWithLockFileBetweenTextFiles(t *testing.T) string {
	t.Helper()
	lock := func(version string) string {
		return "one " + version + "\ntwo " + version + "\nthree " + version + "\n"
	}
	return writeGitRepo(t,
		map[string]string{
			"a.go":   "package a\n\nfunc A() int { return 1 }\n",
			"b.dat":  "\x00\x01\x02one",
			"c.go":   "package a\n\nfunc C() int { return 3 }\n",
			"go.sum": lock("v1"),
		},
		map[string]string{
			"a.go":   "package a\n\nfunc A() int { return 10 }\n",
			"b.dat":  "\x00\x01\x02two",
			"c.go":   "package a\n\nfunc C() int { return 30 }\n",
			"go.sum": lock("v2"),
		})
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
	run("git", "init", "-b", "main")
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() int { return 77 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitRepoWithSecondBranch(t *testing.T) string {
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
	run("git", "init", "-b", "main")
	write("a.go", "package a\n\nfunc A() int { return 1 }\n")
	run("git", "add", ".")
	run("git", "commit", "-m", "c1")
	run("git", "branch", "feature")
	write("a.go", "package a\n\nfunc A() int { return 2 }\n")
	run("git", "add", ".")
	run("git", "commit", "-m", "c2")
	return dir
}

func (m *Model) typeAndEnter(t *testing.T, text string) {
	t.Helper()
	for _, r := range text {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		*m = *updated.(*Model)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
}

// umbrellaWithBranchedRepo makes a dir that is not itself a repo but holds one
// nested repo whose feature branch diverges from main, so an overridden base
// yields a different fingerprint than auto-detection. On macOS the discovered
// root string is unresolved (/var/...) while git's toplevel resolves
// (/private/var/...), which is exactly the split the base keying must survive.
func umbrellaWithBranchedRepo(t *testing.T) (umbrella, repoRoot string) {
	t.Helper()
	umbrella = t.TempDir()
	repoRoot = filepath.Join(umbrella, "solo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(repoRoot, "a.go"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("git", "init", "-b", "main")
	write("package a\n\nfunc A() int { return 1 }\n")
	run("git", "add", ".")
	run("git", "commit", "-m", "c1")
	run("git", "branch", "feature")
	write("package a\n\nfunc A() int { return 2 }\n")
	run("git", "add", ".")
	run("git", "commit", "-m", "c2")
	return umbrella, repoRoot
}

// The CLI writes the base under git's symlink-resolved toplevel, while the UI
// discovers umbrella repos under the raw cwd. This exercises the whole path an
// agent's `review-base` takes: mailbox written the way the CLI writes it (the
// resolved root), the poller applying it, then the UI load. The load must key
// its read the same resolved way or the override silently never reaches review.
func TestCLIReviewBaseReachesLoadAcrossSymlinkBoundary(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	_, repoRoot := umbrellaWithBranchedRepo(t)
	umbrella := filepath.Dir(repoRoot)
	openReviewOn(t, m, "cliboundary", umbrella)
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}

	resolvedRoot := resolveSymlinksOrSelf(m.diff.repoSel)
	if resolvedRoot == m.diff.repoSel {
		t.Skip("temp dir is not symlinked, so there is no raw/resolved boundary to cross")
	}

	branchReload := func() {
		m.diff.scope = git.ScopeBranch
		m.diff.gen++
		m.drainCmds(t, m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false))
		if m.diff.errText != "" {
			t.Fatalf("branch-scope load should not error, err = %q", m.diff.errText)
		}
	}

	branchReload()
	autoFingerprint := m.diff.fingerprint

	// Mirror the CLI exactly: OpenRepo yields the same symlink-resolved toplevel
	// the review-base subcommand stores, so the mailbox holds the resolved root.
	repo, err := m.services.gitDrv.OpenRepo(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Root != resolvedRoot {
		t.Fatalf("test premise broken: git toplevel %q should match the resolved selection %q", repo.Root, resolvedRoot)
	}
	path := m.services.hooks.ReviewBaseFile(sess.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(repo.Root+"\nfeature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())

	branchReload()
	if m.diff.fingerprint == autoFingerprint {
		t.Fatalf("the CLI-declared feature base never reached the load: fingerprint stayed at the auto value %d (base keyed under %q was read under %q)",
			autoFingerprint, resolvedRoot, m.diff.repoSel)
	}
	if len(m.diff.set.Files) == 0 {
		t.Fatal("the feature base should surface the diverging file in review")
	}
}

func TestDiffProbeSetsLoadingSoItDoesNotStack(t *testing.T) {
	m := buildModel(t)
	openReviewOn(t, m, "probe-load", gitTestRepo(t))
	gen := m.diff.gen
	fp := m.diff.fingerprint
	if fp == 0 {
		t.Fatal("loaded review should have a fingerprint")
	}
	cmd := m.handleDiffProbe(diffProbeMsg{
		sessID: m.diff.sessID, scope: m.diff.scope, repoRoot: m.diff.repoSel, fp: fp + 1,
	})
	if cmd == nil {
		t.Fatal("a changed fingerprint should start a reload")
	}
	if !m.diff.loading {
		t.Fatal("reload must set loading so the next probe cannot cancel it")
	}
	if m.diff.gen != gen+1 {
		t.Fatalf("gen = %d, want %d", m.diff.gen, gen+1)
	}
	if stacked := m.handleDiffProbe(diffProbeMsg{
		sessID: m.diff.sessID, scope: m.diff.scope, repoRoot: m.diff.repoSel, fp: fp + 2,
	}); stacked != nil {
		t.Fatal("a probe while loading must not start another reload")
	}
	if m.diff.gen != gen+1 {
		t.Fatalf("stacked probe bumped gen to %d", m.diff.gen)
	}
	if m.diffRefreshCmd() != nil {
		t.Fatal("refresh must wait until the in-flight load lands")
	}
}
