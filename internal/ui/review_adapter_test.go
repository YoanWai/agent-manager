package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	uisurface "github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A session whose cwd is an umbrella of several repos opens review on the
// most-active repo, shows the repo in the header, and the r key picks another.
func TestReviewPicksRepoUnderUmbrella(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	if !m.pickerOpen() {
		t.Fatalf("r should open the repo picker, screen = %v", m.screenID())
	}
	for _, r := range "alph" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.pickerOf(t).FilteredRows(); len(got) != 1 || got[0].Label != "alpha" {
		t.Fatalf("filter should narrow to alpha, got %v", got)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.base != baseReview {
		t.Fatalf("enter should return to review, screen = %v", m.screenID())
	}
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != "alpha" {
		t.Fatalf("enter should select alpha, got %q", got)
	}
}

func TestRepoPickerEscapeKeepsRepo(t *testing.T) {
	t.Parallel()
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
	if m.base != baseReview {
		t.Fatalf("esc should return to review, screen = %v", m.screenID())
	}
	if m.review.Snapshot().RepoSelected != before || filepath.Base(m.review.Snapshot().RepoSelected) != dirtyName {
		t.Fatalf("esc should not change the repo, got %q", m.review.Snapshot().RepoSelected)
	}
}

func TestBranchPickerListsWorktreesAndSwitches(t *testing.T) {
	t.Parallel()
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
	if !m.pickerOpen() {
		t.Fatalf("b should open the branch picker, screen = %v (err=%q)", m.screenID(), m.errBar.text)
	}
	rendered := m.viewPicker(t)
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
// that worktree was declared via a temp path that git resolves elsewhere
// (/var -> /private/var on macOS, where the temp dir lives behind a symlink).
func TestBranchPickerSeedsCursorForSymlinkedWorktree(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	alpha := filepath.Join(umbrella, "alpha")

	linkedParent, err := os.MkdirTemp(os.TempDir(), "am-p2-symlink-seed-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(linkedParent) })
	if resolved, _ := filepath.EvalSymlinks(linkedParent); resolved == linkedParent {
		t.Skip("the temp dir does not resolve to a different path on this system")
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
	if !m.pickerOpen() {
		t.Fatalf("b should open the branch picker, screen = %v (err=%q)", m.screenID(), m.errBar.text)
	}
	resolvedWorktree, _ := filepath.EvalSymlinks(rawWorktree)
	rows := m.pickerOf(t).FilteredRows()
	wantCursor := -1
	for i, row := range rows {
		if resolved, _ := filepath.EvalSymlinks(row.Root); resolved == resolvedWorktree {
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
	if m.pickerOf(t).Cursor() != wantCursor {
		t.Fatalf("cursor should seed on the declared worktree row %d, got %d", wantCursor, m.pickerOf(t).Cursor())
	}
}

func TestReviewOpensOnDeclaredRepo(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	if !m.onList() {
		t.Fatalf("esc should leave review, screen = %v", m.screenID())
	}
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != "bravo" {
		t.Fatalf("the hand-picked repo should win over the declared one on reopen, got %q", got)
	}
}

// A hand-picked repo that disappears must be reported and forgotten, so the
// agent's declaration takes over instead of a dead path shadowing it forever.
func TestVanishedHandPickedRepoIsReportedAndForgotten(t *testing.T) {
	t.Parallel()
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
	if _, still := m.ledger.lifecycle.PickedRepos[sess.ID]; still {
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "gone", umbrella)
	before := m.review.Snapshot().RepoSelected

	m.pressDiffKey(t, 'r')
	if !m.pickerOpen() {
		t.Fatalf("r should open the repo picker, screen = %v", m.screenID())
	}
	for _, r := range "alph" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		*m = *updated.(*Model)
	}
	// Hand the sessions back before buildModel's cleanup kills them: a
	// leaked pane keeps its cwd busy, and Windows will not remove a live
	// process's working directory with the rest of the temp dir.
	sessions := m.workspace.sessions
	t.Cleanup(func() { m.workspace.sessions = sessions })
	m.workspace.sessions = nil

	updated, pick := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if pick == nil {
		t.Fatal("enter did not carry the pick to the adapter")
	}
	updated, cmd := m.Update(pick())
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
	t.Parallel()
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
	if !m.pickerOpen() {
		t.Fatalf("r should open the repo picker, screen = %v", m.screenID())
	}
	for m.pickerOf(t).Cursor() != 1 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		*m = *updated.(*Model)
	}
	onScreen := m.pickerOf(t).FilteredRows()[m.pickerOf(t).Cursor()].Root

	// A reload lands carrying only the repos that still exist, re-ranked.
	replaceReviewRootsForTest(m, []string{realRoots[1], realRoots[0]}, m.review.Snapshot().RepoSelected)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)

	if m.base != baseReview || m.pickerOpen() {
		t.Fatalf("enter should return to review, screen = %v", m.screenID())
	}
	if m.review.Snapshot().RepoSelected != onScreen {
		t.Fatalf("enter should load the repo on the cursor row %q, got %q", onScreen, m.review.Snapshot().RepoSelected)
	}
}

func TestRepoPickerFitsTerminalHeight(t *testing.T) {
	t.Parallel()
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

	view := m.viewPicker(t)
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
	if m.pickerOf(t).Cursor() != len(m.review.Snapshot().RepoRoots)-1 {
		t.Fatalf("up from the top should wrap to the last repo, cursor = %d", m.pickerOf(t).Cursor())
	}
	view = m.viewPicker(t)
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
	t.Parallel()
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
	if !m.pickerOpen() {
		t.Fatalf("B should open the base picker, screen = %v", m.screenID())
	}
	labels := map[string]bool{}
	for _, row := range m.pickerOf(t).Rows() {
		labels[row.Label] = true
	}
	if !labels["auto"] || !labels["feature"] {
		t.Fatalf("picker should list auto and feature, got %v", labels)
	}

	m.typeAndEnter(t, "feature")
	if m.base != baseReview {
		t.Fatalf("enter should return to review, screen = %v", m.screenID())
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
	if !m.pickerOpen() {
		t.Fatalf("B should reopen the base picker, screen = %v", m.screenID())
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
	t.Parallel()
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
	if !m.pickerOpen() {
		t.Fatalf("B must open the base picker after the load errored, screen = %v (err=%q)", m.screenID(), m.errBar.text)
	}
	labels := map[string]bool{}
	for _, row := range m.pickerOf(t).Rows() {
		labels[row.Label] = true
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
// terminal. The cursor row and the one below it paint through different styles.
func TestRepoPickRowEscapesControlBytes(t *testing.T) {
	m := &Model{
		layout: layoutState{width: 120},
	}
	row := uireview.PickerRow{Label: "br\x1b]0;P\x07anch", Root: filepath.FromSlash("/tmp/re\x1b[2Jpo/leaf")}
	m.openReviewPicker([]uireview.PickerRow{row, row}, "⌥ Review branch", uireview.PickRepo, "", uireview.PickerSource{}, "")

	out := m.viewPicker(t)
	if got := strings.Count(out, "br^[]0;P^Ganch"); got != 2 {
		t.Errorf("both labels should read as caret notation, got %d in %q", got, out)
	}
	if got := strings.Count(out, filepath.FromSlash("/tmp/re^[[2Jpo")); got != 2 {
		t.Errorf("both roots should read as caret notation, got %d in %q", got, out)
	}
	if stray := strayControl(out); stray != "" {
		t.Errorf("picker row leaks a control byte near %q", stray)
	}
}

type scriptedReviewPickerReader struct {
	worktrees []git.Worktree
	refs      []string
	base      string
	resolved  map[string]string
	err       error
	calls     []string
}

func (r *scriptedReviewPickerReader) Worktrees(root string) ([]git.Worktree, error) {
	r.calls = append(r.calls, "worktrees:"+root)
	return append([]git.Worktree(nil), r.worktrees...), r.err
}

func (r *scriptedReviewPickerReader) BranchRefs(root string) ([]string, error) {
	r.calls = append(r.calls, "branches:"+root)
	return append([]string(nil), r.refs...), r.err
}

func (r *scriptedReviewPickerReader) ReviewBase(targetID, root string) (string, error) {
	r.calls = append(r.calls, "base:"+targetID+":"+root)
	return r.base, r.err
}

func (r *scriptedReviewPickerReader) Canonical(root string) string {
	r.calls = append(r.calls, "canonical:"+root)
	if resolved := r.resolved[root]; resolved != "" {
		return resolved
	}
	return root
}

func seedReviewPickerModel(t *testing.T, m *Model, targetID, root string) {
	t.Helper()
	m.base = baseReview
	m.gens.foreground = 19
	m.review = uireview.New(false)
	request := m.review.Open(uireview.Target{ID: targetID, Name: targetID, Cwd: root}, git.ScopeUncommitted, root)
	m.review.ApplyLoad(uireview.LoadResult{
		TargetID: targetID, Scope: git.ScopeUncommitted, Generation: request.Generation,
		RepoRoot: root, RepoRoots: []string{root}, Set: diff.Set{},
	})
}

func TestReviewPickerReadsRunOnlyInsideCommands(t *testing.T) {
	t.Run("branches", func(t *testing.T) {
		m := &Model{}
		seedReviewPickerModel(t, m, "s1", "/raw/repo")
		reader := &scriptedReviewPickerReader{
			worktrees: []git.Worktree{{Root: "/actual/main", Branch: "main"}, {Root: "/actual/feature", Branch: "feature"}},
			resolved:  map[string]string{"/raw/repo": "/actual/feature"},
		}

		cmd := m.openBranchPickWithReader(reader)
		if cmd == nil {
			t.Fatal("branch picker did not return a read command")
		}
		if len(reader.calls) != 0 || m.base != baseReview {
			t.Fatalf("open path performed I/O or changed screens: calls=%v screen = %v", reader.calls, m.screenID())
		}

		msg := cmd().(uireviewsource.PickerLoadedMsg)
		wantCalls := []string{"worktrees:/raw/repo", "canonical:/raw/repo", "canonical:/actual/main", "canonical:/actual/feature"}
		if !reflect.DeepEqual(reader.calls, wantCalls) {
			t.Fatalf("worker calls = %v, want %v", reader.calls, wantCalls)
		}
		m.handleReviewPickerLoaded(msg)
		if !m.pickerOpen() || m.pickerOf(t).Cursor() != 1 {
			t.Fatalf("branch result did not open on the canonical current worktree: screen = %v cursor=%d", m.screenID(), m.pickerOf(t).Cursor())
		}
	})

	t.Run("bases", func(t *testing.T) {
		m := &Model{}
		seedReviewPickerModel(t, m, "s2", "/raw/repo")
		reader := &scriptedReviewPickerReader{
			refs: []string{"main", "feature"}, base: "feature",
			resolved: map[string]string{"/raw/repo": "/actual/repo"},
		}

		cmd := m.openBasePickWithReader(reader)
		if cmd == nil {
			t.Fatal("base picker did not return a read command")
		}
		if len(reader.calls) != 0 || m.base != baseReview {
			t.Fatalf("open path performed I/O or changed screens: calls=%v screen = %v", reader.calls, m.screenID())
		}

		msg := cmd().(uireviewsource.PickerLoadedMsg)
		wantCalls := []string{"branches:/raw/repo", "canonical:/raw/repo", "base:s2:/actual/repo"}
		if !reflect.DeepEqual(reader.calls, wantCalls) {
			t.Fatalf("worker calls = %v, want %v", reader.calls, wantCalls)
		}
		m.handleReviewPickerLoaded(msg)
		if !m.pickerOpen() || m.pickerOf(t).Cursor() != 2 || m.pickerOf(t).StoreRoot() != "/actual/repo" {
			t.Fatalf("base result = mode %v cursor %d store root %q", m.screenID(), m.pickerOf(t).Cursor(), m.pickerOf(t).StoreRoot())
		}
	})
}

func TestReviewPickerCompletionCannotStealNewerUI(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Model)
	}{
		{"foreground", func(m *Model) { m.gens.foreground++ }},
		{"screen", func(m *Model) { m.base = baseList }},
		{"quit", func(m *Model) { m.effects.Quitting = true }},
		{"review", func(m *Model) { _, _ = m.review.SelectRepo("/other") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{}
			seedReviewPickerModel(t, m, "s1", "/repo")
			reader := &scriptedReviewPickerReader{worktrees: []git.Worktree{{Root: "/repo", Branch: "main"}}}
			msg := m.openBranchPickWithReader(reader)().(uireviewsource.PickerLoadedMsg)
			tc.mutate(m)
			before := m.screenID()
			m.handleReviewPickerLoaded(msg)
			if m.screenID() != before || m.pickerOpen() {
				t.Fatalf("stale completion stole the screen: before=%v after=%v", before, m.screenID())
			}
		})
	}
}

// A pick comes off the stack before the adapter acts on it, and the adapter
// checks the review it names is the one open now: a load or a base save
// that landed behind the picker moved the review on.
func TestReviewPickerEnterRejectsStaleSource(t *testing.T) {
	for _, field := range []string{"generation", "target", "repo"} {
		t.Run(field, func(t *testing.T) {
			m := buildModel(t)
			seedReviewPickerModel(t, m, "s1", "/repo")
			m.openRepoPick()
			picker := m.pickerOf(t)
			source := picker.Source()
			switch field {
			case "generation":
				source.Generation++
			case "target":
				source.TargetID = "other"
			case "repo":
				source.RepoRoot = "/other"
			}
			picker.Open(picker.Rows(), picker.Title(), uireview.PickRepo, "", source, "")
			generation := m.review.Generation()
			updated, pick := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			*m = *updated.(*Model)
			if pick == nil || m.pickerOpen() {
				t.Fatal("enter did not close the picker and carry the pick to the adapter")
			}
			updated, load := m.Update(pick())
			*m = *updated.(*Model)
			if load != nil || m.review.Generation() != generation {
				t.Fatal("stale picker selection started a review load")
			}
			if m.base != baseReview || m.errBar.text == "" {
				t.Fatalf("stale picker was not refused clearly: screen = %v err=%q", m.screenID(), m.errBar.text)
			}
		})
	}
}

func TestReviewBaseSavePersistsButDoesNotReloadStaleOrQuittingReview(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*Model)
	}{
		{"stale", func(m *Model) { _, _ = m.review.SelectRepo("/new-repo") }},
		{"quitting", func(m *Model) { m.effects.Quitting = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "base-save-"+tc.name, t.TempDir(), "")
			sess := reviewSessionByName(t, m, "base-save-"+tc.name)
			seedReviewPickerModel(t, m, sess.ID, "/raw/repo")
			state := m.review.Snapshot()
			source := uireview.PickerSource{Generation: state.Generation, TargetID: state.SessionID, RepoRoot: state.RepoSelected}

			cmd := m.selectBase("feature", source, "/canonical/repo")
			if cmd == nil || m.effects.Main.Active == nil {
				t.Fatal("accepted base did not enter the FIFO effect lane")
			}
			tc.mutate(m)
			before := m.review.Snapshot()
			msg := cmd().(uieffects.Completed)
			updated, follow := m.handleEffectCompleted(msg)
			*m = *updated.(*Model)
			m.drainCmds(t, follow)

			stored, err := m.services.store.ReviewBase(sess.ID, "/canonical/repo")
			if err != nil || stored != "feature" {
				t.Fatalf("captured base was not persisted: base=%q err=%v", stored, err)
			}
			after := m.review.Snapshot()
			if after.Generation != before.Generation || after.RepoSelected != before.RepoSelected || after.Scope != before.Scope {
				t.Fatalf("stale result reloaded review: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestReviewBaseSaveAllowsOnlyOnePendingRequest(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "base-one", t.TempDir(), "")
	sess := reviewSessionByName(t, m, "base-one")
	seedReviewPickerModel(t, m, sess.ID, "/raw/repo")
	state := m.review.Snapshot()
	source := uireview.PickerSource{Generation: state.Generation, TargetID: state.SessionID, RepoRoot: state.RepoSelected}

	cmd := m.selectBase("feature", source, "/canonical/repo")
	if cmd == nil {
		t.Fatal("first base save was not accepted")
	}
	if second := m.selectBase("main", source, "/canonical/repo"); second != nil {
		t.Fatal("second base save started while the first was pending")
	}
	if len(m.effects.Main.Pending) != 0 || m.effects.Main.Active == nil {
		t.Fatalf("base save jobs active=%v pending=%d, want exactly one", m.effects.Main.Active != nil, len(m.effects.Main.Pending))
	}
	if m.errBar.text == "" {
		t.Fatal("second base save was not refused clearly")
	}

	msg := cmd().(uieffects.Completed)
	updated, follow := m.handleEffectCompleted(msg)
	*m = *updated.(*Model)
	m.drainCmds(t, follow)
	stored, err := m.services.store.ReviewBase(sess.ID, "/canonical/repo")
	if err != nil || stored != "feature" {
		t.Fatalf("first base changed after refusal: base=%q err=%v", stored, err)
	}
}

func TestReviewBaseSaveFailureSurfacesAfterNavigation(t *testing.T) {
	m := &Model{}
	seedReviewPickerModel(t, m, "s1", "/repo")
	m.openHelp()
	_, _ = m.review.SelectRepo("/newer-review")

	cmd := m.applyReviewBase(uireviewsource.BaseResult{
		TargetID: "s1", SourceRepo: "/repo", Generation: 1,
		Err: errors.New("persist base failed"),
	})
	if cmd != nil {
		t.Fatal("failed persistence started a review reload")
	}
	if !helpOpen(m) || m.errBar.text != "persist base failed" {
		t.Fatalf("accepted persistence failure was hidden or stole navigation: screen = %v err=%q", m.screenID(), m.errBar.text)
	}
}

func TestReviewBaseSaveCompletesBehindReviewHelp(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := gitRepoWithSecondBranch(t)
	openReviewOn(t, m, "base-help", repo)
	session, ok := m.diffSession()
	if !ok {
		t.Fatal("review session missing")
	}
	m.drainCmds(t, m.openBasePick())
	if !m.pickerOpen() {
		t.Fatalf("base picker did not open: screen = %v err=%q", m.screenID(), m.errBar.text)
	}
	picker := m.pickerOf(t)
	source, storeRoot := picker.Source(), picker.StoreRoot()
	m.Pop()
	cmd := m.selectBase("feature", source, storeRoot)
	if cmd == nil {
		t.Fatal("base save was not accepted")
	}
	m.openHelp()
	if !helpOpen(m) || m.base != baseReview {
		t.Fatalf("review help did not open: screen = %v open=%t", m.screenID(), helpOpen(m))
	}

	m.drainCmds(t, cmd)
	if !helpOpen(m) {
		t.Fatalf("base completion stole help: screen = %v", m.screenID())
	}
	if m.review.Snapshot().Scope != git.ScopeBranch {
		t.Fatalf("base completion did not continue behind help: scope=%v", m.review.Snapshot().Scope)
	}
	stored, err := m.services.store.ReviewBase(session.ID, storeRoot)
	if err != nil || stored != "feature" {
		t.Fatalf("stored base=%q err=%v", stored, err)
	}

	updated, closeCmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	m.drainCmds(t, closeCmd)
	if m.base != baseReview || m.review.Snapshot().Scope != git.ScopeBranch {
		t.Fatalf("closing help lost completed base: screen = %v scope=%v", m.screenID(), m.review.Snapshot().Scope)
	}
}

func TestReviewBaseSaveCompletesBehindBranchPicker(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := gitRepoWithSecondBranch(t)
	openReviewOn(t, m, "base-branch-picker", repo)
	m.drainCmds(t, m.openBasePick())
	picker := m.pickerOf(t)
	source, storeRoot := picker.Source(), picker.StoreRoot()
	m.Pop()
	saveCmd := m.selectBase("feature", source, storeRoot)
	if saveCmd == nil {
		t.Fatal("base save was not accepted")
	}

	branchCmd := m.openBranchPick()
	if branchCmd == nil {
		t.Fatal("branch picker read was not started")
	}
	m.handleReviewPickerLoaded(branchCmd().(uireviewsource.PickerLoadedMsg))
	if !m.pickerOpen() {
		t.Fatalf("branch picker did not open: screen = %v", m.screenID())
	}
	oldGeneration := m.pickerOf(t).Source().Generation

	m.drainCmds(t, saveCmd)
	if !m.pickerOpen() || m.review.Snapshot().Scope != git.ScopeBranch {
		t.Fatalf("base completion did not reconcile behind picker: screen = %v scope=%v", m.screenID(), m.review.Snapshot().Scope)
	}
	if m.review.Generation() == oldGeneration {
		t.Fatal("base completion did not invalidate the older picker generation")
	}
	generation := m.review.Generation()
	updated, enterCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if enterCmd != nil {
		m.drainCmds(t, enterCmd)
	}
	if m.review.Generation() != generation || m.errBar.text == "" {
		t.Fatalf("older branch picker remained actionable: generation=%d err=%q", m.review.Generation(), m.errBar.text)
	}
}

func TestRetainedRepoPickerRendersCurrentMode(t *testing.T) {
	m := shotModel()
	const name = "❯⌥界e\u0301"
	for _, tc := range []struct {
		mode          presentation.GlyphMode
		title, cursor string
	}{{presentation.GlyphModeBasic, "D", ">"}, {presentation.GlyphModeAdvanced, "⌥", "❯"}} {
		m.prefs.glyphMode = tc.mode
		m.openReviewPicker([]uireview.PickerRow{{Label: name, Root: "/repo"}}, "Review repo", uireview.PickRepo, "", uireview.PickerSource{}, "")
		got := ansi.Strip(m.viewPicker(t))
		m.Pop()
		if !strings.Contains(got, tc.title+" Review repo") || !strings.Contains(got, tc.cursor+" "+name) {
			t.Fatalf("%s picker=%q", tc.mode, got)
		}
	}
}

// A review with two repos under one session, the repo picker's real trigger.
func pickerReview(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.base = baseReview
	target := uireview.Target{ID: "picker"}
	request := seedReviewForTest(m, target, git.ScopeUncommitted, "/tmp/one", diff.Set{}, false)
	m.review.ApplyLoad(uireview.LoadResult{
		TargetID: target.ID, Scope: request.Scope, Generation: request.Generation,
		RepoRoot: "/tmp/one", RepoRoots: []string{"/tmp/one", "/tmp/two"}, Set: diff.Set{},
	})
	return m
}

// r opens the picker over Review, which paints and names its keys, and the
// mode under it stays Review.
func TestReviewPickerOpensOnItsKey(t *testing.T) {
	t.Parallel()
	m := pickerReview(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	*m = *updated.(*Model)
	if !m.pickerOpen() || m.base != baseReview {
		t.Fatalf("r should open the picker over review: open=%t screen = %v", m.pickerOpen(), m.screenID())
	}
	m.prepareFrame()
	frame := ansi.Strip(m.View())
	if !strings.Contains(frame, "Review repo") || !strings.Contains(frame, "two") {
		t.Fatalf("the frame should show the picker and its rows:\n%s", frame)
	}
	if top, _ := m.top(); top.Owner() != uisurface.Manager || len(top.Footer()) == 0 {
		t.Fatal("the picker should keep the keyboard on the manager and name its keys")
	}
}

// esc and a click off the card each close the picker and leave Review as
// it was: the filter is a search, not a draft.
func TestReviewPickerDismisses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		close func(m *Model)
	}{
		{"esc", func(m *Model) { m.Update(tea.KeyMsg{Type: tea.KeyEsc}) }},
		{"backdrop", func(m *Model) {
			m.prepareFrame()
			x, y := m.layout.card.left-1, m.layout.card.top-1
			m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := pickerReview(t)
			m.openRepoPick()
			if top, _ := m.top(); !top.Dismissible() {
				t.Fatal("the picker should be dismissible")
			}
			tc.close(m)
			if m.pickerOpen() || m.effects.Quitting || m.base != baseReview {
				t.Fatalf("%s: open=%t quitting=%t screen = %v", tc.name, m.pickerOpen(), m.effects.Quitting, m.screenID())
			}
			if got := m.review.Snapshot().RepoSelected; got != "/tmp/one" {
				t.Fatalf("closing the picker changed the repo to %q", got)
			}
		})
	}
}

// Enter closes the picker and hands the pick to the adapter as a message,
// so the choice is checked against the review showing when it lands.
func TestReviewPickerEnterCarriesThePick(t *testing.T) {
	t.Parallel()
	m := pickerReview(t)
	m.openRepoPick()
	for _, r := range "two" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.pickerOpen() || cmd == nil {
		t.Fatalf("enter should close the picker and return the pick: open=%t cmd=%v", m.pickerOpen(), cmd != nil)
	}
	pick, ok := cmd().(uireview.PickedMsg)
	if !ok || pick.Choice.Value != "/tmp/two" || pick.Choice.Kind != uireview.PickRepo {
		t.Fatalf("pick = %+v", pick)
	}
}

// A pick for a review that has since moved on is dropped, and the user is
// told to reopen the picker instead of retargeting the newer review.
func TestReviewPickerStalePickIsRefused(t *testing.T) {
	t.Parallel()
	m := pickerReview(t)
	m.openRepoPick()
	source := m.pickerOf(t).Source()
	m.Pop()
	_, _ = m.review.SelectRepo("/tmp/two")
	generation := m.review.Generation()
	updated, cmd := m.Update(uireview.PickedMsg{Choice: uireview.PickerChoice{Kind: uireview.PickRepo, Value: "/tmp/one"}, Source: source})
	*m = *updated.(*Model)
	if cmd != nil || m.review.Generation() != generation || !strings.Contains(m.errBar.text, "reopen the picker") {
		t.Fatalf("stale pick: cmd=%v generation=%d err=%q", cmd != nil, m.review.Generation(), m.errBar.text)
	}
}

// A click on a row moves the cursor there, a click on the cursor row picks
// it, and the wheel steps the cursor the way the arrows do.
func TestReviewPickerClickAndWheel(t *testing.T) {
	t.Parallel()
	m := pickerReview(t)
	m.openReviewPicker([]uireview.PickerRow{{Label: "alpha", Root: "/w/alpha"}, {Label: "beta", Root: "/w/beta"}, {Label: "gamma", Root: "/w/gamma"}}, "Review branch", uireview.PickRepo, "/w/alpha", m.reviewPickerSource(), "")
	x, y := paintedCell(t, m, "beta")
	if m = leftClick(m, x, y); m.pickerOf(t).Cursor() != 1 || !m.pickerOpen() {
		t.Fatalf("a click on beta should move the cursor there, cursor %d open %t", m.pickerOf(t).Cursor(), m.pickerOpen())
	}
	if m = wheel(m, tea.MouseButtonWheelDown, x, y); m.pickerOf(t).Cursor() != 2 {
		t.Fatalf("the wheel should move the cursor on, cursor %d", m.pickerOf(t).Cursor())
	}
	x, y = paintedCell(t, m, "gamma")
	m, cmd := leftClickCmd(m, x, y)
	if m.pickerOpen() || cmd == nil {
		t.Fatal("a click on the cursor row should pick it and close the picker")
	}
	if pick, ok := cmd().(uireview.PickedMsg); !ok || filepath.Base(pick.Choice.Value) != "gamma" {
		t.Fatalf("pick = %+v", cmd())
	}
}

// Leaving Review with the picker up pops it, so a reopened Review starts
// clean and nothing is left on the stack over the list.
func TestReviewPickerLeavesWithReview(t *testing.T) {
	t.Parallel()
	m := pickerReview(t)
	m.openRepoPick()
	m.closeDiff()
	if m.pickerOpen() || !m.onList() {
		t.Fatalf("after closing review: open=%t screen = %v", m.pickerOpen(), m.screenID())
	}
}

// pickerOpen reports a review picker on the stack.
func (m *Model) pickerOpen() bool {
	_, ok := surfaceOf[*uireview.Surface](m)
	return ok
}

// pickerOf is the mounted picker's state, for a test that reads its rows or
// cursor the way the surface does.
func (m *Model) pickerOf(t *testing.T) *uireview.Picker {
	t.Helper()
	s, ok := surfaceOf[*uireview.Surface](m)
	if !ok {
		t.Fatal("no review picker is open")
	}
	return s.Picker()
}

// viewPicker paints the mounted picker's card alone.
func (m *Model) viewPicker(t *testing.T) string {
	t.Helper()
	s, ok := surfaceOf[*uireview.Surface](m)
	if !ok {
		t.Fatal("no review picker is open")
	}
	return s.View(m, "")
}

// The point of the hunk model: one edit deep inside a file too big for the
// whole-file model still reaches the screen.
func TestReviewRendersHunksForBigFile(t *testing.T) {
	fd := bigEditedFile(t)
	m := &Model{layout: layoutState{width: 100, height: 30}, screen: screen{base: baseReview}}
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

// The review header always names the repo and its branch; in branch scope it
// shows the base and the branch it diffs into.
func TestReviewHeaderShowsRepoBranchAndBase(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestDiffScopeCycleAndLayout(t *testing.T) {
	t.Parallel()
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
	if view := ansi.Strip(preparedView(m)); !strings.Contains(view, "split") {
		t.Fatalf("split pill missing:\n%s", view)
	}
}

// Review has to paint exactly the terminal it was given. A frame with more
// rows than the terminal scrolls the top away; a line wider than the
// terminal wraps and pushes every row below it off the bottom, which is how
// the end of a file ends up selected but off screen.
func TestDiffFrameFitsTerminal(t *testing.T) {
	t.Parallel()
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
				m.layout.width, m.layout.height = size.w, size.h
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
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("chmod cannot deny reads on Windows")
	}
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
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 52, 24
	openReviewOn(t, m, "very-long-session-name", gitTestRepo(t))
	got := ansi.Strip(m.viewDiffHeader("very-long-session-name"))
	if !strings.Contains(got, "+") || !strings.Contains(got, "−") {
		t.Fatalf("narrow header dropped counts:\n%s", got)
	}
}

func TestDiffFileListTruncatesAtSlash(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
		m.layout.width, m.layout.height = size.w, size.h
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
