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
tmux command names either `e2e-outer` or `agentmgr`. Cleanup kills only these two
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
