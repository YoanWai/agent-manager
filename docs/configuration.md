# Configuration

Config lives in your OS user config dir (`~/Library/Application Support/agent-manager/config.toml` on macOS, `~/.config/agent-manager/config.toml` on Linux, with `XDG_CONFIG_HOME` honored when set) and is created on first run.

It holds three things. `poll_interval` (default `"2s"`) sets how often panes are polled for status, preview, and stats. `editor` is the command `o` opens a directory in, arguments included (`editor = "code -n"`, `editor = "open -a 'Visual Studio Code'"`); it is run directly rather than through a shell, and quotes group an argument carrying a space. Left unset, Agent Manager falls back to `$AGENT_MANAGER_EDITOR`, then a GUI editor on `PATH`, then `$VISUAL` / `$EDITOR` (see [Opening the editor](usage.md#opening-the-editor)). The two key tables are below.

## Agent CLIs

Agent Manager supports Claude Code, OpenCode, Codex, Grok Build, Gemini CLI, Pi, Command Code, and Hermes Agent, plus the shell `T` opens. Each one's launch command, revive and fork commands, MCP registration, and status rules are built into the binary, so an upgrade brings the current version of all of them. Settings (`s`) has a `CLIs` row that picks which of them the session pickers offer (see [Which CLIs you get offered](usage.md#which-clis-you-get-offered)).

The Pi support requires Pi 0.76.0 or later, because it launches sessions with `--session-id`.

Hermes is tested with Hermes Agent 0.20.0 and launches its classic REPL with `--cli`. This keeps the input, approval, and activity markers stable even when your Hermes preference selects its modern TUI.

A config file written by an earlier release carries a `[tools.<name>]` block per CLI. Those blocks are no longer read. Each was a copy of the defaults as they stood the day your file was written, so a fix for a CLI's new screen stopped at your copy instead of reaching you. The manager says so the first time it opens a file that still has them, and they are yours to delete.

**When a status looks wrong.** The rules are ours to fix, for everyone. [Open an issue](https://github.com/YoanWai/agent-manager/issues/new/choose) with the CLI and its version, plus the pane text it draws, which you can read the way the poller reads it. Replace `SESSION_ID` with the session id:

```bash
tmux -L agentmgr capture-pane -p -t am_SESSION_ID
```

A CLI that is not on the list above is a feature request; the `CLIs` row in Settings ends with `request CLI support`, which opens one prefilled.

## Key bindings

Two tables in config.toml name the keys: `[keybindings.session]` for the keys the manager keeps inside a session, and `[keybindings.list]` for the keys of its own list. Each action takes one key or a list of keys, `"none"` turns it off, and an action left out keeps its default. One key serves one action within a table: a key you name is yours, and an action that only held it by default gives it up and is left without one.

### Inside a session

Inside a session, attached or focused, the manager keeps a few keys for itself and hands every other key to the agent. A `[keybindings.session]` table moves those keys, so one that collides with a key your agent uses can go elsewhere or be given back:

```toml
[keybindings.session]
detach = ["ctrl+q", "f9"]   # back to the manager; one key or a list
review = "alt+r"            # open the session's diff review
editor = "none"             # f3 reaches the agent instead
```

The actions are `detach` (default `["ctrl+q", "ctrl+\\"]`), `review` (default `"ctrl+r"`) and `editor` (default `"f3"`). `"none"` hands the key to the agent like any other. Session keys are written as `ctrl+<letter>` (the symbols `@ \ ] ^ _` too), `alt+<letter or digit>`, or `f1` to `f12`. A key with no modifier is refused here, since it would take a character away from the agent, as are `ctrl+i`, `ctrl+m` and `ctrl+[`, which the terminal sends as tab, enter and escape. Bubble Tea, the framework the manager is built on, cannot read `ctrl+shift` combinations yet, so those are out for now. `detach` always keeps at least one key: it is the way back from a focused session.

### In the list

Every key the list answers to is an action in `[keybindings.list]`, listed in full here and in the picker under Settings: `up`, `down`, `open`, `attach`, `step_in`, `step_out`, `reorder_up`, `reorder_down`, `new_session`, `terminal`, `new_group`, `fork`, `prompt`, `copy_reply`, `review`, `mark_idle`, `rename`, `move`, `editor`, `restart`, `kill`, `kill_all`, `revive`, `revive_all`, `archive`, `restore`, `delete`, `search`, `filter`, `archived`, `empty_groups`, `fold_all`, `resize`, `settings`, `messages`, `help` and `quit`.

```toml
[keybindings.list]
new_session = "N"           # n is free for something else
prompt = ["space", "p"]     # two keys open the quick prompt
quit = "none"               # ctrl+c still quits
```

A list key is a plain character (`n`, `N`, `?`, `|`), a key name (`space`, `enter`, `tab`, `backspace`, `delete`, `up`, `down`, `left`, `right`, `home`, `end`, `pgup`, `pgdn`), `shift+` an arrow or tab, or the `ctrl+`, `alt+` and `f1` to `f12` forms above. A shifted letter is written as its capital. `esc` and `ctrl+c` are not keys a table can take: `esc` cancels everywhere and `ctrl+c` always quits. `settings` keeps at least one key, so the picker stays reachable. The footer, the `?` key map and the empty-list hints all read the table, so a moved key is named where it moved to.

### The picker

Settings (`s` by default) edits both tables: the **keybindings** row opens one picker, the session keys first and the manager's below them, where `↵` binds the key you press next, `a` adds a second key to an action, `d` turns the action off, and `r` names what would move and asks, then puts the shipped keys back on every action. A key its table cannot take is refused there with the reason the file would give. Leaving the picker writes each table you changed back into your config.toml, one line per action, keeping the rest of the file as you wrote it, comments included, and puts the keys to work at once, so no restart is needed.

The same table drives a full-screen attach, where the keys are tmux bindings on the `agentmgr` server, and focus mode, where the manager reads them itself; the session footer, the focus footer and the `?` key map all name whatever the table says. The bindings are reinstalled on every launch and every session create, so a change to the table takes effect when the manager next starts, running sessions included.

State is stored next to the config in `state.db` (SQLite).

## Right-to-left text

Hebrew and Arabic rows are painted as the cells they occupy, the same on every host. A terminal that runs its own bidirectional layout, iTerm2's right-to-left support or WezTerm's `bidi_enabled`, reorders those rows itself; turn that support off to read the frame in the columns Agent Manager paints.
