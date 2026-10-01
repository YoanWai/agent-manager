# Session command file map

This change moves declarations within package `sessioncmd`. It changes no type, signature, declaration body, attached declaration comment, or call path. No compatibility shim or application layer is added.

## Find each concern

| Concern | Source | Tests |
| --- | --- | --- |
| Shared command runtime and caller binding | [runtime.go](../../internal/sessioncmd/runtime.go) and existing [backend.go](../../internal/sessioncmd/backend.go) | Existing backend ownership and refresh tests |
| Session inventory and reads | [session.go](../../internal/sessioncmd/session.go) | [session_test.go](../../internal/sessioncmd/session_test.go), which also retains the shared session harness |
| Group commands and parent paths | [group.go](../../internal/sessioncmd/group.go) | [group_test.go](../../internal/sessioncmd/group_test.go) |
| Session spawning and shared target resolution | [spawn.go](../../internal/sessioncmd/spawn.go) | [spawn_test.go](../../internal/sessioncmd/spawn_test.go) and terminal create tests |
| Session messages and readiness policy | [messages.go](../../internal/sessioncmd/messages.go) | [messages_test.go](../../internal/sessioncmd/messages_test.go) and existing wait tests |
| Session-scoped lifecycle callers | [session_lifecycle.go](../../internal/sessioncmd/session_lifecycle.go) | [session_lifecycle_test.go](../../internal/sessioncmd/session_lifecycle_test.go) |
| Canonical lifecycle implementation | Existing [lifecycle.go](../../internal/sessioncmd/lifecycle.go) | Existing backend lifecycle contract tests |
| Mailbox commands and request validation | [mailbox.go](../../internal/sessioncmd/mailbox.go) | [mailbox_test.go](../../internal/sessioncmd/mailbox_test.go) |
| Terminal commands | [terminal.go](../../internal/sessioncmd/terminal.go) | Existing [terminal_test.go](../../internal/sessioncmd/terminal_test.go) |

`mailbox.go` and its test are exact renames of `sessioncmd.go` and `sessioncmd_test.go`. Runtime declarations previously lived in `terminal.go`, except `runtime.managerAwake`, which came from `session.go`. Group, spawn, message, and session lifecycle declarations previously shared `session.go`. Shared group and spawn helpers move out of `terminal.go` alongside their consumers.

Task, reservation, wait, relaunch, vocabulary, format, owner, backend, and canonical lifecycle files retain their existing implementation. Shared test fixtures remain available throughout the package.

## Verify the mechanical scope

The mechanical split is commit `3a9ffc5a3108352807b930922b0ae2d9182feba1`, against baseline `dc43a4f3a4316e58c5cfb20f39a1006227ed9e3a`. Before and after the move, a Go AST inventory compared all 318 package declarations, including tests. Each declaration body and its attached documentation had the same SHA-256 fingerprint. Imports were recomputed for each destination file, then gofmt was applied.

The comparison excludes file placement and import blocks. Build, vet, and the full isolated race suite provide the separate compilation and behavioral checks. No new behavioral test is needed for an exact move; existing tests retain their bodies and assertions.

The mechanical file split did not establish feature ownership or resolve effect policy. Later increments made View read-only; ordered UI lifecycle commands and partial-result reconciliation are implemented; remaining synchronous families and production execution authority remain tracked in the [conformance audit](roadmap-and-evidence.md).

## Reduce irrelevant fixture startup

A separate test-only follow-up gives 11 task and reservation tests, plus the group subtree test, a store-only session harness. It retains real config loading and independent SQLite command connections. The two group tests that verify spawning and live-pane survival still use real tmux, as do the other pane integration tests.

The selected 14-test race run on the local macOS host took 14.660 seconds before the fixture change and 5.747 seconds after it. Both runs used the same worktree, socket directory, test selection, and race settings. They were uncached and passed. The real-pane harness remains byte-identical to the baseline. This is a measured sample, not a promised duration on every platform. The declaration equivalence check above applies to the mechanical split; the fixture follow-up intentionally changes test setup while keeping production declarations identical.

Whole-package timing samples did not establish an overall speedup. The final full package race run passed all 109 top-level tests in 118.040 seconds; earlier baseline samples were 86.828 to 90.937 seconds. Those results remain an unresolved measurement limit. This commit claims faster SQLite-only fixture coverage, not faster whole-suite execution.

To repeat the focused comparison on each revision, run:

```sh
mkdir -p /tmp/am-fast-tests
env -u TMUX TMUX_TMPDIR=/tmp/am-fast-tests go test -race -count=1 ./internal/sessioncmd \
	-run 'Test(Tasks|Dependencies|OnlyUnfinishedDependencies|ReleasedAndDeletedTasks|DeletingASession|Racing|Reservations|SharedLeases|ALapsedLease|ReleasingBlankPaths|SessionGroups|DeleteGroup)'
```

Use an isolated shell startup environment when host configuration prints into fixture panes. Preserve the full race suite as the integration gate. Use the focused selection for this test-fixture comparison, not as a substitute for the full suite.

## Bind mailbox mutations to the command runtime

A subsequent behavioral fix adds [backend_mailbox.go](../../internal/sessioncmd/backend_mailbox.go): explicitly bound CLI/MCP rename, review repo/base/scope, and review-comment mutations use the backend's hooks and store. A closed backend rejects these operations without falling back to a supplied directory. [mailbox_commands.go](../../internal/sessioncmd/mailbox_commands.go) preserves directory-based standalone callers. The shared validation and mailbox algorithms remain in mailbox.go.

[CLI regressions](../../internal/cli/backend_review_test.go) and [MCP regressions](../../internal/mcpserver/backend_review_test.go) cover every mailbox mutation after backend closure and with a different adapter profile. The alternate profile remains empty; borrowed stores remain caller-owned. These local adapter checks do not establish remote authority or released-client compatibility.
