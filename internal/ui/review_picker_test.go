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

// A session whose cwd is an umbrella of several repos opens review on the
// most-active repo, shows the repo in the header, and the r key picks another.
func TestReviewPicksRepoUnderUmbrella(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
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
	if m.services.gitDrv == nil {
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
	if m.services.gitDrv == nil {
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
	if m.diff.repoSel != rawWorktree {
		t.Fatalf("repoSel should stay the raw declared path, got %q", m.diff.repoSel)
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
	if m.repoPick.cursor != wantCursor {
		t.Fatalf("cursor should seed on the declared worktree row %d, got %d", wantCursor, m.repoPick.cursor)
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
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
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
	if got := filepath.Base(m.diff.repoSel); got != "bravo" {
		t.Fatalf("picking bravo should load it, got %q", got)
	}

	if err := os.RemoveAll(filepath.Join(umbrella, "bravo")); err != nil {
		t.Fatal(err)
	}
	m.errBar.text = ""
	m.diff.gen++
	m.drainCmds(t, m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false))

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
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
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
	if len(m.diff.repoRoots) < 2 {
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
	before := m.diff.repoSel

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
	if m.diff.repoSel != before {
		t.Fatalf("repo should not change when the session is gone, got %q", m.diff.repoSel)
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
	if m.diff.scope == git.ScopeBranch {
		t.Fatal("precondition: scope should start off vs target so the switch is observable")
	}

	m.pressDiffKey(t, 'B')
	if m.mode != modeRepoPick {
		t.Fatalf("B should open the base picker, mode = %v", m.mode)
	}
	labels := map[string]bool{}
	for _, row := range m.repoPick.rows {
		labels[row.label] = true
	}
	if !labels["auto"] || !labels["feature"] {
		t.Fatalf("picker should list auto and feature, got %v", labels)
	}

	m.typeAndEnter(t, "feature")
	if m.mode != modeDiff {
		t.Fatalf("enter should return to review, mode = %v", m.mode)
	}
	if m.diff.scope != git.ScopeBranch {
		t.Errorf("picking a base should switch scope to vs target, got %v", m.diff.scope)
	}
	got, err := m.services.store.ReviewBase(sess.ID, m.diff.repoSel)
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
	baseA, err := m.services.store.ReviewBase(sess.ID, m.diff.repoSel)
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
	cleared, err := m.services.store.ReviewBase(sess.ID, m.diff.repoSel)
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

	if err := m.services.store.SetReviewBase(sess.ID, m.diff.repoSel, "gone-ref"); err != nil {
		t.Fatal(err)
	}
	m.diff.scope = git.ScopeBranch
	m.diff.gen++
	m.drainCmds(t, m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false))
	if m.diff.errText == "" {
		t.Fatal("an unresolvable stored base must error the load")
	}
	if m.diff.set.Repo.Root != "" {
		t.Fatalf("the errored load should clear the diff set, root = %q", m.diff.set.Repo.Root)
	}

	m.drainCmds(t, m.openBasePick())
	if m.mode != modeRepoPick {
		t.Fatalf("B must open the base picker after the load errored, mode = %v (err=%q)", m.mode, m.errBar.text)
	}
	labels := map[string]bool{}
	for _, row := range m.repoPick.rows {
		labels[row.label] = true
	}
	if !labels["auto"] || !labels["feature"] {
		t.Fatalf("picker should list auto and the refs, got %v", labels)
	}

	m.typeAndEnter(t, "auto")
	base, err := m.services.store.ReviewBase(sess.ID, m.diff.repoSel)
	if err != nil {
		t.Fatal(err)
	}
	if base != "" {
		t.Fatalf("auto should clear the bad base, got %q", base)
	}
	if m.diff.errText != "" {
		t.Fatalf("clearing the base should let the reload succeed, err = %q", m.diff.errText)
	}
	if m.diff.set.Repo.Root == "" {
		t.Fatal("the recovery reload should rebuild the diff set")
	}
}

// A picker row names a repo directory or a git ref, and both reach us as raw
// bytes, so the row has to show a control byte rather than hand it to the
// terminal.
func TestRepoPickRowEscapesControlBytes(t *testing.T) {
	m := &Model{
		width: 120,
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
