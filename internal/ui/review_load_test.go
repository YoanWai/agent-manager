package ui

import (
	"errors"
	"fmt"
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiffReviewShowsWholeFile(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")

	m.drainCmds(t, m.openDiff())
	if !m.diff.active || m.mode != modeDiff || m.diff.loading {
		t.Fatalf("diff should be loaded fullscreen, active=%v mode=%v err=%q", m.diff.active, m.mode, m.diff.errText)
	}
	if len(m.diff.set.Files) != 2 {
		t.Fatalf("files = %+v", m.diff.set.Files)
	}

	view := ansi.Strip(m.View())
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
	if len(m.diff.set.Files) != 2 {
		t.Fatalf("want 2 files, got %d", len(m.diff.set.Files))
	}
	if !m.diff.set.Files[0].Loaded() {
		t.Fatal("selected file should be loaded after its background command lands")
	}
	if m.diff.set.Files[1].Loaded() {
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
	var active atomic.Int32
	var peak atomic.Int32
	cmds := make([]tea.Cmd, 8)
	for i := range cmds {
		index := i
		cmds[i] = func() tea.Msg {
			now := active.Add(1)
			for {
				seen := peak.Load()
				if now <= seen || peak.CompareAndSwap(seen, now) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
			return diffFileLoadedMsg{index: index}
		}
	}

	msgs, ok := diffFilesLoadCmd(cmds)().(diffFilesLoadedMsg)
	if !ok || len(msgs) != len(cmds) {
		t.Fatalf("serial load returned %T with %d results", msgs, len(msgs))
	}
	if got := peak.Load(); got != 1 {
		t.Fatalf("refresh loads peaked at %d concurrent jobs, want 1", got)
	}
}

// A load in flight when the comment box opens (e.g. a scope cycle) must not
// swap the set under the editor, even though m.diff.loading is still true.
func TestInFlightLoadDroppedWhileAnnotating(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
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
	for i := range m.diff.set.Files {
		if m.diff.set.Files[i].File.Path == "logo.png" {
			m.diff.fileIdx = i
			m.drainCmds(t, m.loadCurrentDiffFile())
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

	if err := m.services.store.SetReviewBase(sess.ID, m.diff.repoSel, "feature"); err != nil {
		t.Fatal(err)
	}
	m.diff.scope = git.ScopeBranch
	m.diff.gen++
	m.drainCmds(t, m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false))
	if m.diff.errText != "" {
		t.Fatalf("branch-scope load with a valid override should not error, err = %q", m.diff.errText)
	}
	if m.diff.fingerprint == 0 {
		t.Fatal("load should record a non-zero fingerprint")
	}

	msg, ok := m.diffProbeCmd(sess, m.diff.scope)().(diffProbeMsg)
	if !ok {
		t.Fatal("probe closure should yield a diffProbeMsg")
	}
	if msg.repoRoot != m.diff.repoSel {
		t.Fatalf("probe should report the selected repo %q, got %q", m.diff.repoSel, msg.repoRoot)
	}
	if msg.fp != m.diff.fingerprint {
		t.Fatalf("probe fingerprint %d must match the load's %d or review reloads forever (repoSel=%q toplevel=%q)",
			msg.fp, m.diff.fingerprint, m.diff.repoSel, m.diff.set.Repo.Root)
	}
}

func TestCycleDiffScopeReportsAFailedBaseLookup(t *testing.T) {
	m := buildModel(t)
	openReviewOn(t, m, "keepset", gitTestRepo(t))
	if len(m.diff.set.Files) == 0 {
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
	if m.diff.loading {
		t.Fatal("the failed load should have landed")
	}
	if m.diff.errText == "" {
		t.Fatal("the lookup error should reach the review panel")
	}
}

func TestReviewUntrackedFileShowsCountWithoutOpening(t *testing.T) {
	m := buildModel(t)
	openReviewOn(t, m, "counts", gitTestRepo(t))
	var extra *diff.FileDiff
	for i := range m.diff.set.Files {
		if m.diff.set.Files[i].File.Path == "extra.txt" {
			extra = &m.diff.set.Files[i]
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
	var shot *diff.FileDiff
	for i := range m.diff.set.Files {
		if m.diff.set.Files[i].File.Path == "shot.png" {
			shot = &m.diff.set.Files[i]
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
	m := &Model{width: 100, height: 30, mode: modeDiff, diff: diffState{active: true, loading: true, sessID: "s"}}
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
	m := &Model{
		width: 100, height: 30, mode: modeDiff,
		diff: diffState{
			active: true,
			sessID: "s",
			set:    diff.Set{Files: []diff.FileDiff{{File: git.ChangedFile{Path: "main.go"}}}},
		},
	}
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
	roots := append([]string{}, m.diff.repoRoots...)
	sel := m.diff.repoSel
	if len(roots) == 0 {
		t.Fatal("expected repo roots")
	}
	if cmd := m.handleDiffLoaded(diffLoadedMsg{
		sessID:    m.diff.sessID,
		scope:     m.diff.scope,
		gen:       m.diff.gen,
		err:       errors.New("git died"),
		repoRoots: roots,
		repoRoot:  sel,
	}); cmd != nil {
		t.Fatal("errored load should not follow up")
	}
	if m.diff.errText == "" {
		t.Fatal("error text missing")
	}
	if len(m.diff.repoRoots) == 0 {
		t.Fatal("repo list should survive a failed load so r still works")
	}
	m.openRepoPick()
	if m.mode != modeRepoPick {
		t.Fatalf("r should still open, mode = %v", m.mode)
	}
}
