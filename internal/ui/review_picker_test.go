package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

// A session whose cwd is an umbrella of several repos opens review on the
// most-active repo, shows the repo in the header, and the r key picks another.
func TestReviewPicksRepoUnderUmbrella(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "umbrella", umbrella)

	if len(m.review.Snapshot().RepoRoots) != 2 {
		t.Fatalf("want 2 repos resolved, got %v (err=%q)", m.review.Snapshot().RepoRoots, m.review.Snapshot().Error)
	}
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != dirtyName {
		t.Fatalf("want dirty repo %q selected first, got %q", dirtyName, got)
	}
	if !strings.Contains(m.viewDiffHeader("umbrella"), dirtyName) {
		t.Fatalf("header should name the selected repo %q", dirtyName)
	}

	m.pickRepo(t, "alpha")
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != "alpha" {
		t.Fatalf("picker should select the other repo, got %q", got)
	}
	if !strings.Contains(m.viewDiffHeader("umbrella"), "alpha") {
		t.Fatal("header should follow the picked repo")
	}
	m.pickRepo(t, dirtyName)
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != dirtyName {
		t.Fatalf("picker should select back, got %q", got)
	}
}

func TestRepoPickerFiltersAndSelects(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "picker", umbrella)
	if filepath.Base(m.review.Snapshot().RepoSelected) != dirtyName {
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
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != "alpha" {
		t.Fatalf("enter should select alpha, got %q", got)
	}
}

func TestRepoPickerEscapeKeepsRepo(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "escpick", umbrella)
	before := m.review.Snapshot().RepoSelected

	m.pressDiffKey(t, 'r')
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if m.mode != modeDiff {
		t.Fatalf("esc should return to review, mode = %v", m.mode)
	}
	if m.review.Snapshot().RepoSelected != before || filepath.Base(m.review.Snapshot().RepoSelected) != dirtyName {
		t.Fatalf("esc should not change the repo, got %q", m.review.Snapshot().RepoSelected)
	}
}

func TestBranchPickerListsWorktreesAndSwitches(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
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
		t.Fatalf("b should open the branch picker, mode = %v (err=%q)", m.mode, m.errBar.text)
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
	sel, _ := filepath.EvalSymlinks(m.review.Snapshot().RepoSelected)
	if sel != resolved {
		t.Fatalf("enter should switch to the worktree, got %q", m.review.Snapshot().RepoSelected)
	}
}

// The b picker must seed its cursor on the worktree under review even when
// that worktree was declared via a /tmp path that git resolves to
// /private/tmp, since /tmp is a symlink on macOS.
func TestBranchPickerSeedsCursorForSymlinkedWorktree(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
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
	if err := m.services.store.SetReviewRepo(sess.ID, rawWorktree); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	if m.errBar.text != "" {
		t.Fatalf("declared worktree must not be reported missing, err = %q", m.errBar.text)
	}
	if m.review.Snapshot().RepoSelected != rawWorktree {
		t.Fatalf("repoSel should stay the raw declared path, got %q", m.review.Snapshot().RepoSelected)
	}

	m.pressDiffKey(t, 'b')
	if m.mode != modeRepoPick {
		t.Fatalf("b should open the branch picker, mode = %v (err=%q)", m.mode, m.errBar.text)
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
	if m.reviewNav.picker.cursor != wantCursor {
		t.Fatalf("cursor should seed on the declared worktree row %d, got %d", wantCursor, m.reviewNav.picker.cursor)
	}
}

func TestReviewOpensOnDeclaredRepo(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	createSession(t, m, "declared", umbrella, "")
	m.selectSessionRow(t, "declared")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no selected session")
	}
	if err := m.services.store.SetReviewRepo(sess.ID, filepath.Join(umbrella, "alpha")); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != "alpha" {
		t.Fatalf("review should open on the declared repo, got %q (ranking prefers %q)", got, dirtyName)
	}
}

// A repo picked by hand outranks the agent's declaration, and keeps doing so
// after review is closed and reopened.
func TestHandPickedRepoOutlivesReopen(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	createSession(t, m, "picked", umbrella, "")
	m.selectSessionRow(t, "picked")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no selected session")
	}
	if err := m.services.store.SetReviewRepo(sess.ID, filepath.Join(umbrella, "alpha")); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != "alpha" {
		t.Fatalf("review should open on the declared repo, got %q", got)
	}

	m.pickRepo(t, "bravo")
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != "bravo" {
		t.Fatalf("picking bravo should load it, got %q", got)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.mode != modeList {
		t.Fatalf("esc should leave review, mode = %v", m.mode)
	}
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != "bravo" {
		t.Fatalf("the hand-picked repo should win over the declared one on reopen, got %q", got)
	}
}

// A hand-picked repo that disappears must be reported and forgotten, so the
// agent's declaration takes over instead of a dead path shadowing it forever.
func TestVanishedHandPickedRepoIsReportedAndForgotten(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
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
	if err := m.services.store.SetReviewRepo(sess.ID, filepath.Join(umbrella, "alpha")); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	m.pickRepo(t, "bravo")
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != "bravo" {
		t.Fatalf("picking bravo should load it, got %q", got)
	}

	if err := os.RemoveAll(filepath.Join(umbrella, "bravo")); err != nil {
		t.Fatal(err)
	}
	m.errBar.text = ""
	request, accepted := m.review.SelectRepo(m.review.Snapshot().RepoSelected)
	if !accepted {
		t.Fatal("repo reload was rejected")
	}
	m.drainCmds(t, m.reviewLoadCmd(request))

	if !strings.Contains(m.errBar.text, "bravo") {
		t.Fatalf("a vanished hand-picked repo must be surfaced, got err %q", m.errBar.text)
	}
	if !strings.Contains(m.viewDiffStatus(), m.errBar.text) {
		t.Fatalf("review status should show %q", m.errBar.text)
	}
	if _, still := m.ledger.pickedRepos[sess.ID]; still {
		t.Fatal("the dead pick must be forgotten so the declaration can take over")
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != "alpha" {
		t.Fatalf("reopening should land on the declared repo, got %q", got)
	}
}

func TestDeclaredWorktreeOutsideCwdIsAccepted(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
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
	if err := m.services.store.SetReviewRepo(sess.ID, outside); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	if m.errBar.text != "" {
		t.Fatalf("declared worktree must not be reported missing, err = %q", m.errBar.text)
	}
	resolved, _ := filepath.EvalSymlinks(outside)
	sel, _ := filepath.EvalSymlinks(m.review.Snapshot().RepoSelected)
	if sel != resolved {
		t.Fatalf("review should open on the declared worktree, got %q", m.review.Snapshot().RepoSelected)
	}
	found := false
	for _, root := range m.review.Snapshot().RepoRoots {
		if r, _ := filepath.EvalSymlinks(root); r == resolved {
			found = true
		}
	}
	if !found {
		t.Fatal("the declared worktree should appear in the picker roots")
	}
}

// A declared repo the session cwd does not contain must be reported, not
// silently swapped for whatever the ranking put on top.
func TestDeclaredRepoOutsideCwdIsReported(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	createSession(t, m, "elsewhere", umbrella, "")
	m.selectSessionRow(t, "elsewhere")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no selected session")
	}
	if err := m.services.store.SetReviewRepo(sess.ID, filepath.Join(t.TempDir(), "somewhere-else")); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())

	if m.errBar.text == "" {
		t.Fatal("a declared repo outside the session cwd must be surfaced")
	}
	if !strings.Contains(m.viewDiffStatus(), m.errBar.text) {
		t.Fatalf("review status should show %q", m.errBar.text)
	}
	if len(m.review.Snapshot().RepoRoots) < 2 {
		t.Fatal("the picker must stay usable so the user can recover")
	}
}

// Picking a repo after the session has left m.sessions must say so instead of
// dropping the user back into review with the old repo and no explanation.
func TestRepoPickerReportsMissingSession(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "gone", umbrella)
	before := m.review.Snapshot().RepoSelected

	m.pressDiffKey(t, 'r')
	if m.mode != modeRepoPick {
		t.Fatalf("r should open the repo picker, mode = %v", m.mode)
	}
	for _, r := range "alph" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		*m = *updated.(*Model)
	}
	m.workspace.sessions = nil

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if cmd != nil {
		t.Fatal("a missing session must not kick off a diff load")
	}
	if m.errBar.text == "" {
		t.Fatal("picking a repo for a missing session must surface an error")
	}
	if m.review.Snapshot().RepoSelected != before {
		t.Fatalf("repo should not change when the session is gone, got %q", m.review.Snapshot().RepoSelected)
	}
	if !strings.Contains(m.viewDiffStatus(), m.errBar.text) {
		t.Fatalf("review status should show the error %q", m.errBar.text)
	}
}

// The poller reloads repoRoots while the picker is open, so the live list can
// shrink and, because rankRepos is dirty-first, reorder under a parked cursor.
// The picker works off a snapshot, so Enter must load the repo whose row was on
// screen and must never index past the list.
func TestRepoPickerSurvivesShrinkingRootList(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "shrink", umbrella)

	realRoots := append([]string(nil), m.review.Snapshot().RepoRoots...)
	if len(realRoots) != 2 {
		t.Fatalf("want 2 real repos, got %v", realRoots)
	}
	expanded := append([]string(nil), realRoots...)
	for i := len(realRoots); i < 20; i++ {
		expanded = append(expanded, filepath.Join(umbrella, fmt.Sprintf("repo-%02d", i)))
	}
	replaceReviewRootsForTest(m, expanded, m.review.Snapshot().RepoSelected)

	m.pressDiffKey(t, 'r')
	if m.mode != modeRepoPick {
		t.Fatalf("r should open the repo picker, mode = %v", m.mode)
	}
	for m.reviewNav.picker.cursor != 1 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		*m = *updated.(*Model)
	}
	onScreen := m.filteredRows()[m.reviewNav.picker.cursor].root

	// A reload lands carrying only the repos that still exist, re-ranked.
	replaceReviewRootsForTest(m, []string{realRoots[1], realRoots[0]}, m.review.Snapshot().RepoSelected)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)

	if m.mode != modeDiff {
		t.Fatalf("enter should return to review, mode = %v", m.mode)
	}
	if m.reviewNav.picker.cursor >= len(m.reviewNav.picker.rows) {
		t.Fatalf("cursor should stay inside the snapshot, got %d", m.reviewNav.picker.cursor)
	}
	if m.review.Snapshot().RepoSelected != onScreen {
		t.Fatalf("enter should load the repo on the cursor row %q, got %q", onScreen, m.review.Snapshot().RepoSelected)
	}
}

func TestRepoPickerFitsTerminalHeight(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 80, 24
	roots := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		roots = append(roots,
			fmt.Sprintf("/home/someone/very/long/parent/path/for/wrapping/umbrella/repo-%02d", i))
	}
	seedReviewForTest(m, uireview.Target{ID: "picker"}, git.ScopeUncommitted, roots[0], diff.Set{}, false)
	replaceReviewRootsForTest(m, roots, roots[0])
	m.openRepoPick()

	view := m.viewRepoPick()
	if lines := len(strings.Split(view, "\n")); lines > m.layout.height {
		t.Fatalf("picker rendered %d lines, terminal is %d", lines, m.layout.height)
	}
	if !strings.Contains(view, "repo-00") {
		t.Fatal("the cursor row should be visible at the top of the list")
	}
	shown := strings.Count(view, "repo-")
	if shown == 0 || shown >= len(m.review.Snapshot().RepoRoots) {
		t.Fatalf("expected a windowed subset of the repos, %d of %d rendered", shown, len(m.review.Snapshot().RepoRoots))
	}
	if want := fmt.Sprintf("+%d more", len(m.review.Snapshot().RepoRoots)-shown); !strings.Contains(view, want) {
		t.Fatalf("hidden count should match the %d rows actually rendered, want %q in view", shown, want)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	*m = *updated.(*Model)
	if m.reviewNav.picker.cursor != len(m.review.Snapshot().RepoRoots)-1 {
		t.Fatalf("up from the top should wrap to the last repo, cursor = %d", m.reviewNav.picker.cursor)
	}
	view = m.viewRepoPick()
	if lines := len(strings.Split(view, "\n")); lines > m.layout.height {
		t.Fatalf("picker rendered %d lines at the list end, terminal is %d", lines, m.layout.height)
	}
	if !strings.Contains(view, "repo-19") {
		t.Fatal("the cursor must stay visible after moving to the end of the list")
	}
}

// The B picker lists auto plus the repo's refs; picking a ref persists it per
// repo and forces the branch scope, and auto clears the stored base.
func TestBasePickerPersistsSwitchesScopeAndClears(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithSecondBranch(t)
	openReviewOn(t, m, "base", dir)
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}
	if m.review.Snapshot().Scope == git.ScopeBranch {
		t.Fatal("precondition: scope should start off vs target so the switch is observable")
	}

	m.pressDiffKey(t, 'B')
	if m.mode != modeRepoPick {
		t.Fatalf("B should open the base picker, mode = %v", m.mode)
	}
	labels := map[string]bool{}
	for _, row := range m.reviewNav.picker.rows {
		labels[row.label] = true
	}
	if !labels["auto"] || !labels["feature"] {
		t.Fatalf("picker should list auto and feature, got %v", labels)
	}

	m.typeAndEnter(t, "feature")
	if m.mode != modeDiff {
		t.Fatalf("enter should return to review, mode = %v", m.mode)
	}
	if m.review.Snapshot().Scope != git.ScopeBranch {
		t.Errorf("picking a base should switch scope to vs target, got %v", m.review.Snapshot().Scope)
	}
	got, err := m.services.store.ReviewBase(sess.ID, m.review.Snapshot().RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if got != "feature" {
		t.Errorf("stored base = %q, want feature", got)
	}
	// Genuine per-repo round trip: a base stored for a second repo must read
	// back independently, and repo A's base must stay put.
	repoB := gitRepoWithSecondBranch(t)
	if err := m.services.store.SetReviewBase(sess.ID, repoB, "main"); err != nil {
		t.Fatal(err)
	}
	baseA, err := m.services.store.ReviewBase(sess.ID, m.review.Snapshot().RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if baseA != "feature" {
		t.Errorf("repo A base = %q, want feature", baseA)
	}
	baseB, err := m.services.store.ReviewBase(sess.ID, repoB)
	if err != nil {
		t.Fatal(err)
	}
	if baseB != "main" {
		t.Errorf("repo B base = %q, want main", baseB)
	}

	m.pressDiffKey(t, 'B')
	if m.mode != modeRepoPick {
		t.Fatalf("B should reopen the base picker, mode = %v", m.mode)
	}
	m.typeAndEnter(t, "auto")
	cleared, err := m.services.store.ReviewBase(sess.ID, m.review.Snapshot().RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if cleared != "" {
		t.Errorf("auto should clear the stored base, got %q", cleared)
	}
}

// A stored base that no longer resolves errors the branch-scope load and clears
// the diff set, leaving the resolved toplevel empty. The B picker must still
// open - keyed off the raw selection - so auto can clear the bad base, which is
// the only recovery path.
func TestInvalidStoredBaseStillOpensPickerAndRecovers(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithSecondBranch(t)
	openReviewOn(t, m, "invalidbase", dir)
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}

	if err := m.services.store.SetReviewBase(sess.ID, m.review.Snapshot().RepoSelected, "gone-ref"); err != nil {
		t.Fatal(err)
	}
	for m.review.Snapshot().Scope != git.ScopeBranch {
		request, accepted := m.review.CycleScope()
		if !accepted {
			t.Fatal("branch scope request was rejected")
		}
		m.drainCmds(t, m.reviewLoadCmd(request))
	}
	if m.review.Snapshot().Error == "" {
		t.Fatal("an unresolvable stored base must error the load")
	}
	if m.review.Snapshot().Set.Repo.Root != "" {
		t.Fatalf("the errored load should clear the diff set, root = %q", m.review.Snapshot().Set.Repo.Root)
	}

	m.drainCmds(t, m.openBasePick())
	if m.mode != modeRepoPick {
		t.Fatalf("B must open the base picker after the load errored, mode = %v (err=%q)", m.mode, m.errBar.text)
	}
	labels := map[string]bool{}
	for _, row := range m.reviewNav.picker.rows {
		labels[row.label] = true
	}
	if !labels["auto"] || !labels["feature"] {
		t.Fatalf("picker should list auto and the refs, got %v", labels)
	}

	m.typeAndEnter(t, "auto")
	base, err := m.services.store.ReviewBase(sess.ID, m.review.Snapshot().RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if base != "" {
		t.Fatalf("auto should clear the bad base, got %q", base)
	}
	if m.review.Snapshot().Error != "" {
		t.Fatalf("clearing the base should let the reload succeed, err = %q", m.review.Snapshot().Error)
	}
	if m.review.Snapshot().Set.Repo.Root == "" {
		t.Fatal("the recovery reload should rebuild the diff set")
	}
}

// A picker row names a repo directory or a git ref, and both reach us as raw
// bytes, so the row has to show a control byte rather than hand it to the
// terminal.
func TestRepoPickRowEscapesControlBytes(t *testing.T) {
	m := &Model{
		layout: layoutState{width: 120},
	}
	row := pickRow{label: "br\x1b]0;P\x07anch", root: "/tmp/re\x1b[2Jpo/leaf"}

	for _, selected := range []bool{false, true} {
		out := m.repoPickRow(row, selected)
		if !strings.Contains(out, "br^[]0;P^Ganch") {
			t.Errorf("selected=%v: label should read as caret notation, got %q", selected, out)
		}
		if !strings.Contains(out, "/tmp/re^[[2Jpo") {
			t.Errorf("selected=%v: root should read as caret notation, got %q", selected, out)
		}
		if stray := strayControl(out); stray != "" {
			t.Errorf("selected=%v: picker row leaks a control byte near %q", selected, stray)
		}
	}
}
