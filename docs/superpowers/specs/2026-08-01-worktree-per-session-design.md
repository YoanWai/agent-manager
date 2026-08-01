# Worktree-per-session design

Date: 2026-08-01
Status: approved

## Goal

A session can run in its own git worktree, so parallel agents on the same repo never edit the same checkout. The worktree option appears in the new-session form (`n`) and as a toggle in the quick prompt bar (`space`), with a settings default.

## Decisions

Researched conventions first: Claude Code uses `.claude/worktrees/<name>` in-repo with branch `worktree-<name>` and a fresh base from the remote default branch; claude-squad uses a fixed `~/.claude-squad/worktrees/`; vibe-kanban uses `~/.vibe-kanban-workspaces/` with `vk/<id>-<slug>` branches.

- **Placement**: sibling directory `<repoParent>/<repoName>-worktrees/<session-name>`. Visible, short paths, cd-able, and outside the repo so no `.gitignore` edit is needed.
- **Branch**: `am/<session-name>`, based on the remote default branch (`origin/HEAD`). Fallback when no remote or `origin/HEAD` is not cached: the local default branch, then local `HEAD`.
- **Toggle UX**: settings default (off initially) + per-run override. Quick bar: `alt+w` flips it, a `wt` chip renders beside the tool name. Form: a `worktree: on/off` row after the directory field, cycled with left/right.
- **Scope in quick bar**: the toggle applies only when the selection is a group (spawn). Answering an existing session ignores it.
- **Cleanup**: on session delete only, and only when clean. Kill, archive, and revive leave the worktree in place.
- **Failure mode**: worktree on but the target directory is not inside a git repo, the worktree path already exists, or `git worktree add` fails: the spawn is blocked and the error bar names the problem. No silent fallback to a plain session.

## Architecture

One new git-driver primitive pair, one flag threaded through the existing spawn path, two new store columns.

### internal/git

- `AddWorktree(root, sessionName string) (path, branch string, err error)`: resolves the sibling path and `am/` branch, resolves the base ref (`origin/HEAD`, then local default, then `HEAD`), runs `git worktree add -b <branch> <path> <base>`. Errors when the path or branch already exists.
- `RemoveWorktreeIfClean(repoRoot, path, branch string) (removed bool, err error)`: clean means empty `git status --porcelain` in the worktree and no commits on `branch` missing from the default branch. Clean: `git worktree remove` then `git branch -d`. Dirty: returns `removed=false`, no error.
- `RepoRoot(dir string) (string, error)`: `git rev-parse --show-toplevel` wrapper (errors outside a repo).

### internal/store

`sessions` gains two columns, both empty for plain sessions:

- `worktree_repo`: main repo root recorded at creation, so delete cleanup works even after the session is renamed.
- `worktree_branch`: the `am/<launch-name>` branch recorded at creation.

The worktree directory and branch keep the launch-time session name; a later `agent-manager rename` does not move or rename them.

### internal/ui

- `spawnSession` gains a `worktree bool` parameter. When set: resolve the repo root from the chosen dir, call `AddWorktree`, use the returned path as the session `Cwd`, and persist `worktree_repo` + `worktree_branch`. Any error aborts the spawn before tmux is touched.
- Form (`form.go`): new `fieldWorktree` between directory and prompt, seeded from the settings default.
- Quick bar (`quick.go`): `quickState` gains `worktree bool` seeded from the settings default; `alt+w` toggles it; `quickSpawn` passes it through. The bar footer renders a `wt` chip beside the tool name while on.
- Settings (`settings.go`): new entry "worktree default" (on/off), stored in the settings table like the existing quick-close key.
- Lifecycle (`lifecycle.go`): the delete path calls `RemoveWorktreeIfClean` when `worktree_repo` is set. Dirty worktrees are kept and the result message shows the kept path.

Status polling, preview, quick answers, and ctrl+r diff review all operate on `Cwd` and need no changes: the worktree is a normal repo checkout.

## Error handling

- Non-repo dir, existing path/branch, failed `worktree add`: spawn blocked, error surfaced in the error bar.
- Base-ref resolution failures fall through the chain (`origin/HEAD` → local default → `HEAD`); if even `HEAD` cannot resolve, the spawn is blocked.
- Delete-time cleanup errors surface in the error bar; the session record is still deleted (the worktree stays on disk).

## Testing

- `internal/git`: temp-repo tests (existing pattern) for `AddWorktree` (fresh repo, no-remote fallback, existing-path error), `RemoveWorktreeIfClean` (clean removal, dirty keep, unmerged-commit keep), `RepoRoot` (repo, non-repo).
- `internal/ui`: form toggle field cycling and submit threading; quick-bar `alt+w` toggle, chip rendering, spawn-vs-answer scoping; blocked-spawn error paths. Extend `form_test.go` and `quick_test.go`.
- `internal/store`: column round-trip in `store_test.go`.

## Out of scope

- Configurable worktree location (`worktree_dir` config key) — add if asked.
- Moving/renaming the worktree when the session renames itself.
- Copying gitignored files (`.worktreeinclude`-style) into new worktrees.
- Background sweep of stale worktrees.
