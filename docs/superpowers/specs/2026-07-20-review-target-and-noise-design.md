# Review target and diff noise — design

Two changes to full-screen review: let the agent declare what it is working on
(repo and base branch) instead of making the human hunt for it, and stop listing
entries that cannot be reviewed.

## Problem

Review currently guesses, and the guesses are noisy.

A session's working directory is often an umbrella folder holding many repos
(`mreshet` holds 20). Review ranks them and opens on the most-active one, with
`r` cycling to the next. Cycling 20 repos to find the one the agent touched is
backwards: the agent already knows which repo it is working in.

The file list also carries entries that cannot be diffed. `git ls-files
--others` reports untracked *directories* (a nested repo or worktree, which git
refuses to descend into) alongside untracked files, and `git diff --numstat`
covers only tracked changes, so every untracked entry renders as `+0 −0`. A real
session showed 20 files where 18 were nested worktree directories and untracked
screenshots, and the header total counted only the 2 tracked files.

Separately, `Ctrl+R` opens review from inside a session but does nothing from the
session list, where the key is `D`/`x`.

## Decisions

- The base ref persists per session, the way a session's name does, rather than
  resetting each time review closes.
- The base ref is stored per session *and* repo, so a sibling repo without that
  branch keeps its own base.
- The agent declares the active repo; blind cycling is removed.
- The human can still change repo and base, through a filtered picker.
- Selecting a base switches the scope to "vs base".

## Phase 1 — Diff noise

Three defects, all in how untracked entries are collected and counted.

**Untracked directories.** `ChangedFiles` appends every line of `ls-files
--others --exclude-standard -z` as a file. Git emits a nested repository or
worktree as a directory path with a trailing slash, since it will not descend
into another repository. Those entries are dropped: a directory has no content
to diff, and a nested repo's changes belong to that repo's own review.

**Missing line counts.** `BuildSet` reads each file's stat from the `numstat`
map. Untracked files never appear there, so they take the zero value and render
`+0 −0`. When a file's stat is absent, derive it from the diff that was just
built: count the `Add` and `Del` line kinds. This keeps a genuinely empty file at
`+0 −0` while giving a new 200-line file its `+200`.

**Binary files.** `loadFile` already sets `Binary`, but the file list renders the
zero stat as `+0 −0`. Render `binary` for those rows instead.

The header totals are a sum over the file stats, so they correct themselves once
the stats are right.

## Phase 2 — Review target

### Agent-declared repo and base

Two subcommands, each mirroring `agent-manager rename`: the agent runs them
inside its session, they resolve the session from `AGENT_MANAGER_SESSION_ID`,
write a mailbox file, and the poller applies and deletes it.

```
agent-manager review-repo <path>    # the repo this session is working in
agent-manager review-base <ref>     # what "vs base" compares against
```

`review-repo` records an absolute repo root on the session row. `review-base`
records a ref for the session's currently active repo, so the two compose: set
the repo, then set its base.

Both validate before writing and fail loudly rather than storing something the
review cannot use. `review-repo` requires a path that resolves to a git repo
root. `review-base` requires a ref that resolves in the repo review would
currently use for that session: the declared `review_repo` when set, otherwise
the top of the dirty-first ranking.

### Storage

`review_repo` is a new column on the session row, alongside the name the rename
command already writes.

Base refs live in a small `review_bases(session_id, repo_root, base_ref)` table,
keyed by session and repo. A column cannot express per-repo values, and a JSON
blob in a column would need parsing on every load.

### Resolving what to review

`diffLoadCmd` resolves the repo in this order: the session's declared
`review_repo`, then the repo the human picked this session (`repoSel`), then the
top of the existing dirty-first ranking. The ranking stays as the fallback for
sessions where nothing has been declared.

For `ScopeBranch`, `BaseRef` uses the stored base for the active repo when one
exists, and auto-detects `main`/`master`/`origin/HEAD` otherwise. A stored ref
that no longer resolves surfaces as a review error rather than silently falling
back, so the header never claims a comparison that did not happen.

### Worktrees are branches

Agents work in git worktrees, one branch per worktree, and those worktrees live
wherever the agent tooling puts them: under the repo's `.worktrees/`, in a
sibling `<name>-worktrees/` folder, or under the user's config directory.
Directory discovery from the session cwd cannot see them.

The design is AI-driven: the agent declares its worktree with the existing
`agent-manager review-repo <path>` command, and that one declaration names both
the repo and the branch, because a worktree is a branch. Review accepts any
declared path that is a real git worktree root, wherever it lives on disk;
`git rev-parse` at load time is the authority, not containment under the
session cwd. A declaration that is not a git root at all is reported, exactly
as a vanished one is.

The human overrides through pickers, expected to be rare:

`r` opens the repo picker: the repos found under the session directory,
filtered as you type, enter to select. `b` opens a branch picker listing the
active repo's worktrees from `git worktree list --porcelain`, each row showing
the branch name and the worktree path, with the cursor on the current one.
Selecting a row retargets review to that worktree through the same
path-pinned selection the repo picker uses.

### Base picker

A later phase adds `B` over `refs/heads` and `refs/remotes` for the active
repo, with the cursor starting on the current base and an `auto` entry that
clears the stored ref back to auto-detection.

Both are the same filtered-list component, differing only in the rows they show
and what selecting a row does. It follows the existing modal patterns (move,
settings), which already own a mode, a key handler, and a render function.

### Ctrl+R from the list

Bind `ctrl+r` in list mode to open review for the selected session. `D` and `x`
keep working. Escape returns to the list, as it does today; only the in-session
path re-attaches to the session, which stays unchanged.

The tmux root binding sends `C-r` through to the pane for any session not named
`am_*`, so the key reaches the manager's own UI.

## Testing

Phase 1, in `internal/diff` and `internal/git`:

- A repo containing a nested repository lists the tracked changes and omits the
  nested directory.
- An untracked text file reports its line count; an untracked empty file stays
  at `+0 −0`.
- An untracked binary file is marked binary.
- Header totals match the sum of the listed files.

Phase 2:

- `review-repo` and `review-base` reject a missing session id, a path that is
  not a repo, and a ref that does not resolve.
- The poller applies a mailbox file to the session row and deletes it.
- Review opens on the declared repo even when the ranking would pick another.
- A stored base drives `vs base`; a base that stops resolving surfaces an error.
- A base set on one repo does not apply to a sibling repo.
- The repo picker filters and selects; the branch picker's `auto` entry clears
  the stored ref.
- `ctrl+r` from the list opens review on the selected session.

## Sequencing

Phase 1 ships as its own pull request, since it makes review readable again on
its own. Phase 2 follows.

Work happens in an isolated git worktree off `origin/main`, because several
agent sessions share this repository's checkout.
