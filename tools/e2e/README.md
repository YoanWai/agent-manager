# Real TUI smoke

From a fresh checkout with Go, tmux, `/bin/sh`, and Python 3.9+ installed:

```sh
python3 -m unittest discover -s tools/e2e -p 'test_*.py'
python3 tools/e2e/smoke.py
```

The smoke builds the repository binary once before changing HOME. Reuse a build
with `--binary /absolute/path/agent-manager`. `--artifacts /new/path` selects a
new evidence directory; the default is a unique directory in the system temp
folder. Output includes its path, result, and total duration. Keep the smoke
separate from the fast Go suite. The observation budget is 12 seconds per
transition; a successful cached-build run should take less than 30 seconds.

The harness creates its own HOME (the default profile therefore belongs to this
run), XDG directories, and shell configuration directory. It removes inherited
TMUX and uses a short, unique, precreated `/tmp/ame-*` socket directory. Every
tmux command names either `e2e-outer` or `agentmgr`. Cleanup kills only these two
servers within that directory, including on failure. It retains profile state,
frames, command output, return codes, and `result.json` on success and failure.
Artifacts contain only the disposable fixture; remove the reported directory
when its evidence is no longer needed.

The terminal is explicitly 110×30 with `xterm-256color`, truecolor capability,
`NO_COLOR=1`, and `/bin/sh`. Assertions cover Help open, search entry, typing,
commit, clear, wheel consumption followed by acknowledged keyboard scrolling,
close, a terminal row, a real managed shell accepting a unique file-writing
command, and a zero-exit manager process. The shell command acknowledgement
avoids both footer-label false positives and prompt/readiness races. Poll waits
require an observable condition; the 50ms cadence is never a transition delay.

This is bounded local tmux evidence, not the supported terminal/platform/tool
matrix. CLI, stdio MCP, headless/extension flows, hosted CI, historical released
clients, and real SSH remain separate acceptance work.
