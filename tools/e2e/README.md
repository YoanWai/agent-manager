# Real TUI smoke

From a fresh checkout with Go, Git, tmux, `/bin/sh`, and Python 3.9+ installed:

```sh
python3 -m unittest discover -s tools/e2e -p 'test_*.py'
python3 tools/e2e/smoke.py
```

The smoke builds the repository binary once before changing HOME. Reuse a build
with `--binary /absolute/path/agent-manager`. `--artifacts /new/path` selects a
new evidence directory; the default is a unique directory in the system temp
folder. Output includes its path, result, and total duration; `timings.jsonl`
records every observed transition. Keep the smoke separate from the fast Go suite. The observation budget is 12 seconds per
transition; a successful cached-build run should take less than 30 seconds.

The harness creates its own HOME (the default profile therefore belongs to this
run), XDG directories, and shell configuration directory. It removes inherited
TMUX and uses a short, unique, precreated `/tmp/ame-*` socket directory. Every
tmux command uses an absolute `-S` socket path within the fixture. Cleanup kills only these two
servers within that directory, including on failure. A failed cleanup still
attempts the other server and retains the socket directory for recovery.
Failure capture errors are reported without replacing the original failure.
It retains profile state, frames, command output, return codes, and `result.json` on success and failure.
Artifacts contain only the disposable fixture; remove the reported directory
when its evidence is no longer needed.

The terminal is explicitly 110×30 with `xterm-256color`, truecolor capability,
`NO_COLOR=1`, and a controlled wrapper that executes `/bin/sh -i` with a
unique prompt. Git environment overrides and global/system configuration are
removed from the disposable repository fixture. Assertions cover Help open,
search entry, typing,
commit, clear, wheel consumption followed by acknowledged keyboard scrolling,
close, a terminal row, a real managed shell accepting a unique file-writing
command, a real Git diff and review Help, focused input and detach, rail filtering
and folding/unfolding a UI-created group, and a zero-exit manager process.
Before the first command, the harness waits for the unique shell prompt in the
managed pane. A command acknowledgement file then proves execution. The Help
wheel assertion compares the exact final frame with a keyboard-only PageDown
baseline, asserting Help remains open at the exact keyboard-only scroll
position. Poll waits require an observable condition; the 50ms cadence is never a transition delay.

This is bounded local tmux evidence, not the supported terminal/platform/tool
matrix. CI runs this smoke in its own job and retains evidence artifacts for
seven days. CLI, stdio MCP, headless/extension flows, historical released
clients, and real SSH remain separate acceptance work.

## Dialog and accepted-write drain scenarios

```sh
go build -o /tmp/agent-manager-e2e .
python3 tools/e2e/scenarios.py --binary /tmp/agent-manager-e2e
```

The separate scenario gate shares one binary across two disposable profiles.
It covers form spawn, selected-session quick input reaching a real pane with matching durable metadata, terminal/group creation, move, rename, settings persistence
and reopening, fork launch, and zero-exit shutdown. A fixture CLI runs a real
process; its conversation identity is seeded explicitly rather than claiming
real provider session-store discovery. Keyboard navigation selects its live-pane
marker, excluding group summary tables.

The second profile holds a SQLite write lock while a form spawn is accepted.
A duplicate submission reports the pending operation. Ctrl+C requests quit,
then releasing the lock permits the accepted row to commit before zero-exit
shutdown. This proves an accepted blocked write drains, rather than merely
quitting an idle manager. Every condition has a bounded observation deadline.

Remaining process cases include keybinding-file partial failures,
blocked settings/keybindings/focus foreground behavior, ambiguous transport
outcomes, real provider session stores and the wider platform/SSH matrix.

## Partial writes and installation

```sh
python3 tools/e2e/failure_scenarios.py --binary /tmp/agent-manager-e2e
python3 tools/e2e/failure_scenarios.py --binary /tmp/agent-manager-e2e --scenario settings-partial-save
```

Three disposable profiles cover a settings save with eight committed writes
before a later SQLite trigger refusal, an obstructed install-script path, and
a missing fixture CLI installed through a fixture npm before one successful
retry. The partial-save case checks durable earlier preferences, restoration of
the failed optimistic preference on reopen, visible failure and a successful
resave. The file-error case retains the setup dialog and creates no install row.
Retry checks one managed agent row/pane and one installer row; captured request
identity is separately covered by effect unit tests.

The final local run with a controlled installer PATH took 3.32 seconds. Removing partial-error preference
reconciliation made the settings scenario fail on the stale layout. These are
real binary wiring checks with synthetic CLI/installer processes, not actual
provider installers or session discovery. CI reuses the existing binary and
retains failure evidence alongside the smoke. Use `--scenario` to run one case.

## Blocked rename and move

```sh
python3 tools/e2e/blocked_scenarios.py --binary /tmp/agent-manager-e2e
python3 tools/e2e/blocked_scenarios.py --binary /tmp/agent-manager-e2e --scenario move
```

Two disposable profiles hold a SQLite write lock before accepting rename/move,
dismiss and reopen the same target while the worker is blocked, then release
the lock. Accepted rows must commit and the reopened dialog must remain usable at the
observed UI transitions. Durable commit alone does not acknowledge completion
processing in Update; focused unit tests explicitly deliver stale completions
and establish the generation fence. Lock-held frame observations have a two-second
deadline, below the store's five-second busy timeout. The final local run took 8.07 seconds. Both cases require a
zero-exit shutdown. CI shares the existing binary and retains the frames.

The initial move scenario exposed a failure: `openMove` did not advance
the dialog generation, so an older completion closed a reopened card. A focused
unit regression delivers the completion, fails before the generation increment
and passes after it. The binary scenario alone is not a deterministic regression
for that defect; it also passed against the old binary on a later run.

The earlier candidate settings/focus cases did not establish coverage: Settings
preflight blocked and Focus never reached its required fixture state. The current
blocked cases below replace those observations with explicit foreground budgets.

## Blocked Settings and Focus

The blocked scenario suite also accepts a Settings save under a real SQLite
writer lock, then reopens and edits Settings within the two-second foreground
budget. The accepted save becomes durable while the newer dialog retains its
edits. Its Focus case queues focus behind that save and verifies Help opens and
accepts a search while the write remains blocked; later focus remains usable.

```sh
python3 tools/e2e/blocked_scenarios.py --binary /tmp/agent-manager-e2e --scenario settings
python3 tools/e2e/blocked_scenarios.py --binary /tmp/agent-manager-e2e --scenario focus
```

The headless example has a separate-process observation/shutdown regression in
`examples/headless`: it starts its own runtime on a disposable profile/socket,
reads a JSON observation and requires clean SIGTERM shutdown. This is local
runtime and extension-backlog evidence, not paid provider conversation discovery
or the Linux/SSH platform matrix.

## SSH connection

```sh
python3 tools/e2e/ssh_scenarios.py --binary /tmp/agent-manager-e2e
```

A fixture `ssh` first on PATH drops every argument but the remote command and
runs it with `/bin/sh -c` under a second disposable HOME and its own short
tmux socket directory, so the "host" is a real agent-manager with its own
profile and no network, keys or `~/.ssh`. A copy of the binary, first on PATH
at a path unique to the run, serves both sides. The scenario seeds an agent on
the host through its CLI, adds the connection through the `C` dialog, waits
for the remote row, quick-sends to it and checks the host's inbox for the
`<short hostname>: you` sender, kills it and checks its tmux session is gone
on the host, and requires that the local store holds the connection and no
sessions. The first refresh starts the host's headless manager.

Cleanup ends every process by PID: the binary copy's processes (the host's
`serve` among them) and the tmux servers on the run's own sockets, listed in
`ended-processes.json`. No `kill-server` is sent. Attach needs a terminal on
both sides and is left to the unit tests, which check the SSH command it builds.
