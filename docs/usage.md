# Usage

```bash
agent-manager
```

Sessions run inside tmux (`am_*` namespace), so they survive the manager quitting. Inside a session, **Ctrl+Q** detaches back to the manager when your terminal and tmux leave it available; **Ctrl+\\** is an alternate under the same rule. **Ctrl+R** opens the session's diff review and **F3** opens its directory in your editor. In a full-screen attach, the session footer also shows an inner tmux prefix followed by `d` when configured. When nested inside another tmux, send the inner prefix shown in the footer, then press `d`. If both tmux servers use the same prefix, invoke the outer tmux's `send-prefix` binding; if the outer tmux otherwise captures the inner prefix, configure it to forward that key. `agent-manager --version` prints the version.

Agent sessions live on a private tmux server named `agentmgr`, so they never mix with the tmux you run yourself and a `kill-server` on your own socket leaves them alone. To reach one from a plain shell, name that server: `tmux -L agentmgr ls`, then `tmux -L agentmgr attach -t am_<id>`.

## Keys

Tell your agent what you want to review in Agent Manager. Your agent will set up the repository or worktree, target and scope for you to view in the review panel. You can also tell your agent to manage sessions and terminals in Agent Manager; it can set them up and control them for you.

| Key | Action |
|-----|--------|
| `n` | New session (name, tool, directory, worktree toggle, optional starting prompt, group picker) |
| `T` | New terminal tab: a shell under the selected agent, or in the selected group |
| `o` | Open the selected row's directory in your editor |
| `f` | Fork the selected conversation into a named session in the same group and directory |
| `g` | New group (name, parent, default path, worktree default) |
| `enter` | Focus session in place (keys go to the agent, list stays) / fold group |
| `A` | Attach session full screen (Settings can swap it with `enter`) |
| `.` | Mark a finished session idle without entering it |
| `ctrl+q` / `ctrl+\` | Inside a session: back to the manager when the terminal and tmux leave the key available |
| tmux prefix, then `d` | Inside a full-screen attach: back to the manager when the prefix reaches the inner tmux |
| `F3` | Inside a session: open its directory in your editor |
| `→` | Step into the row: focus the session, or open the group. In beta; Settings (`s`) can turn the pair off |
| `←` | Step out: close the group, or — focused, with the caret at the start of the agent's prompt — back to the manager. This needs the tool's prompt marker (its `activity_cutoff`) on the caret's row, so a CLI without one keeps `←` entirely; anywhere else in the prompt it moves the caret as usual |
| `K` / `J` (or `shift+↑` / `shift+↓`) | Reorder session or group among its visible siblings |
| `m` | Move a session to a group, a terminal into a session, or a group under another group |
| `r` | Rename session / edit tool; edit group name and default path |
| `x` | Kill the selected session, or every live session under a group: frees the RAM their agents hold, and the rows stay for `v` |
| `X` | Kill every live session in view |
| `v` | Revive a dead session, or every dead session under a group |
| `V` | Revive every dead session in view |
| `R` | Restart the selected session on an empty context: same name, group, directory and tool |
| `a` / `u` | Archive / restore a session or group. Archive kills the process and keeps the last preview; restore resumes it |
| `d` | Delete session, or a group + its entire subtree |
| `space` | Quick prompt: answer the selected session, or spawn an agent in the selected group |
| `ctrl+r` | Review the selected session's changes: full-screen whole-file diffs, with `c` to comment a line and `C` to send the comments to the agent |
| `F` | Fold / unfold every group |
| `s` | Settings (default tool, theme, theme follows OS, list density, sessions layout, header and computer stats visibility, review layout, after quick send, session keys, ←→ step in/out, spawn in worktree, notifications, notify on finish, CLIs, report a bug, suggest a change, and the version row that updates in place) |
| `\|` | Resize the split: `←→` nudge the divider, `enter` commits, `esc` cancels |
| `t` | Toggle archived view |
| `w` | Filter to sessions that need attention (`waiting`, `finished`, `errored`); press again to show all |
| `M` | Messages (updates, tips; `x` dismisses one for good). The welcome message points at Settings for a bug or an idea. |
| `e` | Hide / show empty groups |
| `/` | Search |
| `?` | The key map for the current screen. From the list it shows every group; from review it shows only review bindings. It scrolls (`↑↓`/`jk`, `pgup`/`pgdn`, `g`/`G`) and `/` searches it down to one line. |
| `q` | Quit (sessions keep running) |

Navigation is keyboard-driven. The manager claims mouse reporting so the wheel stays inside the app and cannot scroll the TUI out of view: a notch scrolls the diff, and in a focused session it walks that pane's scrollback, where click-drag also selects pane text and copies it. Clicking a web link in a focused pane opens it in your browser, in either layout: the mouse claim keeps the terminal's own opener out of reach, so the manager does the opening itself, joining a link the pane wrapped across rows back together first. In a focused agent that tracks the mouse, a click off a link passes straight through to its own clickable UI while a drag still selects and copies; hold `alt` to pass a whole drag through instead, for the agent's own text selection or sliders. In the list the wheel does nothing, since moving the selection with it retargets every key that follows. Fuller mouse support, on by default with a settings toggle, is tracked in [#110](https://github.com/YoanWai/agent-manager/issues/110).

## Quick prompt

Press `space` to dock a prompt bar at the bottom of the sidebar. The target follows the cursor while the bar is open (`↑↓` still navigate):

- On a **session** row, `enter` sends the typed text straight into the session's pane, so the agent gets it as a user message without you attaching. The bar clears and stays open, ready for the next answer; Settings (`s`) can make it close instead.
- On a **group** row, `enter` spawns a new agent in that group and submits the prompt at startup, using the group's default path. This is the shortest path to a fresh agent: `space`, type the task, `enter`, with no form and no name to invent. The spawn tool starts at the Settings default and `tab` (or `alt+m`) cycles it (claude ↔ opencode ↔ any configured tool); the footer shows the current pick. `shift+tab` (or `alt+w`) toggles whether the new agent spawns into its own git worktree, starting from the Settings default; the footer shows `worktree: on` or `worktree: off`, or `worktree: unavailable (not a git repo)` when the target directory cannot hold one. Answering an existing session ignores the toggle, since there is no new session to place in a worktree. The agent starts working on the prompt immediately.

`ctrl+v` pastes an image from the system clipboard as an `[Image #1]` chip at the caret. The image is saved under `agent-manager-pastes` in your temp directory, and on send each chip is swapped back for its path, so the paths reach the agent in the order and the places you pasted them. `backspace` next to a chip removes the whole chip, and an edit that swallows one releases its image. A clipboard holding text rather than an image pastes as text. Pasted images older than seven days are cleared at startup and once a day while the manager runs, so an agent can still open one from an earlier session while temp stays tidy.

`esc` closes the bar.

The new-session form's optional `prompt` field launches an agent the same way. It takes `ctrl+v` and its chips too, since a first task is often the screenshot that explains it: paste the design to match or the crash to read, and the agent opens the file on its first turn. Leaving the form without creating the session releases the images it was holding, the way closing the bar does. Tools whose CLI takes the prompt behind a flag declare it with `prompt_flag`, while a persistent CLI with no startup-prompt argument uses `prompt_mode = "send"` (see [Configuration](configuration.md)).

![answering a working Claude Code session from the prompt bar, without attaching](demo-space.gif)

## Full-screen sessions

The "sessions layout" row in Settings (`s`) hands the session list the whole terminal: `←` or `→` swaps it to `full screen`, the preview column steps aside, and every row takes the full width. The same keys return it to `split`, the default, and the choice is persisted like the split ratio.

Session rows read the same way in either layout, and the list density setting picks their rhythm. Compact keeps a session to one line: mark, name and badges, then a state-picked value riding to the right of the name with `state · tool · age` against the right edge. That value is the agent's last message whenever it has said anything (the question it waits on, its progress, its result) behind a static `↳` washed in the state color, and the task it was given behind the accent's `❯` only while it has not. Comfortable unfolds each session to three lines: the name line, your last message behind a `❯` in the accent, and the agent's last message under it. A terminal keeps two of them, its name line and its last output, since no one is prompting a shell. Both messages come from the session's own record: the prompt echoed into its transcript (typed in an attach, the focus view, or the quick prompt, all the same) and the reply read from above its input box, recovered from the tool's session store when it scrolled away. A `waiting` session's message wears its state color at full strength, the other states at a wash of theirs; a `working` session that has not said anything yet animates a loader. Groups stay one line at any density. In the full-screen layout the meters condense to one labeled line above the key legend: cpu, mem, swap, disk, temperatures and net, plus a messages count that `M` opens. The separate `header` and `computer stats` settings apply in either layout and return hidden chrome to the session list. Messages still appear on their own while there is something to read, then yield those rows when dismissed.

Sessions size themselves to the layout that shows them: the full-screen layout pins their tmux windows to the whole terminal body, so an opened or attached session spans the full width, and switching back to the split re-pins the width while a pane that grew taller keeps its height and is cropped on screen instead.

Opening a session (`enter`, or `→`) takes the whole body too, through the same pipeline focus mode uses in the split: keys go to the agent, `ctrl+r`, `F3` and the footer stay alive, and `ctrl+q`, or `←` with the caret at the start of the agent's prompt, returns to the list. `A` still hands the terminal over with a real tmux attach. Every footer that replaces the key legend for a moment, the quick prompt's, focus's, rename's, resize's, takes a single row in this layout and leaves the rest to the body; in the split those rows are held so the preview box beside them stays where it is. A line of its own says which session you are in, a hairline holding it off the header band above and the pane below: its state dot and name, the tool, the state and how long it has held it on the left, then its directory, its worktree branch, its own cpu and memory, how long ago it started, and a queued count when messages are waiting, against the right edge. The keys are in the footer under the pane. As the terminal narrows the readings give way one at a time, the least telling first, so what fits still shows.

## Which CLIs you get offered

Every configured tool is offered when you create a session, which is more than most people run. Settings (`s`) has a `CLIs` row: `enter` opens a checklist, `space` or `enter` unchecks the tool under the cursor, `esc` saves, and the ones left checked are what the `n` form's `tool` picker and the quick prompt's `tab` cycle through. The last checked tool cannot be unchecked, since a picker with nothing in it could not create a session. It only narrows the pickers, so a session already on an unchecked tool keeps running and revives on that same tool. The last row, `request CLI support`, opens an issue for a CLI we do not ship rules for yet.

## Terminal tabs

`T` opens a shell tab: a session like any other (same list, same row keys, same `enter`, `x`, `v` and `R`) with your shell in the pane instead of an agent. On an agent, the new shell nests under that session, in that agent's group and directory. On a group, it lands in the group as an un-nested sibling, in the group's default path. On a nested shell it joins the same parent; on an un-nested shell it stays un-nested in that shell's group. Either way it opens in that shell's own directory, so a shell you have `cd`'d somewhere hands the next one the same place. A nested shell is named after the session it hangs under, `terminal-review-done` rather than `terminal-0ab5`, and the next one under that session counts up to `terminal-review-done-2`; a shell with no session over it keeps the generated name, and `r` renames any of them. Its status rests at idle throughout: turn tracking belongs to agents, and a shell has no turns.

The shell is the `[tools.terminal]` block in [config.toml](configuration.md). It ships with no command, which leaves the pane on `$SHELL`; set one to open a different shell. What marks it as a shell is `shell = true`, not its name, so a `[tools.terminal]` block you wrote yourself stays the agent CLI you meant it to be.

Shells live in the tree with the agents they belong to, marked with `❯` where an agent carries its status dot. `m` on a terminal moves it onto an agent (nests under that session) or onto a group (un-nests into that group). A group's dots and counts describe its agents, so only agent work shows as in progress.

**The keys that write into a pane refuse a shell.** `space` and the review screen's `C` both paste their text and press Enter, so on a shell a sentence meant for an agent would run as a command. Both say the row is a shell and send nothing; enter the session (`↵`) to type there, where what you type is plainly a command. `f` says the same, since a shell has no conversation to fork.

A shell left on its empty command carries no session id, so `agent-manager rename` run inside one cannot find its session. Rename it from the list with `r`. Give the block a command and the pane gets an id like any other session.

## Opening the editor

`o` opens the row under the cursor in your editor: a session's live working directory (wherever its shell or agent has moved to, not only where it started), the directory it was created in when the live one cannot be read, or a group's default path. It works on a [terminal tab](#terminal-tabs) too — the shell you ran the build in is usually sitting in the directory you want open.

Agent Manager takes the first of these it finds: `editor` in [config.toml](configuration.md), `$AGENT_MANAGER_EDITOR`, a GUI editor on `PATH` (`code`, `cursor`, `windsurf`, `zed`, `subl`, `idea`), then `$VISUAL` or `$EDITOR`. The environment comes last because it usually names the editor you set for git commit messages rather than the one a project should open in.

The line is run directly, never through a shell, so nothing in it is expanded and an `.envrc` that sets `EDITOR` cannot smuggle a command in behind it. Arguments are allowed, and quotes group one that carries a space: `editor = "code -n"`, `editor = "open -a 'Visual Studio Code'"`.

Inside a session, attached or focused, `F3` opens that session's directory the same way. It costs an attach its client, so the manager steps back into the session once a windowed editor is running, or once one that draws in the terminal exits. An editor that fails to start keeps the manager on screen, where you can read why.

Like `ctrl+q` and `ctrl+r`, the manager keeps `F3` for itself inside a session, so a program running in there stops seeing it. Every `ctrl` combination reaches the program instead, `ctrl+o` included: Claude Code shows more lines with it, Gemini CLI toggles copy mode, and in a [terminal tab](#terminal-tabs) `nano` writes the file out.

A known windowed editor (the six above, plus `open` and `xdg-open`) starts detached and the manager stays on screen, with the status line naming what opened. Everything else takes the terminal over the way an attach does and hands it back on exit — that way round because a terminal editor started detached would have nowhere to draw, while a windowed one launched this way only costs a repaint.

## Worktree sessions

A session can spawn into its own git worktree instead of the shared working directory: the `n` form has a `worktree` field between `dir` and `prompt` (`◂ on ▸` / `◂ off ▸`, toggled with `←→`), and the quick prompt's `alt+w` does the same for a group spawn. Settings (`s`) has a "worktree sessions" row that sets the default both start from.

The worktree lives at `<repo>-worktrees/<name>` next to the repo, on a new branch `am/<name>`. Its starting point is the remote's default branch (`origin/HEAD`) when that resolves, falling back to a local `main` or `master`, and finally to `HEAD`. A worktree that fails to create blocks the spawn with an error instead of falling back to a shared directory.

A directory that is not a git repo cannot hold a worktree, so the field and the quick prompt's footer read `unavailable (not a git repo)` in place of on/off, the toggle says why when pressed, and the session spawns in that directory as a plain session. This is what a group path sitting above several repos does: the umbrella itself is not a repo, so its sessions launch in it directly.

Renaming a session (`r`, or the agent's own `agent-manager rename`) keeps its worktree directory at the spawn-time path and renames only its branch to `am/<new name>`. The directory keeps the slug it was given at spawn (for example `claude-7a72`) for as long as the worktree exists, even as the session name and `am/` branch change. The stable directory keeps the running agent's working path valid, while the branch still carries the name that appears in review and the eventual PR. A branch that already exists reports that and keeps the session on its old name. A worktree you have renamed or removed by hand is left alone and the session still takes the new name.

Deleting (`d`) a session that holds a worktree removes the worktree and its branch when it is clean: no uncommitted changes and no commits ahead of its base. A dirty worktree is left in place and its path shown, so nothing is lost. Note that "clean" is judged by `git status`, so gitignored files inside the worktree (a `.env`, local config, build output) are removed along with the directory. Killing, archiving, and reviving a session never touch its worktree.

## Killing and reviving sessions

`x` ends a session that is holding RAM you want back, and on a group row it ends every live session under it; `X` ends every live session in view. Each asks to confirm first, and what it ends is the tmux session, not the record: the row stays in the tree, marked `dead`, with its name, group, and conversation id intact.

![ending every session under a group for the RAM, then reviving the whole subtree on its own conversations](demo-revive.gif)

`v` relaunches a dead session under its old id, keeping its name, group, and history. When the manager holds that session's own conversation id, revive resumes **that exact conversation** through the tool's `resume_by_id_command`: `claude --resume {id}`, `codex resume {id}`, `opencode --session {id}`, `grok --resume {id}`, `gemini --resume {id}`, `pi --session {id}`, `hermes --cli --resume {id}`, `cmd --session {id}`.

Quitting the agent inside a session leaves the window alive on a shell, and `v` brings it back there: the manager types the launch command into that shell, so the agent returns in the pane it left, carrying the session id, the MCP registration and the hook settings a launch gives it. The pane names that key the moment the agent exits. Those values stay exported in the shell as well, so `agent-manager rename` and the rest of the subcommands keep working from it, and so does an agent you start there by hand.

The id arrives one of two ways: tools with a `session_id_flag` launch under an id the manager mints, and tools that mint their own are read back by a `session_store` capturer (`codex`, `opencode`, `gemini`, `hermes`, `command-code`). The comfortable list density shows the id once it is known. Without one, a tool with a `resume_picker_command` opens its own session picker in the pane on revive (`claude --resume`, `codex resume`, `cmd --resume`, `pi --resume`, bare `grok`, `gemini -i /resume`, `hermes --cli sessions browse`), so the user picks the conversation; opencode's picker exists only inside its TUI, so revive launches it bare and the manager types `/sessions` at the composer. A relaunched session binds the picked conversation's id back after the pane settles on it: the manager snapshots the tool's store right before the relaunch and binds only the single conversation whose activity outruns that snapshot on two consecutive polls, so a conversation merely written before the revive is never mistaken for the pick. Tools without a picker fall back to `revive_command` (`claude --continue`), which resumes the working directory's most recent conversation, and the manager says so in the status line, since sessions sharing a directory would otherwise land on the wrong one. On a group row `v` revives every dead session under it, and `V` revives every dead session in view; both revive what they can and name the first failure rather than stopping.

## Restarting a session on an empty context

`R` keeps the row and drops the context: same name, group, tool, and working directory, a managed worktree included, launched on a conversation the agent has never seen. It is what you want when a session has piled up context you are done with, where reviving it would spend the budget re-reading history or land straight in a compact.

It asks to confirm first, and it works on a live session too: the running agent ends, then the fresh one launches. The conversation it was on is retired rather than resumed: the manager mints a new id for tools that take one (`session_id_flag`) and captures the new one for tools that mint their own (`session_store`). The retired conversation is left on disk untouched, and the row stops pointing at it, so a later `v` resumes the conversation the restart started rather than the context it dropped. The row changes hands only once the new agent is up, so a launch that cannot start (a tool gone from `PATH`, a directory that moved) leaves the session on the conversation it had, still there for `v`.

## Forking sessions

1. Select a session and press `f`.
2. Enter a name.
3. Press `enter`.

The fork uses the source session's tool, group, working directory, and conversation history.

Claude Code, OpenCode, Codex, Grok, Gemini CLI, Pi, and Command Code include default fork commands. A custom tool needs a `fork_command` in its configuration. The source session must have a captured conversation ID.

A fork shares its source session's managed worktree. Agent Manager keeps the worktree until you delete the last session that uses it. You cannot rename the worktree while another session uses it.

## Self-naming sessions

Sessions spawned without a custom name (every quick spawn, and the form with the name left blank) get a placeholder like `claude-a1b2`, and their first prompt opens by asking the agent to run `agent-manager rename "<name>"` once with a short name for the broad feature of the session (not a single subtask). The directive also tells the agent not to rename again unless you ask. When the first prompt cannot carry the directive (a `/slash` command, or no prompt at all), the manager sends it as its own message once the tool's input box appears in the pane. The subcommand drops the name into a per-session file; the manager picks it up on the next poll and updates the sidebar row and the tmux status bar. This works with any tool, since it only needs the agent to read its prompt and run one shell command.

Sessions you name yourself keep that name: the first prompt only notes that `agent-manager rename` is available later if you ask, and does not instruct the agent to rename now. You can still ask an agent to rename its session later, or run `agent-manager rename` yourself from a shell inside the session.

## Declaring the repo under review

A session's working directory is often an umbrella folder holding many repos, so review can only guess which one the agent means. An agent that knows which repo it is working in can say so by running `agent-manager review-repo <path>` from a shell inside its session. The subcommand checks that the path is (or sits inside) a git repo, resolves it to the repo root, and drops it into a per-session file; the manager picks it up on the next poll and review opens on that repo the next time you open it. A path that is not inside a git repo is rejected, so a declaration is always a fact rather than a guess.

An agent can also declare what its branch diffs against by running `agent-manager review-base <ref>` from inside its worktree: the ref is validated in that repo, stored per session and repo, and the "vs target" scope uses it from then on. `agent-manager review-base --clear` returns to automatic detection. A stored ref that stops resolving surfaces as an error in review, and `B` opens a target picker (the repo's branches plus an `auto` entry) to set or clear it by hand.

Agents usually work in git worktrees, one branch per worktree, and those worktrees can live anywhere on disk. A declared path that is a worktree root is accepted wherever it lives, so one `review-repo` call names both the repo and the branch under review. Review resolves its target in a fixed order: a repo or worktree you picked by hand with `r` or `b` wins for as long as the manager is running, then the agent's declared repo, then the ranking (dirty working trees first, then most recent commit). When the picked or declared path stops being a git repo, review says so in the status line and `r` is there to pick the right one.

## MCP: how agents discover these commands

Every session of an MCP-capable tool carries the agent-manager MCP server on spawn and revive, so its agent sees the whole workspace as native tools with descriptions telling it when to call each: its own session, the other agent sessions running beside it, the groups they are filed under, and the managed terminals. No per-project setup. Pi is the shipped tool whose agent has no MCP client, so those sessions rely on the subcommands alone. The server lives in the same binary (`agent-manager mcp`, stdio) and identifies the calling session through its environment.

| Tool | Action |
|------|--------|
| `rename` | Rename the calling session |
| `review` | Declare the repo under review, the base ref and the diff scope, in one call |
| `review_comment` | Mark a sent review comment handled after addressing it, or reopen it |
| `list_sessions` | List every agent session with its id, CLI, group, directory, worktree branch and status |
| `create_session` | Start another agent CLI on a named task, optionally in its own git worktree |
| `read_session` | Read what another agent's screen currently shows |
| `send_session` | Queue a message for another agent, delivered once it is at rest |
| `message_status` | Check whether a message is queued, held, delivered, dropped or answered |
| `wait_for_session` | Park until another session stops working, instead of polling it |
| `revive_session` | Bring a dead session back, resuming the conversation it held |
| `kill_session` | Stop a running agent, keeping its row and last screen |
| `archive_session` | File a finished session out of the active list, or restore it |
| `task` | The shared work list in one tool: `action` is `list`, `create`, `claim`, `finish`, `release` or `delete` |
| `reserve_files` | Declare the files this session is editing, and see who else claims them |
| `release_files` | Give those claims back |
| `list_reservations` | See what every session is editing right now |
| `list_groups` | List groups with their default directories, worktree defaults and session counts |
| `delete_group` | remove a group whose work is done; sessions still filed there move to the root rather than stopping |
| `create_group` | Add a group, nested with a slash path, to file a fleet under |
| `list_terminals` | List active managed terminals and their current directories |
| `create_terminal` | Open a terminal under the calling session, or beside it when that session is itself a terminal, unless `nest` is false |
| `send_terminal` | Submit a command or send exact keys to a running terminal |
| `read_terminal` | Read the plain-text content currently visible in a terminal |
| `close_terminal` | Close a finished terminal nested under the caller: kill the pane and delete the row |

### Spawning and steering other agents

`create_session` gives an agent the same spawn the `n` form gives a human: a name, a CLI, a group, a working directory, a first prompt and a worktree choice. A session created this way is a normal row in the list, and the manager picks it up on its next poll, so it attaches, revives, forks and reviews like any other.

Each field falls back the way the form does. The CLI defaults to the one the calling agent runs, the group and directory default to the caller's, an explicit group uses that group's nearest inherited default path, and an explicit directory wins over both. A name is the agent's to choose and should describe the work; leaving it empty generates a placeholder and asks the new session to rename itself, exactly as a promptless spawn from the form does. Passing `worktree: true` adds a git worktree and branch off the directory's repo, which is what keeps several agents working in one project from editing the same checkout; omitting it inherits the group's default, then the global setting.

`read_session` returns the target's current screen, and its last captured screen once the session has stopped. `kill_session` ends the process and leaves the row dead with its last screen, `revive_session` brings it back on the conversation it held, and `archive_session` files a finished row away or restores it. An agent that quit while its window stayed open is relaunched inside that pane, so the row keeps the screen its last life left there.

### Messages between agents

`send_session` queues a message rather than typing it immediately. Several agent CLIs keep their input line drawn underneath an approval dialog, so a message written at that moment would answer the dialog instead of being read. The manager holds it and types it in on the first poll where the target is at rest: its input region is drawn, its status is not mid-turn, its own rules report no dialog on screen, and nobody has a line part way written at its prompt. Delivery is at most once, and a message the manager cannot prove reached the pane is retired as `dropped` rather than repeated, so its sender knows to send it again.

The gate is the recipient tool's own rules, because text typed onto a dialog picks an option: while one is on screen the queue waits, and `message_status` reports those messages as `held`, naming the session to go and answer. A question the agent left at a resting prompt trips no rule, and a message goes in there as an ordinary turn would, labelled as coming from another agent. `held` also covers a recipient the manager will never type into as things stand, a session archived or stopped since the message was queued, and says which it is.

The message arrives labelled as coming from another session rather than from the user, with the sender's name and the id to answer on. The sender's own text sits inside a fence the manager mints at delivery, so a message written to imitate that label reads as what it is: the sender wrote it before the fence existed and cannot reproduce it. A receiving agent treats it as it would any untrusted input: it cannot approve a permission prompt, and it cannot change that session's configuration. Queue caps, a per-sender rate limit, a size cap on one message and a whitespace-insensitive fingerprint keep two agents from talking each other into a loop. `message_status` reports whether a message is queued, held, delivered, dropped or answered; answering a session acknowledges everything it sent.

Delivery needs the manager running, since its poller is what types the message in. A message queued while Agent Manager is closed waits until it opens again, and `send_session` says so in its result rather than implying the message landed. A target that could never be reached is refused outright rather than queued: a session that is not running, an archived one, which the poller skips, and a tool declaring no `activity_cutoff`, which leaves nothing to read readiness from.

### Waiting and the shared task list

`wait_for_session` parks a single tool call until a session reaches one of the states that mean it stopped working, so an agent that spawned work does not read screens in a loop while it waits. A timeout returns the session's current state with `reached` false, because a timeout is an answer rather than a failure; `outcome` separates that from the session dying before it ever reached one of the awaited states. It is an ordinary tool call, which is what makes it work with every MCP client.

The task list is the manager's shared to-do list, visible to every session, all of it behind the one `task` tool. `create` puts work on it, `claim` takes a piece (by id, or the oldest one nothing is blocking), and `finish` marks it done, which unblocks every task that depended on it. A claim is a single atomic write, so two agents racing for the same task cannot both win: the loser is told who holds it. A session that is deleted hands its claims back to the list rather than parking them forever.

### File reservations

A worktree per session stops two agents overwriting one checkout, and that is the right answer whenever the work divides cleanly. It does not help when sessions deliberately share a checkout, and it defers the other kind of collision to merge time: two agents making incompatible decisions about the same interface find out only when the branches meet.

`reserve_files` declares the paths a session is about to edit. Overlap with a lease another session holds comes back as conflicts, naming the holder and what they said they were doing, so the two can settle it through `send_session` before either commits. The lease is advisory throughout: nothing is blocked, and an agent may edit anyway. It expires on its own, so a session that dies holding one never keeps the repo to itself, and `release_files` hands it back as soon as the edits land. An exclusive lease conflicts with any other lease on the same paths; two shared leases sit side by side. Matching compares a pattern against a literal path in both directions, so two patterns that each contain wildcards are only compared exactly, which is another reason these are a conversation starter rather than a lock.

### Terminals

`create_terminal` nests under the calling session unless `nest` is false, and a call from a terminal opens the new shell beside it: under the same agent, or un-nested in the same group when that terminal is itself un-nested. It defaults to the calling agent's group and live pane directory. A group other than the caller's needs `nest: false`, since a nested terminal lives in its parent's group; that group then supplies its nearest inherited default path, and an explicit directory wins over both. `close_terminal` kills the pane and removes the row once the job is finished, and it reaches only the terminals nested under the calling session: a shell someone else opened, or one deliberately left un-nested, is the user's to close. `send_terminal` accepts exactly one of a command, which is pasted and submitted with Enter, or a sequence of tmux key names such as `C-c`, `Up`, and `Enter`. `read_terminal` returns the current screen rather than unlimited scrollback.

The server's MCP initialization instructions teach agents to use these tools without waiting for an explicit request: list sessions and delegate a parallel workstream to a named `create_session` before running it in series, and open a terminal for human-visible work such as SSH, when the user should be able to watch it, attach, or take over. They list and reuse a relevant running terminal first; `create_terminal` nests under the caller unless `nest` is false; they send the command and read its screen while the job runs; and they call `close_terminal` when that job ends, unless the terminal is being left for the user. One-shot local commands stay in the agent's normal tools. The same guidance is repeated in the individual tool descriptions for clients that expose tools but not server instructions.

Every one of these tools acts on the user's machine. Agents should treat `send_terminal` with the same care as typing into an attached shell, and treat `create_session` and `kill_session` as what they are: starting a real agent process that spends tokens, and interrupting one that may be mid-task. Inspect the target returned by `list_sessions` or `list_terminals` first, and read the result before continuing.

Registration is per tool. Claude gets a generated `--mcp-config` file. Codex gets `-c mcp_servers...` overrides. OpenCode gets an `OPENCODE_CONFIG` merge file. Grok, Gemini, and Command Code each get a one-time `mcp add --scope user` entry on their first launch. Hermes gets its own one-time `mcp add` flow, which needs the MCP SDK its installer treats as optional: a Hermes still missing it refuses the spawn with a dialog pointing at `hermes setup`, so a Hermes session always carries these tools. A spawn whose CLI is not on PATH is refused the same way, with the vendor's portable installer for a built-in agent, or the package manager on this machine for anything else.

Pi does not include an MCP client. Its sessions reach the same workspace through the subcommands: `agent-manager --help` lists them, from `sessions`, `spawn`, `send` and `wait` to the shared task list, file reservations, terminals and the review declarations.

A custom tool opts in with `mcp = "<style>"` in its config section. Set `mcp = "none"` to disable registration.

## Diff review

Press `ctrl+r` on a session to open a full-screen review of its repo: changed files with +/− counts on the left, the whole file on the right with syntax highlighting and changed lines tinted, so every edit reads in full context. The diff refreshes as the agent keeps editing.

Tell your agent what you want to review in Agent Manager. Your agent can declare the repository or worktree, target and scope together, so the panel opens on those changes when you press `ctrl+r`, without you configuring each picker.

| Key | Action |
|-----|--------|
| `↑↓` / `jk`, `ctrl+d` / `ctrl+u`, `pgup` / `pgdn` | Scroll the file |
| `g` / `G` | Jump to top / bottom |
| `J` / `K` (or `tab` / `shift+tab`) | Next / previous file |
| `n` / `N` | Jump between changes |
| `u` | Toggle unified and side-by-side |
| `s` | Cycle the scope: uncommitted, vs target, last commit, staged |
| `r` | Pick the repo when the session's directory holds several (type to filter) |
| `b` | Switch review to another of the repo's worktrees, listed by branch |
| `B` | Pick the target (merge-into branch) the "vs target" scope compares against |
| `space` | Mark a file reviewed in the current scope |
| `f` | Show code files only, hiding images, compiled assets and lock files from the list; press again to show them |
| `c` / `d` | Write or drop a draft; mark feedback from a sent round handled or open |
| `C` | Send the current drafts to the agent as one review prompt (`enter` or `y` confirms, `esc` cancels) |
| `o` / `F3` | Open the current file in your editor |
| `?` | Review bindings only; `esc` returns to the review |
| `esc` / `q` | Close the review |

Each changeable value in the header wears its own key, so the scope, layout, repo, and target pills read as `s`, `u`, `r`, `B` legends at a glance.

![review, side by side, with the changed lines tinted in full file context](screenshot-review.png)

Comments and reviewed-file marks are saved as you work and return after Agent Manager restarts. A reviewed mark belongs to the scope it was taken in: it records that you reviewed the exact diff that scope showed, so each scope keeps its own marks and cycling scopes leaves them untouched. `C` sends only the current drafts as one prompt and records them as the next numbered review round. Every sent comment stays inline as the session's review history, labelled with its review round and point number. Open comments keep the accent wash; handled comments settle into a subtle dark green wash, and `d` toggles the status locally.

Each point sent to the agent carries a stable comment id. After addressing it, an MCP-capable agent marks it handled with `review_comment`; an agent using shell commands runs `agent-manager review-comment <comment-id>`. Either can reopen it, with `handled: false` or `--reopen`. The status is stored immediately, so opening review shows the current state at once; a panel already open picks it up on its next refresh. The comment itself remains in place. The header names the latest review round and marks it changed when the scope or the code differs from what was sent. A comment whose original code can no longer be found is marked outdated instead of silently moving to an unrelated line.

![review mode: scrolling a changed file, switching to unified, jumping to the next file, then a line comment sent back to the agent](demo-diff.gif)

## Groups

![folding the tree, creating a nested group, reordering, and archiving one](demo-groups.gif)

Groups are paths (`backend/api/auth`) forming a tree of unlimited depth. Sessions can live at any node, including the root. Create subgroups inline with `g`, reorder both groups and sessions with `K` / `J` (or `shift+↑↓`; the order persists), fold a subtree with `enter` on its row, fold or unfold the whole tree with `F`, hide or restore empty groups visually with `e`, and edit a group's name and default path with `r`. On a session, `r` renames it and `tab` cycles the tool. Quitting one CLI in a session's pane and starting another there moves the row onto that CLI on the next poll, with the status rules and the revive command that come with it. The move needs one answer: exactly one configured tool has to run the binary the pane is running, so two blocks of the same CLI, or a CLI whose process name is its runtime rather than itself (one installed as a node script, say), leave the row where it is and `tab` sets it by hand.

## Status

Each session's tmux pane is polled (default every 2s) to derive a status:

| Mark | Status | Meaning |
|------|--------|---------|
| `◐` | `working` | The agent is busy on a turn |
| `◆` | `waiting` | Blocked on you: a dialog, a permission ask, or a plain-text question |
| `●` | `finished` | Turn ended — an alert that clears to `idle` once you enter the session, or on `.` |
| `✕` | `errored` | The tool reported an error |
| `○` | `idle` | Nothing running |
| `✕` | `dead` | The tmux session is gone |
| `◌` | `starting` | The pane is still launching |

Every row carries its mark, and each state has its own color from the active theme, so a glance down the rail tells you who needs you. The key map (`?`) lists the marks under "the mark on a session row".

A session stuck on the wrong mark is usually a rules question: the `[tools.<name>]` block in your own config is what the poller matches, and it keeps the rules it already has when a release ships better ones. [Configuration](configuration.md) has the two-line reset and how to read the pane the poller reads.

`w` narrows the list to sessions that need attention (`waiting`, `finished`, `errored`). Press again to show every status. An `ATTENTION` badge sits over the list with the key that clears it, and the session counts follow the filter; folds open so matches are not hidden. The archived view (`t`) and hidden empty groups (`e`) label themselves the same way. The badges take whatever room the rail has: padded away from the entries on a tall terminal, tight against them on a short one, and yielding to the entries once the list is down to its last rows.

![the session tree, with a waiting agent's permission prompt in the preview](screenshot-sessions.png)

Each row carries its status and tool inline, and a folded group keeps a count per status so a collapsed subtree still tells you whether anything needs you. Selecting a session shows the tail of its pane on the right, which is how a `waiting` agent's actual question reaches you without attaching. A session with no window left, archived or killed, shows the snapshot taken when it still had one.

Detection matches per-tool regex rules against the visible pane, analyzes the newest turn to tell `finished` from `waiting`, and treats streaming output (content changing between polls) as `working`. A turn that ends without any turn-summary line still resolves: when a `working` pane goes quiet, the turn counts as `finished`, or `waiting` when it ends on a question. Work that outlives the turn which started it (background agents and shells) is matched by `busy_line`, so a turn-end summary keeps reading as `working` while that work runs. A usage or rate-limit banner (`limit_line`) is `errored`. Polling keeps running while you are inside a session, so statuses stay live. The selected session's pane tail renders in the preview panel, and moving the cursor fetches the preview immediately.

For Claude Code, status comes first-hand from [hook events](https://docs.anthropic.com/en/docs/claude-code/hooks) instead of pane guessing: sessions launch with a generated `--settings` file whose hooks write the lifecycle state (`working`, `waiting`, `finished`, `idle`) to a per-session status file that the poller reads first. A `StopFailure` of `rate_limit` writes `errored`. Pane rules still refine it — hooks cannot see a plain-text question, an Esc interrupt, or an error line, so a matching pane verdict upgrades the hook status — and they take over fully as fallback when the hook file is missing or stale. Enabled per tool with `status_source = "claude-hooks"`.

## Notifications

When a session's status flips to `waiting` or `errored`, the manager fires one notification with the session name, tool, and state — once per transition, never per poll — so you can look away from the list without missing an agent that needs you. This is tool-neutral: every configured CLI reaches the same notification path after the manager classifies its status. The macOS desktop path gives waiting, finished, and errored the Funk, Hero, and Basso sounds respectively. Linux sends matching standard sound, icon, category, and urgency hints through `notify-send`; the desktop notification server uses the capabilities it supports and safely ignores the rest. Inside Ghostty or cmux the state travels as an OSC 777 escape to the drawing terminal, which owns its presentation and sound and attributes it to the right window and workspace. Because that escape rides the terminal connection, it also reaches you when the manager runs on a remote host over SSH. Other terminals use the OS desktop path, while a headless box or WSL installation without one falls back to the terminal bell. Settings (`s`) has a `notifications` row that silences them (on by default) and a `notify on finish` row that adds `finished` transitions (off by default).

## Stats

The header shows a fleet summary: per-status session counts, plus `agents total usage: cpu N% · ram M% · X GB` for every live agent's full process tree (shell, agent, and children). CPU is that tree's CPU time over the last poll as a share of total machine capacity (same 0–100% unit as the computer gauge). RAM is resident set as a share of installed memory, with absolute size beside it. The selected session's detail line uses the same scale for that session alone.

The Computer block in the sessions panel shows machine gauges:

- **CPU**: whole-machine utilization (0-100%)
- **Memory**: used/total. On macOS this matches Activity Monitor's Memory Used (resident RAM minus free, speculative, and reclaimable file cache). On Linux it is `Total - MemAvailable`, so file cache is not counted as used.
- **Swap**: used/total of the current swap allocation (`used/total * 100`). On macOS the swap file grows under pressure, so the denominator is the live size from `vm.swapusage`, not a fixed partition.
- **Disk**: fill of the root filesystem (used / (used + available)), with free space from the kernel's available figure
- **Network**: up/down rates on real NICs only (loopback, utun, bridges, and similar virtual interfaces are excluded)
- **Temperature**: `cpu`, `gpu` and `soc` readings in °C, each the hottest sensor in its category, sampled every 5s. Apple Silicon draws no CPU/GPU line, so its dies report as one `soc` figure. A reading appears when the machine exposes that sensor.

On Windows under WSL2, agent-manager runs inside the Linux guest, whose `/proc` describes the VM rather than the machine. There the CPU, memory, disk, and agent-percentage figures come from the Windows host instead, sampled through PowerShell interop every 30 seconds; swap, network, and temperatures remain the guest's own. If interop is unavailable, the guest numbers show unchanged.

## Themes

`s` opens Settings, where `↑↓` move between fields and `←→` change the focused one.

![settings, with the theme picker and its palette swatches](screenshot-settings.png)

Fifteen palettes ship. Nine dark: `classic`, `solarized dark`, `catppuccin mocha`, `tokyo night`, `gruvbox dark`, `nord`, `dracula`, `rosé pine`, and `monochrome`. Six light: `solarized light`, `catppuccin latte`, `tokyo night day`, `gruvbox light`, `rosé pine dawn`, and `paper`. The swatch strip beside the name previews the palette, and the theme applies as you step through it, so the picker is a live preview of the whole UI. The manager also matches the terminal's own background to the palette, so the window has no seam against it, and restores the terminal's background on exit. Your pick is saved with the rest of the state and restored on the next run.

**theme follows OS** resolves the palette at startup from the environment's light/dark preference: the OS setting on macOS and Linux desktops, and the terminal's own background elsewhere, including over SSH. A theme already on the detected side stays; only a mismatch switches, to `classic` or `solarized light`. Your manual pick is kept separately, so turning the toggle off returns to it, and stepping the theme picker by hand turns the toggle off. Agent panes render on the theme's own backdrop, and the pane declares that background to the agent inside it, so an agent that auto-detects its palette resolves to the same side the manager is drawing. A session already running keeps the palette it resolved at launch until it is restarted.

## Updates

`agent-manager update` brings the installed binary to the newest release from a shell, without opening the manager. An install owned by a package manager (Homebrew, mise, Arch) hands the terminal to that manager's upgrade command; a direct install downloads the newest release, verifies its checksum, and swaps the binary in place. `--json` prints the outcome as a record instead of a sentence.
