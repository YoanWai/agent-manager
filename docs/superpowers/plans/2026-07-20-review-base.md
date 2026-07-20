# Review Base Implementation Plan (Phase 2b, fold into #39)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development.

**Goal:** The agent declares the base its branch diffs against (`agent-manager review-base <ref>`); "vs base" uses it; `B` opens a base picker. AI-driven, human override rare.

**Architecture:** Same mailbox pattern as `review-repo`. Bases persist per session and repo in `review_bases`. The diff layer takes an optional base override threaded through `BuildSet` and stored on `Set` so lazy loads reuse it. A stored ref that stops resolving surfaces an error, never a silent fallback.

## Global Constraints

- Worktree `/tmp/am-p2`, branch `feat/review-target`. Never the shared checkout.
- Explicit paths staged; never `git add -A`. No AI attribution. Comments: one-line WHY only.
- Fail loudly. No speculative code. `gofmt -l internal/ .` empty, `go vet` clean, `go test ./...` green per commit.

---

### Task 1: Plumbing (git, store, hooks, CLI, poller)

**Files:** `internal/git/git.go`(+test), `internal/store/store.go`(+test), `internal/hooks/hooks.go`(+test), `main.go`(+test), `internal/ui/poller.go`(+test)

**Interfaces produced:**
- `(*git.Driver) BranchRefs(root string) ([]string, error)` — short names from `for-each-ref --format=%(refname:short) refs/heads refs/remotes`, excluding `origin/HEAD` and `origin`itself.
- `(*git.Driver) ResolveRef(root, ref string) error` — nil when `rev-parse --verify -q <ref>^{commit}` succeeds.
- `(*store.Store) SetReviewBase(sessionID, repoRoot, baseRef string) error` — upsert on `review_bases(session_id, repo_root, base_ref)` PK(session_id, repo_root); empty baseRef deletes.
- `(*store.Store) ReviewBase(sessionID, repoRoot string) (string, error)` — "" when unset; only `sql.ErrNoRows` swallowed.
- `(*hooks.Manager) ReviewBaseFile(id) string` (`<id>.reviewbase`), `ReadReviewBase(id) (root, ref string, found bool)` (two lines: root, ref), `RemoveReviewBase(id) error`.
- `runReviewBase(args []string, sessionID, configDir string) error` in `main.go`: validates like `runReviewRepo`; resolves the repo from the PROCESS CWD (`git rev-parse --show-toplevel` via `ResolveRepos(".")` semantics — the agent runs it inside its worktree), validates the ref with `ResolveRef` there, writes `root\nref` to the mailbox. `agent-manager review-base <ref>`; also accept `--clear` writing an empty ref line meaning delete.
- `(p *poller) applyPendingReviewBase(sess *store.Session) error` beside the review-repo one: read, `SetReviewBase(sess.ID, root, ref)` (empty ref → clear), always consume; store failure keeps the mailbox.
- Delete path: `Store.Delete` also deletes from `review_bases`; session delete removes the `.reviewbase` mailbox.

Tests mirror the review-repo ones per layer (round-trip+clear, mailbox lifecycle, CLI validation incl. bad ref and non-repo cwd, poller apply+consume). TDD, commit per layer or one commit total.

---

### Task 2: Diff override + `B` picker

**Files:** `internal/diff/diff.go`(+test), `internal/ui/diffview.go`, `internal/ui/repopicker.go`, `internal/ui/modals.go`(+tests in review_regression_test.go)

**Interfaces:**
- `diff.BuildSet(driver, cwd string, scope git.Scope, baseOverride string) (Set, error)` — for ScopeBranch with override: `ResolveRef` it; on failure return an error naming the ref (fail loudly, no fallback); on success use it as candidate (merge-base against HEAD, describe `<ref>@<short>`). `Set` gains `BaseOverride string`; `EnsureFile` uses it. Empty override keeps auto-detection.
- `diffLoadCmd`: for the selected root, read `m.store.ReviewBase(sess.ID, root)` inside the command (store is goroutine-safe sqlite) and pass it to `BuildSet` and to the fingerprint's BaseRef derivation. NOTE `diffLoadCmd` runs before the root is final only in the append path — read the base AFTER resolving the final root.
- Probe path (`diffProbeCmd`) and refresh derive the same override so fingerprints agree.
- `B` in `handleDiffKey`: opens the shared picker with rows = `auto` + `BranchRefs` of current root, cursor on current base (or `auto`), title "Diff base". Enter: `auto` → `SetReviewBase(sess.ID, root, "")`; ref → `SetReviewBase(sess.ID, root, ref)`; then reload; if scope is not ScopeBranch, switch scope to ScopeBranch so the pick is visible.
- Header: when ScopeBranch with an override, the existing BaseDesc pill shows `<ref>@<short>` already via describe — verify.
- Footer gains `{"B", "base"}` in review.

Tests: stored base drives vs-base (build two branches, set base to the second, assert files diffed against it); invalid stored base surfaces an error and no silent fallback; `B` picker lists refs incl. `auto`, picking persists and reloads, `auto` clears; base on repo A does not apply to repo B (per-repo keying); scope auto-switches to vs base on pick.

---

### Task 3: Docs + verify + ship

README: `review-base` beside `review-repo` (agent runs it from its worktree; one ref; `--clear`), `B` in the review key list. No em/en dashes, positive statements. Full gates. Live verify against the real repo. Push. Final whole-branch review, fix criticals, merge #39, release.

## Self-Review
Spec section "base branch" fully mapped: subcommand T1, storage T1, resolution+error T2, picker T2 (`B` per the spec's deferred-base note), docs T3. Types: `BuildSet` signature change ripples to `diffview.go` callers and `main_test`/diff tests — T2 owns all call sites. `ReadReviewBase` two-line format defined T1, consumed T1 poller only.
