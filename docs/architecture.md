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
- `ui/effects`: the effect contract (`Request`, `Runner[S]`, `Bind`, `Result`), the FIFO lanes and the quit drain's bookkeeping. It knows no request type and imports nothing from the module.
- `ui/composer`, `ui/pathcomplete`: the prompt box that holds pasted images, and directory completion for a path field.
- `ui/frame`, `ui/statusbar`, `ui/startup`, `ui/preview`, `ui/workspace`: pure painters and values for the card chrome and header, the status bar and its toast, the startup wordmark, the pane preview and the rail foot readings.
- `ui/session`, `ui/terminal`: the value-only parts of the session lifecycle (editor lines, unreachable-session messages), the copy and fix a refused launch shows, and what takes over the terminal (links, child processes).
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
  - `ui/launchhint/install`: the copy a refused launch shows, and the installer the setup-step card types into a shell tab, the status that shell records, and the launch retried once the binary is on PATH.
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
- `ui/surface`: the surface stack's contract, a leaf a feature package implements to put its own screen on the root's stack. `ui/help`, `ui/review`, `ui/notices`, `ui/connections`, `ui/settings`, `ui/dialogs`, `ui/geometry`, `ui/focus`, `ui/spawn` and `ui/quick` do. See [ADR 0001](adr/0001-surface-stack.md).
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

- **Dependency boundaries.** `go run ./tools/architecture/check-ui-boundaries` lists each package's transitive production imports on darwin, linux and windows and fails on any other package of this module. The allowed lists live in `tools/architecture/check-ui-boundaries/boundaries.go`, one line per package, and `-write` regenerates them; a package not marked runtime that reaches the store, tmux, git, config, status or SSH fails both the check and the rewrite, so a new import arrives as a reviewed diff and never as a quiet widening. Leaves (`ui/surface`, `ui/effects`, `ui/presentation`, `git/value`, `catalog/value`, `remote/value`) import nothing from the module, and `ui/theme` only `systheme` and `ui/effects`; runtime packages (`*/persist`, `*/runtime`, `*/source`, `*/link`, `*/install`, `*/lifecycle`, `*/launch`, `*/apply`) may reach the store, tmux, git and SSH. No boundary lists `internal/ui`, so a child package that imports the root fails. The runtime packages listed under Packages import runtime packages too; each line in the tool names its whole graph, and none reaches `ui`. No other feature package imports `remote` or `sessioncmd`. Test imports are not checked.
- **File names.** `go run ./tools/architecture/check-file-names`. Every non-test file in `internal/ui` starts with a feature name from the tool's `packaged` or `rootOnly` map (plus `keys.go`, `mouse.go`, `updates.go`, `ids.go`). A packaged feature has its own package under `internal/ui`, and its root files are only `<feature>_adapter*.go`, `*_effect.go` and `*_surface.go`, besides the older files in `rootExtras`, a list that only shrinks. Those files adapt: the `<feature>Root` types the package's `Root` interface is answered by, the effect binding and the surface opener. State and view stay in the package. `rootOnly` is closed: a new feature starts as a package and joins `packaged`. Every `_test.go` file pairs with a source beside it (`<source>_test.go` or `<source>_<aspect>_test.go`), except `*_helpers_test.go`, `*_bench_test.go`, `main_test.go` and the `allowed` list in the tool.
- **File sizes.** `go run ./tools/architecture/check-file-sizes`. A non-test Go
  file of 1,000 lines or more fails. Split by concern.
- **Host width.** `go test ./internal/ui -run TestFeatureHostsStayNarrow`
  (`internal/ui/model_host_test.go`). Every `*Host` interface in `internal/ui`,
  embedded hosts included, has at most 8 methods, and no method mentions `Model`,
  `services`, `effectServices`, `effectRequest`, `uieffects.Request`, `store.Store`,
  `tmux.Driver` or `git.Driver`. The test reads the root's `*Host` and `Root`
  interfaces and every child package's `*Root`. A feature that needs more returns
  a typed request that a root adapter runs.
- **Effect coverage.** `go test ./internal/ui -run TestEveryPackageEffectIsBoundAndApplied` (`internal/ui/effects_adapter_registry_test.go`). Every request a feature package declares has its `Services` bound in `bindEffect`, and every result has a case in `applyEffect`.
- **Surface coverage.** `go test ./internal/ui -run TestEverySurfaceAnswersTheMouse` (`internal/ui/mouse_test.go`). Every type with a `Leave(surface.Host)` method, in the root or a child package, needs an opener in `mouseOpeners` and must take a wheel and a click without touching the stack under it, so a screen cannot ship keyboard-only. `TestModelFieldCap` caps `Model` at 21 fields, so state goes to its feature.

Not in CI: `go run ./tools/architecture/check-ui-moves BASE TARGET` (TARGET may
be `WORKTREE`) confirms a move or split within a package kept every declaration
and comment, so a pure-move commit can be reviewed as one. Imports and file
locations may change; anything else fails it.

## Decisions

**Effects run off `Update` on ordered lanes.** `Update` never blocks. A
feature captures a value request (target, writers, configuration, pane size)
and the root queues it on a FIFO lane (`internal/ui/effects`, routed by `internal/ui/effects_adapter.go`). One job
runs at a time per lane, workers never read `Model`, and a completion applies durable
results before it reports an error, so a partial failure still shows what
committed. Request generations reject stale replies, and quit drains accepted
jobs. Raw keys, mouse and paste run on a second lane, so typing never waits behind
a spawn or a poll-locked reflow. `View` returns a frame prepared after
dispatch; it changes no state.

**Each feature package owns its effects.** `ui/effects` holds the contract and no request type. A feature package declares its requests (`EffectName`, `EffectCopy`, `Run(Services)`), its results, and a `Services` struct of what the work needs. The root names no request. `effects_adapter_executor.go` has two switches: `bindEffect`, one `uieffects.Bind` line per package `Services`, and `applyEffect`, one type-switch case per package result that hands the result to the feature's adapter. There is no root request type or catch-all. A request with no binding fails with "binds no services", and `TestEveryPackageEffectIsBoundAndApplied` fails first.

**One surface stack decides who takes input.** The base screen is the list or Review; cards, resize and focus sit over it as surfaces, and the root walks the stack from the top for keys, mouse, frame and footer. The `mode` enum is gone. [ADR 0001](adr/0001-surface-stack.md) has the contract and the rules.

**Features own state behind narrow hosts, and new ones start as child packages.** A feature is a type with its state, keys and view, and it reaches the root only through a small `Root` or host interface it declares. Help, Review, Focus, Rail, Settings, Spawn, Quick, Dialogs, Notices and Connections each live in a child package whose imports `check-ui-boundaries` enforces; a new feature starts as one. The child gets copied values and returns typed requests; root adapters run the store, tmux, git and SSH work. State flows from the package to the root as returned values: `rail.Model` returns a `Decision` carrying an `Intent` and `Mutation`s, and the root applies them. A `Root` or host interface is for reads. Some existing ones still carry writes (`quick.Root.ClearAccepted`); new code returns the value. There is no shared event bus or effect interpreter, because it would hide who owns what. Line counts and field counts do not prove a boundary; the checks above do.

**Runtime resources stay with the root.** The tmux control client's mechanics
live in `ui/focus/watch`; the root holds and closes the instance (`focus_adapter_watch.go`).
The clipboard (`focus_adapter.go`, `composer_adapter.go`) and the IME cursor
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
the quick bar, terminal spawns (all through `internal/ui/spawn_adapter_effect.go`) and
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
