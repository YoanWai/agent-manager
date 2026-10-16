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

## Dialog scenarios

```sh
go build -o /tmp/agent-manager-e2e .
python3 tools/e2e/scenarios.py --binary /tmp/agent-manager-e2e
```

The scenario gate covers form spawn, selected-session quick input reaching a
real pane with matching durable metadata, terminal/group creation, move,
rename, settings persistence and reopening, fork launch, and zero-exit
shutdown. A fixture CLI runs a real process; its conversation identity is
seeded explicitly rather than claiming real provider session-store discovery.
Keyboard navigation selects its live-pane marker, excluding group summary
tables. Every condition has a bounded observation deadline.

## Installation failures

```sh
python3 tools/e2e/failure_scenarios.py --binary /tmp/agent-manager-e2e
python3 tools/e2e/failure_scenarios.py --binary /tmp/agent-manager-e2e --scenario install-retry
```

Two disposable profiles cover an obstructed install-script path and a missing
fixture CLI installed through a fixture npm before one successful retry. The
file-error case retains the setup dialog and creates no install row. Retry
checks one managed agent row/pane and one installer row. These are real binary
wiring checks with synthetic CLI/installer processes, not actual provider
installers. CI reuses the existing binary and retains failure evidence
alongside the smoke. Use `--scenario` to run one case.
