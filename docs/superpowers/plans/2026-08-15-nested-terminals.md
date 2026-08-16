# Nested Terminals Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A shell can hang under an agent in one inline list, move in or out with `m`, and MCP `create_terminal` nests by default; agents close a finished shell and only open one for human-visible work.

**Architecture:** `sessions.parent_id` plus `PlaceSession` / `Children` in `internal/store`. The list emits an agent's children at `depth+1`. `T` and MCP set the parent from the cursor or caller. `m` lists groups and agents. Follow-actions on an agent include `Children`. `close_terminal` deletes the row.

**Tech Stack:** Go, bubbletea, modernc.org/sqlite, tmux via `internal/tmux`, MCP via `internal/mcpserver`.

**Spec:** `docs/superpowers/specs/2026-08-15-nested-terminals-design.md`

## Global Constraints

- Work in `/Users/yoan/Desktop/projects/agent-manager-worktrees/nested-terminals` on `am/nested-terminals`. Rebase onto `origin/main` before the first code commit (branch is behind).
- Every `go test` that can touch tmux: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ...`. Never a bare `go test`. Never `tmux kill-server` on the default socket.
- Socket dir must stay short: `/tmp/amtest` only.
- `gofmt -l .` prints nothing. `go vet ./...` is clean before the last commit.
- Zero code comments except a non-obvious why. No em dashes. No AI attribution in commits.
- `parent_id` empty string means un-nested. `nest` on MCP is `*bool`; omitted means true.
- Only a shell may have a parent; that parent is an agent; one level.
- Store enforces the graph (exists, not self, parent has no parent). UI / `sessioncmd` refuse a shell as parent.

## File map

| File | Responsibility |
| --- | --- |
| `internal/store/store.go` | `Session.ParentID`, migration, `PlaceSession`, `Children`, sibling `sort_order` |
| `internal/store/store_test.go` | Store graph and reorder tests |
| `internal/ui/model.go` | `rebuildRows` emits children; drop pinned split |
| `internal/ui/listview.go` | Drop Terminals block painting |
| `internal/ui/terminal.go` | `T` sets `ParentID` from the cursor |
| `internal/ui/move.go` | Picker lists agents; calls `PlaceSession` |
| `internal/ui/form.go` | `groupOption` can name an agent target |
| `internal/ui/modals.go` | Move picker labels; drop settings row |
| `internal/ui/lifecycle.go` | Agent follow-set; archive loop |
| `internal/ui/keys.go` | Reorder by `parent_id` |
| `internal/ui/rename.go` | Refuse shell tool when the row has children |
| `internal/ui/settings.go` | Drop `terminal rows` |
| `internal/sessioncmd/terminal.go` | `Nest *bool`, parent fields, `Close` |
| `internal/mcpserver/mcpserver.go` | `nest`, `close_terminal`, new instructions |
| `docs/usage.md`, `internal/ui/help.go` | User-facing copy |

---

### Task 1: Store `parent_id`, `Children`, sibling sort

**Files:**
- Modify: `internal/store/store.go` (`Session`, `init` migrations, `CreateSession`, `ListSessions`, `Get`)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: existing `newTestStore`, `sample`
- Produces: `Session.ParentID string`; `Children(parentID string) ([]Session, error)`; `CreateSession` writes `parent_id` and assigns `sort_order` among `(group_name, parent_id)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/store_test.go`:

```go
func TestParentIDRoundTrip(t *testing.T) {
	st := newTestStore(t)
	parent := sample("agent", "g1")
	if err := st.CreateSession(parent); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child := sample("sh", "g1")
	child.Tool = "terminal"
	child.ParentID = "agent"
	if err := st.CreateSession(child); err != nil {
		t.Fatalf("create child: %v", err)
	}
	got, err := st.Get("sh")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ParentID != "agent" {
		t.Fatalf("ParentID = %q, want agent", got.ParentID)
	}
	list, err := st.ListSessions(false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byID := map[string]Session{}
	for _, sess := range list {
		byID[sess.ID] = sess
	}
	if byID["sh"].ParentID != "agent" || byID["agent"].ParentID != "" {
		t.Fatalf("list parent ids: %+v", byID)
	}
}

func TestChildrenIncludesArchivedAndOrder(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g")); err != nil {
		t.Fatalf("parent: %v", err)
	}
	first := sample("a", "g")
	first.ParentID = "agent"
	second := sample("b", "g")
	second.ParentID = "agent"
	if err := st.CreateSession(first); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := st.CreateSession(second); err != nil {
		t.Fatalf("second: %v", err)
	}
	if err := st.SetArchived("a", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	kids, err := st.Children("agent")
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if len(kids) != 2 || kids[0].ID != "a" || kids[1].ID != "b" {
		t.Fatalf("children = %+v", kids)
	}
}

func TestDeleteParentLeavesChildRow(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g")); err != nil {
		t.Fatalf("parent: %v", err)
	}
	child := sample("sh", "g")
	child.ParentID = "agent"
	if err := st.CreateSession(child); err != nil {
		t.Fatalf("child: %v", err)
	}
	if err := st.Delete("agent"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err := st.Get("sh")
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if got.ParentID != "agent" {
		t.Fatalf("store must not cascade, ParentID = %q", got.ParentID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/store/ -run 'TestParentIDRoundTrip|TestChildrenIncludesArchivedAndOrder|TestDeleteParentLeavesChildRow' -v`

Expected: FAIL, compile error `unknown field ParentID` and `st.Children undefined`.

- [ ] **Step 3: Implement**

On `Session`, after `PendingInputClaimed`:

```go
	ParentID string
```

In `init` migrations, append:

```go
		`ALTER TABLE sessions ADD COLUMN parent_id TEXT NOT NULL DEFAULT ''`,
```

`CreateSession` INSERT: add `parent_id` to the column list and values. Change the `sort_order` subquery to:

```sql
(SELECT COALESCE(MAX(sort_order)+1, 0) FROM sessions WHERE group_name = ? AND parent_id = ?)
```

Bind `sess.Group, sess.ParentID` for that subquery (two extra args at the end; the existing last `sess.Group` was only for sort_order, replace that pair).

`ListSessions` and `Get`: add `parent_id` to the SELECT (after `pending_claimed`) and scan into `&sess.ParentID`.

Add:

```go
func (s *Store) Children(parentID string) ([]Session, error) {
	if parentID == "" {
		return nil, nil
	}
	sessions, err := s.ListSessions(true)
	if err != nil {
		return nil, err
	}
	var kids []Session
	for _, sess := range sessions {
		if sess.ParentID == parentID {
			kids = append(kids, sess)
		}
	}
	return kids, nil
}
```

`ListSessions` already orders by `group_name, sort_order, created_at`, so `Children` keeps sibling order.

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/store/ -run 'TestParentIDRoundTrip|TestChildrenIncludesArchivedAndOrder|TestDeleteParentLeavesChildRow|TestCreateAndList' -v`

Expected: PASS. Existing create/list still works with empty `parent_id`.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): persist session parent_id and list children"
```

---

### Task 2: `PlaceSession` and sibling reorder

**Files:**
- Modify: `internal/store/store.go` (`MoveSession`, `ReorderSession`, `SwapSessionOrder`)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `Session.ParentID`, `Children`
- Produces: `PlaceSession(id, group, parentID string) error`. `MoveSession(id, group)` becomes `PlaceSession(id, group, "")`. Reorder only swaps rows that share `(group_name, parent_id)`. Moving an agent updates each child's `group_name`.

- [ ] **Step 1: Write the failing tests**

```go
func TestPlaceSessionNestsAndUnnests(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g1")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := st.CreateSession(sample("sh", "g1")); err != nil {
		t.Fatalf("shell: %v", err)
	}
	if err := st.PlaceSession("sh", "g1", "agent"); err != nil {
		t.Fatalf("nest: %v", err)
	}
	got, err := st.Get("sh")
	if err != nil || got.ParentID != "agent" || got.Group != "g1" {
		t.Fatalf("nested = %+v err %v", got, err)
	}
	if err := st.PlaceSession("sh", "g2", ""); err != nil {
		t.Fatalf("unnest: %v", err)
	}
	got, err = st.Get("sh")
	if err != nil || got.ParentID != "" || got.Group != "g2" {
		t.Fatalf("unnested = %+v err %v", got, err)
	}
}

func TestPlaceSessionRejectsBadParent(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	child := sample("mid", "g")
	child.ParentID = "agent"
	if err := st.CreateSession(child); err != nil {
		t.Fatalf("mid: %v", err)
	}
	if err := st.CreateSession(sample("sh", "g")); err != nil {
		t.Fatalf("sh: %v", err)
	}
	if err := st.PlaceSession("sh", "g", "missing"); err == nil {
		t.Fatal("missing parent")
	}
	if err := st.PlaceSession("agent", "g", "agent"); err == nil {
		t.Fatal("self parent")
	}
	if err := st.PlaceSession("sh", "g", "mid"); err == nil {
		t.Fatal("parent that already has a parent")
	}
}

func TestPlaceSessionMovesAgentChildren(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g1")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	child := sample("sh", "g1")
	child.ParentID = "agent"
	if err := st.CreateSession(child); err != nil {
		t.Fatalf("child: %v", err)
	}
	if err := st.PlaceSession("agent", "g2", ""); err != nil {
		t.Fatalf("move agent: %v", err)
	}
	agent, _ := st.Get("agent")
	sh, _ := st.Get("sh")
	if agent.Group != "g2" || sh.Group != "g2" || sh.ParentID != "agent" {
		t.Fatalf("agent=%+v child=%+v", agent, sh)
	}
}

func TestReorderSessionStaysInSiblingSet(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := st.CreateSession(sample("other", "g")); err != nil {
		t.Fatalf("other: %v", err)
	}
	a := sample("a", "g")
	a.ParentID = "agent"
	b := sample("b", "g")
	b.ParentID = "agent"
	if err := st.CreateSession(a); err != nil {
		t.Fatalf("a: %v", err)
	}
	if err := st.CreateSession(b); err != nil {
		t.Fatalf("b: %v", err)
	}
	moved, err := st.ReorderSession("a", 1, false)
	if err != nil || !moved {
		t.Fatalf("reorder child: moved=%v err=%v", moved, err)
	}
	kids, _ := st.Children("agent")
	if len(kids) != 2 || kids[0].ID != "b" || kids[1].ID != "a" {
		t.Fatalf("child order %+v", kids)
	}
	if err := st.SwapSessionOrder("agent", "a"); err == nil {
		t.Fatal("agent and its child are not siblings")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/store/ -run 'TestPlaceSession|TestReorderSessionStaysInSiblingSet' -v`

Expected: FAIL, `PlaceSession undefined` and/or swap of agent with child succeeding.

- [ ] **Step 3: Implement**

```go
func (s *Store) PlaceSession(id, group, parentID string) error {
	parentID = strings.TrimSpace(parentID)
	if parentID != "" {
		if parentID == id {
			return fmt.Errorf("session %s cannot be its own parent", id)
		}
		parent, err := s.Get(parentID)
		if err != nil {
			return fmt.Errorf("parent %s: %w", parentID, err)
		}
		if parent.ParentID != "" {
			return fmt.Errorf("parent %s already has a parent", parentID)
		}
		group = parent.Group
	}
	res, err := s.db.Exec(
		`UPDATE sessions SET group_name = ?, parent_id = ?,
		 sort_order = (SELECT COALESCE(MAX(sort_order)+1, 0) FROM sessions WHERE group_name = ? AND parent_id = ?)
		 WHERE id = ?`,
		group, parentID, group, parentID, id)
	if err != nil {
		return err
	}
	if err := requireRow(res, id); err != nil {
		return err
	}
	if parentID == "" {
		if _, err := s.db.Exec(`UPDATE sessions SET group_name = ? WHERE parent_id = ?`, group, id); err != nil {
			return err
		}
	}
	return s.ensureGroup(group)
}

func (s *Store) MoveSession(id, group string) error {
	return s.PlaceSession(id, group, "")
}
```

In `ReorderSession`, change the sibling query to `WHERE group_name = ? AND parent_id = ?` and pass `sess.Group, sess.ParentID`.

In `SwapSessionOrder`, after loading both rows, require `sess.ParentID == target.ParentID` (in addition to the same group). Query siblings with `AND parent_id = ?`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/store/ -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): place sessions under a parent and reorder siblings"
```

---

### Task 3: Inline list, drop the Terminals block

**Files:**
- Modify: `internal/ui/model.go` (`rebuildRows`, drop `pinnedShells` / `shellsPinned` / `treeRows` / `pinnedShell`)
- Modify: `internal/ui/listview.go` (drop `terminalSectionLines` and the pinned paint path)
- Modify: `internal/ui/settings.go`, `internal/ui/modals.go`, `internal/ui/keys.go` (`terminalPlacementSetting`)
- Modify: `internal/ui/terminals_section_test.go` (delete pinned tests; keep or move any inline glyph coverage that still belongs)
- Test: `internal/ui/listview_test.go` or a new `internal/ui/nest_test.go`

**Interfaces:**
- Consumes: `Session.ParentID`
- Produces: one tree. Un-nested sessions in a group, then each agent's children at `depth+1`. Orphan `parent_id` (parent missing from `m.sessions`) paints as un-nested. Search that matches a child also shows the parent. Settings has no `terminal rows`.

- [ ] **Step 1: Write the failing tests**

Add `internal/ui/nest_test.go`:

```go
package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
)

func TestRebuildRowsNestsChildrenUnderParent(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	child := store.Session{
		ID: "sh1", Name: "term-one", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}
	if err := m.store.CreateSession(child); err != nil {
		t.Fatalf("child: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	var names []string
	var depths []int
	for _, row := range m.rows {
		if row.isGroup {
			continue
		}
		names = append(names, row.sess.Name)
		depths = append(depths, row.depth)
	}
	if len(names) < 2 || names[0] != "coder" || names[1] != "term-one" {
		t.Fatalf("order = %v", names)
	}
	if depths[1] != depths[0]+1 {
		t.Fatalf("depths = %v", depths)
	}
}

func TestSearchMatchingChildKeepsParent(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	if err := m.store.CreateSession(store.Session{
		ID: "sh1", Name: "ssh-prod", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.search = "ssh-prod"
	m.rebuildRows()
	names := []string{}
	for _, row := range m.rows {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	if !strings.Contains(strings.Join(names, ","), "coder") || !strings.Contains(strings.Join(names, ","), "ssh-prod") {
		t.Fatalf("search dropped parent: %v", names)
	}
}

func TestOrphanParentIDPaintsUnnested(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.store.CreateSession(store.Session{
		ID: "gone", Name: "parent", Tool: "claude", Cwd: dir,
		Group: "backend", Status: status.Idle,
	}); err != nil {
		t.Fatalf("parent: %v", err)
	}
	if err := m.store.CreateSession(store.Session{
		ID: "sh1", Name: "loose", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: "gone", Status: status.Idle,
	}); err != nil {
		t.Fatalf("orphan: %v", err)
	}
	if err := m.store.Delete("gone"); err != nil {
		t.Fatalf("delete parent: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for _, row := range m.rows {
		if !row.isGroup && row.sess.Name == "loose" && row.depth == 1 {
			return
		}
	}
	t.Fatalf("orphan should sit un-nested in backend: %+v", m.rows)
}

func TestSettingsHasNoTerminalRows(t *testing.T) {
	m := buildModel(t)
	if strings.Contains(ansi.Strip(m.viewSettings()), "terminal rows") {
		t.Fatal("terminal rows setting must be gone")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run 'TestRebuildRowsNestsChildrenUnderParent|TestSearchMatchingChildKeepsParent|TestOrphanParentIDPaintsUnnested|TestSettingsHasNoTerminalRows' -v`

Expected: FAIL (child not under parent, and/or settings still shows `terminal rows`).

- [ ] **Step 3: Implement**

In `rebuildRows`, stop splitting shells into a pinned tail. For each listed session:

- If `ParentID != ""` and that parent exists in `m.sessions`, collect it in `childrenByParent[ParentID]`.
- Otherwise put it in `sessionsByGroup[Group]` (un-nested or orphan).

If `search != ""`, after the match filter, if a collected child is visible and its parent is not in `sessionsByGroup`, insert the parent from `m.sessions` into `sessionsByGroup` so the indent has a home.

When walking a group, emit each un-nested session at `depth+1`, then that session's `childrenByParent[id]` at `depth+2`.

Set `m.pinnedShells = 0`. Delete `pinnedShell`, `treeRows` (callers use `m.rows`), `shellsPinned` on `Model` and settings, `storedShellsPinned`, `terminalPlacementSetting`, `settingsFieldTerminals`, the settings persist/toggle, and the `terminal rows` line in `viewSettings`.

In `listview.go`, stop calling `terminalSectionLines`; the rail is one window over `m.rows`. Resting shells keep the `❯` glyph (`sessionGlyph` already does this when not pinned).

Delete pinned-only tests in `terminals_section_test.go`. Keep tests that still describe inline glyphs, or move them next to `terminal_test.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -count=1`

Expected: PASS. Fix every test that still reads `shellsPinned`, `pinnedShells`, or `terminal rows`.

- [ ] **Step 5: Commit**

```bash
git add internal/ui
git commit -m "feat(ui): nest child shells in the session tree"
```

---

### Task 4: `T` sets parent from the cursor

**Files:**
- Modify: `internal/ui/terminal.go` (`openTerminal`)
- Test: `internal/ui/terminal_test.go`

**Interfaces:**
- Consumes: `Session.ParentID`, `CreateSession` via `launchNewSession`
- Produces: `T` on an agent nests under it; on a group, un-nested in that group; on a nested shell, sibling under the same parent; on an un-nested shell, another un-nested sibling.

- [ ] **Step 1: Write the failing tests**

```go
func TestOpenTerminalOnAgentNests(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	agent, _ := m.selected()
	shell := spawnTerminal(t, m)
	if shell.ParentID != agent.ID || shell.Group != agent.Group {
		t.Fatalf("shell parent=%q group=%q, want %q / %q", shell.ParentID, shell.Group, agent.ID, agent.Group)
	}
}

func TestOpenTerminalOnGroupIsUnnested(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")
	shell := spawnTerminal(t, m)
	if shell.ParentID != "" || shell.Group != "backend" {
		t.Fatalf("group T = %+v", shell)
	}
}

func TestOpenTerminalOnNestedShellSharesParent(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	first := spawnTerminal(t, m)
	m.selectSessionRow(t, first.Name)
	second := spawnTerminal(t, m)
	if second.ParentID != first.ParentID || second.ParentID == "" {
		t.Fatalf("second parent=%q first parent=%q", second.ParentID, first.ParentID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run 'TestOpenTerminalOnAgentNests|TestOpenTerminalOnGroupIsUnnested|TestOpenTerminalOnNestedShellSharesParent' -v`

Expected: FAIL, `ParentID` empty on the agent case.

- [ ] **Step 3: Implement**

In `openTerminal`, after `sess` is built with `Group: m.contextGroup()`:

```go
	if entry, ok := m.selectedRow(); ok && !entry.isGroup {
		if m.isShell(entry.sess.Tool) {
			sess.ParentID = entry.sess.ParentID
			sess.Group = entry.sess.Group
		} else {
			sess.ParentID = entry.sess.ID
			sess.Group = entry.sess.Group
		}
	}
```

`launchNewSession` already passes the `store.Session` through to `CreateSession`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run 'TestOpenTerminal' -count=1`

Expected: PASS, including the existing group-directory tests.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/terminal.go internal/ui/terminal_test.go
git commit -m "feat(ui): nest T under the selected agent"
```

---

### Task 5: `m` moves a terminal into a session

**Files:**
- Modify: `internal/ui/form.go` (`groupOption`)
- Modify: `internal/ui/move.go`
- Modify: `internal/ui/modals.go` (`viewMove`, `viewGroupPicker`)
- Test: `internal/ui/move_test.go`

**Interfaces:**
- Consumes: `PlaceSession`, `Session.ParentID`
- Produces: `groupOption` gains `sessID` and `name`. Moving a terminal onto an agent calls `PlaceSession(id, agent.Group, agent.ID)`. Onto a group: `PlaceSession(id, group, "")`. Agent/group moves still list groups only. Moving an agent takes children via `PlaceSession`.

- [ ] **Step 1: Write the failing tests**

```go
func TestMoveTerminalOntoAgentNests(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	createSession(t, m, "coder", dir, "backend")
	m.selectGroupRow(t, "backend")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	agent, _ := m.store.Get(m.sessionRows()[0].ID)
	for i, opt := range m.form.groups {
		if opt.sessID == agent.ID {
			m.form.groupIndex = i
			break
		}
	}
	if m.form.groups[m.form.groupIndex].sessID != agent.ID {
		t.Fatal("picker must list the agent")
	}
	_, cmd := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got, err := m.store.Get(shell.ID)
	if err != nil || got.ParentID != agent.ID {
		t.Fatalf("nested = %+v err %v", got, err)
	}
}

func TestMoveTerminalOntoGroupUnnests(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.store.CreateGroup("other", dir); err != nil {
		t.Fatalf("other: %v", err)
	}
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	pickGroup(t, m, "other")
	_, cmd := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got, _ := m.store.Get(shell.ID)
	if got.ParentID != "" || got.Group != "other" {
		t.Fatalf("unnest = %+v", got)
	}
}

func TestMoveAgentPickerHasNoSessionTargets(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	createSession(t, m, "coder", dir, "backend")
	createSession(t, m, "other", dir, "backend")
	m.selectSessionRow(t, "coder")
	m.openMove()
	for _, opt := range m.form.groups {
		if opt.sessID != "" {
			t.Fatalf("agent move listed session %q", opt.sessID)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run 'TestMoveTerminalOntoAgentNests|TestMoveTerminalOntoGroupUnnests|TestMoveAgentPickerHasNoSessionTargets' -v`

Expected: FAIL, `opt.sessID` undefined or picker has no agent.

- [ ] **Step 3: Implement**

```go
type groupOption struct {
	path   string
	depth  int
	sessID string
	name   string
}
```

`viewGroupPicker`: if `opt.sessID != ""`, label is `strings.Repeat("  ", opt.depth) + opt.name`.

`viewMove` title: `⇄ Move`.

`openMove` on a shell: build the group tree as today, and after each group append that group's non-archived agents (`!m.isShell`) as options with `sessID`, `name`, `path` = agent.Group, `depth` = group depth + 1. Root agents go under the root option.

`openMove` on an agent or group: keep `rebuildGroupOptions` only.

`handleMoveKey` enter for a session:

```go
	opt := m.form.groups[m.form.groupIndex]
	if err := m.store.PlaceSession(m.moveID, opt.path, opt.sessID); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
```

Same-parent / same-group no-op: if the session already has that `ParentID` and `Group`, close the picker with no write.

`pickGroup` still matches `opt.path` with `sessID == ""` so form tests stay valid.

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run 'TestMove' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/form.go internal/ui/move.go internal/ui/move_test.go internal/ui/modals.go
git commit -m "feat(ui): move a terminal into a session"
```

---

### Task 6: Follow on kill, archive, delete, revive

**Files:**
- Modify: `internal/ui/lifecycle.go`
- Test: `internal/ui/lifecycle_test.go`

**Interfaces:**
- Consumes: `Children(parentID string) ([]Session, error)`
- Produces: `sessionAndChildren(sess) []Session`. Kill/archive/delete/restore/revive on an agent use that set. Confirm copy names the extra terminals. `applyConfirmedArchived` writes every id. The same keys on a child stay single-session. Restart and fork stay single-session.

- [ ] **Step 1: Write the failing tests**

Follow the file's existing create-session + confirm pattern. Core cases:

```go
func TestKillAgentIncludesChildren(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	_, cmd := m.killSelected()
	m.applyCmd(t, cmd)
	if !strings.Contains(m.confirm.label, "terminal") {
		t.Fatalf("confirm should name terminals: %q", m.confirm.label)
	}
	ids := map[string]bool{}
	for _, sess := range m.confirm.sessions {
		ids[sess.ID] = true
	}
	if !ids[shell.ID] {
		t.Fatal("kill confirm omitted the child")
	}
}

func TestKillChildIsSingleSession(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.killSelected()
	if len(m.confirm.sessions) != 1 || m.confirm.sessions[0].ID != shell.ID {
		t.Fatalf("child kill = %+v", m.confirm.sessions)
	}
}

func TestArchiveAgentPersistsEveryChild(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.applyCmd(t, cmd)
	child, err := m.store.Get(shell.ID)
	if err != nil || !child.Archived {
		t.Fatalf("child archived=%v err=%v", child.Archived, err)
	}
}
```

Also assert delete-confirm on an agent includes the child id, and revive-confirm on a dead agent includes dead children. Restart confirm must stay length 1.

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run 'TestKillAgentIncludesChildren|TestKillChildIsSingleSession|TestArchiveAgentPersistsEveryChild' -v`

Expected: FAIL, confirm set is only the agent and/or child not archived.

- [ ] **Step 3: Implement**

```go
func (m *Model) sessionAndChildren(sess store.Session) ([]store.Session, error) {
	kids, err := m.store.Children(sess.ID)
	if err != nil {
		return nil, err
	}
	out := make([]store.Session, 0, 1+len(kids))
	out = append(out, sess)
	return append(out, kids...), nil
}
```

`killSelected` / `archiveSelected` / `restoreSelected` / `prepareDelete` / `reviveSelected` on a session: use `sessionAndChildren` instead of `[]store.Session{entry.sess}`.

Confirm label when `len(set) > 1`:

```text
kill coder and 2 terminals? frees their RAM, v revives them.
```

Use the existing single-session wording when there are no children.

```go
func (m *Model) applyConfirmedArchived(archived bool) error {
	if m.confirm.isGroup {
		return m.store.SetGroupArchived(m.confirm.path, archived)
	}
	for _, sess := range m.confirm.sessions {
		if err := m.store.SetArchived(sess.ID, archived); err != nil {
			return err
		}
	}
	return nil
}
```

Do not change `restartSelected` or fork.

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run 'TestKill|TestArchive|TestRevive|TestDelete|TestRestart' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/lifecycle.go internal/ui/lifecycle_test.go
git commit -m "feat(ui): agent kill archive delete revive follow child shells"
```

---

### Task 7: Reorder by parent, refuse shell tool with children

**Files:**
- Modify: `internal/ui/keys.go` (`visibleReorderTarget`)
- Modify: `internal/ui/rename.go` (tool change)
- Test: `internal/ui/listview_test.go` or `internal/ui/nest_test.go`; `internal/ui/rename_test.go`

**Interfaces:**
- Consumes: `Session.ParentID`, `Children`
- Produces: `J`/`K` on an agent skips child rows and swaps with the next un-nested sibling. On a child, only siblings with the same `ParentID`. Changing an agent with children to a shell tool sets `errBar` and keeps the tool.

- [ ] **Step 1: Write the failing tests**

```go
func TestReorderAgentSkipsChildren(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	createSession(t, m, "coder", dir, "backend")
	createSession(t, m, "other", dir, "backend")
	m.selectSessionRow(t, "coder")
	spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	_, cmd := m.reorderSelected(1)
	m.applyCmd(t, cmd)
	var names []string
	for _, row := range m.rows {
		if !row.isGroup && row.sess.ParentID == "" {
			names = append(names, row.sess.Name)
		}
	}
	if len(names) < 2 || names[0] != "other" || names[1] != "coder" {
		t.Fatalf("un-nested order %v", names)
	}
}

func TestRenameAgentToShellWithChildrenRefused(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	m.openRename()
	for i, name := range m.rename.toolNames {
		if name == "terminal" {
			m.rename.toolIndex = i
		}
	}
	_, cmd := m.handleRenameKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text == "" {
		t.Fatal("expected refuse")
	}
	got, _ := m.store.Get(m.sessionRows()[0].ID)
	if m.isShell(got.Tool) {
		t.Fatalf("tool became %q", got.Tool)
	}
}
```

Wire `handleRenameKey` the same way existing rename tests do (read `rename_test.go` and match that enter path).

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -run 'TestReorderAgentSkipsChildren|TestRenameAgentToShellWithChildrenRefused' -v`

Expected: FAIL.

- [ ] **Step 3: Implement**

`visibleReorderTarget` for a session: candidate must be a session, same `Group`, same `ParentID`. Skip groups. The walk already skips other depths if you compare `ParentID` rather than "any session in the group".

Before `UpdateTool` in rename save:

```go
	if toolChanged && m.isShell(tool) {
		kids, err := m.store.Children(m.rename.sessID)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		if len(kids) > 0 {
			m.errBar.text = "move its terminals first"
			return m, nil
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/ui/ -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/keys.go internal/ui/rename.go internal/ui/nest_test.go internal/ui/rename_test.go
git commit -m "feat(ui): reorder nest siblings and block shell-tool parents"
```

---

### Task 8: `sessioncmd` nest default and `Close`

**Files:**
- Modify: `internal/sessioncmd/terminal.go`
- Test: `internal/sessioncmd/terminal_test.go`

**Interfaces:**
- Consumes: `PlaceSession` is not needed at create time; `CreateSession` writes `ParentID`. `Close` uses existing kill + `store.Delete`.
- Produces:

```go
type Terminal struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Group      string `json:"group"`
	Directory  string `json:"directory"`
	Status     string `json:"status"`
	Running    bool   `json:"running"`
	ParentID   string `json:"parent_id"`
	ParentName string `json:"parent_name"`
}

type CreateTerminalOptions struct {
	Group     *string
	Directory string
	Nest      *bool
}

func (t *Terminals) Close(sessionID, terminalID string) error
```

Omitted `Nest` or `true`: `ParentID = caller.ID`, group = caller.Group. Explicit `group` that differs from the caller is an error (`set nest false to place in another group`). `nest: false`: empty parent, today's group rules. `Close` refuses an agent, a missing id, or an archived shell.

- [ ] **Step 1: Write the failing tests**

In `internal/sessioncmd/terminal_test.go`, using `newTerminalHarness`:

```go
func TestCreateNestsUnderCallerByDefault(t *testing.T) {
	h := newTerminalHarness(t)
	created, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ParentID != h.caller.ID || created.ParentName != h.caller.Name {
		t.Fatalf("created = %+v", created)
	}
}

func TestCreateNestFalseIsUnnested(t *testing.T) {
	h := newTerminalHarness(t)
	nest := false
	created, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Nest: &nest})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ParentID != "" {
		t.Fatalf("parent = %q", created.ParentID)
	}
}

func TestCreateNestTrueRejectsOtherGroup(t *testing.T) {
	h := newTerminalHarness(t)
	if err := h.store.CreateGroup("elsewhere", h.caller.Cwd); err != nil {
		t.Fatalf("group: %v", err)
	}
	group := "elsewhere"
	_, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{Group: &group})
	if err == nil || !strings.Contains(err.Error(), "nest false") {
		t.Fatalf("err = %v", err)
	}
}

func TestCloseDeletesShellAndRefusesAgent(t *testing.T) {
	h := newTerminalHarness(t)
	created, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.terminals.Close(h.caller.ID, h.caller.ID); err == nil {
		t.Fatal("close agent")
	}
	if err := h.terminals.Close(h.caller.ID, created.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := h.store.Get(created.ID); err == nil {
		t.Fatal("row still present")
	}
	if h.driver.Exists(created.ID) {
		t.Fatal("pane still live")
	}
}
```

Update existing `Create` tests that pass a different `group` without `Nest: &false` so they still express their intent (`TestCreate` inherited-group cases need `nest := false`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/sessioncmd/ -run 'TestCreateNests|TestCreateNestFalse|TestCreateNestTrueRejects|TestCloseDeletes' -v`

Expected: FAIL, `ParentID` empty / `Close` undefined / group create still succeeds.

- [ ] **Step 3: Implement**

`info` fills `ParentID` and looks up `ParentName` via `store.Get` when `ParentID != ""`.

`Create`: `nest := true`; if `opts.Nest != nil` use `*opts.Nest`. If nest and `opts.Group != nil` and `strings.TrimSpace(*opts.Group) != caller.Group`, return `fmt.Errorf("set nest false to place in another group")`. If nest, `sess.ParentID = caller.ID` and `sess.Group = caller.Group`. If not nest, keep `createTarget` as it is.

```go
func (t *Terminals) Close(sessionID, terminalID string) error {
	runtime, err := t.open()
	if err != nil {
		return err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return err
	}
	sess, err := runtime.terminal(terminalID)
	if err != nil {
		return err
	}
	_ = runtime.driver.Kill(sess.ID)
	return runtime.store.Delete(sess.ID)
}
```

`runtime.terminal` already refuses agents and archived shells.

- [ ] **Step 4: Run tests to verify they pass**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/sessioncmd/ -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sessioncmd/terminal.go internal/sessioncmd/terminal_test.go
git commit -m "feat(sessioncmd): nest created terminals and close them"
```

---

### Task 9: MCP tools, instructions, docs

**Files:**
- Modify: `internal/mcpserver/mcpserver.go`
- Modify: `internal/mcpserver/mcpserver_test.go`
- Modify: `docs/usage.md` (Terminal tabs, MCP table)
- Modify: `internal/ui/help.go` (`T` and `m` lines)

**Interfaces:**
- Consumes: `CreateTerminalOptions.Nest`, `Terminals.Close`, `Terminal.ParentID`
- Produces: `create_terminal` `nest *bool`; `close_terminal(terminal_id)`; instructions that name SSH, forbid one-shot local create, and tell the agent to close a finished job.

- [ ] **Step 1: Write the failing tests**

Update `TestListsAllTools` to require `"close_terminal"`.

Replace `TestServerTeachesProactiveTerminalWorkflow` expected fragments with:

```go
	for _, want := range []string{
		"SSH",
		"one-shot",
		"list_terminals",
		"create_terminal",
		"close_terminal",
		"nests under this session",
	} {
```

Update `TestTerminalDescriptionsTeachWhenAndHowToChainTools`:

```go
		"create_terminal": {"SSH", "one-shot", "nests", "close_terminal"},
		"close_terminal":  {"finished", "kills", "SSH"},
```

Add a fake `Close` on `fakeTerminalCommands` and a test that `close_terminal` forwards `terminal_id`.

Add a test that `create_terminal` with no args calls `Create` with `Nest == nil`, and with `"nest": false` calls it with `Nest` pointing at false.

- [ ] **Step 2: Run tests to verify they fail**

Run: `env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/mcpserver/ -count=1`

Expected: FAIL, missing tool / missing SSH wording.

- [ ] **Step 3: Implement**

`createTerminalArgs`:

```go
	Nest *bool `json:"nest,omitempty" jsonschema:"when true or omitted, nest under this agent; false places an un-nested terminal in group"`
```

Pass `Nest: args.Nest` into `Create`.

`closeTerminalArgs`: `TerminalID string`.

```go
		Name: "close_terminal",
		Description: "Delete a managed terminal you opened once its job is finished: kills the pane and removes the row. " +
			"Leave it running when you opened it for the user (for example an SSH session they may attach to). " +
			"Refuses agent sessions.",
```

Extend `terminalCommands` with `Close(sessionID, terminalID string) error`.

Replace `serverInstructions` with:

```text
Use Agent Manager's terminal tools when the session itself is the point: the user should be able to watch it, approve what happens, attach, or take over. SSH into a host is the canonical case. Do not create a terminal for one-shot local commands or other internal work; those stay in your normal tools.

Before opening a new terminal, call list_terminals and reuse a relevant running terminal when possible. create_terminal nests under this session unless nest is false. Use send_terminal and read_terminal while the job runs. When the job is finished and the terminal is not being left for the user, call close_terminal.

Sending a terminal command executes on the user's machine and follows the same safety and approval expectations as normal shell execution.
```

`create_terminal` description: human-visible work (SSH), not one-shot local, nests unless `nest` is false, then `send_terminal`.

`docs/usage.md`: shells live in the tree; `T` on an agent nests; `m` moves a terminal into a session or a group. Delete the pinned-block subsection and the settings mention. MCP table: `create_terminal` nests under the caller unless `nest` is false; add `close_terminal`. Say agents open a terminal for human-visible work (SSH) and close it when that job ends.

`help.go`: `T` = `new terminal tab: a shell under the selected agent, or in the selected group`; `m` = `move it to a group, or a terminal into a session`.

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./internal/mcpserver/ ./internal/sessioncmd/ ./internal/store/ ./internal/ui/ -count=1
gofmt -l .
go vet ./...
```

Expected: all PASS, `gofmt -l .` empty, vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/mcpserver docs/usage.md internal/ui/help.go
git commit -m "feat(mcp): nest by default, close finished terminals, teach SSH"
```

---

## Self-review

Spec coverage:

| Spec | Task |
| --- | --- |
| `parent_id`, one level, store graph | 1, 2 |
| `PlaceSession`, sibling sort, agent move takes children | 2 |
| Inline tree, orphan paints un-nested, search keeps parent | 3 |
| Drop pinned / `terminal rows` | 3 |
| `T` parent table | 4 |
| `m` onto agent / group | 5 |
| Follow kill/archive/delete/restore/revive; archive loop | 6 |
| Restart/fork stay single | 6 (do not change) |
| `J`/`K` sibling set | 7 |
| Refuse shell tool with children | 7 |
| MCP nest default `*bool`, other-group error | 8, 9 |
| `close_terminal` | 8, 9 |
| Agent policy (SSH / one-shot / close) | 9 |
| Docs / help | 9 |

No placeholders. `PlaceSession`, `Children`, `Close`, `Nest *bool` names match across tasks. `MoveSession` remains a one-line wrapper so form/move compile through Task 4.
