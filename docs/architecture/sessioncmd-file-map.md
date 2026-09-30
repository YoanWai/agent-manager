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

The baseline is commit `dc43a4f3a4316e58c5cfb20f39a1006227ed9e3a`. Before and after the move, a Go AST inventory compared all 318 package declarations, including tests. Each declaration body and its attached documentation had the same SHA-256 fingerprint. Imports were recomputed for each destination file, then gofmt was applied.

The comparison excludes file placement and import blocks. Build, vet, and the full isolated race suite provide the separate compilation and behavioral checks. No new behavioral test is needed for an exact move; existing tests retain their bodies and assertions.

The file split does not fix synchronous UI lifecycle effects, rendering mutations, partial-result reconciliation, or production execution authority. Those gaps remain in the [conformance audit](roadmap-and-evidence.md).
