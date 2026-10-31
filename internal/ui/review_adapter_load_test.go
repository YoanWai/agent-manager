package ui

import (
	"errors"
	"fmt"
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffReviewShowsWholeFile(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")

	m.drainCmds(t, m.openDiff())
	if !m.review.Snapshot().Active || m.base != baseReview || m.review.Snapshot().Loading {
		t.Fatalf("diff should be loaded fullscreen, active=%v screen = %v err=%q", m.review.Snapshot().Active, m.screenID(), m.review.Snapshot().Error)
	}
	if len(m.review.Snapshot().Set.Files) != 2 {
		t.Fatalf("files = %+v", m.review.Snapshot().Set.Files)
	}

	view := ansi.Strip(preparedView(m))
	if !strings.Contains(view, "review · coder") || !strings.Contains(view, "files") {
		t.Fatalf("fullscreen review layout missing:\n%s", view)
	}
	if !strings.Contains(view, "package main") || !strings.Contains(view, "println(1)") {
		t.Fatalf("whole-file content missing:\n%s", view)
	}
	if !strings.Contains(view, "func main() {}") {
		t.Fatalf("deleted line should interleave:\n%s", view)
	}
}

func TestReviewLoadsFilesOnDemand(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "lazy", gitRepoWithTwoChangedFiles(t))
	if len(m.review.Snapshot().Set.Files) != 2 {
		t.Fatalf("want 2 files, got %d", len(m.review.Snapshot().Set.Files))
	}
	if !m.review.Snapshot().Set.Files[0].Loaded() {
		t.Fatal("selected file should be loaded after its background command lands")
	}
	if m.review.Snapshot().Set.Files[1].Loaded() {
		t.Fatal("unselected file should remain unloaded")
	}

	cmd := m.switchDiffFile(1)
	if cmd == nil {
		t.Fatal("switching to an unloaded file should schedule a load")
	}
	if m.currentFileDiff().Loaded() {
		t.Fatal("file loading should not block the navigation handler")
	}
	if body := ansi.Strip(m.viewDiffCode(80, 20)); !strings.Contains(body, "loading file") {
		t.Fatalf("unloaded file should render a loading state, got %q", body)
	}
	m.drainCmds(t, cmd)
	if !m.currentFileDiff().Loaded() {
		t.Fatal("file should install after its background command lands")
	}
}

func TestRefreshFileLoadsRunSerially(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "serial", gitRepoWithTwoChangedFiles(t))
	set := m.review.SetCopy()
	requests := make([]uireview.FileRequest, 8)
	for i := range requests {
		requests[i] = uireview.FileRequest{
			TargetID: m.review.Snapshot().SessionID, Scope: m.review.Snapshot().Scope,
			Generation: m.review.Snapshot().Generation, RepoRoot: set.Repo.Root,
			Index: 1, Path: set.Files[1].File.Path, Set: set,
		}
	}
	msgs, ok := uireviewsource.FilesCmd(m.reviewGit(), requests)().(uireviewsource.FilesResult)
	if !ok || len(msgs) != len(requests) {
		t.Fatalf("serial load returned %T with %d results", msgs, len(msgs))
	}
}

// A load in flight when the comment box opens (e.g. a scope cycle) must not
// swap the set under the editor, even though m.review.Snapshot().Loading is still true.
func TestInFlightLoadDroppedWhileAnnotating(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "inflight", gitRepoWithTwoChangedFiles(t))
	linesBefore := len(m.currentFileDiff().Lines)
	m.openAnnotate()
	state := m.review.Snapshot()
	stale := uireview.LoadResult{TargetID: state.SessionID, Scope: state.Scope, Generation: state.Generation}
	if cmd := m.handleReviewLoad(stale); cmd != nil {
		t.Fatal("load must be dropped while annotating")
	}
	if m.review.Snapshot().Loading {
		t.Fatal("in-flight flag must clear so probes resume")
	}
	if got := len(m.currentFileDiff().Lines); got != linesBefore {
		t.Errorf("set swapped under the comment box: %d -> %d", linesBefore, got)
	}
}

func TestBinaryFileShowsBinaryNotZeroCounts(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x00binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	openReviewOn(t, m, "binary", dir)
	for i := range m.review.Snapshot().Set.Files {
		if m.review.Snapshot().Set.Files[i].File.Path == "logo.png" {
			m.drainCmds(t, selectReviewFile(m, i))
			break
		}
	}

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
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
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
	run("git", "init", "-b", "main")
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

	files := m.review.Snapshot().Set.Files
	index := -1
	for i := range files {
		if files[i].File.Path == "zz.bin" {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("zz.bin missing from the diff set")
	}
	if files[index].Loaded() || files[index].Binary {
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

// Probe and load must derive the base and fingerprint identically. With an
// unresolved umbrella root and a stored override, the probe has to read the
// base under the raw selection - not the resolved toplevel - or its fingerprint
// diverges from the load's and review reloads every tick forever.
func TestProbeAndLoadAgreeOnFingerprint(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithBranchedRepo(t)
	openReviewOn(t, m, "probe", umbrella)
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}

	if err := m.services.store.SetReviewBase(sess.ID, m.review.Snapshot().RepoSelected, "feature"); err != nil {
		t.Fatal(err)
	}
	for m.review.Snapshot().Scope != git.ScopeBranch {
		request, accepted := m.review.CycleScope()
		if !accepted {
			t.Fatal("branch scope request was rejected")
		}
		m.drainCmds(t, m.reviewLoadCmd(request))
	}
	if m.review.Snapshot().Error != "" {
		t.Fatalf("branch-scope load with a valid override should not error, err = %q", m.review.Snapshot().Error)
	}
	if m.review.Snapshot().Fingerprint == 0 {
		t.Fatal("load should record a non-zero fingerprint")
	}

	state := m.review.Snapshot()
	msg, ok := m.reviewProbeCmd(uireview.ProbeRequest{
		Target: uireviewsource.Target(sess), Scope: state.Scope, RepoSelected: state.RepoSelected,
		GitRoot: state.Set.Repo.Root,
	})().(uireview.ProbeResult)
	if !ok {
		t.Fatal("probe closure should yield a review.ProbeResult")
	}
	if msg.RepoSelected != m.review.Snapshot().RepoSelected {
		t.Fatalf("probe should report the selected repo %q, got %q", m.review.Snapshot().RepoSelected, msg.RepoSelected)
	}
	if msg.Fingerprint != m.review.Snapshot().Fingerprint {
		t.Fatalf("probe fingerprint %d must match the load's %d or review reloads forever (repoSel=%q toplevel=%q)",
			msg.Fingerprint, m.review.Snapshot().Fingerprint, m.review.Snapshot().RepoSelected, m.review.Snapshot().Set.Repo.Root)
	}
}

func TestCycleDiffScopeReportsAFailedBaseLookup(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	openReviewOn(t, m, "keepset", gitTestRepo(t))
	if len(m.review.Snapshot().Set.Files) == 0 {
		t.Fatal("expected files")
	}
	if err := m.services.store.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := m.cycleDiffScope()
	if cmd == nil {
		t.Fatal("cycling the scope should start a load")
	}
	m.drainCmds(t, cmd)
	if m.review.Snapshot().Loading {
		t.Fatal("the failed load should have landed")
	}
	if m.review.Snapshot().Error == "" {
		t.Fatal("the lookup error should reach the review panel")
	}
}

func TestReviewUntrackedFileShowsCountWithoutOpening(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	openReviewOn(t, m, "counts", gitTestRepo(t))
	var extra *uireview.FileSummary
	files := m.review.Snapshot().Set.Files
	for i := range files {
		if files[i].File.Path == "extra.txt" {
			extra = &files[i]
		}
	}
	if extra == nil {
		t.Fatal("extra.txt missing")
	}
	if extra.Loaded() {
		t.Fatal("unselected untracked file should stay unloaded")
	}
	if !extra.StatKnown() || extra.Stat.Adds < 1 {
		t.Fatalf("untracked extra.txt should already have a +N, known=%v stat=%+v", extra.StatKnown(), extra.Stat)
	}
	list := ansi.Strip(m.viewDiffFileList(60, 20))
	if strings.Contains(list, "?") {
		t.Fatalf("file list should not use ? for a counted untracked file:\n%s", list)
	}
	if !strings.Contains(list, "+") {
		t.Fatalf("file list should show adds for extra.txt:\n%s", list)
	}
}

func TestReviewUntrackedImageShowsBinaryWithoutOpening(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "shot.png"), []byte("\x89PNG\r\n\x1a\n\x00\x00binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	openReviewOn(t, m, "shots", dir)
	var shot *uireview.FileSummary
	files := m.review.Snapshot().Set.Files
	for i := range files {
		if files[i].File.Path == "shot.png" {
			shot = &files[i]
		}
	}
	if shot == nil {
		t.Fatal("shot.png missing")
	}
	if shot.Loaded() {
		t.Fatal("unselected image should stay unloaded")
	}
	if !shot.StatKnown() || !shot.Stat.Binary {
		t.Fatalf("untracked image should count as binary, known=%v stat=%+v", shot.StatKnown(), shot.Stat)
	}
	list := ansi.Strip(m.viewDiffFileList(60, 20))
	if !strings.Contains(list, "binary") {
		t.Fatalf("file list should say binary, not ?:\n%s", list)
	}
}

func TestReviewShowsLoaderWhileDiffLoads(t *testing.T) {
	m := &Model{layout: layoutState{width: 100, height: 30}, screen: screen{base: baseReview}}
	seedReviewForTest(m, uireview.Target{ID: "s"}, git.ScopeUncommitted, "/repo", diff.Set{}, false)
	code := ansi.Strip(m.viewDiffCode(80, 20))
	if !strings.Contains(code, "loading diff") {
		t.Fatalf("code pane should carry the diff loader, got %q", code)
	}
	if strings.Count(code, "●") != 1 || strings.Count(code, "•") != 1 {
		t.Fatalf("diff loader should show the ring, got %q", code)
	}
	list := ansi.Strip(m.viewDiffFileList(28, 10))
	if !strings.Contains(list, "loading diff") {
		t.Fatalf("file list should carry the compact loader, got %q", list)
	}
	if cmd := m.startStartupTick(); cmd == nil {
		t.Fatal("loading review should start the loader tick")
	}
	first := code
	m.Update(uistartup.TickMsg{})
	if next := ansi.Strip(m.viewDiffCode(80, 20)); next == first {
		t.Fatal("diff loader did not move on the tick")
	}
}

func TestReviewShowsLoaderWhileFileLoads(t *testing.T) {
	m := &Model{layout: layoutState{width: 100, height: 30}, screen: screen{base: baseReview}}
	seedReviewForTest(m, uireview.Target{ID: "s"}, git.ScopeUncommitted, "/repo",
		diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{{File: git.ChangedFile{Path: "main.go"}}}}, true)
	code := ansi.Strip(m.viewDiffCode(80, 20))
	if !strings.Contains(code, "loading file") {
		t.Fatalf("code pane should carry the file loader, got %q", code)
	}
	if strings.Count(code, "●") != 1 {
		t.Fatalf("file loader should show the ring, got %q", code)
	}
	list := ansi.Strip(m.viewDiffFileList(40, 8))
	if strings.Contains(list, "loading") {
		t.Fatalf("file list should keep the files while one loads, got %q", list)
	}
}

func TestFailedDiffLoadKeepsRepoPicker(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	openReviewOn(t, m, "keeprepo", gitTestRepo(t))
	roots := append([]string{}, m.review.Snapshot().RepoRoots...)
	sel := m.review.Snapshot().RepoSelected
	if len(roots) == 0 {
		t.Fatal("expected repo roots")
	}
	if cmd := m.handleReviewLoad(uireview.LoadResult{
		TargetID:   m.review.Snapshot().SessionID,
		Scope:      m.review.Snapshot().Scope,
		Generation: m.review.Snapshot().Generation,
		Err:        errors.New("git died"),
		RepoRoots:  roots,
		RepoRoot:   sel,
	}); cmd != nil {
		t.Fatal("errored load should not follow up")
	}
	if m.review.Snapshot().Error == "" {
		t.Fatal("error text missing")
	}
	if len(m.review.Snapshot().RepoRoots) == 0 {
		t.Fatal("repo list should survive a failed load so r still works")
	}
	m.openRepoPick()
	if !m.pickerOpen() {
		t.Fatalf("r should still open, screen = %v", m.screenID())
	}
}

func TestReviewTargetsTheGroupBaseUnlessOnePicked(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := gitRepoWithSecondBranch(t)
	if err := m.services.store.CreateGroup("grp", repo); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.services.store.SetGroupBase("grp", "feature"); err != nil {
		t.Fatalf("set base: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "based", repo, "grp")
	m.selectSessionRow(t, "based")
	m.drainCmds(t, m.openDiff())
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}
	load := func() uireview.LoadResult {
		state := m.review.Snapshot()
		msg := m.reviewLoadCmd(uireview.LoadRequest{
			Target: uireviewsource.Target(sess), Scope: git.ScopeBranch, Generation: state.Generation,
			RepoRoot: state.RepoSelected, RepoRoots: []string{state.RepoSelected},
		})()
		return msg.(uireviewsource.LoadMsg).Result
	}

	if result := load(); !strings.HasPrefix(result.Set.BaseDesc, "feature@") {
		t.Fatalf("vs target should read the group's base, got %q (err=%v)", result.Set.BaseDesc, result.Err)
	}
	if err := m.services.store.SetReviewBase(sess.ID, m.review.Snapshot().RepoSelected, "main"); err != nil {
		t.Fatal(err)
	}
	if result := load(); !strings.HasPrefix(result.Set.BaseDesc, "main@") {
		t.Fatalf("a base picked in review wins over the group's, got %q", result.Set.BaseDesc)
	}
}

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
		if _, ok := msg.(uistartup.TickMsg); ok {
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
	if !m.pickerOpen() {
		t.Fatalf("r should open the repo picker, screen = %v", m.screenID())
	}
	for _, r := range name {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		*m = *updated.(*Model)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.base != baseReview {
		t.Fatalf("enter should return to review, screen = %v", m.screenID())
	}
}

func openReviewOn(t *testing.T, m *Model, name, dir string) {
	t.Helper()
	createSession(t, m, name, dir, "")
	m.selectSessionRow(t, name)
	m.drainCmds(t, m.openDiff())
	if m.base != baseReview {
		t.Fatalf("openDiff should enter review, err = %q", m.errBar.text)
	}
}

// While a comment is being written or confirmed, background reloads pause,
// and an in-flight reload result is dropped instead of shifting lines under
// the open editor.
func TestNoReloadWhileAnnotating(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "ann", gitRepoWithTwoChangedFiles(t))
	linesBefore := len(m.currentFileDiff().Lines)

	m.openAnnotate()
	if !m.review.Snapshot().Annotating {
		t.Fatal("openAnnotate should enter annotating mode")
	}
	for i := 0; i < 4; i++ {
		if cmd := m.diffRefreshCmd(); cmd != nil {
			t.Fatal("probe must pause while annotating")
		}
	}
	// An in-flight reload from before the comment box opened is dropped.
	state := m.review.Snapshot()
	stale := uireview.LoadResult{TargetID: state.SessionID, Scope: state.Scope, Generation: state.Generation}
	if cmd := m.handleReviewLoad(stale); cmd != nil {
		t.Fatal("stale reload should be dropped without follow-up")
	}
	if got := len(m.currentFileDiff().Lines); got != linesBefore {
		t.Errorf("reload replaced the diff under the comment box: %d -> %d lines", linesBefore, got)
	}

	m.review.AnnotationKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.review.SetSendConfirm(true)
	if cmd := m.diffRefreshCmd(); cmd != nil {
		t.Fatal("probe must pause while confirming a send")
	}
}

// refreshDiff drives the silent same-scope reload path (the probe piggyback),
// the only reload that re-anchors comments.
func (m *Model) refreshDiff(t *testing.T) {
	t.Helper()
	state := m.review.Snapshot()
	request, ok := m.review.ApplyProbe(uireview.ProbeResult{
		TargetID: state.SessionID, Scope: state.Scope, RepoSelected: state.RepoSelected,
		Fingerprint: state.Fingerprint + 1,
	})
	if !ok {
		t.Fatal("review refresh request was rejected")
	}
	m.drainCmds(t, m.reviewLoadCmd(request))
}

// A scope cycle loads a different file set; it must not re-anchor a comment's
// stored line against content it was never made against.
func TestScopeCycleDoesNotReanchor(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "scoped", gitRepoWithTwoChangedFiles(t))
	m.pressDiffKey(t, 'n')
	m.openAnnotate()
	typeReviewAnnotation(m, "note")
	m.applyCmd(t, m.saveAnnotation())
	before := m.review.Annotations()[0].Line

	m.drainCmds(t, m.cycleDiffScope())
	if got := m.review.Annotations()[0].Line; got != before {
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
	t.Parallel()
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

	resolvedRoot := uireviewsource.Canonical(m.review.Snapshot().RepoSelected)
	if resolvedRoot == m.review.Snapshot().RepoSelected {
		t.Skip("temp dir is not symlinked, so there is no raw/resolved boundary to cross")
	}

	branchReload := func() {
		state := m.review.Snapshot()
		var request uireview.LoadRequest
		var accepted bool
		if state.Scope != git.ScopeBranch {
			for m.review.Snapshot().Scope != git.ScopeBranch {
				request, accepted = m.review.CycleScope()
				if !accepted {
					t.Fatal("branch scope request was rejected")
				}
				m.drainCmds(t, m.reviewLoadCmd(request))
			}
		} else {
			state = m.review.Snapshot()
			request, accepted = m.review.ApplyProbe(uireview.ProbeResult{
				TargetID: state.SessionID, Scope: state.Scope, RepoSelected: state.RepoSelected,
				Fingerprint: state.Fingerprint + 1,
			})
			if !accepted {
				t.Fatal("branch reload request was rejected")
			}
			m.drainCmds(t, m.reviewLoadCmd(request))
		}
		if m.review.Snapshot().Error != "" {
			t.Fatalf("branch-scope load should not error, err = %q", m.review.Snapshot().Error)
		}
	}

	branchReload()
	autoFingerprint := m.review.Snapshot().Fingerprint

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
	if m.review.Snapshot().Fingerprint == autoFingerprint {
		t.Fatalf("the CLI-declared feature base never reached the load: fingerprint stayed at the auto value %d (base keyed under %q was read under %q)",
			autoFingerprint, resolvedRoot, m.review.Snapshot().RepoSelected)
	}
	if len(m.review.Snapshot().Set.Files) == 0 {
		t.Fatal("the feature base should surface the diverging file in review")
	}
}

func TestDiffProbeSetsLoadingSoItDoesNotStack(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	openReviewOn(t, m, "probe-load", gitTestRepo(t))
	gen := m.review.Snapshot().Generation
	fp := m.review.Snapshot().Fingerprint
	if fp == 0 {
		t.Fatal("loaded review should have a fingerprint")
	}
	cmd := m.handleReviewProbe(uireview.ProbeResult{
		TargetID: m.review.Snapshot().SessionID, Scope: m.review.Snapshot().Scope, RepoSelected: m.review.Snapshot().RepoSelected, Fingerprint: fp + 1,
	})
	if cmd == nil {
		t.Fatal("a changed fingerprint should start a reload")
	}
	if !m.review.Snapshot().Loading {
		t.Fatal("reload must set loading so the next probe cannot cancel it")
	}
	if m.review.Snapshot().Generation != gen+1 {
		t.Fatalf("gen = %d, want %d", m.review.Snapshot().Generation, gen+1)
	}
	if stacked := m.handleReviewProbe(uireview.ProbeResult{
		TargetID: m.review.Snapshot().SessionID, Scope: m.review.Snapshot().Scope, RepoSelected: m.review.Snapshot().RepoSelected, Fingerprint: fp + 2,
	}); stacked != nil {
		t.Fatal("a probe while loading must not start another reload")
	}
	if m.review.Snapshot().Generation != gen+1 {
		t.Fatalf("stacked probe bumped gen to %d", m.review.Snapshot().Generation)
	}
	if m.diffRefreshCmd() != nil {
		t.Fatal("refresh must wait until the in-flight load lands")
	}
}
