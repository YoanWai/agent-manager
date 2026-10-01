# Upstream adoption and rollback

This proposal is a source for small upstream changes, not a merge candidate as one unit. Its combined change set is too large for one review, it contains follow-up work beyond the historical proposal head, and its release evidence does not cover every provider, owner, transport, or supported platform.

## Record the fork point, then refresh upstream separately

The proposal-head comparison below was recorded on 2026-10-01 against the fork's `origin/main`. A separate fetch of `https://github.com/YoanWai/agent-manager.git` then advanced the local `upstream/main` ref. Keep both facts: the first explains the proposal history, while the second is the base an upstream adoption branch must use.

```sh
main_sha=$(git ls-remote origin refs/heads/main | awk '{print $1}')
git merge-base HEAD "$main_sha"
git rev-list --left-right --count "$main_sha"...HEAD
git diff --shortstat "$main_sha"...HEAD
git rev-parse upstream/main
git rev-list --left-right --count upstream/main...HEAD
git diff --shortstat "$main_sha"..upstream/main
```

| Item | Observed value |
| --- | --- |
| Fork `origin/main` snapshot | `dc471a97f3ca4fdd06eb822a31e8e5119c39b997` |
| Recorded proposal head | `7c7c58990a6f197e2b0f097c0d3660e2cd43040b` |
| Merge base | `dc471a97f3ca4fdd06eb822a31e8e5119c39b997` |
| Recorded proposal distance from its fork | 0 behind, 37 ahead |
| Recorded proposal three-dot diff | 403 files, 50,188 insertions, 32,221 deletions |
| Freshly fetched `upstream/main` | `d84f3b024f4c8eb8b9eb6a523b29de3586995cb3` |
| Proposal-head divergence from fresh upstream | 12 upstream commits absent, 37 proposal commits absent upstream |
| Upstream changes since the fork point | 104 files, 5,660 insertions, 168 deletions |

More than half of the recorded proposal's changed-file distribution is under `internal/ui`; `internal/sessioncmd` and `internal/execution` are the next largest production areas. File extraction accounts for much of the volume, but the branch also changes runtime ownership, asynchronous effects, coordination behavior, storage schema, and acceptance tooling. The 12 newer upstream commits include catalog, model/profile, and Oh My Pi work that this branch has not reconciled. The later follow-up is also absent from the proposal-head statistics above. Fetch current upstream and regenerate both comparisons before creating each slice.

## Adopt in five bounded stages

| Stage | Upstream unit | Evidence required before merge | Rollback boundary |
| --- | --- | --- | --- |
| 1. Reconstruct the baseline | Create a fresh branch from freshly fetched upstream `main`. Record the source commits and rebuild a minimal patch rather than merging or rebasing this proposal wholesale. Establish baseline full-race, build, vet, format, and actual-binary smoke results. | A clean baseline and a reviewed file/behavior inventory. No proposal code lands in this stage. | Delete the staging branch; production is unchanged. |
| 2. Separate application and commands | Land explicit application/execution composition and shared lifecycle seams in one reviewable unit. Land the mechanical session command concern split and store-only test fixture change separately. Preserve all command vocabulary and real tmux lifecycle tests. | Focused backend lifecycle contracts, store-backed task claim races with independent SQLite connections, the process/race selections, then the full race suite. | A composition-only or mechanical command slice contains no schema migration when extracted at this boundary: revert that slice before dependent UI work and keep the database. This rollback statement does not apply to the later delivery-receipt slice. |
| 3. Extract UI features | First land concern-only file organization. Then adopt Help, Review, Focus, and Rail as one feature PR at a time, retaining root adapters, cross-feature navigation, and final frame composition. Add the dependency guard with the first package it can validate. Land the local fast gate only after all packages it names exist. | Pure feature tests, alias/ownership tests, production dependency guards, root dispatch contracts, actual-binary smoke, and the full suite for each feature. | Revert one feature and its adapter together. Do not revert unrelated packages or rewrite profile state. |
| 4. Move effects off Update | Adopt one effect family per PR: lifecycle and Rail first, then geometry/attach, spawn/fork/group, rename/move, settings/keys, Review, and Focus/detach. Preserve FIFO ordering, captured identity, generation fences, partial durable reconciliation, and accepted-work drain on shutdown. | Blocked-adapter tests, stale-completion tests, partial-failure tests, the process/race matrix, relevant E2E scenarios, and the complete race suite. | Stop the candidate manager cleanly, revert the affected family, and restart one prior-version manager. Concurrent old/new managers on one profile are outside the proven authority model. |
| 5. Isolate product changes and pilot | Review coordination-mode behavior and new CLI/provider support as product PRs, separate from the architecture adoption. Run compatibility and process gates against built artifacts. Start with disposable profiles, then at most one explicitly approved single-manager profile. | Released-client matrix for named contracts, real TUI scenarios, a rollback rehearsal, and fresh hosted checks for the exact head. Expand only after the unsupported acceptance rows below are closed. | Stop only the owned manager and private socket, retain its artifacts and a database copy, restore the previous binary, and verify reads before resuming work. Never use a default-socket `tmux kill-server` as rollback. |

The source commits are an audit map rather than a safe cherry-pick sequence. `4097f7c` and `f7d76d4` identify the application/lifecycle foundation; `3a9ffc5` and `560a463` identify the command split and fixture change; `7773665` through the Help, Review, Focus, and Rail commits identify feature extraction; the later `d5ad28b` through `7c7c589` series contains effect and pilot behavior. Rebuild each unit on fresh main because later commits reconcile earlier designs and incorporate an upstream merge.

## Use escalating gates without weakening CI

Run [the fast gate](../../tools/testing/README.md) during implementation. Run its separate process/race matrix whenever the slice touches goroutine ordering, SQLite writers, the application runner, session lifecycle, or tmux. Before merge, keep the repository's complete race suite and hosted CI unchanged.

Use the [real TUI harness](../../tools/e2e/README.md) for binary, terminal, and adapter wiring. Use the [compatibility matrix](../../tools/compatibility/README.md) for the released CLI/MCP versions and two-manager cases it explicitly names. Passing a lower gate never waives a higher gate; a failing selected test remains a blocker rather than a reason to remove it from a selection.

## Do not claim acceptance that the evidence does not provide

| Acceptance area | Current evidence | Required before claiming support |
| --- | --- | --- |
| Real providers | Synthetic CLIs and installer fixtures cover command wiring, retries, and seeded conversation identity. | Exercise supported vendor installers and real provider session-store discovery, including ambiguous and failed discovery. |
| Extensions and mutations | Released v0.38.0/v0.39.0 checks cover named CLI/MCP reads plus sequential and concurrent task contracts. | Test installed extensions and every mutation family promised across supported version combinations. |
| Automatic delivery authority | Current cooperating owners hold the profile [delivery guard](delivery-ownership.md) from durable admission through bounded tmux transport and the token-matched receipt. A two-runner test with independent SQLite connections and a real pane proves that the peer skips while delivery is in flight and that one confirmed receipt remains. The late-paste result in [In-flight delivery](in-flight-delivery.md) is historical evidence from the pre-guard implementation, not the current expected outcome. | Perform the documented offline cutover: stop every legacy writer and already admitted transport, back up DB/WAL state, install the receipt schema and writer fences with one current binary, then move all automatic writers and receipt readers together. The guard does not claim authority over human keyboard/mouse/paste input, and it does not establish remote or saved-connection authorization. Mixed legacy/current writers remain unsupported. |
| Installer interruption | Clean quit is refused while an installer is starting or tracked, and the user can finish it, kill its terminal with the session controls, or attach and interrupt it. | Persist installer status, captured retry, and image ownership before claiming recovery after a process crash, SIGKILL, host restart, or power loss. Those recovery cases are currently unsupported. |
| Remote transport | The production proposal contains no completed saved-connection or SSH adapter rollout. | Test real SSH setup, reconnect, failure, generation changes, scoped authorization, and cleanup on supported hosts. |
| Platforms and terminals | Local macOS, hosted Linux checks for earlier published heads, private tmux process tests, and controlled terminal fixtures provide bounded evidence. | Run fresh hosted checks for the exact candidate and the declared macOS, Linux, WSL/SSH, terminal, input, and architecture matrix. |

The frozen working tree therefore supports architecture review, disposable local gates against its recorded fork, and the guarded automatic-delivery contract for cooperating current owners after the offline cutover. It does not establish reconciliation with fresh upstream, current hosted CI, real-provider acceptance, remote workspace support, authority over human pane input, or a mixed legacy/current rollout.

## Rehearse rollback before a live pilot

1. Build and checksum both the candidate and previous known-good binary; record the exact commits and gate artifacts.
2. Stop every manager, CLI, and MCP writer for the selected disposable profile. Copy its config plus the SQLite database and any WAL state as one offline rollback set. Never begin with a live developer profile.
3. Start one candidate manager on a private socket, exercise the stage's read and mutation paths, and capture the observable result.
4. Stop that exact process. For a code-only slice whose schema comparison is empty, restore the previous binary without rewriting the database. For the delivery-receipt migration, restore the pre-migration database/WAL rollback set offline before starting the old binary; its historical writer is intentionally fenced from the migrated schema. Start one manager and verify sessions, groups, tasks, reservations, inbox state, receipts, and archived rows remain readable.
5. Abort expansion on duplicate effects, ambiguous ownership, stale completions changing reopened UI, SQLite errors, provider identity ambiguity, or a command targeting an unowned tmux server. Preserve the failed profile and logs for diagnosis.

Database restoration is a separate recovery action for slices that migrate storage. The current follow-up adds attempt-token, receipt, outcome, and writer-fence migrations described in [Delivery ownership](delivery-ownership.md), so a whole-worktree rollback requires the offline pre-migration database/WAL set. Each upstream slice must state its own schema delta, old-writer policy, and forward/backward rehearsal instead of inheriting the composition-only rollback rule.

Do not stop the candidate while its UI reports a tracked installer. A clean quit refuses and names the installer terminal to finish, kill, or attach. A process crash, SIGKILL, host restart, or power loss can leave that durable terminal row without the in-memory status watcher, captured retry, or prompt-image ownership; inspect and resolve that shell manually rather than starting the installer again blindly.
