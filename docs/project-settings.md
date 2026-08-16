# Project settings

A repository can tell agent-manager how to make a fresh worktree usable and
what to run inside it. Put that in `.agent-manager/settings.toml` at the
repository root and commit it: what it takes to bootstrap the project is the
same for everyone working on it, so it belongs next to the code rather than
in one person's `~/.config/agent-manager/config.toml`.

Everything here is opt-in. A repository without the file behaves exactly as
it did before.

```toml
# Runs once inside every new worktree, before its agent starts.
setup = """
npm ci
cp ../myapp/.env .
"""

# The low end of the port range worktrees are handed. Optional; 3100 by default.
port_base = 3100

[run.dev]
command = "npm run dev -- --port $AGENT_MANAGER_PORT"
description = "dev server"
default = true

[run.test]
command = "npm test -- --watch"
```

## The setup script

A worktree is a fresh checkout. Everything the project needs that git does
not track — installed dependencies, an `.env`, generated files — is missing
from it, and an agent started into that tree spends its first turn on errors
that are not the code's. `setup` is what closes that gap.

It runs in the session's own pane, in the new worktree, before the agent, so
you watch it happen instead of waiting on a spinner. It is handed to `sh`, so
it can be a pipeline or several lines.

- **On success** the agent starts exactly as it would have with no setup
  script at all.
- **On failure** the agent does not start. The pane says which exit code it
  got and leaves you a shell, already in the worktree, to fix it from. Press
  `R` to restart the session once you have.

Setup only runs for sessions spawned **into a worktree** — the `alt+w` toggle
in the quick prompt, the worktree field in the new-session form, or a group
that defaults to worktrees. A session pointed at a checkout you already
prepared by hand is left alone.

Discovery never climbs above the repository. `setup` and every run command
are handed to a shell, so a settings file found in a directory *above* your
checkout would be someone else's code running as you — and a shared parent
directory is exactly where one would be planted. The walk stops at the
repository root; a directory outside any repository is read on its own.

Settings are read from the worktree, so they are versioned with the branch
and a branch that changes its own setup script gets the new one. A settings
file that is written but not yet committed is not in the checkout at all, so
that case falls back to the repository the worktree branched from — the first
attempt works rather than silently doing nothing.

## Run scripts

`p` runs a project command in a terminal tab beside the session under the
cursor, in that session's directory.

- One run script, or one marked `default = true`, starts straight away.
- Several with no default open a picker, because the wrong pick here starts a
  server or a test watcher rather than doing nothing.

Each is an ordinary shell session in the list: it keeps its scrollback, and
`x`, `v`, `a` and `d` treat it like any other row. It is named for both the
script and the session it runs beside, so `dev` in two worktrees stays
tellable apart.

`description` is what the picker shows; the command itself is shown when you
leave it out.

## Ports

Running five agents at once is not much use if their dev servers all want
port 3000. Every worktree is handed a block of ten ports, and
`$AGENT_MANAGER_PORT` — the first of them — is exported into setup scripts,
run scripts, and the agent's own pane. A project whose server binds it can
have every worktree serving at once. A project that ignores it is unaffected.

The block is derived from the worktree's directory name rather than handed
out by a counter, so it survives restarts of both the session and the
manager: a worktree keeps the address you bookmarked for as long as it keeps
its name. A block already listening is skipped, so two names that happen to
collide can still both run.

Need more than one port? Derive them: `$((AGENT_MANAGER_PORT + 1))`.

A block is only handed out when **every** port in it is free, not just the
first, so a project deriving a second port is never given a block whose upper
half already belongs to something else.

`port_base` moves the range. The default of 3100 sits above where a
hand-started `npm run dev` usually lands, so a worktree server and one you
started yourself do not fight. It must leave room for the whole range, so it
is rejected outside 1-64536 rather than silently handing out a port nothing
can bind.
