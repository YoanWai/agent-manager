package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	"github.com/charmbracelet/x/ansi"
)

func TestDiffReviewShowsWholeFile(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")

	m.drainCmds(t, m.openDiff())
	if !m.review.Snapshot().Active || m.mode != modeDiff || m.review.Snapshot().Loading {
		t.Fatalf("diff should be loaded fullscreen, active=%v mode=%v err=%q", m.review.Snapshot().Active, m.mode, m.review.Snapshot().Error)
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
	msgs, ok := m.reviewFilesCmd(requests)().(reviewFilesResult)
	if !ok || len(msgs) != len(requests) {
		t.Fatalf("serial load returned %T with %d results", msgs, len(msgs))
	}
}

// A load in flight when the comment box opens (e.g. a scope cycle) must not
// swap the set under the editor, even though m.review.Snapshot().Loading is still true.
func TestInFlightLoadDroppedWhileAnnotating(t *testing.T) {
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
		Target: reviewTarget(sess), Scope: state.Scope, RepoSelected: state.RepoSelected,
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
	m := &Model{width: 100, height: 30, mode: modeDiff}
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
	m.Update(startupTickMsg{})
	if next := ansi.Strip(m.viewDiffCode(80, 20)); next == first {
		t.Fatal("diff loader did not move on the tick")
	}
}

func TestReviewShowsLoaderWhileFileLoads(t *testing.T) {
	m := &Model{width: 100, height: 30, mode: modeDiff}
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
	if m.mode != modeRepoPick {
		t.Fatalf("r should still open, mode = %v", m.mode)
	}
}
