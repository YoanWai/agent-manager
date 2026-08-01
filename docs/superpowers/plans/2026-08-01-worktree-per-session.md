# Worktree-per-session Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A session can run in its own git worktree (`<repoParent>/<repoName>-worktrees/<session-name>`, branch `am/<session-name>`), toggled in the new-session form, the quick prompt bar (`alt+w`), and a settings default; clean worktrees are removed on session delete.

**Architecture:** New primitives in `internal/git` (`RepoRoot`, `AddWorktree`, `RemoveWorktreeIfClean`), two new columns in `internal/store` (`worktree_repo`, `worktree_branch`), and a `worktree bool` threaded through the existing spawn path in `internal/ui`. Spec: `docs/superpowers/specs/2026-08-01-worktree-per-session-design.md`.

**Tech Stack:** Go, bubbletea TUI, modernc.org/sqlite, git CLI via `internal/git.Driver.run`.

## Global Constraints

- Work happens in the isolated worktree `.claude/worktrees/worktree-sessions` on branch `worktree-worktree-sessions`. Never cd to the main checkout.
- Any command that runs `go test` on `./internal/ui/...` (or `./...`) MUST be `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ...` — the ui tests drive tmux and a bare run hits the user's live tmux server. `git` and `store` package tests are included in `./...`, so always use the isolated form.
- Zero code comments except non-obvious WHY. No em dashes anywhere. No AI attribution in commits.
- Error policy: no silent fallbacks. Worktree failures block the spawn with a visible error.
- Failure mode wording (spec): blocked spawn errors surface in `m.errBar.text`.
- Branch prefix is exactly `am/`. Worktree dir is exactly `<repoParent>/<repoName>-worktrees/<sanitized-session-name>`.
- Settings key: `worktree_default`, values `"on"` / `"off"`, default off (missing key = off).
- After all tasks: `npx` gate does not apply (Go project); run `go vet ./...` and the full isolated test suite instead.

---

### Task 1: Store columns for worktree sessions

**Files:**
- Modify: `internal/store/store.go` (Session struct ~line 18, `init` migrations ~line 90, `CreateSession` ~line 217, `ListSessions` ~line 263, `Get` ~line 294, and any other `SELECT id, name, tool, cwd` site — find them all with `grep -n "SELECT id, name" internal/store/store.go`)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `store.Session.WorktreeRepo string`, `store.Session.WorktreeBranch string`, persisted by `CreateSession` and returned by `ListSessions`, `Get`, `SessionsInSubtree`.

- [ ] **Step 1: Write the failing test**

Append to `internal/store/store_test.go`, following the file's existing open-a-temp-store pattern:

```go
func TestSessionWorktreeColumnsRoundTrip(t *testing.T) {
	s := openTestStore(t)
	sess := Session{
		ID: "wt1", Name: "feat", Tool: "claude", Cwd: "/tmp/repo-worktrees/feat",
		WorktreeRepo: "/tmp/repo", WorktreeBranch: "am/feat",
	}
	if err := s.CreateSession(sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.Get("wt1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.WorktreeRepo != "/tmp/repo" || got.WorktreeBranch != "am/feat" {
		t.Fatalf("worktree fields lost: %+v", got)
	}
	list, err := s.ListSessions(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list[0].WorktreeRepo != "/tmp/repo" || list[0].WorktreeBranch != "am/feat" {
		t.Fatalf("list dropped worktree fields: %+v", list[0])
	}
}
```

If `store_test.go` has no `openTestStore` helper, use the same inline pattern its other tests use (`Open(filepath.Join(t.TempDir(), "state.db"))` with `t.Cleanup(store.Close)`).

- [ ] **Step 2: Run test to verify it fails**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/store/ -run TestSessionWorktreeColumnsRoundTrip -v`
Expected: FAIL, compile error `unknown field WorktreeRepo`.

- [ ] **Step 3: Implement**

In `Session` struct, after `AgentSessionID`:

```go
	// WorktreeRepo and WorktreeBranch are set only for sessions running in
	// their own git worktree: the main repo root and the am/ branch recorded
	// at creation, so delete-time cleanup survives later renames.
	WorktreeRepo   string
	WorktreeBranch string
```

Append to the `migrations` slice in `init`:

```go
		`ALTER TABLE sessions ADD COLUMN worktree_repo TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN worktree_branch TEXT NOT NULL DEFAULT ''`,
```

Update `CreateSession`'s INSERT to include the columns and values (`worktree_repo, worktree_branch` after `agent_session_id`, bound to `sess.WorktreeRepo, sess.WorktreeBranch`). Update every `SELECT id, name, tool, cwd, ...` query to also select `worktree_repo, worktree_branch`, and every matching `rows.Scan`/`QueryRow(...).Scan` to scan into `&sess.WorktreeRepo, &sess.WorktreeBranch` in the same position.

- [ ] **Step 4: Run store tests**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/store/ -v`
Expected: all PASS (round-trip test plus no regressions in existing tests).

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): record worktree repo and branch per session"
```

---

### Task 2: Git driver: RepoRoot and AddWorktree

**Files:**
- Modify: `internal/git/git.go` (append near `Worktrees` ~line 626)
- Test: `internal/git/git_test.go`

**Interfaces:**
- Consumes: existing `d.run(dir string, args ...string) (string, error)` (output is whitespace-trimmed) and the `testRepo`/`commit`/`write` helpers in `git_test.go`.
- Produces:
  - `func (d *Driver) RepoRoot(dir string) (string, error)`
  - `func (d *Driver) AddWorktree(root, sessionName string) (path, branch string, err error)`
  - unexported `worktreeBase(root string) string` and `sanitizeWorktreeName(name string) string`

- [ ] **Step 1: Write the failing tests**

Append to `internal/git/git_test.go`:

```go
func TestRepoRoot(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	root, err := driver.RepoRoot(dir)
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	if resolved, _ := filepath.EvalSymlinks(dir); root != resolved && root != dir {
		t.Fatalf("root = %q, want %q", root, dir)
	}
	if _, err := driver.RepoRoot(t.TempDir()); err == nil {
		t.Fatal("non-repo dir should error")
	}
}

func TestAddWorktree(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")

	path, branch, err := driver.AddWorktree(dir, "my feat/1")
	if err != nil {
		t.Fatalf("add worktree: %v", err)
	}
	wantPath := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-worktrees", "my-feat-1")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}
	if branch != "am/my-feat-1" {
		t.Fatalf("branch = %q", branch)
	}
	if _, err := os.Stat(filepath.Join(path, "a.txt")); err != nil {
		t.Fatalf("worktree missing checkout: %v", err)
	}

	if _, _, err := driver.AddWorktree(dir, "my feat/1"); err == nil {
		t.Fatal("existing path should error")
	}
}

func TestAddWorktreeEmptyRepoFails(t *testing.T) {
	driver, dir := testRepo(t)
	if _, _, err := driver.AddWorktree(dir, "feat"); err == nil {
		t.Fatal("repo with no commits has no base ref, want error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/git/ -run 'TestRepoRoot|TestAddWorktree' -v`
Expected: FAIL, compile error `driver.RepoRoot undefined`.

- [ ] **Step 3: Implement**

Append to `internal/git/git.go` (add `"regexp"` to imports if absent):

```go
func (d *Driver) RepoRoot(dir string) (string, error) {
	top, err := d.run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not inside a git repository: %s", dir)
	}
	return top, nil
}

var worktreeNamePattern = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeWorktreeName(name string) string {
	return strings.Trim(worktreeNamePattern.ReplaceAllString(name, "-"), "-.")
}

// worktreeBase picks the ref a session worktree branches from: the remote
// default branch when cached, else a local default branch, else HEAD.
func (d *Driver) worktreeBase(root string) string {
	if _, err := d.run(root, "rev-parse", "--verify", "--quiet", "origin/HEAD"); err == nil {
		return "origin/HEAD"
	}
	for _, candidate := range []string{"main", "master"} {
		if _, err := d.run(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+candidate); err == nil {
			return candidate
		}
	}
	if _, err := d.run(root, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		return "HEAD"
	}
	return ""
}

func (d *Driver) AddWorktree(root, sessionName string) (string, string, error) {
	name := sanitizeWorktreeName(sessionName)
	if name == "" {
		return "", "", fmt.Errorf("session name %q leaves nothing usable for a worktree directory", sessionName)
	}
	path := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-worktrees", name)
	if _, err := os.Stat(path); err == nil {
		return "", "", fmt.Errorf("worktree path already exists: %s", path)
	}
	branch := "am/" + name
	base := d.worktreeBase(root)
	if base == "" {
		return "", "", fmt.Errorf("no base ref for a worktree in %s: repository has no commits", root)
	}
	if _, err := d.run(root, "worktree", "add", "-b", branch, path, base); err != nil {
		return "", "", err
	}
	return path, branch, nil
}
```

Note: `git rev-parse --verify --quiet` exits non-zero for a missing ref, which `d.run` surfaces as an error; that drives the fallback chain. `origin/HEAD` resolves only when cached locally, matching the spec's no-network behavior.

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/git/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/git.go internal/git/git_test.go
git commit -m "feat(git): add worktree creation with base-ref fallback chain"
```

---

### Task 3: Git driver: RemoveWorktreeIfClean

**Files:**
- Modify: `internal/git/git.go`
- Test: `internal/git/git_test.go`

**Interfaces:**
- Consumes: `AddWorktree` from Task 2.
- Produces: `func (d *Driver) RemoveWorktreeIfClean(root, path, branch string) (removed bool, err error)`. `removed=false, err=nil` means the worktree held work and was kept.

- [ ] **Step 1: Write the failing tests**

```go
func TestRemoveWorktreeIfClean(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	path, branch, err := driver.AddWorktree(dir, "clean")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	removed, err := driver.RemoveWorktreeIfClean(dir, path, branch)
	if err != nil || !removed {
		t.Fatalf("clean worktree should remove: removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("worktree directory still on disk")
	}
	if _, err := driver.run(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		t.Fatal("branch should be deleted")
	}
}

func TestRemoveWorktreeKeepsDirtyAndAhead(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")

	dirtyPath, dirtyBranch, err := driver.AddWorktree(dir, "dirty")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	write(t, dirtyPath, "b.txt", "uncommitted")
	if removed, err := driver.RemoveWorktreeIfClean(dir, dirtyPath, dirtyBranch); err != nil || removed {
		t.Fatalf("dirty worktree must be kept: removed=%v err=%v", removed, err)
	}

	aheadPath, aheadBranch, err := driver.AddWorktree(dir, "ahead")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	write(t, aheadPath, "c.txt", "committed")
	commit(t, aheadPath, "work")
	if removed, err := driver.RemoveWorktreeIfClean(dir, aheadPath, aheadBranch); err != nil || removed {
		t.Fatalf("worktree with unmerged commits must be kept: removed=%v err=%v", removed, err)
	}
}

func TestRemoveWorktreeMissingDirKeeps(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "a.txt", "x")
	commit(t, dir, "seed")
	removed, err := driver.RemoveWorktreeIfClean(dir, filepath.Join(t.TempDir(), "gone"), "am/gone")
	if err != nil || removed {
		t.Fatalf("missing dir: removed=%v err=%v", removed, err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/git/ -run TestRemoveWorktree -v`
Expected: FAIL, compile error `RemoveWorktreeIfClean undefined`.

- [ ] **Step 3: Implement**

```go
// RemoveWorktreeIfClean removes a session's worktree and its am/ branch
// only when nothing would be lost: no uncommitted or untracked files, and
// no commits missing from the base branch. A kept worktree is not an error.
func (d *Driver) RemoveWorktreeIfClean(root, path, branch string) (bool, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	}
	porcelain, err := d.run(path, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if porcelain != "" {
		return false, nil
	}
	base := d.worktreeBase(root)
	if base == "" {
		return false, fmt.Errorf("no base ref in %s to compare %s against", root, branch)
	}
	ahead, err := d.run(path, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return false, err
	}
	if ahead != "0" {
		return false, nil
	}
	if _, err := d.run(root, "worktree", "remove", path); err != nil {
		return false, err
	}
	if _, err := d.run(root, "branch", "-d", branch); err != nil {
		return false, err
	}
	return true, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/git/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/git.go internal/git/git_test.go
git commit -m "feat(git): remove a session worktree only when clean"
```

---

### Task 4: Spawn path, settings default helper, and form toggle

**Files:**
- Modify: `internal/ui/form.go` (field consts ~line 20, `form` struct ~line 41, `openForm` ~line 153, `handleFormKey` ~line 212, `formFocus` ~line 307, `submitForm` ~line 330, `spawnSession` ~line 402)
- Modify: `internal/ui/keys.go` (~line 382, beside `quickCloseSetting`)
- Modify: `internal/ui/settings.go` (beside `defaultTool` ~line 11)
- Modify: `internal/ui/quick.go:260` (`quickSpawn`'s `spawnSession` call gets the new argument, `false` for now; Task 5 wires the real toggle)
- Modify: `internal/ui/modals.go` (`viewForm` ~line 60)
- Test: `internal/ui/form_test.go`

**Interfaces:**
- Consumes: `store.Session.WorktreeRepo/.WorktreeBranch` (Task 1), `m.gitDrv.RepoRoot`, `m.gitDrv.AddWorktree` (Task 2). `m.gitDrv` can be nil when git is not installed (see `New` in `model.go:380`).
- Produces:
  - `spawnSession(toolName, name, dir, group, prompt string, autoNamed, worktree bool) error` (signature change; both callers updated in this task)
  - `const worktreeSetting = "worktree_default"` in `keys.go`
  - `func (m *Model) defaultWorktree() bool` in `settings.go`
  - `fieldWorktree` form field between `fieldDir` and `fieldPrompt`, backed by `form.worktree bool`

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/form_test.go`. `buildModel` (helpers_test.go) skips without tmux; the git driver is real, so build a temp repo inline:

```go
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
		{"commit", "--allow-empty", "-m", "seed"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func TestFormWorktreeToggleSeedsFromSetting(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	if m.form.worktree {
		t.Fatal("worktree should default off with no setting")
	}
	m.mode = modeList
	if err := m.store.SetSetting(worktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.openForm()
	if !m.form.worktree {
		t.Fatal("worktree should seed on from setting")
	}
}

func TestSpawnWorktreeSessionCreatesWorktree(t *testing.T) {
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)

	if err := m.spawnSession("claude", "wt-feat", repo, "", "", false, true); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sessions, err := m.store.ListSessions(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	sess := sessions[0]
	wantCwd := filepath.Join(filepath.Dir(sess.WorktreeRepo), filepath.Base(sess.WorktreeRepo)+"-worktrees", "wt-feat")
	if sess.Cwd != wantCwd {
		t.Fatalf("cwd = %q, want %q", sess.Cwd, wantCwd)
	}
	if sess.WorktreeBranch != "am/wt-feat" {
		t.Fatalf("branch = %q", sess.WorktreeBranch)
	}
	if _, err := os.Stat(sess.Cwd); err != nil {
		t.Fatalf("worktree dir missing: %v", err)
	}
}

func TestSpawnWorktreeInNonRepoBlocks(t *testing.T) {
	m := buildModel(t)
	plain := t.TempDir()
	err := m.spawnSession("claude", "wt-fail", plain, "", "", false, true)
	if err == nil {
		t.Fatal("non-repo dir must block the spawn")
	}
	sessions, listErr := m.store.ListSessions(true)
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}
	if len(sessions) != 0 {
		t.Fatal("no session row should exist after a blocked spawn")
	}
}
```

Add `"os"`, `"os/exec"`, `"path/filepath"` to the test file imports if absent.

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run 'TestFormWorktree|TestSpawnWorktree' -v`
Expected: FAIL, compile errors (`worktreeSetting` undefined, wrong `spawnSession` arity).

- [ ] **Step 3: Implement**

`keys.go`, next to `quickCloseSetting`:

```go
const worktreeSetting = "worktree_default"
```

`settings.go`, after `defaultTool`:

```go
// defaultWorktree reports whether new sessions spawn into their own git
// worktree by default. Off unless the stored choice says "on"; a store
// error is surfaced but still yields off.
func (m *Model) defaultWorktree() bool {
	chosen, err := m.store.Setting(worktreeSetting)
	if err != nil {
		m.errBar.text = "reading worktree setting: " + err.Error()
		return false
	}
	return chosen == "on"
}
```

`form.go` field consts — insert between `fieldDir` and `fieldPrompt`:

```go
const (
	fieldName = iota
	fieldTool
	fieldDir
	fieldWorktree
	fieldPrompt
	fieldGroup
	fieldCount
)
```

`form` struct gains `worktree bool`. In `openForm`, after `m.form.dir.SetValue(...)`: `m.form.worktree = m.defaultWorktree()`.

`handleFormKey` left/right cases gain a worktree branch alongside the tool one:

```go
	case "left":
		if m.form.focus == fieldTool {
			m.cycleTool(-1)
			return m, nil
		}
		if m.form.focus == fieldWorktree {
			m.form.worktree = !m.form.worktree
			return m, nil
		}
	case "right":
		if m.form.focus == fieldTool {
			m.cycleTool(1)
			return m, nil
		}
		if m.form.focus == fieldWorktree {
			m.form.worktree = !m.form.worktree
			return m, nil
		}
```

`formFocus` needs no change (fieldWorktree has no textinput; the existing switch just focuses nothing for it, same as fieldTool/fieldGroup).

`submitForm` passes the toggle: `m.spawnSession(toolName, name, dir, group, prompt, autoNamed, m.form.worktree)`.

`quick.go:260` passes `false` for now: `m.spawnSession(toolName, name, dir, group, prompt, true, false)`.

`spawnSession` new signature and worktree resolution before `m.buildLaunch`:

```go
func (m *Model) spawnSession(toolName, name, dir, group, prompt string, autoNamed, worktree bool) error {
	tool := m.cfg.Tools[toolName]
	id := newID()
	worktreeRepo, worktreeBranch := "", ""
	if worktree {
		if m.gitDrv == nil {
			return errors.New("worktree sessions need git installed")
		}
		root, err := m.gitDrv.RepoRoot(dir)
		if err != nil {
			return err
		}
		path, branch, err := m.gitDrv.AddWorktree(root, name)
		if err != nil {
			return err
		}
		dir = path
		worktreeRepo, worktreeBranch = root, branch
	}
	...
```

and the `store.Session` literal gains `WorktreeRepo: worktreeRepo, WorktreeBranch: worktreeBranch`. Add `"errors"` to form.go imports. The tmux-create failure path already kills the tmux session; extend it to also roll back a just-created worktree:

```go
	if err := m.store.CreateSession(sess); err != nil {
		_ = m.tmux.Kill(id)
		_ = m.hooks.Remove(id)
		if worktreeRepo != "" {
			_, _ = m.gitDrv.RemoveWorktreeIfClean(worktreeRepo, dir, worktreeBranch)
		}
		return err
	}
```

Apply the same worktree rollback in the `m.tmux.Create` error return.

`modals.go` `viewForm`, after the dir field block:

```go
	worktreeVal := "off"
	if m.form.worktree {
		worktreeVal = "on"
	}
	b.WriteString(formField("worktree", "◂ "+worktreeVal+" ▸", m.form.focus == fieldWorktree))
```

Match the exact `formField` call style used by the tool field at `modals.go:68` (read it first; mirror its arrow/value rendering).

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run 'TestFormWorktree|TestSpawnWorktree' -v`
Expected: PASS.

- [ ] **Step 5: Run whole ui package**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/`
Expected: PASS (catches the quickSpawn arity change and any form-navigation regressions).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/form.go internal/ui/keys.go internal/ui/settings.go internal/ui/quick.go internal/ui/modals.go internal/ui/form_test.go
git commit -m "feat(ui): worktree toggle in new-session form and spawn path"
```

---

### Task 5: Quick bar toggle (alt+w) and chip

**Files:**
- Modify: `internal/ui/quick.go` (`openQuickMode` ~line 91, `handleQuickKey` ~line 133, `quickSpawn` ~line 244) and the `quickState` struct (in `model.go`; find with `grep -n "type quickState" internal/ui/*.go`)
- Modify: `internal/ui/view.go:384` (quick bar footer hints)
- Test: `internal/ui/quick_test.go`

**Interfaces:**
- Consumes: `spawnSession(..., worktree bool)` (Task 4), `m.defaultWorktree()` (Task 4).
- Produces: `quickState.worktree bool`; `alt+w` toggles it; footer hint shows `worktree: on|off`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/quick_test.go`, following its existing key-driving pattern (find how other tests send keys, e.g. the `alt+m` cycle test near line 471, and mirror it):

```go
func TestQuickWorktreeToggle(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	if m.quick.worktree {
		t.Fatal("worktree should default off")
	}
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}, Alt: true})
	if !m.quick.worktree {
		t.Fatal("alt+w should toggle worktree on")
	}
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}, Alt: true})
	if m.quick.worktree {
		t.Fatal("alt+w should toggle worktree back off")
	}
}

func TestQuickWorktreeSeedsFromSetting(t *testing.T) {
	m := buildModel(t)
	if err := m.store.SetSetting(worktreeSetting, "on"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	m.openQuickMode()
	if !m.quick.worktree {
		t.Fatal("quick bar should seed worktree on from setting")
	}
}

func TestQuickSpawnRespectsWorktreeToggleInNonRepo(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("grp", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	m.openQuickMode()
	m.quick.worktree = true
	m.quick.input.SetValue("do a thing")
	m.submitQuick()
	if m.errBar.text == "" {
		t.Fatal("worktree spawn into a non-repo group dir must surface an error")
	}
	sessions, err := m.store.ListSessions(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatal("blocked spawn must not create a session")
	}
}
```

If `selectGroupRow` does not exist in the test helpers, use the inline row-scan pattern from `TestNewSessionPreselectsContextGroup` (`for i, r := range m.rows { if r.isGroup && r.group == "grp" { m.cursor = i } }`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run TestQuickWorktree -v`
Expected: FAIL, compile error `m.quick.worktree undefined`.

- [ ] **Step 3: Implement**

`quickState` struct gains `worktree bool`. In `openQuickMode`, add `worktree: m.defaultWorktree()` to the `quickState` literal. In `handleQuickKey`, add a case (next to `"tab", "alt+m"`):

```go
	case "alt+w":
		m.quick.worktree = !m.quick.worktree
		return m, nil
```

`quickSpawn` passes the toggle instead of Task 4's `false`: `m.spawnSession(toolName, name, dir, group, prompt, true, m.quick.worktree)`.

`view.go:384` footer gains the state chip:

```go
	worktreeHint := "off"
	if m.quick.worktree {
		worktreeHint = "on"
	}
```

and append `{"⌥w", "worktree: " + worktreeHint}` to the hint pairs on that line, after the tool entry.

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run TestQuick -v`
Expected: PASS, existing quick tests included.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/quick.go internal/ui/view.go internal/ui/quick_test.go internal/ui/model.go
git commit -m "feat(ui): alt+w worktree toggle in quick prompt bar"
```

---

### Task 6: Settings entry

**Files:**
- Modify: `internal/ui/model.go` (`settingsState` ~line 260, field consts ~line 271)
- Modify: `internal/ui/settings.go` (`openSettings` ~line 69, save block in `handleSettingsKey` ~line 99, `cycleSetting` ~line 143)
- Modify: `internal/ui/modals.go` (`viewSettings` ~line 159)
- Test: `internal/ui/settings_test.go`

**Interfaces:**
- Consumes: `worktreeSetting`, `m.defaultWorktree()` (Task 4).
- Produces: settings row "worktree sessions" persisting `"on"`/`"off"` under `worktree_default`.

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/settings_test.go`, mirroring its existing open-cycle-save pattern:

```go
func TestSettingsWorktreeDefaultPersists(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	for m.settings.field != settingsFieldWorktree {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if chosen, err := m.store.Setting(worktreeSetting); err != nil || chosen != "on" {
		t.Fatalf("want stored on, got %q err %v", chosen, err)
	}
	if !m.defaultWorktree() {
		t.Fatal("defaultWorktree should now report on")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run TestSettingsWorktree -v`
Expected: FAIL, compile error `settingsFieldWorktree undefined`.

- [ ] **Step 3: Implement**

`model.go`: `settingsState` gains `worktreeDefault bool`; consts gain `settingsFieldWorktree` before `settingsFieldCount`.

`settings.go` `openSettings` literal gains `worktreeDefault: m.defaultWorktree()`. `cycleSetting` gains:

```go
	case settingsFieldWorktree:
		m.settings.worktreeDefault = !m.settings.worktreeDefault
```

Save block gains:

```go
		worktreeChoice := "off"
		if m.settings.worktreeDefault {
			worktreeChoice = "on"
		}
		if err := m.store.SetSetting(worktreeSetting, worktreeChoice); err != nil {
			m.errBar.text = err.Error()
		}
```

`modals.go` `viewSettings` gains a row after the quick-close row:

```go
	worktreeDefault := "off"
	if m.settings.worktreeDefault {
		worktreeDefault = "on"
	}
```

and in `body`: `row(settingsFieldWorktree, "worktree sessions", worktreeDefault) + "\n" +` placed to match the const order.

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run TestSettings -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/model.go internal/ui/settings.go internal/ui/modals.go internal/ui/settings_test.go
git commit -m "feat(ui): worktree default in settings"
```

---

### Task 7: Delete-time cleanup

**Files:**
- Modify: `internal/ui/lifecycle.go` (`actionDelete` branch in `handleConfirmKey` ~line 528)
- Test: `internal/ui/lifecycle_test.go`

**Interfaces:**
- Consumes: `sess.WorktreeRepo/.WorktreeBranch` (Task 1), `m.gitDrv.RemoveWorktreeIfClean` (Task 3).
- Produces: deleting a worktree session removes its worktree when clean; a dirty worktree survives with an error-bar note naming the kept path. Cleanup failures never block the record delete.

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/lifecycle_test.go` (reuse `initGitRepo` from Task 4's form_test.go):

```go
func deleteSession(t *testing.T, m *Model, name string) {
	t.Helper()
	m.selectSessionRow(t, name)
	m.prepareDelete()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
}

func TestDeleteRemovesCleanWorktree(t *testing.T) {
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	if err := m.spawnSession("claude", "wt-clean", repo, "", "", false, true); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sessions, _ := m.store.ListSessions(true)
	worktreePath := sessions[0].Cwd
	m.applyCmd(t, m.refreshCmd())

	deleteSession(t, m, "wt-clean")
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatal("clean worktree should be removed on delete")
	}
}

func TestDeleteKeepsDirtyWorktree(t *testing.T) {
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	if err := m.spawnSession("claude", "wt-dirty", repo, "", "", false, true); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sessions, _ := m.store.ListSessions(true)
	worktreePath := sessions[0].Cwd
	if err := os.WriteFile(filepath.Join(worktreePath, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())

	deleteSession(t, m, "wt-dirty")
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatal("dirty worktree must survive delete")
	}
	if !strings.Contains(m.errBar.text, worktreePath) {
		t.Fatalf("error bar should name the kept path, got %q", m.errBar.text)
	}
	if remaining, _ := m.store.ListSessions(true); len(remaining) != 0 {
		t.Fatal("session record should still be deleted")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run 'TestDeleteRemovesClean|TestDeleteKeepsDirty' -v`
Expected: FAIL (worktree survives / no error-bar note), not a compile error.

- [ ] **Step 3: Implement**

In `handleConfirmKey`'s `actionDelete` loop, after `m.store.Delete(sess.ID)` succeeds:

```go
				if sess.WorktreeRepo != "" && m.gitDrv != nil {
					removed, err := m.gitDrv.RemoveWorktreeIfClean(sess.WorktreeRepo, sess.Cwd, sess.WorktreeBranch)
					if err != nil {
						m.errBar.text = "worktree cleanup: " + err.Error()
					} else if !removed {
						m.errBar.text = "worktree kept (has work): " + sess.Cwd
					}
				}
```

Note the loop's earlier steps `return` on error; this block deliberately only writes the error bar, because the spec keeps record deletion authoritative.

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run TestDelete -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/lifecycle.go internal/ui/lifecycle_test.go
git commit -m "feat(ui): remove a clean worktree when its session is deleted"
```

---

### Task 8: Docs and full verification

**Files:**
- Modify: `README.md` ("Not here yet" line; keys table if it lists form fields)
- Modify: `docs/usage.md` (keys reference: form worktree field, quick bar `alt+w`, settings row, delete behavior)
- No new code.

**Interfaces:** none.

- [ ] **Step 1: Update README**

In the "Not here yet:" sentence, remove "worktree creation, " and keep the rest verbatim. Add one sentence to the usage section near the `n` key description: sessions can spawn into their own git worktree (`<repo>-worktrees/<name>`, branch `am/<name>`), toggled in the form, with `alt+w` in the quick prompt, or by default in settings.

- [ ] **Step 2: Update docs/usage.md**

Read the file's structure first, then document: the form's worktree field, `alt+w` in the quick-prompt key list, the settings "worktree sessions" row, the base-ref rule (remote default branch, falling back to the local default branch then HEAD), and delete-time cleanup (clean worktrees removed, dirty kept with the path shown).

- [ ] **Step 3: Full gate**

Run: `go vet ./... && go build ./... && env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./...`
Expected: all PASS.

- [ ] **Step 4: Commit**

```bash
git add README.md docs/usage.md
git commit -m "docs: worktree-per-session usage"
```
