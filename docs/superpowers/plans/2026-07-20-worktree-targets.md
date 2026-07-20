# Worktree Targets Implementation Plan (fold into #39)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the agent's declared review target work when it is a git worktree living anywhere on disk, and give the human a `b` picker over the active repo's worktrees (branches), so review is AI-driven with rare manual override.

**Architecture:** `git worktree list --porcelain` becomes the source of branch data. The diff loader accepts any declared or picked root that `git rev-parse` confirms is a repo root, appending it to the resolved candidates instead of requiring containment under the session cwd. A `b` picker lists the active repo's worktrees with branch names and retargets through the same path-pinned selection the `r` picker uses.

**Tech Stack:** Go, bubbletea/lipgloss, git CLI via `internal/git`.

## Global Constraints

- Work in the worktree `/tmp/am-p2` on branch `feat/review-target`. Never touch the shared checkout at /Users/yoan/Desktop/projects/agent-manager.
- Stage explicit paths. Never `git add -A`.
- Zero code comments unless a non-obvious WHY needs recording; one short line max.
- No AI attribution in commit messages.
- Fail loudly; no silent fallbacks masking a real problem. No speculative code for unreachable cases.
- `gofmt -l internal/ .` prints nothing, `go vet ./...` clean, `go test ./...` green before each commit.
- Spec: `docs/superpowers/specs/2026-07-20-review-target-and-noise-design.md` (section "Worktrees are branches").

---

### Task 1: Enumerate a repo's worktrees

**Files:**
- Modify: `internal/git/git.go`
- Test: `internal/git/git_test.go`

**Interfaces:**
- Produces:
  - `type Worktree struct { Root, Branch string }`
  - `func (d *Driver) Worktrees(root string) ([]Worktree, error)` — parses `git worktree list --porcelain`; `Branch` is the short name (strip `refs/heads/`); a detached worktree gets its short HEAD sha as Branch.
  - `func (d *Driver) IsRepoRoot(dir string) bool` — true when `rev-parse --show-toplevel` inside `dir` succeeds and returns `dir` itself (compare after `filepath.EvalSymlinks` on both sides, macOS `/tmp` resolves through `/private`).

- [ ] **Step 1: Write the failing test**

Add to `internal/git/git_test.go`:

```go
func TestWorktreesListsBranches(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.go", "package a\n")
	commit(t, dir, "init")
	wtDir := filepath.Join(t.TempDir(), "wt-feature")
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("worktree", "add", "-b", "feature/x", wtDir)

	worktrees, err := driver.Worktrees(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(worktrees) != 2 {
		t.Fatalf("want 2 worktrees, got %+v", worktrees)
	}
	byBranch := map[string]string{}
	for _, wt := range worktrees {
		byBranch[wt.Branch] = wt.Root
	}
	if _, ok := byBranch["feature/x"]; !ok {
		t.Fatalf("feature/x missing: %+v", worktrees)
	}
	if _, ok := byBranch["main"]; !ok {
		t.Fatalf("main missing: %+v", worktrees)
	}
}

func TestIsRepoRoot(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.go", "package a\n")
	commit(t, dir, "init")
	if !driver.IsRepoRoot(dir) {
		t.Fatal("repo root should be recognised")
	}
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if driver.IsRepoRoot(sub) {
		t.Fatal("a subdirectory is not the root")
	}
	if driver.IsRepoRoot(t.TempDir()) {
		t.Fatal("a non-repo dir is not a root")
	}
}
```

- [ ] **Step 2: Run to verify failure** — `cd /tmp/am-p2 && go test ./internal/git/ -run 'TestWorktrees|TestIsRepoRoot' -v` fails to compile: `driver.Worktrees undefined`.

- [ ] **Step 3: Implement**

Add to `internal/git/git.go`:

```go
type Worktree struct {
	Root   string
	Branch string
}

func (d *Driver) Worktrees(root string) ([]Worktree, error) {
	out, err := d.run(root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var worktrees []Worktree
	var current Worktree
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = Worktree{Root: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "HEAD "):
			sha := strings.TrimPrefix(line, "HEAD ")
			if len(sha) > 7 {
				sha = sha[:7]
			}
			current.Branch = sha
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "":
			if current.Root != "" {
				worktrees = append(worktrees, current)
				current = Worktree{}
			}
		}
	}
	if current.Root != "" {
		worktrees = append(worktrees, current)
	}
	return worktrees, nil
}

func (d *Driver) IsRepoRoot(dir string) bool {
	top, err := d.run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	resolvedTop, err := filepath.EvalSymlinks(top)
	if err != nil {
		return false
	}
	return resolvedDir == resolvedTop
}
```

- [ ] **Step 4: Verify pass** — `go test ./internal/git/ -v` all green.
- [ ] **Step 5: Commit** — `git add internal/git/git.go internal/git/git_test.go && git commit -m "feat: enumerate a repo's worktrees with branch names"`

---

### Task 2: Accept a declared or picked worktree anywhere on disk

Today `diffLoadCmd` reports any `repoWant` absent from the discovered roots and falls back to the ranking. Agents' worktrees live outside the session cwd, so the declaration this branch exists for never takes effect in the dominant workflow.

**Files:**
- Modify: `internal/ui/diffview.go` (`diffLoadCmd`, `handleDiffLoaded`)
- Test: `internal/ui/review_regression_test.go`

**Interfaces:**
- Consumes: `driver.IsRepoRoot` (Task 1).
- Produces: no signature changes. New behaviour: a `repoWant` that is a valid repo root but absent from the discovered roots is appended to `msg.repoRoots` and selected; `repoWant` is reported (and a dead hand-pick forgotten) only when `IsRepoRoot` says it is not a repo.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Verify failure** — the test fails on the `m.err` assertion ("no longer under the session directory") or on `repoSel`.

- [ ] **Step 3: Implement**

In `diffLoadCmd`, when `repoWant != ""` and no discovered root matches: call `driver.IsRepoRoot(repoWant)`. True: append `repoWant` to `roots`, select it. False: keep the current reporting-and-fallback behaviour. Preserve the existing empty-`repoWant` ranking path untouched. Keep `handleDiffLoaded`'s forget-dead-pick logic keyed to the reported case, so a hand-picked worktree outside the cwd is NOT forgotten, while a genuinely dead path still is. `TestVanishedHandPickedRepoIsReportedAndForgotten` must keep passing (a deleted directory fails `IsRepoRoot`, so it still reports and forgets).

- [ ] **Step 4: Verify pass** — `go test ./internal/ui/ -v` all green, including `TestVanishedHandPickedRepoIsReportedAndForgotten` and `TestReviewOpensOnDeclaredRepo`.
- [ ] **Step 5: Commit** — `git add internal/ui/diffview.go internal/ui/review_regression_test.go && git commit -m "feat: accept a declared worktree wherever it lives"`

---

### Task 3: The `b` branch picker

**Files:**
- Modify: `internal/ui/repopicker.go` (generalise the picker state to serve both lists), `internal/ui/diffview.go` (`b` key, footer), `internal/ui/model.go` (dispatch), `internal/ui/modals.go` (help)
- Test: `internal/ui/review_regression_test.go`

**Interfaces:**
- Consumes: `driver.Worktrees` (Task 1), the existing `repoPickState`/`selectRepo` machinery.
- Produces:
  - `func (m *Model) openBranchPick() tea.Cmd` — no-op with a `m.err` notice when worktree listing fails; opens `modeRepoPick` with rows labelled `branch  path` and the cursor on the current worktree.
  - The picker rows become `[]pickRow{label, root string}` so one modal serves both: `r` fills it with repos (label = base name), `b` fills it with worktrees (label = branch). Filtering matches label and root, case-insensitive. Enter calls the existing `selectRepo(row.root)`.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Verify failure** — fails: no `b` binding, mode stays `modeDiff`.

- [ ] **Step 3: Implement** per the interfaces above. `b` in `handleDiffKey` calls `openBranchPick()`. Listing worktrees shells out to git, so do it in the key handler synchronously (it is fast and local); on error set `m.err` and stay in review. Rows for `b`: every worktree of the currently selected repo (resolve via `m.diff.set.Repo.Root`), labelled by branch. Footer gains `{"b", "branch"}` when review is active and the repo has more than one worktree; help modal line added beside the `r` line.

- [ ] **Step 4: Verify pass** — `go test ./internal/ui/ -v` all green, including every existing picker test (adapt their row access to `pickRow` if the refactor touches them, without weakening assertions).
- [ ] **Step 5: Commit** — `git add internal/ui/repopicker.go internal/ui/diffview.go internal/ui/model.go internal/ui/modals.go internal/ui/review_regression_test.go && git commit -m "feat: pick the review branch from the repo's worktrees"`

---

### Task 4: Docs, live verification, push

- [ ] **Step 1:** Update `README.md`: `review-repo` accepts a worktree path wherever it lives, one declaration names repo and branch, `b` lists the repo's worktrees. No em or en dashes; positive statements only.
- [ ] **Step 2:** Full gates: `gofmt -l internal/ .` empty, `go vet ./...` clean, `go test ./...` green.
- [ ] **Step 3:** Commit docs, push the branch.

## Self-Review

Spec coverage: "Worktrees are branches" maps to Tasks 1-3; the base picker section is explicitly deferred. Placeholders: none; Task 2 Step 3 and Task 3 Step 3 describe behaviour precisely where quoting the full surrounding functions would go stale. Type consistency: `Worktree{Root, Branch}` (Task 1) consumed in Task 3; `IsRepoRoot` (Task 1) consumed in Task 2; `selectRepo(root string)` reused unchanged.
