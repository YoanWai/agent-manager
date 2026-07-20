# Review Repo Target Implementation Plan (Phase 2a)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the agent declare which repo its session is working in, so review opens there instead of making the human cycle twenty repos, and reach review with the same `Ctrl+R` from the list as from inside a session.

**Architecture:** The agent runs `agent-manager review-repo <path>`, which writes a mailbox file the poller applies to a `review_targets` row, exactly as `agent-manager rename` already works. The diff loader prefers that declared repo over its dirty-first ranking. The `r` key becomes a type-to-filter repo picker instead of a blind cycle.

**Tech Stack:** Go, `charmbracelet/bubbletea` + `lipgloss` for the TUI, SQLite via `modernc.org/sqlite`, the `git` CLI shelled out from `internal/git`.

## Global Constraints

- Work in the worktree `/tmp/am-p2` on branch `feat/review-target`. Several agent sessions share the main checkout; never work there.
- Stage explicit paths. Never `git add -A`.
- Zero code comments unless a non-obvious WHY needs recording. One short line max. Never describe WHAT the code does.
- No AI attribution in commit messages.
- `gofmt -l internal/ .` clean, `go vet ./...` clean, `go test ./...` green before each commit.
- Fail loudly. No silent fallback that masks a real problem.
- No speculative code for cases that cannot occur.
- Spec: `docs/superpowers/specs/2026-07-20-review-target-and-noise-design.md`

Phase 2b (the base branch: `review-base`, a `review_bases` table, the `b` branch picker) is a separate plan. Do not build it here.

---

### Task 1: Store the declared repo

A dedicated table keeps the change off the three `sessions` SELECT sites and their scan lists.

**Files:**
- Modify: `internal/store/store.go` (the `migrations` slice around line 84; add accessors near the other session accessors)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces, all on `*Store`:
  - `SetReviewRepo(sessionID, repoRoot string) error` — upserts; an empty `repoRoot` clears the row.
  - `ReviewRepo(sessionID string) (string, error)` — returns `""` when nothing is declared. A missing row is not an error.

- [ ] **Step 1: Write the failing test**

Add to `internal/store/store_test.go`:

```go
func TestReviewRepoRoundTrip(t *testing.T) {
	st := openTestStore(t)
	if got, err := st.ReviewRepo("s1"); err != nil || got != "" {
		t.Fatalf("unset review repo = %q, %v; want empty, nil", got, err)
	}
	if err := st.SetReviewRepo("s1", "/repos/alpha"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.ReviewRepo("s1"); err != nil || got != "/repos/alpha" {
		t.Fatalf("review repo = %q, %v; want /repos/alpha", got, err)
	}
	if err := st.SetReviewRepo("s1", "/repos/bravo"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ReviewRepo("s1"); got != "/repos/bravo" {
		t.Fatalf("review repo after update = %q, want /repos/bravo", got)
	}
	if err := st.SetReviewRepo("s1", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.ReviewRepo("s1"); got != "" {
		t.Fatalf("review repo after clear = %q, want empty", got)
	}
}
```

If `store_test.go` has no `openTestStore` helper, use whatever helper the existing tests in that file use to build a `*Store` over a temp database, and adapt the first line to match.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /tmp/am-p2 && go test ./internal/store/ -run TestReviewRepoRoundTrip -v`
Expected: FAIL to compile with `st.ReviewRepo undefined`

- [ ] **Step 3: Write minimal implementation**

Append to the `migrations` slice in `internal/store/store.go`:

```go
		`CREATE TABLE IF NOT EXISTS review_targets (
			session_id TEXT PRIMARY KEY,
			repo_root  TEXT NOT NULL
		)`,
```

Add the accessors:

```go
func (s *Store) SetReviewRepo(sessionID, repoRoot string) error {
	if repoRoot == "" {
		_, err := s.db.Exec(`DELETE FROM review_targets WHERE session_id = ?`, sessionID)
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO review_targets (session_id, repo_root) VALUES (?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET repo_root = excluded.repo_root`,
		sessionID, repoRoot,
	)
	return err
}

func (s *Store) ReviewRepo(sessionID string) (string, error) {
	var root string
	err := s.db.QueryRow(`SELECT repo_root FROM review_targets WHERE session_id = ?`, sessionID).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return root, nil
}
```

Ensure `errors` and `database/sql` are imported in `store.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /tmp/am-p2 && go test ./internal/store/ -v`
Expected: PASS, all existing store tests included

- [ ] **Step 5: Commit**

```bash
cd /tmp/am-p2
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat: store the repo a session declares for review"
```

---

### Task 2: Mailbox file for the declared repo

`internal/hooks` owns the files an agent writes for the manager to pick up. `NameFile`/`ReadName`/`RemoveName` (around line 135) is the pattern to mirror.

**Files:**
- Modify: `internal/hooks/hooks.go` (next to `NameFile`, `ReadName`, `RemoveName`)
- Test: `internal/hooks/hooks_test.go`

**Interfaces:**
- Consumes: `Manager.dir`, `removeIfExists(path string) error` — both already in `hooks.go`.
- Produces, all on `*Manager`:
  - `ReviewRepoFile(id string) string` — path `<dir>/<id>.reviewrepo`
  - `ReadReviewRepo(id string) (root string, found bool)` — trims surrounding whitespace; `found` reports the file existed
  - `RemoveReviewRepo(id string) error`

- [ ] **Step 1: Write the failing test**

Add to `internal/hooks/hooks_test.go`:

```go
func TestReviewRepoMailbox(t *testing.T) {
	manager := NewManager(t.TempDir())
	if _, found := manager.ReadReviewRepo("abc"); found {
		t.Fatal("no mailbox should exist yet")
	}
	path := manager.ReviewRepoFile("abc")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("  /repos/alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, found := manager.ReadReviewRepo("abc")
	if !found || root != "/repos/alpha" {
		t.Fatalf("read = %q, %v; want /repos/alpha, true", root, found)
	}
	if err := manager.RemoveReviewRepo("abc"); err != nil {
		t.Fatal(err)
	}
	if _, found := manager.ReadReviewRepo("abc"); found {
		t.Fatal("mailbox should be gone after removal")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /tmp/am-p2 && go test ./internal/hooks/ -run TestReviewRepoMailbox -v`
Expected: FAIL to compile with `manager.ReadReviewRepo undefined`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/hooks/hooks.go`:

```go
// ReviewRepoFile is the mailbox the review-repo subcommand writes the repo
// a session is working in into; the poller applies and deletes it.
func (m *Manager) ReviewRepoFile(id string) string {
	return filepath.Join(m.dir, id+".reviewrepo")
}

func (m *Manager) ReadReviewRepo(id string) (root string, found bool) {
	raw, err := os.ReadFile(m.ReviewRepoFile(id))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(raw)), true
}

func (m *Manager) RemoveReviewRepo(id string) error {
	return removeIfExists(m.ReviewRepoFile(id))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /tmp/am-p2 && go test ./internal/hooks/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /tmp/am-p2
git add internal/hooks/hooks.go internal/hooks/hooks_test.go
git commit -m "feat: mailbox for a session's declared review repo"
```

---

### Task 3: The `review-repo` subcommand

`main.go` dispatches subcommands before starting the TUI (see the `rename` branch at line 26 and `runRename` at line 52). This mirrors it, and validates before writing so a bad path fails at the agent's prompt rather than silently breaking review later.

**Files:**
- Modify: `main.go` (the dispatch in `main`, and a new `runReviewRepo` beside `runRename`)
- Test: `main_test.go`

**Interfaces:**
- Consumes: `hooks.EnvSessionID`, `sessionIDPattern`, `hooks.NewManager(configDir).ReviewRepoFile(id)`, `git.New()` and `(*git.Driver).ResolveRepos(dir string) ([]string, error)` — all existing.
- Produces: `runReviewRepo(args []string, sessionID, configDir string) error`, matching `runRename`'s shape so the tests can call it directly.

- [ ] **Step 1: Write the failing test**

Add to `main_test.go`:

```go
func TestRunReviewRepoWritesMailbox(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	configDir := t.TempDir()
	if err := runReviewRepo([]string{repo}, "abc123", configDir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(hooks.NewManager(configDir).ReviewRepoFile("abc123"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) == "" {
		t.Fatal("mailbox should hold the resolved repo root")
	}
}

func TestRunReviewRepoRejectsBadInput(t *testing.T) {
	configDir := t.TempDir()
	if err := runReviewRepo([]string{t.TempDir()}, "", configDir); err == nil {
		t.Error("missing session id should fail")
	}
	if err := runReviewRepo([]string{t.TempDir()}, "abc123", configDir); err == nil {
		t.Error("a path that is not a repo should fail")
	}
	if err := runReviewRepo(nil, "abc123", configDir); err == nil {
		t.Error("a missing path argument should fail")
	}
}
```

Ensure `os`, `strings`, `os/exec`, and the `hooks` package are imported in `main_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /tmp/am-p2 && go test . -run TestRunReviewRepo -v`
Expected: FAIL to compile with `undefined: runReviewRepo`

- [ ] **Step 3: Write minimal implementation**

In `main.go`, add the dispatch beside the `rename` branch:

```go
	if len(os.Args) > 1 && os.Args[1] == "review-repo" {
		dir, err := config.Dir()
		if err == nil {
			err = runReviewRepo(os.Args[2:], os.Getenv(hooks.EnvSessionID), dir)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "agent-manager:", err)
			os.Exit(1)
		}
		return
	}
```

Add beside `runRename`:

```go
// runReviewRepo records the repo a session is working in, so review opens
// there instead of guessing from the working directory.
func runReviewRepo(args []string, sessionID, configDir string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf(`usage: agent-manager review-repo <path>`)
	}
	if sessionID == "" {
		return fmt.Errorf("not inside an agent-manager session (%s is unset)", hooks.EnvSessionID)
	}
	if !sessionIDPattern.MatchString(sessionID) {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	driver, err := git.New()
	if err != nil {
		return err
	}
	roots, err := driver.ResolveRepos(strings.TrimSpace(args[0]))
	if err != nil {
		return fmt.Errorf("%s is not a git repository: %w", args[0], err)
	}
	root := roots[0]
	path := hooks.NewManager(configDir).ReviewRepoFile(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(root), 0o644); err != nil {
		return err
	}
	fmt.Println("review repo set to", root)
	return nil
}
```

Add `"github.com/YoanWai/agent-manager/internal/git"` to `main.go`'s imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /tmp/am-p2 && go test . -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /tmp/am-p2
git add main.go main_test.go
git commit -m "feat: agent-manager review-repo declares a session's repo"
```

---

### Task 4: Poller applies the declared repo

`applyPendingRename` (`internal/ui/poller.go:371`) is the pattern: read the mailbox, write the store, delete the file.

**Files:**
- Modify: `internal/ui/poller.go` (call site near line 217, new function beside `applyPendingRename`)
- Test: `internal/ui/poller_test.go`

**Interfaces:**
- Consumes: `p.hooks.ReadReviewRepo` / `RemoveReviewRepo` (Task 2), `p.store.SetReviewRepo` (Task 1).
- Produces: `func (p *poller) applyPendingReviewRepo(sess *store.Session) error`.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/poller_test.go`, adapting the poller construction to match whatever the existing tests in that file do:

```go
func TestPollerAppliesPendingReviewRepo(t *testing.T) {
	p, sess := newTestPollerWithSession(t)
	path := p.hooks.ReviewRepoFile(sess.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("/repos/alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.applyPendingReviewRepo(&sess); err != nil {
		t.Fatal(err)
	}
	got, err := p.store.ReviewRepo(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/repos/alpha" {
		t.Fatalf("stored review repo = %q, want /repos/alpha", got)
	}
	if _, found := p.hooks.ReadReviewRepo(sess.ID); found {
		t.Fatal("mailbox should be consumed")
	}
}
```

If `internal/ui/poller_test.go` has no helper that builds a poller with a session, write `newTestPollerWithSession` in that file using the same construction the existing rename test uses; if no such test exists, build the poller the same way `poller.go`'s constructor is called from `model.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /tmp/am-p2 && go test ./internal/ui/ -run TestPollerAppliesPendingReviewRepo -v`
Expected: FAIL to compile with `p.applyPendingReviewRepo undefined`

- [ ] **Step 3: Write minimal implementation**

Add beside `applyPendingRename` in `internal/ui/poller.go`:

```go
func (p *poller) applyPendingReviewRepo(sess *store.Session) error {
	root, found := p.hooks.ReadReviewRepo(sess.ID)
	if !found {
		return nil
	}
	if root != "" {
		if err := p.store.SetReviewRepo(sess.ID, root); err != nil {
			return err
		}
	}
	return p.hooks.RemoveReviewRepo(sess.ID)
}
```

Call it next to the existing rename call around line 217:

```go
		if err := p.applyPendingReviewRepo(&sessions[i]); err != nil {
			return err
		}
```

Match the surrounding error handling exactly — read the lines around 217 and follow whatever that loop does with an error from `applyPendingRename`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /tmp/am-p2 && go test ./internal/ui/ -v -run 'Poller|Rename'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /tmp/am-p2
git add internal/ui/poller.go internal/ui/poller_test.go
git commit -m "feat: poller applies a session's declared review repo"
```

---

### Task 5: Review opens on the declared repo

`diffLoadCmd` (`internal/ui/diffview.go`) resolves repos via `driver.ResolveRepos(sess.Cwd)` and picks by matching `repoWant` against the returned roots, falling back to index 0 (the dirty-first ranking). The declared repo becomes the preferred `repoWant`.

Resolution order: the human's explicit pick this session (`repoSel`) wins, then the declared repo, then the ranking. A human who picks a repo should not have it yanked away by the agent on the next poll.

**Files:**
- Modify: `internal/ui/diffview.go` (`retargetDiff`, which resets `repoSel` when review retargets to a session)
- Test: `internal/ui/review_regression_test.go`

**Interfaces:**
- Consumes: `m.store.ReviewRepo(sessionID string) (string, error)` (Task 1); `m.diff.repoSel string` and `diffLoadCmd(sess store.Session, scope git.Scope, gen int, repoWant string, refresh bool) tea.Cmd` — both existing.
- Produces: no signature change. `retargetDiff` seeds `m.diff.repoSel` from the store instead of clearing it to `""`.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/review_regression_test.go`:

```go
func TestReviewOpensOnDeclaredRepo(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	createSession(t, m, "declared", umbrella, "")
	m.selectSessionRow(t, "declared")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no selected session")
	}
	// alpha is the clean repo, so the ranking would never choose it.
	if err := m.store.SetReviewRepo(sess.ID, filepath.Join(umbrella, "alpha")); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("review should open on the declared repo, got %q (ranking prefers %q)", got, dirtyName)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /tmp/am-p2 && go test ./internal/ui/ -run TestReviewOpensOnDeclaredRepo -v`
Expected: FAIL with `review should open on the declared repo, got "bravo"`

- [ ] **Step 3: Write minimal implementation**

In `retargetDiff`, replace the line that clears the repo selection (`m.diff.repoSel = ""`):

```go
	m.diff.repoSel = ""
	if declared, err := m.store.ReviewRepo(sess.ID); err != nil {
		m.err = err.Error()
	} else {
		m.diff.repoSel = declared
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /tmp/am-p2 && go test ./internal/ui/ -v`
Expected: PASS, including the existing `TestReviewCyclesReposUnderUmbrella` and `TestRepoSelectionSurvivesReload`

- [ ] **Step 5: Commit**

```bash
cd /tmp/am-p2
git add internal/ui/diffview.go internal/ui/review_regression_test.go
git commit -m "feat: review opens on the repo the agent declared"
```

---

### Task 6: Replace the repo cycle with a filtered picker

`r` currently calls `cycleDiffRepo`, stepping one repo per press. With twenty repos that is unusable. Replace it with a modal listing the resolved repos, narrowed as you type.

Follow the existing modal shape: a `mode` constant, an `open` function that sets state, a `handleXKey` that owns the keymap, and a render function called from the view. `modeMove` (`internal/ui/keys.go:954`) is the closest template.

**Files:**
- Modify: `internal/ui/model.go` (a `modeRepoPick` constant beside `modeMove`; dispatch it in `handleKey`)
- Create: `internal/ui/repopicker.go` (state, open, key handling, render)
- Modify: `internal/ui/diffview.go` (delete `cycleDiffRepo`; point `r` at the picker; footer hint)
- Test: `internal/ui/review_regression_test.go`

**Interfaces:**
- Consumes: `m.diff.repoRoots []string`, `m.diff.repoSel string`, `m.diff.repoIdx int`, `m.diffSession() (store.Session, bool)`, `m.diffLoadCmd(sess, scope, gen, repoWant, refresh)` — all existing in `diffview.go`.
- Produces:
  - `mode` constant `modeRepoPick`
  - `func (m *Model) openRepoPick()` — no-op unless `len(m.diff.repoRoots) > 1`
  - `func (m *Model) handleRepoPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd)` — `esc` cancels, `up`/`down` move, printable runes and `backspace` edit the filter, `enter` selects
  - `func (m *Model) viewRepoPick() string`
  - `func (m *Model) filteredRepoRoots() []string` — case-insensitive substring match on the full root path; the unfiltered list when the filter is empty

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/review_regression_test.go`:

```go
func TestRepoPickerFiltersAndSelects(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
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
	if got := m.filteredRepoRoots(); len(got) != 1 || filepath.Base(got[0]) != "alpha" {
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
	if m.gitDrv == nil {
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /tmp/am-p2 && go test ./internal/ui/ -run TestRepoPicker -v`
Expected: FAIL to compile with `undefined: modeRepoPick`

- [ ] **Step 3: Write the implementation**

Add the mode constant in `internal/ui/model.go` beside `modeMove`:

```go
	modeRepoPick
```

Dispatch it in `handleKey`'s mode switch, beside the `modeMove` case:

```go
	case modeRepoPick:
		return m.handleRepoPickKey(msg)
```

Create `internal/ui/repopicker.go`:

```go
package ui

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type repoPickState struct {
	filter string
	cursor int
}

func (m *Model) openRepoPick() {
	if len(m.diff.repoRoots) < 2 {
		return
	}
	m.repoPick = repoPickState{}
	for i, root := range m.diff.repoRoots {
		if root == m.diff.repoSel {
			m.repoPick.cursor = i
			break
		}
	}
	m.mode = modeRepoPick
	m.err = ""
}

func (m *Model) filteredRepoRoots() []string {
	if m.repoPick.filter == "" {
		return m.diff.repoRoots
	}
	needle := strings.ToLower(m.repoPick.filter)
	var out []string
	for _, root := range m.diff.repoRoots {
		if strings.Contains(strings.ToLower(root), needle) {
			out = append(out, root)
		}
	}
	return out
}

func (m *Model) handleRepoPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.filteredRepoRoots()
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = modeDiff
		return m, nil
	case tea.KeyUp:
		m.moveRepoPickCursor(-1, len(rows))
		return m, nil
	case tea.KeyDown:
		m.moveRepoPickCursor(1, len(rows))
		return m, nil
	case tea.KeyBackspace:
		if m.repoPick.filter != "" {
			m.repoPick.filter = m.repoPick.filter[:len(m.repoPick.filter)-1]
			m.repoPick.cursor = 0
		}
		return m, nil
	case tea.KeyEnter:
		if len(rows) == 0 {
			return m, nil
		}
		m.mode = modeDiff
		return m, m.selectRepo(rows[min(m.repoPick.cursor, len(rows)-1)])
	case tea.KeyRunes:
		m.repoPick.filter += string(msg.Runes)
		m.repoPick.cursor = 0
		return m, nil
	}
	return m, nil
}

func (m *Model) moveRepoPickCursor(delta, count int) {
	if count == 0 {
		m.repoPick.cursor = 0
		return
	}
	m.repoPick.cursor = (m.repoPick.cursor + delta + count) % count
}

func (m *Model) selectRepo(root string) tea.Cmd {
	sess, ok := m.diffSession()
	if !ok {
		return nil
	}
	m.diff.repoSel = root
	m.diff.gen++
	m.diff.loading = true
	m.diff.errText = ""
	m.diff.fileIdx = 0
	m.diff.scroll = 0
	m.diff.cursorLine = 0
	return m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false)
}

func (m *Model) viewRepoPick() string {
	rows := m.filteredRepoRoots()
	var body strings.Builder
	body.WriteString(titleStyle.Render("Review repo") + "\n")
	body.WriteString(subtleStyle.Render("type to filter · ↑↓ move · enter select · esc cancel") + "\n\n")
	body.WriteString(mutedStyle.Render("filter: ") + m.repoPick.filter + "\n\n")
	if len(rows) == 0 {
		body.WriteString(subtleStyle.Render("no repo matches"))
	}
	for i, root := range rows {
		marker := "  "
		line := filepath.Base(root) + subtleStyle.Render("  "+filepath.Dir(root))
		if i == min(m.repoPick.cursor, len(rows)-1) {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("▸ ")
			line = lipgloss.NewStyle().Foreground(colorAccent).Render(filepath.Base(root)) +
				subtleStyle.Render("  "+filepath.Dir(root))
		}
		body.WriteString(marker + line + "\n")
	}
	return body.String()
}
```

If `titleStyle` does not exist in the `ui` package, use whichever style the other modals use for their heading — read `internal/ui/modals.go` and match it. Same for `min`: if the package targets a Go version without the builtin, add a small local helper rather than importing anything.

Add the state field to the `Model` struct in `internal/ui/model.go`, beside `moveID`:

```go
	repoPick repoPickState
```

Render it wherever the other modals are rendered — find where `modeMove` picks its view in the top-level `View` and add the `modeRepoPick` case alongside, calling `m.viewRepoPick()`.

In `internal/ui/diffview.go`, delete `cycleDiffRepo` entirely and change the `r` case in `handleDiffKey`:

```go
	case "r":
		m.openRepoPick()
```

Update the footer hint in `viewDiffFooter` so `r` reads as a picker rather than a cycle:

```go
	if len(m.diff.repoRoots) > 1 {
		pairs = append(pairs, [2]string{"r", "repo: " + filepath.Base(m.diff.repoRoots[m.diff.repoIdx])})
	}
```

Leave that line as-is if it already reads this way; the label is still accurate for a picker.

Update the help modal line in `internal/ui/modals.go` that currently describes `r` as cycling repos, so it describes picking one.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /tmp/am-p2 && go test ./internal/ui/ -v`
Expected: PASS. `TestReviewCyclesReposUnderUmbrella` tests the deleted cycle behaviour — rewrite it to drive the picker instead, keeping its intent (selecting a different repo works, and the header follows), or delete it if `TestRepoPickerFiltersAndSelects` fully covers it. Say which you did in your report.

- [ ] **Step 5: Commit**

```bash
cd /tmp/am-p2
git add internal/ui/repopicker.go internal/ui/model.go internal/ui/diffview.go internal/ui/modals.go internal/ui/review_regression_test.go
git commit -m "feat: pick the review repo from a filtered list"
```

---

### Task 7: Ctrl+R opens review from the list

Inside a session, `Ctrl+R` opens review through a tmux binding that sets a marker and detaches (`internal/tmux/tmux.go:127`). From the session list the key does nothing; review is on `D`/`x`. Bind it so one key means the same thing in both places.

The tmux root binding passes `C-r` through to the pane for any session not named `am_*`, so the key reaches the manager's own UI.

**Files:**
- Modify: `internal/ui/keys.go` (the list-mode key switch in `handleKey`, around line 92)
- Modify: `internal/ui/modals.go` (help text)
- Test: `internal/ui/review_regression_test.go`

**Interfaces:**
- Consumes: `m.openDiff() tea.Cmd` — existing.
- Produces: no new functions.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/review_regression_test.go`:

```go
func TestCtrlRFromListOpensReview(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "ctrlr", gitRepoWithTwoChangedFiles(t), "")
	m.selectSessionRow(t, "ctrlr")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.mode != modeDiff {
		t.Fatalf("ctrl+r from the list should open review, mode = %v (err=%q)", m.mode, m.err)
	}
	if m.diff.reattachID != "" {
		t.Fatal("review opened from the list should return to the list, not re-attach")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /tmp/am-p2 && go test ./internal/ui/ -run TestCtrlRFromListOpensReview -v`
Expected: FAIL with `ctrl+r from the list should open review, mode = 0`

- [ ] **Step 3: Write minimal implementation**

In `internal/ui/keys.go`, add to the list-mode switch beside the `"D", "x"` case:

```go
	case "ctrl+r":
		return m, m.openDiff()
```

In `internal/ui/modals.go`, update the review help line so it names both keys:

```go
		{"D / x / ctrl+r", "review changes: whole-file diffs, comment lines, send to agent"},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /tmp/am-p2 && go test ./internal/ui/ -v`
Expected: PASS

- [ ] **Step 5: Verify the whole suite and formatting**

Run:
```bash
cd /tmp/am-p2
gofmt -l internal/ .
go vet ./...
go test ./...
```
Expected: `gofmt` prints nothing, `go vet` prints nothing, every package reports `ok`

- [ ] **Step 6: Commit**

```bash
cd /tmp/am-p2
git add internal/ui/keys.go internal/ui/modals.go internal/ui/review_regression_test.go
git commit -m "feat: ctrl+r opens review from the session list"
```

---

### Task 8: Document the subcommand and open the PR

An agent only uses `review-repo` if something tells it to. The README documents the CLI.

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: the `review-repo` subcommand from Task 3.
- Produces: a pull request against `main`.

- [ ] **Step 1: Document the subcommand**

Find where `README.md` documents `agent-manager rename` and add `review-repo` beside it, in the same voice and format. State that the agent runs it inside its own session, that it takes a path to a git repo, and that review then opens on that repo instead of guessing. Keep it to the same length as the `rename` entry. If the README does not document `rename`, add a short "Commands agents can run" section covering both.

- [ ] **Step 2: Verify the docs match the code**

Run: `cd /tmp/am-p2 && go run . review-repo 2>&1 | head -3`
Expected: the usage line from Task 3, matching what the README says the command takes

- [ ] **Step 3: Commit and push**

```bash
cd /tmp/am-p2
git add README.md
git commit -m "docs: document the review-repo subcommand"
git push -u origin feat/review-target
```

- [ ] **Step 4: Open the pull request**

```bash
cd /tmp/am-p2
gh pr create --title "feat: let the agent declare the repo under review" --body "$(cat <<'BODY'
## Problem

A session's working directory is often an umbrella folder holding many repos; `mreshet` holds twenty. Review ranked them and opened on the most-active one, with `r` cycling to the next. Cycling twenty repos to reach the one the agent touched is backwards, since the agent already knows which repo it is working in.

Separately, `Ctrl+R` opened review from inside a session but did nothing from the session list, where review was on `D`/`x`.

## Change

- `agent-manager review-repo <path>` lets a session's agent declare the repo it is working in. It follows the same mailbox pattern as `agent-manager rename`: the command validates the path is a git repo and writes a file, the poller applies it to a `review_targets` row and deletes it.
- Review opens on the declared repo. A repo the human picked during the session still wins, so the agent cannot yank the view away mid-review; with nothing declared, the existing dirty-first ranking still applies.
- `r` now opens a filtered repo picker instead of cycling one repo per press.
- `Ctrl+R` opens review from the list too, matching the in-session binding. `D` and `x` keep working.

## Tests

- The store round-trips and clears a declared repo.
- The mailbox is written, read, and consumed.
- `review-repo` rejects a missing session id, a path that is not a repo, and a missing argument.
- The poller applies a pending declaration and deletes the mailbox.
- Review opens on the declared repo even when the ranking prefers another.
- The picker filters, selects, and leaves the repo unchanged on escape.
- `Ctrl+R` from the list opens review without re-attaching.

Full suite, `go vet`, `gofmt` clean.
BODY
)"
```
Expected: the command prints the new pull request URL

---

## Self-Review

**Spec coverage.** The spec's Phase 2 covers the agent-declared repo (Tasks 1-5), the repo picker (Task 6), and `Ctrl+R` from the list (Task 7). The spec's `review-base` subcommand, `review_bases` table, `BaseRef` override, and `b` branch picker are deliberately deferred to the Phase 2b plan, as stated in the Global Constraints.

**Placeholders.** Every code step carries the code to write. Three steps say to match an existing pattern rather than quoting it — the poller's error handling at the Task 4 call site, the modal heading style in Task 6, and the README's `rename` entry in Task 8. Each names the exact file and symbol to read, because copying a stale snapshot of surrounding code into the plan would be worse than pointing at the real thing.

**Type consistency.** `SetReviewRepo`/`ReviewRepo` are defined in Task 1 and consumed in Tasks 4 and 5. `ReviewRepoFile`/`ReadReviewRepo`/`RemoveReviewRepo` are defined in Task 2 and consumed in Tasks 3 and 4. `runReviewRepo(args []string, sessionID, configDir string) error` matches `runRename`'s shape. `repoPickState`, `openRepoPick`, `filteredRepoRoots`, `handleRepoPickKey`, `selectRepo`, and `viewRepoPick` are all defined in Task 6 and used only there and in its tests. `diffLoadCmd`'s existing five-argument signature is used unchanged in Task 6.

One conflict found and resolved: Task 6 deletes `cycleDiffRepo`, which `TestReviewCyclesReposUnderUmbrella` (added in the umbrella-repo PR) exercises. Task 6 Step 4 now names that test and requires the implementer to rewrite or delete it, and to say which.
