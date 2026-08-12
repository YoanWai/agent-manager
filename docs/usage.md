# Usage

```bash
agent-manager
```

Sessions run inside tmux (`am_*` namespace), so they survive the manager quitting. Inside a session, **Ctrl+Q** detaches back to the manager and **Ctrl+R** opens its diff review. `agent-manager --version` prints the version.

Agent sessions live on a private tmux server named `agentmgr`, so they never mix with the tmux you run yourself and a `kill-server` on your own socket leaves them alone. To reach one from a plain shell, name that server: `tmux -L agentmgr ls`, then `tmux -L agentmgr attach -t am_<id>`.

## Keys

| Key | Action |
|-----|--------|
| `n` | New session (name, tool, directory, worktree toggle, optional starting prompt, group picker) |
| `T` | New terminal tab: a plain shell in the selected group, with no agent in it |
| `f` | Fork the selected conversation into a named session in the same group and directory |
| `g` | New group (name, parent, default path) |
| `enter` | Focus session in place (keys go to the agent, list stays) / fold group |
| `A` | Attach session full screen (Settings can swap it with `enter`) |
| `ctrl+q` | Inside a session: back to the manager |
| `→` | Step into the row: focus the session, or open the group. In beta; Settings (`s`) can turn the pair off |
| `←` | Step out: close the group, or — focused, with the caret at the start of the agent's prompt — back to the manager. This needs the tool's prompt marker (its `activity_cutoff`) on the caret's row, so a CLI without one keeps `←` entirely; anywhere else in the prompt it moves the caret as usual |
| `K` / `J` (or `shift+↑` / `shift+↓`) | Reorder session or group among its visible siblings |
| `m` | Move session to another group |
| `r` | Rename session / edit tool; edit group name and default path |
| `x` | Kill the selected session, or every live session under a group: frees the RAM their agents hold, and the rows stay for `v` |
| `X` | Kill every live session in view |
| `v` | Revive a dead session, or every dead session under a group |
| `V` | Revive every dead session in view |
| `R` | Restart the selected session on an empty context: same name, group, directory and tool |
| `a` / `u` | Archive / restore a session, or a group and its entire subtree |
| `d` | Delete session, or a group + its entire subtree |
| `space` | Quick prompt: answer the selected session, or spawn an agent in the selected group |
| `ctrl+r` | Review the selected session's changes: full-screen whole-file diffs, with `c` to comment a line and `C` to send the comments to the agent |
| `F` | Fold / unfold every group |
| `s` | Settings (quick-spawn tool, theme, theme follows OS, list density, review layout, after quick send, session keys, ←→ step in/out, worktree sessions, report a bug) |
| `\|` | Resize the split: `←→` nudge the divider, `enter` commits, `esc` cancels |
| `t` | Toggle archived view |
| `w` | Filter to sessions that need attention (`waiting`, `finished`, `errored`); press again to show all |
| `M` | Messages (updates, tips; `x` dismisses one for good). The welcome message points at Settings for bug reports. |
| `e` | Hide / show empty groups |
| `/` | Search |
| `?` | The key map: every binding, grouped by what it acts on. It scrolls (`↑↓`/`jk`, `pgup`/`pgdn`, `g`/`G`) and `/` searches it down to one line. |
| `q` | Quit (sessions keep running) |

Navigation is keyboard-driven. The manager claims mouse reporting so the wheel stays inside the app and cannot scroll the TUI out of view: a notch scrolls the diff, and in a focused session it walks that pane's scrollback, where click-drag also selects pane text and copies it. In the list the wheel does nothing, since moving the selection with it retargets every key that follows. Fuller mouse support, on by default with a settings toggle, is tracked in [#110](https://github.com/YoanWai/agent-manager/issues/110).

## Quick prompt

Press `space` to dock a prompt bar at the bottom of the sidebar. The target follows the cursor while the bar is open (`↑↓` still navigate):

- On a **session** row, `enter` sends the typed text straight into the session's pane, so the agent gets it as a user message without you attaching. The bar clears and stays open, ready for the next answer; Settings (`s`) can make it close instead.
- On a **group** row, `enter` spawns a new agent in that group with the prompt embedded, using the group's default path. The spawn tool starts at the Settings default and `tab` cycles it (claude ↔ opencode ↔ any configured tool); the footer shows the current pick. `alt+w` toggles whether the new agent spawns into its own git worktree, starting from the Settings default; the footer shows `worktree: on` or `worktree: off`, or `worktree: unavailable (not a git repo)` when the target directory cannot hold one. Answering an existing session ignores the toggle, since there is no new session to place in a worktree. The agent starts working on the prompt immediately.

`ctrl+v` pastes an image from the system clipboard as an `[Image #1]` chip at the caret. The image is saved under `agent-manager-pastes` in your temp directory, and on send each chip is swapped back for its path, so the paths reach the agent in the order and the places you pasted them. `backspace` next to a chip removes the whole chip, and an edit that swallows one releases its image. A clipboard holding text rather than an image pastes as text. Pasted images older than seven days are cleared at startup and once a day while the manager runs, so an agent can still open one from an earlier session while temp stays tidy.

`esc` closes the bar. The new-session form's optional `prompt` field launches an agent the same way; tools whose CLI takes the prompt behind a flag declare it with `prompt_flag` (see [Configuration](configuration.md)).

![answering a working Claude Code session from the prompt bar, without attaching](demo-space.gif)

## Terminal tabs

`T` opens a shell tab in the group under the cursor: a session like any other — same list, same row keys, same `enter`, `x`, `v` and `R` — with your shell in the pane instead of an agent. It lands in the selected session's directory, wherever that session has moved to, or in the group's default path when a group row is selected, so the shell that runs the tests sits next to the agent that wrote them. Its status rests at idle throughout, a build included: turn tracking belongs to agents, and a shell has no turns.

The shell is the `[tools.terminal]` block in [config.toml](configuration.md). It ships with no command, which leaves the pane on `$SHELL`; set one to open a different shell. What marks it as a shell is `shell = true`, not its name, so a `[tools.terminal]` block you wrote yourself stays the agent CLI you meant it to be.

### Where the shells sit

Shells gather under a **Terminals** rule pinned to the foot of the list, holding every shell from every group, each row naming the group it belongs to. The tree scrolls above it; the block keeps at most half the list and scrolls inside itself once you have more shells than that. Settings (`s`) has a `terminal rows` row that switches it from `pinned` to `inline`, which puts every shell back among the agents in its own group, marked with `❯` where an agent carries its status dot.

A group's dots and counts describe its agents either way, so the shell you left running a build never shows up as work in progress.

**The keys that write into a pane refuse a shell.** `space` and the review screen's `C` both paste their text and press Enter, so on a shell a sentence meant for an agent would run as a command. Both say the row is a shell and send nothing; enter the session (`↵`) to type there, where what you type is plainly a command. `f` says the same, since a shell has no conversation to fork.

A shell left on its empty command carries no session id, so `agent-manager rename` run inside one cannot find its session. Rename it from the list with `r`. Give the block a command and the pane gets an id like any other session.

## Worktree sessions

A session can spawn into its own git worktree instead of the shared working directory: the `n` form has a `worktree` field between `dir` and `prompt` (`◂ on ▸` / `◂ off ▸`, toggled with `←→`), and the quick prompt's `alt+w` does the same for a group spawn. Settings (`s`) has a "worktree sessions" row that sets the default both start from.

The worktree lives at `<repo>-worktrees/<name>` next to the repo, on a new branch `am/<name>`. Its starting point is the remote's default branch (`origin/HEAD`) when that resolves, falling back to a local `main` or `master`, and finally to `HEAD`. A worktree that fails to create blocks the spawn with an error instead of falling back to a shared directory.

A directory that is not a git repo cannot hold a worktree, so the field and the quick prompt's footer read `unavailable (not a git repo)` in place of on/off, the toggle says why when pressed, and the session spawns in that directory as a plain session. This is what a group path sitting above several repos does: the umbrella itself is not a repo, so its sessions launch in it directly.

Renaming a session (`r`, or the agent's own `agent-manager rename`) carries its worktree along: the directory becomes `<repo>-worktrees/<new name>` and the branch becomes `am/<new name>`, so an agent that names itself after the work it picked up is reviewed and merged under that name. A destination directory or branch that already exists reports that and keeps the session where it is, so the name, the directory and the branch always agree. A worktree you have renamed or removed by hand is left alone and the session still takes the new name.

Deleting (`d`) a session that holds a worktree removes the worktree and its branch when it is clean: no uncommitted changes and no commits ahead of its base. A dirty worktree is left in place and its path shown, so nothing is lost. Note that "clean" is judged by `git status`, so gitignored files inside the worktree (a `.env`, local config, build output) are removed along with the directory. Killing, archiving, and reviving a session never touch its worktree.

## Killing and reviving sessions

`x` ends a session that is holding RAM you want back, and on a group row it ends every live session under it; `X` ends every live session in view. Each asks to confirm first, and what it ends is the tmux session, not the record: the row stays in the tree, marked `dead`, with its name, group, and conversation id intact.

![ending every session under a group for the RAM, then reviving the whole subtree on its own conversations](demo-revive.gif)

`v` relaunches a dead session under its old id, keeping its name, group, and history. When the manager holds that session's own conversation id, revive resumes **that exact conversation** through the tool's `resume_by_id_command`: `claude --resume {id}`, `codex resume {id}`, `opencode --session {id}`, `grok --resume {id}`, `gemini --resume {id}`, `pi --session {id}`.

The id arrives one of two ways: tools with a `session_id_flag` launch under an id the manager mints, and tools that mint their own are read back by a `session_store` capturer (`codex`, `opencode`). Without an id, revive falls back to `revive_command` (`claude --continue`), which resumes the working directory's most recent conversation, and the manager says so in the status line, since sessions sharing a directory would otherwise land on the wrong one. On a group row `v` revives every dead session under it, and `V` revives every dead session in view; both revive what they can and name the first failure rather than stopping.

## Restarting a session on an empty context

`R` keeps the row and drops the context: same name, group, tool, and working directory, a managed worktree included, launched on a conversation the agent has never seen. It is what you want when a session has piled up context you are done with, where reviving it would spend the budget re-reading history or land straight in a compact.

It asks to confirm first, and it works on a live session too: the running agent ends, then the fresh one launches. The conversation it was on is retired rather than resumed: the manager mints a new id for tools that take one (`session_id_flag`) and captures the new one for tools that mint their own (`session_store`). The retired conversation is left on disk untouched, and the row stops pointing at it, so a later `v` resumes the conversation the restart started rather than the context it dropped. The row changes hands only once the new agent is up, so a launch that cannot start (a tool gone from `PATH`, a directory that moved) leaves the session on the conversation it had, still there for `v`.

## Forking sessions

1. Select a session and press `f`.
2. Enter a name.
3. Press `enter`.

The fork uses the source session's tool, group, working directory, and conversation history.

Claude Code and Codex support forks by default. A custom tool needs a `fork_command` in its configuration. The source session must have a captured conversation ID.

A fork shares its source session's managed worktree. Agent Manager keeps the worktree until you delete the last session that uses it. You cannot rename the worktree while another session uses it.

## Self-naming sessions

Sessions spawned without a custom name (every quick spawn, and the form with the name left blank) get a placeholder like `claude-a1b2`, and their first prompt opens by asking the agent to run `agent-manager rename "<name>"` once with a short name for the broad feature of the session (not a single subtask). The directive also tells the agent not to rename again unless you ask. When the first prompt cannot carry the directive (a `/slash` command, or no prompt at all), the manager sends it as its own message once the tool's input box appears in the pane. The subcommand drops the name into a per-session file; the manager picks it up on the next poll and updates the sidebar row and the tmux status bar. This works with any tool, since it only needs the agent to read its prompt and run one shell command.

Sessions you name yourself keep that name: the first prompt only notes that `agent-manager rename` is available later if you ask, and does not instruct the agent to rename now. You can still ask an agent to rename its session later, or run `agent-manager rename` yourself from a shell inside the session.

## Declaring the repo under review

A session's working directory is often an umbrella folder holding many repos, so review can only guess which one the agent means. An agent that knows which repo it is working in can say so by running `agent-manager review-repo <path>` from a shell inside its session. The subcommand checks that the path is (or sits inside) a git repo, resolves it to the repo root, and drops it into a per-session file; the manager picks it up on the next poll and review opens on that repo the next time you open it. A path that is not inside a git repo is rejected, so a declaration is always a fact rather than a guess.

An agent can also declare what its branch diffs against by running `agent-manager review-base <ref>` from inside its worktree: the ref is validated in that repo, stored per session and repo, and the "vs target" scope uses it from then on. `agent-manager review-base --clear` returns to automatic detection. A stored ref that stops resolving surfaces as an error in review, and `B` opens a target picker (the repo's branches plus an `auto` entry) to set or clear it by hand.

Agents usually work in git worktrees, one branch per worktree, and those worktrees can live anywhere on disk. A declared path that is a worktree root is accepted wherever it lives, so one `review-repo` call names both the repo and the branch under review. Review resolves its target in a fixed order: a repo you picked by hand with `r` or `b` wins for as long as the manager is running, then the agent's declared repo, then the ranking (dirty working trees first, then most recent commit). When the picked or declared path stops being a git repo, review says so in the status line and `r` is there to pick the right one.

## MCP: how agents discover these commands

Every session the manager spawns or revives carries the agent-manager MCP server, so MCP-capable agents see `rename`, `review_repo`, `review_base` and `review_mode` (which sets the diff scope review opens on) as native tools with descriptions telling them when to call each: no prompt injection, no per-project setup. The server lives in the same binary (`agent-manager mcp`, stdio) and identifies the calling session through its environment.

Registration is per tool. Claude gets a generated `--mcp-config` file. Codex gets `-c mcp_servers...` overrides. OpenCode gets an `OPENCODE_CONFIG` merge file. Grok and Gemini each get a one-time `mcp add --scope user` entry on their first launch.

Pi does not include an MCP client. The `rename`, `review-repo`, and `review-base` commands still work inside Pi sessions.

A custom tool opts in with `mcp = "<style>"` in its config section. Set `mcp = "none"` to disable registration.

## Diff review

Press `ctrl+r` on a session to open a full-screen review of its repo: changed files with +/− counts on the left, the whole file on the right with syntax highlighting and changed lines tinted, so every edit reads in full context. The diff refreshes as the agent keeps editing.

| Key | Action |
|-----|--------|
| `↑↓` / `jk`, `ctrl+d` / `ctrl+u` | Scroll the file |
| `g` / `G` | Jump to top / bottom |
| `J` / `K` (or `tab` / `shift+tab`) | Previous / next file |
| `n` / `N` | Jump between changes |
| `u` | Toggle unified and side-by-side |
| `s` | Cycle the scope: uncommitted, vs target, last commit, staged |
| `r` | Pick the repo when the session's directory holds several (type to filter) |
| `b` | Pick the branch from the repo's worktrees |
| `B` | Pick the target (merge-into branch) the "vs target" scope compares against |
| `space` | Mark a file reviewed |
| `c` / `d` | Write / drop a line comment |
| `C` | Send every comment to the agent as one review prompt (`enter` or `y` confirms) |
| `esc` / `q` | Close the review |

Each changeable value in the header wears its own key, so the scope, layout, repo, and target pills read as `s`, `u`, `r`, `B` legends at a glance.

![review, side by side, with the changed lines tinted in full file context](screenshot-review.png)

Comments stay on the review screen until you send them: `C` flattens every one of them into a single prompt, asks you to confirm, and delivers it into the agent's pane, so the agent starts addressing your notes while you watch the diff update.

![review mode: scrolling a changed file, switching to unified, jumping to the next file, then a line comment sent back to the agent](demo-diff.gif)

## Groups

![folding the tree, creating a nested group, reordering, and archiving one](demo-groups.gif)

Groups are paths (`backend/api/auth`) forming a tree of unlimited depth. Sessions can live at any node, including the root. Create subgroups inline with `g`, reorder both groups and sessions with `K` / `J` (or `shift+↑↓`; the order persists), fold a subtree with `enter` on its row, fold or unfold the whole tree with `F`, hide or restore empty groups visually with `e`, and edit a group's name and default path with `r`. On a session, `r` renames it and `tab` cycles the tool (status rules and revive follow the new tool; useful when you quit one agent in the pane and start another).

## Status

Each session's tmux pane is polled (default every 2s) to derive a status:

| Status | Meaning |
|--------|---------|
| `working` | The agent is busy on a turn |
| `waiting` | Blocked on you: a dialog, a permission ask, or a plain-text question |
| `finished` | Turn ended — an alert that clears to `idle` once you enter the session |
| `errored` | The tool reported an error |
| `idle` | Nothing running |
| `dead` | The tmux session is gone |

`w` narrows the list to sessions that need attention (`waiting`, `finished`, `errored`). Press again to show every status. An `ATTENTION` badge sits over the list with the key that clears it, and the session counts follow the filter; folds open so matches are not hidden. The archived view (`t`) and hidden empty groups (`e`) label themselves the same way, so the list always names every filter it is under.

![the session tree, with a waiting agent's permission prompt in the preview](screenshot-sessions.png)

Each row carries its status and tool inline, and a folded group keeps a count per status so a collapsed subtree still tells you whether anything needs you. Selecting a session shows the tail of its pane on the right, which is how a `waiting` agent's actual question reaches you without attaching. A session with no window left, archived or killed, shows the snapshot taken when it still had one.

Detection matches per-tool regex rules against the visible pane, analyzes the newest turn to tell `finished` from `waiting`, and treats streaming output (content changing between polls) as `working`. A turn that ends without any turn-summary line still resolves: when a `working` pane goes quiet, the turn counts as `finished`, or `waiting` when it ends on a question. Work that outlives the turn which started it (background agents) is matched by `busy_line`, so a turn-end summary keeps reading as `working` while that work runs. Polling keeps running while you are inside a session, so statuses stay live. The selected session's pane tail renders in the preview panel, and moving the cursor fetches the preview immediately.

For Claude Code, status comes first-hand from [hook events](https://docs.anthropic.com/en/docs/claude-code/hooks) instead of pane guessing: sessions launch with a generated `--settings` file whose hooks write the lifecycle state (`working`, `waiting`, `finished`, `idle`) to a per-session status file that the poller reads first. Pane rules still refine it — hooks cannot see a plain-text question, an Esc interrupt, or an error line, so a matching pane verdict upgrades the hook status — and they take over fully as fallback when the hook file is missing or stale. Enabled per tool with `status_source = "claude-hooks"`.

## Stats

The header shows a fleet summary: per-status session counts, plus `agents total usage: cpu N% · ram M% · X GB` for every live agent's full process tree (shell, agent, and children). CPU is that tree's CPU time over the last poll as a share of total machine capacity (same 0–100% unit as the computer gauge). RAM is resident set as a share of installed memory, with absolute size beside it. The selected session's detail line uses the same scale for that session alone.

The Computer block in the sessions panel shows machine gauges:

- **CPU**: whole-machine utilization (0-100%)
- **Memory**: used/total. On macOS this matches Activity Monitor's Memory Used (resident RAM minus free, speculative, and reclaimable file cache). On Linux it is `Total - MemAvailable`, so file cache is not counted as used.
- **Swap**: used/total of the current swap allocation (`used/total * 100`). On macOS the swap file grows under pressure, so the denominator is the live size from `vm.swapusage`, not a fixed partition.
- **Disk**: fill of the root filesystem (used / (used + available)), with free space from the kernel's available figure
- **Network**: up/down rates on real NICs only (loopback, utun, bridges, and similar virtual interfaces are excluded)
- **Temperature**: `cpu`, `gpu` and `soc` readings in °C, each the hottest sensor in its category, sampled every 5s. Apple Silicon draws no CPU/GPU line, so its dies report as one `soc` figure. A reading appears when the machine exposes that sensor.

## Themes

`s` opens Settings, where `↑↓` move between fields and `←→` change the focused one.

![settings, with the theme picker and its palette swatches](screenshot-settings.png)

Fifteen palettes ship. Nine dark: `classic`, `solarized dark`, `catppuccin mocha`, `tokyo night`, `gruvbox dark`, `nord`, `dracula`, `rosé pine`, and `monochrome`. Six light: `solarized light`, `catppuccin latte`, `tokyo night day`, `gruvbox light`, `rosé pine dawn`, and `paper`. The swatch strip beside the name previews the palette, and the theme applies as you step through it, so the picker is a live preview of the whole UI. The manager also matches the terminal's own background to the palette, so the window has no seam against it, and restores the terminal's background on exit. Your pick is saved with the rest of the state and restored on the next run.

**theme follows OS** resolves the palette at startup from the environment's light/dark preference: the OS setting on macOS and Linux desktops, and the terminal's own background elsewhere, including over SSH. A theme already on the detected side stays; only a mismatch switches, to `classic` or `solarized light`. Your manual pick is kept separately, so turning the toggle off returns to it, and stepping the theme picker by hand turns the toggle off. Agent panes render on the theme's own backdrop, and the pane declares that background to the agent inside it, so an agent that auto-detects its palette resolves to the same side the manager is drawing. A session already running keeps the palette it resolved at launch until it is restarted.

