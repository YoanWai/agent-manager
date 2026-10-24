# Architecture

How the code is laid out, what CI enforces, and why. Product rules and
invariants live in [AGENTS.md](../AGENTS.md).

## Packages

Under `internal/`:

- `app`: assembles the local services and owns their lifetime.
- `sessioncmd`: the session commands (spawn, archive, revive, messages, tasks). `Lifecycle` runs ordered effects.
- `cli`, `mcpserver`: translate shell and MCP requests into `sessioncmd` commands.
- `execution`: UI-free poller and runner: capture, delivery, mailbox, status, notifications.
- `ui`: the Bubble Tea program. One `Model` routes messages, runs effects, composes the frame.
- `ui/help`, `ui/review`, `ui/focus`, `ui/rail`, `ui/settings`: feature packages with private models.
- `ui/connections`: the SSH connections: their list and each host's last refresh, the row a selection names, the selected row's screen and the add and edit dialog. The root's `connections_adapter*.go` paints them and routes actions; see [SSH connections](ssh-connections.md).
- `ui/connections/link`: the connections' runtime: the SSH client, the store writes that keep the list, the host polls, the screen reads and the attach. The root answers its `Root` and runs its calls on the remote lane.
- `remote`: drives another host's agent-manager over `ssh`, and holds the rule that a session drives only the remote terminals it opened. `remote/value`: what a connection is, row refs and failure kinds, with no SSH.
- `ui/spawn`, `ui/quick`: the New Session form with its model choice, and the quick bar; the root keeps their prompt box and runs their spawn.
- `ui/dialogs`: the confirm, rename, move, fork and launch fix dialogs.
- `ui/notices`: the messages panel, its notice list and release-note rendering.
- `ui/notices/release`: the panel's runtime: release and feed checks, the what's-new greeting recorded in settings, and the self-update.
- `ui/focus/watch`: the focused pane's watcher: control-mode client, polled fallback, pushed frames.
- Runtime packages. Each holds one feature's state and the store, tmux, git and network work behind it. It answers a narrow `Root` the root adapts, and never imports `ui`:
  - `ui/settings/persist`: the cached stored values, the writes a save hands the effect lane, and the read-back a partial failure reconciles from.
  - `ui/launchhint/install`: the installer the setup-step card types into a shell tab, the status that shell records, and the launch retried once the binary is on PATH.
  - `ui/rail/persist`: the Rail's stored folds and order, and the group and session placement writes.
  - `ui/dialogs/apply`: the fork branch, the group and session renames, and the store and git writes they make.
  - `ui/focus/runtime`: the pane watcher, the writes forwarded to the agent's pane, and the entry and detach probes.
  - `ui/geometry/panes`: pinning each session's window to the preview box, the sizes it keeps between passes, the box it publishes for launches with no manager, and the divider ratio.
  - `ui/preview/capture`: the preview's capture timer and settle, and the composer drafts held until their session turns working.
  - `ui/review/source`: the git, store and tmux work for a Review load, draft save, comment mark, round send, base pick and picker read.
  - `ui/session/lifecycle`: archive, restore, delete, kill, restart, revive, after-turn and attach, with the ledger a stale poll is reconciled against.
  - `ui/spawn/launch`: the spawn and group creation, the catalog and worktree answers, and the base ref and its fetch.
- `ui/theme`: the built-in palettes and color math, a leaf any UI package may import.
- `ui/presentation`: pure text helpers shared by `ui` and its feature packages.
- `ui/surface`: the surface stack's contract, a leaf a feature package implements to put its own card on the root's stack. `ui/help`, `ui/review`, `ui/notices`, `ui/connections`, `ui/settings`, `ui/dialogs`, `ui/geometry` and `ui/focus` do.
- `store`: SQLite state: sessions, groups, settings, inbox, tasks, reservations.
- `tmux`: the `Driver` on the dedicated socket, the control-mode client, bindings, attach.
- `status`: classifies pane output into agent states.
- `config`: built-in tool definitions and CLI install.
- `catalog`: asks each agent CLI for its models, efforts and profiles. `catalog/value`: what it answered.
- `launch`: builds the shell command and environment for a managed session.
- `agentsession`, `conversation`, `sessionreport`: find and track the conversation an agent is on.
- `hooks`: Claude Code hook events written to status files.
- `mcpreg`: registers the MCP server in spawned sessions.
- `git`: git subprocesses and filesystem discovery. `git/value`: pure scope, repo and file values.
- `diff`: loads changed files through `git`. `diff/model`: pure diff and line models.
- `keybind`: key names and their spellings per surface.
- `clipboard`, `notify`, `termseq`, `systheme`, `sysstat`: clipboard, notifications, terminal escapes, OS theme, host stats.
- `update`, `feed`, `report`: release checks, message feed, bug and feature reports.
- `atomicfile`, `deps`, `pwsh`, `proctree`, `wsl`: small helpers for file writes, install hints, PowerShell, Windows processes, WSL detection.
- `testenv`, `testfixture`: test helpers.

## Rules CI enforces

Each check runs in `.github/workflows/ci.yml` or in `go test ./...`.

- **Dependency boundaries.** `go run ./tools/architecture/check-ui-boundaries` lists each package's transitive production imports and fails on any other package of this module. Allowed, besides itself: `ui/help` imports `keybind`, `ui/presentation` and `ui/surface`; `ui/notices` imports `keybind`, `ui/presentation` and `ui/surface`; `ui/notices/release` imports `ui/notices` with its imports, `ui/terminal` with its imports, `config` (with its `deps`, `tmux`, `update`, `wsl`, and on Windows `proctree` and `pwsh`) and `feed`; `ui/settings` imports `keybind`, `ui/presentation`, `ui/theme` and `ui/surface`; `ui/geometry` imports `keybind` and `ui/surface`; `ui/focus` imports `keybind`, `ui/spawn` and its imports, and `ui/surface`; `ui/rail` imports `keybind`, `ui/presentation`, `ui/theme` and `remote/value`; `ui/review` imports `diff/model`, `git/value`, `ui/presentation` and `ui/surface`; `ui/spawn` imports `catalog/value` and `ui/presentation`; `ui/quick` imports those, `ui/spawn`, `ui/composer` with its imports, and `ui/surface`; `ui/pathcomplete` imports `ui/presentation`; `ui/composer` imports `clipboard` (with its `termseq` and `wsl`) and `ui/spawn` with its imports; `ui/connections` imports `remote/value`, `ui/dialogs` with its imports, `ui/presentation` and `ui/surface`; `ui/connections/link` is a runtime package, so it imports `remote`, `sessioncmd`, `store`, `ui/rail`, `ui/terminal` and what those need; `diff/model` imports `git/value`; `ui/focus/watch` imports `tmux` (with its `deps`, `keybind`, and on Windows `proctree` and `pwsh`); `ui/dialogs` imports `ui/presentation`, `ui/surface`, `ui/pathcomplete` and `ui/spawn` with its imports; `ui/theme`, `ui/surface`, `git/value`, `catalog/value`, `remote/value` and `ui/presentation` import nothing from the module. The runtime packages listed under Packages import runtime packages too; each line in the tool names its whole graph, and none reaches `ui`. No other feature package imports `remote` or `sessioncmd`. Test imports are not checked.
- **File names.** `go run ./tools/architecture/check-file-names`. Every non-test file in `internal/ui` starts with a feature name from the tool's `packaged` or `rootOnly` map (plus `keys.go`, `mouse.go`, `updates.go`, `ids.go`). A packaged feature has its own package under `internal/ui`, and its root files are only `<feature>_adapter*.go` and `*_effect.go`, besides the older files in `rootExtras`, a list that only shrinks. `rootOnly` is closed: a new feature starts as a package and joins `packaged`. Every `_test.go` file pairs with a source beside it (`<source>_test.go` or `<source>_<aspect>_test.go`), except `*_helpers_test.go`, `*_bench_test.go`, `main_test.go` and the `allowed` list in the tool.
- **File sizes.** `go run ./tools/architecture/check-file-sizes`. A non-test Go
  file of 1,000 lines or more fails. Split by concern.
- **Host width.** `go test ./internal/ui -run TestFeatureHostsStayNarrow`
  (`internal/ui/model_host_test.go`). Every `*Host` interface in `internal/ui`,
  embedded hosts included, has at most 8 methods, and no method mentions `Model`,
  `services`, `effectServices`, `effectRequest`, `store.Store`, `tmux.Driver` or
  `git.Driver`. A feature that needs more returns a typed request that a root
  adapter runs.

- **Mouse coverage.** `go test ./internal/ui -run TestEveryModeAnswersTheMouse` (`internal/ui/mouse_coverage_test.go`). Every `mode` has a case in `clickCard` and in `wheelCard`, or an entry in that file's exemptions saying why it does without, such as a card with nothing to scroll. A new mode fails until it does one or the other, so a screen cannot ship keyboard-only.

Not in CI: `go run ./tools/architecture/check-ui-moves BASE TARGET` (TARGET may
be `WORKTREE`) confirms a move or split within a package kept every declaration
and comment, so a pure-move commit can be reviewed as one. Imports and file
locations may change; anything else fails it.

## Decisions

**Effects run off `Update` on ordered lanes.** `Update` never blocks. A
feature captures a value request (target, writers, configuration, pane size)
and the root queues it on one FIFO lane (`internal/ui/effects`, routed by `internal/ui/effects_adapter.go`). One job
runs at a time, workers never read `Model`, and a completion applies durable
results before it reports an error, so a partial failure still shows what
committed. Request generations reject stale replies, and quit drains accepted
jobs. Raw keys, mouse and paste run on a second lane, so typing never waits behind
a spawn or a poll-locked reflow. `View` returns a frame prepared after
dispatch; it changes no state.

**Features own state behind narrow hosts, and new ones start as child packages.** A feature is a type with its state, keys and view, and it reaches the root only through a small host interface it declares (`pathComplete`, `composer`, and the rest in `internal/ui`). Help, Review, Focus, Rail, Settings, Spawn, Quick, Dialogs, Notices and Connections each live in a child package whose imports `check-ui-boundaries` enforces; a new feature starts as one. The child gets copied values and returns typed requests; root adapters run the store, tmux, git and SSH work. There is no shared event bus or effect interpreter, because it would hide who owns what. Line counts and field counts do not prove a boundary; the checks above do.

**Runtime resources stay with the root.** The tmux control client's mechanics
live in `ui/focus/watch`; the root holds and closes the instance (`focus_adapter_watch.go`).
The clipboard (`focus_adapter.go`, `composer.go`) and the IME cursor
(`frame_adapter_ime.go`) are held and published by `internal/ui`. Feature packages
see copied pane facts and hand back typed outcomes. That keeps the child packages
testable without tmux or a terminal, and keeps one place that closes what it
opened on quit.

**Theme styles are published whole.** `applyTheme` builds a palette's styles
and swaps them in with one atomic pointer (`internal/ui/theme/styles.go`), so
Models on several goroutines never race on them; a `Styles` value on `Model`
would have to thread through every free render helper for no other gain.

**Platform differences live in `*_unix.go` and `*_windows.go` files.** A
branch on `runtime.GOOS` in shared code hides the other platform. Instead each
package splits the difference into a file pair behind a build tag
(`internal/ui/launchhint/install/script_unix.go`, `internal/tmux/platform_windows.go`,
`internal/store/delivery_guard_unix.go`), so the counterpart is a file you can
see is missing. AGENTS.md requires the counterpart or a named skip.

**One spawn path through `sessioncmd.Lifecycle.Spawn`.** The New Session form,
the quick bar, terminal spawns (all through `internal/ui/spawn_effect.go`) and
the CLI and MCP `spawn` (`internal/sessioncmd/spawn.go`) all call it. It opens
the requested worktree, fills the row from the launch plan, launches, and hands
the worktree back if the launch fails. Two paths would drift on exactly those
failure cases. `Lifecycle` likewise owns the ordered durable and pane effects
of the other session commands, and each frontend keeps its own policy: a human
archive reconciles selection, a session archive refuses to archive itself.

**Quit drains accepted work.** A quit refuses new work and drains the jobs
already accepted, plus the Rail follow-ups they cause, before `tea.Quit`; a
running or unsettled installer refuses a clean quit instead. Git and filesystem
work share no deadline, so the drain has no fixed bound: a second quit stops
waiting, drops the jobs that never started, and the manager exits non-zero
naming each as "never started" or "was still running". Shutdown then waits at
most five seconds (`effectStopWait`) for begun effects and reports the same way.

**Automatic delivery is at most once.** The poller's inbox and pending-input
delivery (`internal/execution/polldelivery.go`) claims a row with a
compare-and-set before the paste and records it delivered or dropped after. A
claim left behind by a manager that died between the two is retired, never
typed again: a message once its claim is older than `inboxClaimGrace`, a
pending input on the next poll. The schema is unchanged from `main`, so older
and newer binaries can share a profile.
