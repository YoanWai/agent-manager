package ui

import (
	"maps"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// gitRepoWithCommitStack builds main with one commit and a feature branch of
// three: one starts a.go, two extends it and adds b.go, three adds c.go.
func gitRepoWithCommitStack(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "t@t")
	runGit(t, dir, "config", "user.name", "t")
	writeRepoFile(t, dir, "base.go", "package a\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "base")
	runGit(t, dir, "checkout", "-q", "-b", "feature")
	for _, step := range []struct {
		subject string
		files   map[string]string
	}{
		{"one", map[string]string{"a.go": "package a\n\nfunc One() {}\n"}},
		{"two", map[string]string{"a.go": "package a\n\nfunc One() {}\n\nfunc Two() {}\n", "b.go": "package a\n"}},
		{"three", map[string]string{"c.go": "package a\n"}},
	} {
		for name, content := range step.files {
			writeRepoFile(t, dir, name, content)
		}
		runGit(t, dir, "add", "-A")
		runGit(t, dir, "commit", "-m", step.subject)
	}
	return dir
}

func gitRev(t *testing.T, dir, rev string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", rev).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func (m *Model) diffPaths() []string {
	var paths []string
	for _, fd := range m.review.Snapshot().Set.Files {
		paths = append(paths, fd.File.Path)
	}
	return paths
}

func (m *Model) commitSubjects() []string {
	var subjects []string
	for _, c := range m.review.Snapshot().Commits {
		subjects = append(subjects, c.Subject)
	}
	return subjects
}

func (m *Model) enterCommitScope(t *testing.T) {
	t.Helper()
	start := m.review.Scope()
	for m.review.Scope() != git.ScopeCommit {
		m.pressDiffKey(t, 's')
		if m.review.Scope() == start {
			t.Fatalf("s cycled back to %v without reaching the commit scope, err = %q", start, m.review.Snapshot().Error)
		}
	}
	if err := m.review.Snapshot().Error; err != "" {
		t.Fatalf("commit scope load failed: %s", err)
	}
}

func openCommitScope(t *testing.T, m *Model, dir string) {
	t.Helper()
	openReviewOn(t, m, "stack", dir)
	m.enterCommitScope(t)
	m.pressDiffKey(t, '}')
}

// commentOn leaves a draft on the line of path holding text.
func (m *Model) commentOn(t *testing.T, path, text, comment string) {
	t.Helper()
	index := slices.Index(m.diffPaths(), path)
	if index < 0 {
		t.Fatalf("%s is not in the set: %v", path, m.diffPaths())
	}
	m.drainCmds(t, selectReviewFile(m, index))
	fd := m.currentFileDiff()
	line := slices.IndexFunc(fd.Lines, func(line diff.Line) bool { return line.Text == text })
	if line < 0 {
		t.Fatalf("no line %q in %s", text, path)
	}
	m.setCursorDiffLine(line)
	m.openAnnotate()
	typeReviewAnnotation(m, comment)
	m.applyCmd(t, m.saveAnnotation())
}

func (m *Model) commentsShownOn(path string) []string {
	fd := m.fileDiffByPath(path)
	if fd == nil {
		return nil
	}
	var shown []string
	for _, line := range fd.Lines {
		for _, note := range m.review.AnnotationsAt(path, line) {
			shown = append(shown, note.Text)
		}
	}
	return shown
}

func (m *Model) markEveryFileReviewed(t *testing.T) {
	t.Helper()
	m.drainCmds(t, m.reviewCommands(m.review.MarkAll()))
}

// The commit scope opens on the oldest commit with a file left unreviewed,
// and on HEAD once every commit is done.
func TestCommitScopeStartsOnOldestUnreviewed(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	openReviewOn(t, m, "stack", dir)
	m.enterCommitScope(t)
	if got, want := m.review.Snapshot().CommitPin, (uireview.CommitPin{SHA: gitRev(t, dir, "HEAD~2"), Index: 2}); got != want {
		t.Fatalf("pin = %+v, want the oldest %+v", got, want)
	}
	if at, total := m.review.CommitPosition(); at != 1 || total != 3 {
		t.Fatalf("position = %d/%d, want 1/3", at, total)
	}
	m.applyCmd(t, m.toggleReviewed())

	m.pressDiffKey(t, 's')
	m.enterCommitScope(t)
	if got, want := m.review.Snapshot().CommitPin, (uireview.CommitPin{SHA: gitRev(t, dir, "HEAD~1"), Index: 1}); got != want {
		t.Fatalf("pin = %+v, want two %+v once one is reviewed", got, want)
	}
	m.markEveryFileReviewed(t)
	commits := m.review.Snapshot().Commits
	if !m.review.CommitReviewed(commits[1]) {
		t.Fatalf("two should read reviewed, marks = %v", m.review.SavedState().Reviewed)
	}

	m.pressDiffKey(t, 's')
	m.enterCommitScope(t)
	if got := m.review.Snapshot().CommitPin; got != (uireview.CommitPin{}) {
		t.Fatalf("pin = %+v, want HEAD once the older commits are reviewed", got)
	}
	rail := ansi.Strip(m.viewDiffFull())
	for _, want := range []string{"commits  3/3", "✔ " + commits[2].Short + " one", "✔ " + commits[1].Short + " two", "○ " + commits[0].Short + " three"} {
		if !strings.Contains(rail, want) {
			t.Fatalf("rail missing %q:\n%s", want, rail)
		}
	}
}

// Review reopened in the commit scope resumes on the commit last shown, and
// with none shown yet starts on the oldest unreviewed.
func TestCommitScopeReopensWhereReviewLeftOff(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	createSession(t, m, "stack", dir, "")
	m.selectSessionRow(t, "stack")
	sess, _ := m.selected()
	if err := m.services.store.SetReviewScope(sess.ID, "commit"); err != nil {
		t.Fatal(err)
	}
	reopen := func() uireview.CommitPin {
		t.Helper()
		m.drainCmds(t, m.closeDiff())
		m.drainCmds(t, m.openDiff())
		if err := m.review.Snapshot().Error; err != "" {
			t.Fatalf("commit scope load failed: %s", err)
		}
		return m.review.Snapshot().CommitPin
	}
	if got, want := reopen(), (uireview.CommitPin{SHA: gitRev(t, dir, "HEAD~2"), Index: 2}); got != want {
		t.Fatalf("first open pin = %+v, want the oldest %+v", got, want)
	}
	m.pressDiffKey(t, ']')
	if got, want := reopen(), (uireview.CommitPin{SHA: gitRev(t, dir, "HEAD~1"), Index: 1}); got != want {
		t.Fatalf("reopened pin = %+v, want the one left on %+v", got, want)
	}
	m.pressDiffKey(t, '}')
	if got := reopen(); got != (uireview.CommitPin{}) {
		t.Fatalf("reopened pin = %+v, want HEAD where review was left", got)
	}
}

// A branch with no commits of its own lists the target's recent history,
// none of it waiting on review, so the scope opens on HEAD there.
func TestCommitScopeOpensOnHeadOverHistory(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	runGit(t, dir, "branch", "-f", "main", "feature")
	openReviewOn(t, m, "stack", dir)
	m.enterCommitScope(t)
	if got := m.review.Snapshot().CommitPin; got != (uireview.CommitPin{}) {
		t.Fatalf("pin = %+v, want HEAD", got)
	}
	if at, total := m.review.CommitPosition(); at != 4 || total != 4 {
		t.Fatalf("position = %d/%d, want 4/4", at, total)
	}
}

func TestCommitScopeStepsAndPicks(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	openCommitScope(t, m, dir)

	if got, want := m.commitSubjects(), []string{"three", "two", "one"}; !slices.Equal(got, want) {
		t.Fatalf("commits = %v, want %v", got, want)
	}
	state := m.review.Snapshot()
	if state.CommitPin != (uireview.CommitPin{}) || state.Set.Commit != gitRev(t, dir, "HEAD") {
		t.Fatalf("commit scope should open following HEAD, pin = %+v commit = %s", state.CommitPin, state.Set.Commit)
	}
	if header := ansi.Strip(m.viewDiffHeader("stack")); !strings.Contains(header, "L  3/3 "+state.Commits[0].Short+" three") || !strings.Contains(header, "main → feature") {
		t.Fatalf("header should name the commit and target: %q", header)
	}

	m.pressDiffKey(t, '[')
	if got, want := m.review.Snapshot().CommitPin, (uireview.CommitPin{SHA: gitRev(t, dir, "HEAD~1"), Index: 1}); got != want {
		t.Fatalf("pin = %+v, want %+v", got, want)
	}
	if got, want := m.diffPaths(), []string{"a.go", "b.go"}; !slices.Equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	m.pressDiffKey(t, '[')
	m.pressDiffKey(t, '[')
	if got, want := m.diffPaths(), []string{"a.go"}; !slices.Equal(got, want) || m.review.Snapshot().CommitPin.Index != 2 {
		t.Fatalf("stepping past the oldest should stop there, files = %v pin = %+v", got, m.review.Snapshot().CommitPin)
	}
	m.pressDiffKey(t, ']')
	m.pressDiffKey(t, ']')
	if got := m.review.Snapshot().CommitPin; got != (uireview.CommitPin{}) {
		t.Fatalf("reaching the newest should follow HEAD again, pin = %+v", got)
	}

	m.pressDiffKey(t, 'L')
	if m.mode != modeRepoPick {
		t.Fatalf("L should open the commit picker, mode = %v", m.mode)
	}
	m.typeAndEnter(t, "two")
	if got, want := m.review.Snapshot().CommitPin, (uireview.CommitPin{SHA: gitRev(t, dir, "HEAD~1"), Index: 1}); got != want {
		t.Fatalf("picked pin = %+v, want %+v", got, want)
	}
}

// A comment taken on one commit shows on that commit only, though the same
// line sits in the same file of its neighbour.
func TestCommitScopeKeepsCommentsOnTheirCommit(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	openCommitScope(t, m, dir)
	m.pressDiffKey(t, '[')
	m.commentOn(t, "a.go", "func One() {}", "on two")

	m.pressDiffKey(t, '[')
	m.drainCmds(t, m.loadCurrentDiffFile())
	if got := m.commentsShownOn("a.go"); len(got) != 0 {
		t.Fatalf("commit one shows %v, want none", got)
	}
	m.pressDiffKey(t, ']')
	m.drainCmds(t, m.loadCurrentDiffFile())
	if got, want := m.commentsShownOn("a.go"), []string{"on two"}; !slices.Equal(got, want) {
		t.Fatalf("commit two shows %v, want %v", got, want)
	}
}

// Following HEAD, a new commit starts clean: the previous HEAD's marks and
// comments stay with it rather than reading as the new commit's.
func TestCommitScopeNewCommitStartsClean(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	openCommitScope(t, m, dir)
	m.commentOn(t, "c.go", "package a", "on three")
	m.applyCmd(t, m.toggleReviewed())
	if !m.fileReviewed("c.go") {
		t.Fatal("precondition: c.go should be marked reviewed")
	}

	writeRepoFile(t, dir, "c.go", "package a\n\nfunc C() {}\n")
	runGit(t, dir, "commit", "-am", "four")
	m.refreshDiff(t)
	m.drainCmds(t, m.loadCurrentDiffFile())
	if commit := m.review.Snapshot().Set.Commit; commit != gitRev(t, dir, "HEAD") || m.fileReviewed("c.go") {
		t.Fatalf("new HEAD should show unreviewed, commit = %s reviewed = %v", commit, m.fileReviewed("c.go"))
	}
	if got := m.commentsShownOn("c.go"); len(got) != 0 {
		t.Fatalf("new HEAD shows %v, want none", got)
	}

	m.pressDiffKey(t, '[')
	m.drainCmds(t, m.loadCurrentDiffFile())
	if !m.fileReviewed("c.go") {
		t.Fatal("the previous commit should keep its reviewed mark")
	}
	if got, want := m.commentsShownOn("c.go"), []string{"on three"}; !slices.Equal(got, want) {
		t.Fatalf("previous commit shows %v, want %v", got, want)
	}
}

// A rebase rewrites every commit; the pinned commit and the review state
// taken on it follow to the replacement.
func TestCommitScopeFollowsRebase(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	openCommitScope(t, m, dir)
	m.pressDiffKey(t, '[')
	old := m.review.Snapshot().Set.Commit
	m.commentOn(t, "b.go", "package a", "on two")
	m.applyCmd(t, m.toggleReviewed())

	runGit(t, dir, "checkout", "-q", "main")
	writeRepoFile(t, dir, "up.go", "package a\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-m", "upstream")
	runGit(t, dir, "checkout", "-q", "feature")
	runGit(t, dir, "rebase", "-q", "main")
	m.refreshDiff(t)
	m.drainCmds(t, m.loadCurrentDiffFile())

	rebased := gitRev(t, dir, "HEAD~1")
	state := m.review.Snapshot()
	if want := (uireview.CommitPin{SHA: rebased, Index: 1}); state.CommitPin != want {
		t.Fatalf("pin = %+v, want %+v", state.CommitPin, want)
	}
	if want := "commit " + old[:12] + " was rewritten as " + rebased[:12]; state.Notice != want {
		t.Fatalf("notice = %q, want %q", state.Notice, want)
	}
	if !m.fileReviewed("b.go") {
		t.Fatal("the reviewed mark should follow the rebased commit")
	}
	if got, want := m.commentsShownOn("b.go"), []string{"on two"}; !slices.Equal(got, want) {
		t.Fatalf("rebased commit shows %v, want %v", got, want)
	}
	m.drainEffects(t)
	saved, err := m.services.store.ReviewState(state.SessionID, state.RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Comments) != 1 || saved.Comments[0].Scope != "commit@"+rebased {
		t.Fatalf("persisted comments = %+v, want one on %s", saved.Comments, rebased)
	}
}

// A dropped commit leaves nothing to match; review shows the commit now at
// its place and says so.
func TestCommitScopeFallsBackWhenCommitIsDropped(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	openCommitScope(t, m, dir)
	m.pressDiffKey(t, '[')
	old := m.review.Snapshot().Set.Commit

	runGit(t, dir, "rebase", "-q", "--onto", "HEAD~2", "HEAD~1")
	m.refreshDiff(t)

	if got, want := m.commitSubjects(), []string{"three", "one"}; !slices.Equal(got, want) {
		t.Fatalf("commits = %v, want %v", got, want)
	}
	state := m.review.Snapshot()
	if want := (uireview.CommitPin{SHA: gitRev(t, dir, "HEAD~1"), Index: 1}); state.CommitPin != want {
		t.Fatalf("pin = %+v, want %+v", state.CommitPin, want)
	}
	if want := "commit " + old[:12] + " is no longer on the branch, showing " + state.Commits[1].Short; state.Notice != want {
		t.Fatalf("notice = %q, want %q", state.Notice, want)
	}
}

// Picking a target in the commit scope keeps the scope and bounds its list.
func TestCommitScopeTargetBoundsTheList(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	runGit(t, dir, "branch", "partway", "HEAD~1")
	openCommitScope(t, m, dir)

	m.pressDiffKey(t, 'B')
	m.typeAndEnter(t, "partway")
	if got := m.review.Scope(); got != git.ScopeCommit {
		t.Fatalf("scope = %v, want commit", got)
	}
	if got, want := m.commitSubjects(), []string{"three"}; !slices.Equal(got, want) {
		t.Fatalf("commits = %v, want %v", got, want)
	}
}

func TestCommitScopeProbeAgreesWithLoad(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	openCommitScope(t, m, dir)
	m.pressDiffKey(t, '[')
	sess, _ := m.diffSession()
	msg, ok := m.diffProbeCmd(sess, m.review.Scope())().(uireview.ProbeResult)
	if fp := m.review.Snapshot().Fingerprint; !ok || msg.Fingerprint != fp {
		t.Fatalf("probe fingerprint %d must match the load's %d", msg.Fingerprint, fp)
	}
}

// A marks every listed file reviewed, loading the ones not opened yet, and
// a second press clears them.
func TestMarkAllReviewed(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	openCommitScope(t, m, dir)
	m.pressDiffKey(t, '[')
	if m.review.Snapshot().Set.Files[1].Loaded() {
		t.Fatal("precondition: b.go should not be loaded before A")
	}

	m.pressDiffKey(t, 'A')
	state := m.review.Snapshot()
	if !m.review.CommitReviewed(state.Commits[1]) || state.Notice != "marked 2 files reviewed" {
		t.Fatalf("A should mark the commit reviewed, marks = %v notice = %q", m.review.SavedState().Reviewed, state.Notice)
	}
	m.drainEffects(t)
	saved, err := m.services.store.ReviewState(state.SessionID, state.RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if want := m.review.SavedState().Reviewed; !maps.Equal(saved.Reviewed, want) {
		t.Fatalf("persisted marks = %v, want %v", saved.Reviewed, want)
	}

	m.pressDiffKey(t, 'A')
	if marks, notice := m.review.SavedState().Reviewed, m.review.Snapshot().Notice; len(marks) != 0 || notice != "cleared 2 reviewed files" {
		t.Fatalf("second A should clear, marks = %v notice = %q", marks, notice)
	}
}

// Clearing with A schedules no loads, so a file it passes over unloaded is
// still loadable afterwards rather than stuck as loading.
func TestMarkAllClearLeavesUnloadedFilesLoadable(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "two", gitRepoWithTwoChangedFiles(t))
	m.pressDiffKey(t, 'A')
	// A load that is no refresh brings back only the file under the cursor.
	sess, _ := m.diffSession()
	state := m.review.Snapshot()
	m.drainCmds(t, m.diffLoadCmd(sess, state.Scope, state.Generation, state.RepoSelected, false))
	state = m.review.Snapshot()
	other := 1 - state.FileIndex
	path := state.Set.Files[other].File.Path
	if state.Set.Files[other].Loaded() || !m.fileReviewed(path) {
		t.Fatalf("precondition: %s should be marked reviewed but not loaded", path)
	}

	m.applyCmd(t, m.reviewCommands(m.review.MarkAll()))
	if m.fileReviewed(path) {
		t.Fatalf("clear should unmark %s", path)
	}
	if requests := m.review.SwitchFile(other - state.FileIndex); len(requests.Files) != 1 {
		t.Fatalf("%s should still load after the clear, requests = %+v", path, requests)
	}
}

func TestCommitPickerNeedsTheCommitScopeAndCommits(t *testing.T) {
	m := buildModel(t)
	openReviewOn(t, m, "stack", gitRepoWithCommitStack(t))
	m.pressDiffKey(t, 'L')
	if m.mode != modeDiff || m.errBar.text != "s to the commit scope first" {
		t.Fatalf("L outside the commit scope: mode = %v, err = %q", m.mode, m.errBar.text)
	}

	unborn := t.TempDir()
	runGit(t, unborn, "init", "-q", "-b", "main")
	m.pressDiffKey(t, 'q')
	openReviewOn(t, m, "empty", unborn)
	m.enterCommitScope(t)
	m.pressDiffKey(t, 'L')
	if m.mode != modeDiff || m.errBar.text != "no commits to review" {
		t.Fatalf("L with no commits: mode = %v, err = %q", m.mode, m.errBar.text)
	}
}

// Stepping past either end says what is left rather than doing nothing.
func TestCommitScopeEndsSayWhatIsLeft(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	openCommitScope(t, m, dir)
	m.pressDiffKey(t, ']')
	if got, want := m.review.Snapshot().Notice, "HEAD is the newest commit, 3 of 3 not fully reviewed"; got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
	m.pressDiffKey(t, '{')
	m.pressDiffKey(t, '[')
	if got, want := m.review.Snapshot().Notice, "oldest commit, 3 of 3 not fully reviewed"; got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
	for range m.review.Snapshot().Commits {
		m.pressDiffKey(t, 'A')
		m.pressDiffKey(t, ']')
	}
	if got, want := m.review.Snapshot().Notice, "all 3 commits reviewed"; got != want {
		t.Fatalf("notice = %q, want %q", got, want)
	}
}

// A step names the commit it is loading while the load runs, so the counter,
// the rail and the header's pills move straight to it rather than through 0
// or a blank header.
func TestCommitStepHoldsPositionWhileLoading(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	openCommitScope(t, m, dir)
	m.pressDiffKey(t, '{')
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	*m = *updated.(*Model)
	if at, total := m.review.CommitPosition(); at != 2 || total != 3 {
		t.Fatalf("position while loading = %d/%d, want 2/3", at, total)
	}
	if rail := ansi.Strip(m.viewDiffFull()); !strings.Contains(rail, "commits  2/3") {
		t.Fatalf("rail while loading should read 2/3:\n%s", rail)
	}
	commits := m.review.Snapshot().Commits
	if header := ansi.Strip(m.viewDiffHeader("stack")); !strings.Contains(header, "L  2/3 "+commits[1].Short+" two") || !strings.Contains(header, "B  main → feature") {
		t.Fatalf("header while loading = %q, want the target and the commit being loaded", header)
	}
	m.drainCmds(t, cmd)
	if at, _ := m.review.CommitPosition(); at != 2 {
		t.Fatalf("position after load = %d, want 2", at)
	}
}

// The commit rail sits above the files, and a click on a file still opens
// the file painted there; the commit pill opens the commit picker.
func TestCommitScopeClicksReachEveryControl(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithCommitStack(t)
	openCommitScope(t, m, dir)
	commits := m.review.Snapshot().Commits
	x, y := paintedCell(t, m, "[/]")
	m, cmd := leftClickCmd(m, x, y)
	m.drainCmds(t, cmd)
	if got, want := m.review.Snapshot().CommitPin, (uireview.CommitPin{SHA: commits[1].SHA, Index: 1}); got != want {
		t.Fatalf("a click on [/] should step one older, pin = %+v, want %+v", got, want)
	}
	x, y = paintedCell(t, m, "b.go")
	if m = leftPress(m, x, y); m.currentFileDiff().File.Path != "b.go" {
		t.Fatalf("a click on b.go should open it, files = %v index = %d", m.diffPaths(), m.review.Snapshot().FileIndex)
	}
	x, y = paintedCell(t, m, commits[2].Short+" one")
	var updated tea.Model
	updated, cmd = m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	m.drainCmds(t, cmd)
	if got, want := m.review.Snapshot().CommitPin, (uireview.CommitPin{SHA: commits[2].SHA, Index: 2}); got != want {
		t.Fatalf("a click on a rail row should show it, pin = %+v, want %+v", got, want)
	}
	if m = clickLabel(t, m, "L  1/3"); m.mode != modeRepoPick || m.reviewNav.picker.Title() != "⌥ Review commit" {
		t.Fatalf("a click on the commit pill should open the commit picker, mode = %v", m.mode)
	}
}
