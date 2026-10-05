# Configuration

Everything you can change lives in Settings (`s` by default): the keys, the editor `o` opens, the theme, and the rest of its rows (see [Keys](usage.md#keys)). The manager stores each choice in `state.db` (SQLite) in your OS user config dir, `~/Library/Application Support/agent-manager/` on macOS and `~/.config/agent-manager/` on Linux, with `XDG_CONFIG_HOME` honored when set. Changes are saved as you leave Settings and take effect then, with no restart.

The **editor** row picks the command `o` opens a directory in (see [Opening the editor](usage.md#opening-the-editor)), and the **keybindings** row opens the [key picker](#key-bindings). Panes are polled every two seconds for status, preview, and stats.

## Upgrading from config.toml

Earlier releases read a `config.toml` in that directory. The first start after upgrading carries its `editor` line and its `[keybindings.session]` and `[keybindings.list]` tables into Settings, so every key you moved stays where you put it. That happens once: from then on Settings is the only place the manager reads, and the file is yours to delete. A file the manager cannot read is named in Messages (`M`) with the reason, and the keys start from the defaults.

The poll interval is fixed at two seconds, so a `poll_interval` line in that file has no effect. `[tools.<name>]` blocks have none either: every CLI's commands and status rules are built into the binary, which is how a fix for a CLI's new screen reaches you on upgrade.

## Agent CLIs

Agent Manager supports Claude Code, OpenCode, Codex, Grok Build, Gemini CLI, Antigravity CLI, Pi, Command Code, Hermes Agent, Muse Code, and Oh My Pi, plus the shell `T` opens. Each one's launch command, revive and fork commands, MCP registration, status rules, and the flags a session's model, effort and profile launch with are built into the binary, so an upgrade brings the current version of all of them. The models, efforts and profiles themselves are read from each CLI when you create a session (see [Model, effort and profile](usage.md#model-effort-and-profile)). Hermes, Antigravity and Oh My Pi have no fork, and Pi and Oh My Pi get no MCP registration. Settings (`s`) has a `CLIs` row that picks which of them the session pickers offer (see [Which CLIs you get offered](usage.md#which-clis-you-get-offered)).

The Pi support requires Pi 0.76.0 or later, because it launches sessions with `--session-id`. Its model list needs Pi 0.84.3 or later, because earlier releases save every model they are asked about as your default.

Hermes is tested with Hermes Agent 0.20.0 and launches its classic REPL with `--cli`. This keeps the input, approval, and activity markers stable even when your Hermes preference selects its modern TUI.

Grok Build launches with `--no-leader`, so every session runs its own agent. A shared Grok leader runs each session's shell commands under the environment of the session that started it, which would make one session's agent-manager commands act as another. `[cli] use_leader` in Grok's `config.toml` keeps applying to the Grok sessions you start yourself.

Antigravity CLI is tested with agy 1.2.14. Its first launch registers the `agent-manager` server with `agy mcp add`, which writes `~/.gemini/config/mcp_config.json`, the file the Antigravity IDE reads its servers from too. agy has no command-line fork, and its `/fork` moves the running session onto the copy, so `f` does not offer one.

Muse Code is tested with Muse 1.3.0 and 1.4.0. Muse reads MCP servers only from its settings file, so the first Muse launch adds the `agent-manager` server to `~/.config/muse/settings.json` (under `$XDG_CONFIG_HOME` when set) and keeps every other setting and server. A symlinked settings file is written through the link. Muse starts that server with none of the session's environment, so the server finds its session through its process tree, which needs the manager's tmux server at the default `TMUX_TMPDIR`. Muse forks only from inside a running session: `f` types `/fork` into the source and opens the fork in its own pane.

Oh My Pi (`omp`) is tested with omp 18.2.11 and 18.4.2 in its default band composer and glyphs; another composer shape or glyph mode (Settings in omp) draws a screen the status rules do not read. omp mints its own session id and writes the session file once the first reply lands. The manager reads the id from the breadcrumb omp keeps for the session's own terminal, `~/.omp/agent/terminal-sessions/<tty>` (under `$PI_CODING_AGENT_DIR` when set), so sessions sharing a directory never swap conversations, and revive runs `omp --resume {id}`; without an id, `omp --resume` opens its picker. A session kept under `--profile`, `--session-dir`, or omp's XDG layout is not read back. omp sessions coordinate through the Agent Manager shell subcommands and get no MCP registration.

**When a status looks wrong.** The rules are ours to fix, for everyone. [Open an issue](https://github.com/YoanWai/agent-manager/issues/new/choose) with the CLI and its version, plus the pane text it draws, which you can read the way the poller reads it. Replace `SESSION_ID` with the session id:

```bash
tmux -L agentmgr capture-pane -p -t am_SESSION_ID
```

A CLI that is not on the list above is a feature request; the `CLIs` row in Settings ends with `request CLI support`, which opens one prefilled.

## Key bindings

The **keybindings** row in Settings opens one picker with two tables: the keys the manager keeps inside a session, and the keys of its own list. Each action takes one key or several, and an action can be turned off. One key serves one action within a table.

### The picker

The session keys come first and the manager's below them. `↵` binds the key you press next, `a` adds a second key to an action, `d` turns the action off, and `r` names what would move and asks, then puts the shipped keys back on every action. A key its table cannot take is refused there with the reason. Leaving the picker stores each table you changed and puts the keys to work at once, so no restart is needed.

Only a key you moved is stored. An action at its shipped default follows that default, so a release that changes one reaches you, and a key you move back to its default follows it again. When a release adds an action whose default key you already gave to another action, your choice stays and the new action starts without a key, ready for you to give it one.

### Inside a session

Inside a session, attached or focused, the manager keeps a few keys for itself and hands every other key to the agent. The actions are `detach` (default `ctrl+q` and `ctrl+\`, back to the manager), `review` (default `ctrl+r`, the session's diff review), `editor` (default `f3`, its directory in your editor) and `tmux_prefix` (off by default). Moving one frees a key that collides with a key your agent uses, and turning one off hands its key to the agent like any other.

A session key is `ctrl+<letter>` (the symbols `@ \ ] ^ _` too), `alt+<letter or digit>`, or `f1` to `f12`. A key with no modifier is refused here, since it would take a character away from the agent, as are `ctrl+i`, `ctrl+m` and `ctrl+[`, which the terminal sends as tab, enter and escape. Bubble Tea, the framework the manager is built on, cannot read `ctrl+shift` combinations yet, so those are out for now. `detach` always keeps at least one key: it is the way back from a focused session.

`tmux_prefix` gives managed sessions a tmux prefix of their own, one key or two for tmux's `prefix` and `prefix2`. With it set, the key your own tmux.conf uses as its prefix is free to be a session key, `detach` for one. Turned off, sessions go back to the prefix your tmux.conf sets.

### In the list

Every key the list answers to is an action in the picker: `up`, `down`, `open`, `attach`, `step_in`, `step_out`, `reorder_up`, `reorder_down`, `new_session`, `terminal`, `new_group`, `fork`, `prompt`, `copy_reply`, `review`, `mark_idle`, `rename`, `move`, `editor`, `restart`, `kill`, `kill_all`, `revive`, `revive_all`, `archive`, `restore`, `delete`, `search`, `filter`, `archived`, `empty_groups`, `fold_all`, `resize`, `settings`, `messages`, `help` and `quit`.

A list key is a plain character (`n`, `N`, `?`, `|`), a key name (`space`, `enter`, `tab`, `backspace`, `delete`, `up`, `down`, `left`, `right`, `home`, `end`, `pgup`, `pgdn`), `shift+` an arrow or tab, or the `ctrl+`, `alt+` and `f1` to `f12` forms above. `esc` and `ctrl+c` stay as they are: `esc` cancels everywhere and `ctrl+c` always quits, even with `quit` turned off. `settings` keeps at least one key, so the picker stays reachable. The footer, the `?` key map and the empty-list hints all read the table, so a moved key is named where it moved to.

### Where the keys apply

The same table drives a full-screen attach, where the keys are tmux bindings on the `agentmgr` server, and focus mode, where the manager reads them itself; the session footer, the focus footer and the `?` key map all name whatever the table says. A session created from the `agent-manager` CLI or by an agent through the MCP server reads the same stored table, so it carries your keys too.

## Right-to-left text

Hebrew and Arabic rows are painted as the cells they occupy, the same on every host. A terminal that runs its own bidirectional layout, iTerm2's right-to-left support or WezTerm's `bidi_enabled`, reorders those rows itself; turn that support off to read the frame in the columns Agent Manager paints.
