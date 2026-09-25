package config

// starterConfig is the file a first run writes: what the manager reads
// from it, and nothing else.
const starterConfig = `poll_interval = "2s"

# The editor "o" opens a directory in, arguments allowed: "code -n", or
# "open -a 'Visual Studio Code'". Quotes group an argument that carries a
# space; the line is run directly, never through a shell. Left unset,
# Agent Manager takes $AGENT_MANAGER_EDITOR, then the first GUI editor on
# PATH (code, cursor, windsurf, zed, subl, idea), then $VISUAL or $EDITOR.
# editor = "code"

# The keys the manager keeps for itself inside a session; every other key
# reaches the agent. An action takes one key or a list, written as
# ctrl+<key>, alt+<key> or f1..f12, and "none" hands its key to the agent.
# [keybindings.session]
# detach = ["ctrl+q", "ctrl+\\"]
# review = "ctrl+r"
# editor = "f3"

# The keys of the manager's own list, one line per action; Settings > keys
# in the manager names them all. A plain character, a key name (space,
# enter, up, shift+up ...), ctrl+<key>, alt+<key> or f1..f12; "none" turns
# an action off. esc and ctrl+c stay as they are.
# [keybindings.list]
# new_session = "N"
# prompt = ["space", "p"]
# quit = "none"
`

// builtinTools is the only source of tool definitions, so a release that
// fixes a CLI's new screen fixes it for everyone on that release.
const builtinTools = `# Rules are matched top-down against the visible pane text (ANSI stripped);
# first match wins, except a matching waiting rule outranks a working match.
# A limit_line match is errored even when a turn-end summary or a limit
# dialog would otherwise settle the turn.
# When no rule matches, the newest turn decides:
# the content region is the text above the last activity_cutoff match
# (the tool's input box). If the region's last content line — skipping
# chrome_line matches (blanks, separators, input-box borders) — is a
# turn_end marker, the turn just ended: finished, or waiting when the
# line above it carries a question mark. A blocked_line there (e.g. an
# interrupt banner) also derives waiting. Otherwise default_status
# applies, and a region that changed since the previous poll counts as
# working (streaming output often renders without any spinner). A turn
# that closes without any turn_end marker still resolves: when a working
# region stops changing and nothing matches, its last content line
# decides finished versus waiting (question mark waits).

[tools.claude]
command = "claude"
# revive (v) launches a new session with this id, so it can later resume
# that exact conversation regardless of what else ran in the directory
session_id_flag = "--session-id"
resume_by_id_command = "claude --resume {id}"
resume_picker_command = "claude --resume"
fork_command = "claude --resume {id} --fork-session --session-id {new_id} --name {name}"
# fallback when a session predates id tracking: resumes the last conversation there
revive_command = "claude --continue"
# hooks report status events directly; the pane rules below stay as fallback
status_source = "claude-hooks"
default_status = "idle"
activity_cutoff = "(?m)^❯"
turn_end = "^[✻✳✶✽✢·✦✧+*] \\S+ for \\d.*$"
# the effort badge sits right-aligned above the composer while a prompt is typed
chrome_line = "^\\s*[─q]{4,}.*$|^[\\s─q]*$|^\\s*✔ Update installed · Restart to update\\s*$|^\\s*new task\\? /clear to save .*$|^\\s*(?:[○◐●◉◈]|effort:) \\S+ · /effort$|^\\s*(?:✦|effort:) ultracode · "
# a prompt echo or queued message owns its wrapped rows and the send-now
# hint; the spinner owns the tip, effort badge and notices drawn under it;
# the welcome logo owns the version, model and directory beside it
chrome_block = "^❯ |^[✻✳✶✽✢·✦✧+*] \\S+…|^\\s*▐▛███▛█ "
blocked_line = "Interrupted ·"
# recap blocks ("※ recap: …") render below the turn-end summary
trailing_note = "^※"
# a question dialog draws its selected option on the composer's own row
# ("❯ 1. Spaces"), where a numbered draft would sit; this footer under it
# is what tells the two apart
dialog_footer = "(?m)^\\s*Enter to select\\b"
# background agents keep running after the turn that spawned them ends, and
# the line saying so carries the same shape as a turn-end summary:
# "✻ Waiting for 2 background agents to finish". Shells and monitors left
# running ("· 1 shell still running") can outlive their use, so they do not
# count.
busy_line = "^[✻✳✶✽✢·✦✧+*] Waiting for \\d+ background agents? to finish"
# a usage/rate-limit banner sits above the turn-end summary
limit_line = "(?m)You've hit your .+limit"
# every message and tool call opens on a bullet at the left edge; the
# glyph is ⏺ on current Claude Code and ● on older releases
message_start = "^[●⏺] "
# the bullet of a step still running blinks, and its off frame is a blank cell
blinking_marker = "⏺"
# a tool call's result is drawn under this glyph, Claude Code's alone:
# the box-drawing characters a table is built from open content rows too
tool_result = "^\\s*⎿"
# a submitted prompt echoes into the transcript on its own ❯ line
user_echo = "^❯ "
# the composer's placeholder while messages sit queued; without it the
# wording reads back as a typed draft
input_placeholder = "^Press up to (?:edit queued messages|select a queued message)"
rules = [
  # selection dialogs (trust prompt, permission asks, questions) block on the user
  { state = "waiting", pattern = "Enter to confirm" },
  { state = "waiting", pattern = "(?m)^[ \\x{A0}]*❯[ \\x{A0}]+\\d+\\." },
  # spinner row of an active turn, any duration format:
  # "✳ Drizzling… (6s · thinking)" / "✽ Zigzagging… (3m 18s · ↓ 1.4k tokens)"
  { state = "working", pattern = "(?m)^[✻✳✶✽✢·✦✧+*] \\S+… \\(" },
  { state = "working", pattern = "esc to interrupt" },
  { state = "errored", pattern = "(?im)^\\s*error:" },
]

[tools.opencode]
command = "opencode"
# opencode mints its own session id; capture it after launch and resume it
session_store = "opencode"
resume_by_id_command = "opencode --session {id}"
fork_command = "opencode --session {id} --fork"
# opencode's session picker exists only inside the running TUI: /sessions.
# Passing it via the prompt flag would submit it to the model, so revive
# launches bare opencode and the manager types the shortcut at its composer.
resume_picker_command = "opencode"
resume_picker_keys = "/sessions"
revive_command = "opencode --continue"
# opencode's positional argument is the project path, so the optional
# session prompt travels behind this flag
prompt_flag = "--prompt"
default_status = "idle"
activity_cutoff = "(?m)^\\s*╹"
# The composer is the gutter row the caret sits on: opencode keeps the caret
# on the draft's own text row (live-verified, caret tracking every keystroke),
# and parks it at the text-start column of a blank gutter row when the
# composer is empty. The prefix stops at the bar on purpose, since captured
# rows keep their trailing blanks; the blank-continuation and wrapped-line
# rows a multi-line draft adds are told apart by the row above them, which
# carries the same bar with text past it.
input_prefix = "(?m)^\\s*┃"
turn_end = "^\\s*▣ +.+· [\\dhms. ]+\\s*$"
chrome_line = "^\\s*(┃.*)?$"
input_placeholder = "^Ask anything\\.\\.\\."
# a submitted prompt echoes into the transcript inside the same ┃ gutter
# the composer draws; the composer's own block hugs the cutoff and is
# trimmed before the echo is read
user_echo = "^\\s*┃\\s{2,}"
limit_line = "(?i)requires more credits|(?:Usage|Free|Go) limit reached"
# the footer swaps its path for a knight-rider spinner ("■■■⬝⬝⬝⬝⬝") only
# while a turn runs, provider retries included
busy_footer = "(?m)^\\s*[■⬝]+ "
rules = [
  { state = "errored", pattern = "(?i)requires more credits" },
  { state = "errored", pattern = "(?im)^\\s*error\\b" },
  # spinner row while running: "▣  Build · GLM-5.2" (a finished turn
  # gains a duration: "▣  Build · GLM-5.2 · 22.0s")
  { state = "working", pattern = "(?m)^\\s*▣ +[^·\\n]+· [^·\\n]+$" },
  { state = "working", pattern = "esc interrupt" },
]

[tools.codex]
command = "codex"
# codex mints its own session id; capture it after launch and resume it
session_store = "codex"
resume_by_id_command = "codex resume {id}"
resume_picker_command = "codex resume"
fork_command = "codex fork {id}"
# fallback: resumes the most recent session in the working directory
revive_command = "codex resume --last"
default_status = "idle"
activity_cutoff = "(?m)^›"
# a completed turn closes with either a "─ Worked for 12s ─" or bare divider
# above the input box
turn_end = "(?m)^(?:─+ Worked for [\\dhms. ]+─+|─+)$"
chrome_line = "^\\s*─*\\s*$"
# every message and tool call opens on a "• " bullet
message_start = "^• "
# a command's output is drawn under this glyph, on its own indented row
tool_result = "^\\s*└ "
input_placeholder = "^Ask Codex to do anything"
# a submitted prompt echoes into the transcript on its own › line
user_echo = "^› "
limit_line = "(?m)You've hit your usage limit"
rules = [
  # bottom-pane dialogs (command approval, choice prompts, first-run trust)
  # select a numbered option and block on the user's answer
  { state = "waiting", pattern = "(?m)^\\s*›\\s+\\d+\\." },
  { state = "waiting", pattern = "(?m)Press enter to (confirm|continue)\\b" },
  { state = "waiting", pattern = "(?m)enter to submit answer\\b" },
  # active status row is the final row above the input box; anchoring its full
  # shape keeps an answer that quotes "esc to interrupt" from looking active
  { state = "working", pattern = "(?m)^[ \\t]*(?:• )?[^\\n]*\\([\\dhms. ]+ [•·] esc to interrupt\\)(?: · [^\\n]*)?[ \\t]*\\n(?:[ \\t]+└[^\\n]*\\n(?:[ \\t]{4}[^\\n]*\\n)*)?[ \\t\\n]*\\z" },
  { state = "errored", pattern = "(?im)^\\s*■.*\\berror\\b" },
]

[tools.muse]
command = "muse"
session_store = "muse"
resume_by_id_command = "muse resume {id}"
resume_picker_command = "muse resume"
revive_command = "muse resume --last"
# Muse has no CLI interface for registering MCP servers.
mcp = "none"
default_status = "idle"
activity_cutoff = "(?m)^❯"
chrome_line = "^\\s*─+(?: .*)?$|^\\s*$"
message_start = "^◆ "
input_placeholder = "^Type @ to search and insert workspace file paths$"
user_echo = "^❯ "
rules = [
  { state = "waiting", pattern = "(?m)^\\s*Resume a previous session\\s*$" },
  { state = "waiting", pattern = "(?m)^Do you trust this workspace\\?$" },
  { state = "waiting", pattern = "(?m)^> \\d+  " },
  { state = "working", pattern = "(?m)^[◇◈◆] [^\\n]*\\([\\dhms. ]+ · esc to interrupt\\)\\s*$" },
  { state = "errored", pattern = "(?m)^retained session not found: session [^\\n]+ has no saved log(?:\\n[ \\t]*)*\\z" },
]

[tools.grok]
command = "grok"
session_id_flag = "--session-id"
resume_by_id_command = "grok --resume {id}"
fork_command = "grok --resume {id} --fork-session --session-id {new_id}"
# bare grok opens its startup screen, whose "Resume session" picker lets the
# user choose; grok --resume alone would resume the latest instead
resume_picker_command = "grok"
# fallback: resumes the most recent session for the working directory
revive_command = "grok --continue"
default_status = "idle"
# boxed fullscreen and flush-left minimal; indented transcript prompt lines stay out
activity_cutoff = "(?m)^(?:\\s*│ )?❯"
# turn summary above the input box. Grok prints a live "Worked for 1m20s"
# timer while subagents run; only the real end line gains "stop" (and usually
# "[hooks: N]"). Trailing period after the duration is optional.
turn_end = "(?m)^\\s*Worked for [\\dhms. ]+s\\.?(?:\\s|$).*\\bstop\\b"
# box, scrollbar, header, opt-in card, minimal hint, model footer, hook rows
chrome_line = "^\\s*[┃❙│─━╭╮╰╯█▴▾]*\\s*$|^\\s*⎇ |^\\s*Help improve Grok\\b|^\\s*Off by default\\. Opt-in|^\\s*Read Terms and Privacy Policy|^\\s*minimal ·|^\\s*Grok \\d|^\\s*◆ (?:user_prompt_submit|session_start)\\b|^\\s*✓ |^\\s*Shift\\+Tab:"
# the duration line sits under the reply; LastMessage steps over it so the row quotes the reply
trailing_note = "^Worked for "
limit_line = "(?i)You've hit the rate limit|You hit your free usage limit|You've reached your free Grok Build usage limit|usage limit reached|out of credits"
rules = [
  # first-run "Do you trust this directory?" and other y/n prompts block on the user
  { state = "waiting", pattern = "(?m)^\\s*(Yes, proceed|No, quit)\\s{2,}[yn]\\s*$" },
  # an approval dialog replaces the input box; it blocks on the user's choice
  { state = "waiting", pattern = "(?m)^\\s*\\d+/\\d+:select\\b" },
  { state = "waiting", pattern = "(?m)\\d \\([●○]\\) " },
  # active turn: a rotating braille spinner with an elapsed timer
  # ("⠹ Delete file… 2.5s"). A pending approval freezes it to a ◆ glyph.
  { state = "working", pattern = "(?m)[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏] .*… \\d" },
  { state = "errored", pattern = "(?im)^\\s*error:" },
]

[tools.gemini]
command = "gemini"
# revive (v) launches a new session with this id, so it can later resume
# that exact conversation regardless of what else ran in the directory
session_id_flag = "--session-id"
resume_by_id_command = "gemini --resume {id}"
# gemini has no fork flag; --session-file imports a session file as a brand
# new conversation (fresh id), so the fork hands it the source's file. The
# forked id is captured back via the gemini session store.
fork_command = "gemini --session-file {session_file}"
session_store = "gemini"
# /resume in interactive mode opens gemini's saved-conversation picker
resume_picker_command = "gemini -i /resume"
# fallback when a session predates id tracking: resumes the project's most
# recent session
revive_command = "gemini --resume latest"
default_status = "idle"
# the composer line: "> " normally, "! " in shell mode, "* " in yolo mode
activity_cutoff = "(?m)^\\s*[>!*] "
# box borders, the composer's ▄/▀ background rows, the right-aligned
# "? for shortcuts" hint (its ? must not read as a question) and the
# approval-mode banner ("Shift+Tab to accept edits", "auto-accept edits
# shift+tab to manual", ...) are all chrome above the composer
chrome_line = "^\\s*[╭╮╰╯│─▄▀█]*\\s*$|^\\s*\\? for shortcuts\\s*$|^\\s*press tab twice for more\\s*$|^\\s*Press Ctrl\\+O to show more lines.*$|(?i)^\\s*(auto-accept edits |plan |yolo )?\\S*tab\\S* to (accept edits|manual|plan|auto-accept edits)\\b.*$"
limit_line = "Usage limit reached"
# model replies open on a "✦ " glyph
message_start = "^\\s*✦ "
input_placeholder = "^Type your message or @path/to/file"
# a submitted prompt echoes into the transcript on its own "> " line
user_echo = "^\\s*> "
rules = [
  # selected row of an approval/trust dialog, inside its bordered box:
  # "│ ● 1. Allow once"
  { state = "waiting", pattern = "(?m)^[\\s│]*●\\s*\\d+\\." },
  # loading-line tip while a tool call blocks on the user's answer
  { state = "waiting", pattern = "Waiting for user confirmation" },
  # active turn status line: "(esc to cancel, 12s)"
  { state = "working", pattern = "esc to cancel" },
  # error messages render with a "✕ " prefix
  { state = "errored", pattern = "(?m)^✕ " },
]

[tools.hermes]
# The classic REPL exposes stable prompt markers for status and prompt delivery.
command = "hermes --cli"
# Hermes creates its session id on first input and records it in state.db.
session_store = "hermes"
resume_by_id_command = "hermes --cli --resume {id}"
# the interactive session browser; Enter on a row resumes it
resume_picker_command = "hermes --cli sessions browse"
revive_command = "hermes --cli --continue"
# Hermes only accepts startup text through chat -q, which is one-shot and
# exits. Start the real REPL, then submit the prompt when its composer appears.
prompt_mode = "send"
# Hermes sessions carry the agent-manager MCP tools. Registration needs
# Hermes's MCP SDK; when it is missing, the spawn stops and the manager
# offers the pip line that adds it to the Python that runs Hermes.
mcp = "hermes"
default_status = "idle"
activity_cutoff = "(?m)^\\s*(?:\\S+\\s+)?[❯>$#›»→]\\s"
chrome_line = "^\\s*[─╭╮╰╯│┌┐└┘]*\\s*$|^\\s*⚕ .*$|^\\s*[┌╭]─+ .+ ─+[┐╮]\\s*$|^\\s*Initializing agent\\.\\.\\.\\s*$"
# a submitted prompt echoes into the transcript on its own "● " row,
# between the short rules hermes brackets it with
user_echo = "^● "
busy_line = "(?:▶|⚙|⛓) \\d+"
limit_line = "(?i)Rate limited|usage limit reached|Nous Portal rate limit"
rules = [
  { state = "waiting", pattern = "↑/↓ to select, Enter to confirm" },
  { state = "waiting", pattern = "type (?:password|secret).*ESC to skip" },
  { state = "waiting", pattern = "type your answer (?:here )?and press Enter" },
  { state = "waiting", pattern = "(?:Run setup now|Set up a provider now)\\? \\[Y/n\\]" },
  { state = "working", pattern = "msg=interrupt · /queue · /bg · /steer · Ctrl\\+C cancel" },
]

# The terminal tab "T" spawns: a shell in the group's directory, listed
# beside the agents but with nothing running in it. An empty command leaves
# the pane on $SHELL; set one to open a different shell instead. shell = true
# is what marks it: the CLI pickers skip it, and the keys that write into a
# pane refuse it, because a sentence typed at a shell is a command.
[tools.terminal]
command = ""
shell = true
default_status = "idle"
# A generic prompt row (a bare marker, or "yoan@mac ~ %") is where ← can
# hand focus back to the list without costing the shell a keystroke. Up to
# three leading tokens cover user@host-and-path prompts; % and ➜ cover
# stock zsh and oh-my-zsh.
input_prefix = "(?m)^\\s*(?:\\S+\\s+){0,3}[❯>$#›»→%➜]\\s"

[tools.pi]
command = "pi"
session_id_flag = "--session-id"
resume_by_id_command = "pi --session {id}"
fork_command = "pi --fork {id} --session-id {new_id}"
resume_picker_command = "pi --resume"
revive_command = "pi --continue"
# Pi shows a spinner for active work. A resting pane is a finished turn until
# the user acknowledges it; a resumed conversation is already acknowledged.
default_status = "finished"
# The composer is a bare blank row between rules with no marker of its own;
# pi draws its block cursor as a reverse-video space there and parks the
# terminal caret on that cell. Zero width on purpose: any caret position on
# the row is the prompt head, and text before the caret is what rules a
# draft out.
input_prefix = "^"
# Start the activity region at the pane origin. Pane reflow then cannot look
# like streaming output when Agent Manager attaches or detaches. The rows
# that bound the composer are the plain rule and, since pi 0.85, the top
# one with the spinner drawn inside it ("── ⠹ Working ───"); the arrow-step
# head check reads either as the input box's edge rather than a draft.
activity_cutoff = "(?ms)\\A.*^(?:─+[ \\t]+[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏][ \\t]+[^\\n]*?[ \\t]+)?─{8,}[ \\t]*$"
chrome_line = "^[ \\t]*─{8,}[ \\t]*$"
rules = [
  { state = "idle", pattern = "(?ms)^[ \\t]*Resumed session[ \\t]*\\n[ \\t]*\\n─{8,}[ \\t]*\\n(?:[ \\t]*\\n)*─{8,}[ \\t]*(?:\\n[^\\n]*){2,5}[ \\t]*(?:\\n[ \\t]*)*\\z" },
  { state = "waiting", pattern = "(?ms)^[ \\t]*Project trust[ \\t]*\\n.*\\n─{8,}[ \\t]*(?:\\n[ \\t]*)*\\z" },
  { state = "errored", pattern = "(?ms)^[ \\t]*Error:[^\\n]*(?:\\n[ \\t]+[^ \\t\\n][^\\n]*){0,8}\\n[ \\t]*\\n─{8,}[ \\t]*\\n(?:[ \\t]*\\n)*─{8,}[ \\t]*(?:\\n[^\\n]*){2,5}[ \\t]*(?:\\n[ \\t]*)*\\z" },
  { state = "errored", pattern = "(?ms)^[ \\t]*[^\\n]*rate limit reached[^\\n]*\\n[ \\t]*\\n─{8,}[ \\t]*\\n(?:[ \\t]*\\n)*─{8,}[ \\t]*(?:\\n[^\\n]*){2,5}[ \\t]*(?:\\n[ \\t]*)*\\z" },
  { state = "waiting", pattern = "(?ms)\\?[ \\t]*\\n[ \\t]*\\n─{8,}[ \\t]*\\n(?:[ \\t]*\\n)*─{8,}[ \\t]*(?:\\n[^\\n]*){2,5}[ \\t]*(?:\\n[ \\t]*)*\\z" },
  # The spinner sits on a line of its own above the composer, or since pi
  # 0.85 inside the composer's top border ("── ⠹ Working ───"); custom
  # editors keep the standalone shape, so both are live. The composer may
  # hold a draft typed mid-turn.
  { state = "working", pattern = "(?ms)^[ \\t]*(?:[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏][ \\t]+(?:Working|Running|Retrying|Compacting context|Auto-compacting|Context overflow detected, Auto-compacting|Summarizing branch)\\b[^\\n]*\\n[ \\t]*\\n─{8,}[ \\t]*|─+[ \\t]+[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏][ \\t]+(?:Working|Running|Retrying|Compacting context|Auto-compacting|Context overflow detected, Auto-compacting|Summarizing branch)\\b[^\\n]*)\\n(?:(?:[^─\\n][^\\n]*)?\\n)*─{8,}[ \\t]*(?:\\n[^\\n]*){2,5}[ \\t]*(?:\\n[ \\t]*)*\\z" },
]

[tools.command-code]
command = "cmd"
# command-code mints its own session id; capture it after launch and resume it
session_store = "command-code"
resume_by_id_command = "cmd --session {id}"
fork_command = "cmd --session {id} --fork-session --name {name}"
resume_picker_command = "cmd --resume"
# fallback: resumes the most recent conversation for the directory
revive_command = "cmd --continue"
default_status = "idle"
activity_cutoff = "(?m)^❯"
# A turn closes with "✻ Thought for 7 seconds [ctrl+o to expand]" or, for
# shell-running turns, "✻ Worked for 12s"; the expand hint rides the same row.
turn_end = "^\\s*✻ (?:Thought|Worked) for [\\dhms. ]+.*$"
# recap blocks (TASTE, SHELL, TODOS, SEARCH) render below the turn-end
# summary, their continuation rows indented under a └
trailing_note = "^\\s*[A-Z][A-Z]+ {2,}"
# the assistant message opens on a static ⠶ first-row marker
message_start = "^⠶ "
# a submitted prompt echoes into the transcript on its own ❯ line
user_echo = "^❯ "
input_placeholder = "^Ask your question"
limit_line = "^\\s*⚠ You have insufficient credits"
chrome_line = "^\\s*[─]{4,}\\s*$|^# .*$|^[ \\t█]*$|^\\s*\\? for shortcuts.*$|^\\s*» .*$"
# The composer paints its own block cursor inside the placeholder when empty;
# the terminal cursor parks below the footer the whole time. The placeholder
# is how the arrow step knows the caret sits at the head of an empty prompt.
# It shows on a pristine prompt only: once a prompt has been typed the
# composer clears to a bare marker, which reads as empty just the same.
composer_placeholder = "Ask your question..."
rules = [
  # selection dialogs (trust, tool approval, pickers) number their options
  # behind the prompt marker
  { state = "waiting", pattern = "(?m)^\\s*❯ \\d+\\. " },
  # the dialog footer below the options opens the match scope up so the
  # numbered rows above the marker stay visible to the rules; the trust
  # dialog spells it "↑/↓ to navigate" and the approvals "↑/↓ navigate"
  { state = "waiting", pattern = "↑/↓ (?:to )?navigate" },
  # the busy footer under a streaming turn: "○ Channeling…  esc to
  # interrupt • 116m 57s • ↓ 41.1k". The esc hint can drop at narrow
  # widths, and the tail is a duration or a duration plus a token count,
  # never bare digits.
  { state = "working", pattern = "(?m)^ [·○◇☆✧⌘] [^\\n]*?(?:esc to interrupt[ \\t]*•[ \\t]*[\\dhms. ]*[\\ds]| [\\dhms. ]*[\\ds])([ \\t]*•[ \\t]*[↓↑] [\\d.]+k?)?$" },
  { state = "errored", pattern = "(?im)^\\s*(?:⚠ )?Error:" },
]
`
