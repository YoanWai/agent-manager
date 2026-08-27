package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Rule struct {
	State   string `toml:"state"`
	Pattern string `toml:"pattern"`
}

type Tool struct {
	Command string `toml:"command"`
	// Shell marks a block that opens a plain shell rather than an agent
	// CLI: it is what T spawns, it stays out of the CLI pickers, and the
	// keys that write into a pane refuse it, since a sentence typed at a
	// shell is a command. Never inferred, so a tool block only means this
	// when its author said so.
	Shell         bool   `toml:"shell"`
	ReviveCommand string `toml:"revive_command"`
	PromptFlag    string `toml:"prompt_flag"`
	PromptMode    string `toml:"prompt_mode"`
	// SessionIDFlag makes a new session launch with an id we choose (e.g.
	// claude/grok/pi "--session-id <uuid>"), so revive can later resume that
	// exact conversation deterministically.
	SessionIDFlag string `toml:"session_id_flag"`
	// ResumeByIDCommand resumes a specific conversation; "{id}" is replaced
	// with the session's agent id. Preferred over ReviveCommand, which only
	// resumes the working directory's most recent conversation.
	ResumeByIDCommand string `toml:"resume_by_id_command"`
	// ResumePickerCommand launches the tool's own session picker when revive
	// has no captured conversation id, instead of the blind revive_command
	// fallback, which resumes the directory's newest conversation.
	ResumePickerCommand string `toml:"resume_picker_command"`
	// ResumePickerKeys is typed into the pane once the tool's composer shows,
	// for a picker that only exists inside the running TUI (opencode's
	// /sessions). Passing it as a prompt flag would submit it to the model
	// instead of opening the picker.
	ResumePickerKeys string `toml:"resume_picker_keys"`
	// ForkCommand creates a new conversation from an existing one. Templates
	// can use {id}, {session_file}, {new_id}, and {name}; Agent Manager quotes
	// each value. {session_file} needs SessionStore to keep one ("gemini").
	ForkCommand string `toml:"fork_command"`
	// SessionStore names the built-in capturer that reads back the id a tool
	// minted itself when it has no SessionIDFlag ("codex", "opencode",
	// "gemini", "hermes" or "command-code").
	SessionStore string `toml:"session_store"`
	// MCP picks how the agent-manager MCP server is registered into this
	// tool's sessions: "claude", "codex", "opencode", "grok", "gemini",
	// "hermes", "command-code" or "none".
	// Empty uses the tool's config key when it names a known style.
	MCP            string `toml:"mcp"`
	StatusSource   string `toml:"status_source"`
	DefaultStatus  string `toml:"default_status"`
	ActivityCutoff string `toml:"activity_cutoff"`
	TurnEnd        string `toml:"turn_end"`
	ChromeLine     string `toml:"chrome_line"`
	BlockedLine    string `toml:"blocked_line"`
	TrailingNote   string `toml:"trailing_note"`
	// BusyLine marks work that outlives the turn which started it, such as
	// background agents and shells. Matching it in the newest turn keeps a
	// turn-end summary from resolving to finished while that work runs.
	BusyLine string `toml:"busy_line"`
	// LimitLine is a usage or rate-limit banner. Matching it in the newest
	// turn is errored even when a turn-end summary or a limit dialog would
	// otherwise settle the turn.
	LimitLine string `toml:"limit_line"`
	// MessageStart marks the first line of a message the tool prints, so a
	// caller quoting the last reply starts at its beginning rather than
	// its tail. Tools without one quote the newest content line instead.
	MessageStart string `toml:"message_start"`
	// InputPlaceholder is the hint a composer paints on its empty input
	// row; a draft matching it is the tool's wording, not the user's.
	InputPlaceholder string `toml:"input_placeholder"`
	// UserEcho marks a line where the tool echoes a submitted prompt into
	// its transcript, which is how the last thing sent to a session is
	// read back regardless of who typed it or from where.
	UserEcho string `toml:"user_echo"`
	// DialogFooter is a line the tool draws under its input marker while a
	// dialog owns the input box. It tells a dialog that reuses that marker
	// for its selected option from a draft typed at a resting composer, so
	// the rows below the marker join what the rules read.
	DialogFooter string `toml:"dialog_footer"`
	// InputPrefix locates the composer's input row for the arrow-step pair
	// (Left leaving focus at the prompt head). It replaces the reuse of
	// activity_cutoff for that check, for tools whose input line carries no
	// marker the cutoff would find: pi composes on a bare blank row it
	// marks only with its own block cursor, and opencode on one of its
	// blank gutter rows whose blanks a draft replaces. Whether a caret on
	// such a row really sits at the prompt head also reads one row of
	// context above; see caretRowEndsAPromptHead.
	InputPrefix string `toml:"input_prefix"`
	// ComposerPlaceholder is the placeholder text a tool paints inside its
	// empty composer, and a draft replaces. It serves the arrow-step pair
	// for tools whose terminal cursor never enters the composer: the real
	// caret cell cannot say where the composer's caret is, so the visible
	// placeholder is the evidence that it sits at the head of an empty
	// prompt. Left unfocuses only while the placeholder is on screen.
	ComposerPlaceholder string `toml:"composer_placeholder"`
	Rules               []Rule `toml:"rules"`
}

type Config struct {
	PollInterval Duration `toml:"poll_interval"`
	// Editor is the command the o key opens a directory in, arguments
	// included. Empty falls back to $AGENT_MANAGER_EDITOR, then a GUI
	// editor found on PATH, then $VISUAL / $EDITOR.
	Editor string          `toml:"editor"`
	Tools  map[string]Tool `toml:"tools"`
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "agent-manager"), nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

func Load() (Config, error) {
	dir, err := Dir()
	if err != nil {
		return Config{}, err
	}
	return LoadDir(dir)
}

// LoadDir loads the configuration kept in dir. Session-scoped commands
// already receive the manager's config directory, so they must not resolve
// it again from a possibly different process environment.
func LoadDir(dir string) (Config, error) {
	path := filepath.Join(dir, "config.toml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := writeDefault(path); err != nil {
			return Config{}, err
		}
	}
	var cfg Config
	if err := decodeInto(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.backfillToolDefaults(); err != nil {
		return Config{}, err
	}
	cfg.applyDefaults()
	return cfg, nil
}

// backfillToolDefaults fills fields the built-in tools gained after a
// user's config.toml was written: existing tools keep their values, but
// any field left at its zero value inherits the built-in default, and
// tools absent from the file are added. This lets older configs pick up
// new capabilities (a new prompt_flag, extra rules) without a rewrite.
func (c *Config) backfillToolDefaults() error {
	if c.Tools == nil {
		c.Tools = map[string]Tool{}
	}
	builtin, err := Default()
	if err != nil {
		return err
	}
	for name, def := range builtin.Tools {
		user, ok := c.Tools[name]
		if !ok {
			c.Tools[name] = def
			continue
		}
		c.Tools[name] = mergeTool(name, user, def)
	}
	return nil
}

// busyLineAgentsOnly is the busy_line claude shipped with before Claude
// Code started reporting background shells the same way. A config carrying
// it verbatim was written by an older release and takes the current
// pattern; one edited by hand keeps what its author wrote.
const busyLineAgentsOnly = `^[✻✳✶✽✢·✦✧+*] Waiting for \d+ background agents? to finish`

// The command-code matching rules #385 shipped, which no longer match the
// shapes current Command Code draws. A stored config.toml carries these
// verbatim and keeps them over any new default, so mergeTool rewrites
// exactly these stale values the way claude's busy line is rewritten.
const (
	oldCmdTurnEnd    = `^\s*✻ Worked for [\dhms. ]+$`
	oldCmdWorking    = `(?m)^ [·○◇☆✧⌘] \S+.*(?:esc to interrupt| \d+)$`
	oldCmdChromeLine = `^\s*[─]{4,}\s*$|^# .*$|^[ \t█]*$|^\s*\? for shortcuts.*$`
)

// The pi matching rules used to pin the pane tail at exactly two footer
// lines under the composer, so a pi extension drawing a taller footer kept
// every rule from matching in every state. A stored config.toml
// carries these old strings verbatim and keeps them over any new default,
// so mergeTool rewrites exactly these stale values the way the command-code
// rules are rewritten.
const (
	oldPiIdleRule      = `(?ms)^[ \t]*Resumed session[ \t]*\n[ \t]*\n─{8,}[ \t]*\n(?:[ \t]*\n)*─{8,}[ \t]*` + oldPiFooterTail
	oldPiErrorRule     = `(?ms)^[ \t]*Error:[^\n]*(?:\n[ \t]+[^ \t\n][^\n]*){0,8}\n[ \t]*\n─{8,}[ \t]*\n(?:[ \t]*\n)*─{8,}[ \t]*` + oldPiFooterTail
	oldPiRateLimitRule = `(?ms)^[ \t]*[^\n]*rate limit reached[^\n]*\n[ \t]*\n─{8,}[ \t]*\n(?:[ \t]*\n)*─{8,}[ \t]*` + oldPiFooterTail
	oldPiQuestionRule  = `(?ms)\?[ \t]*\n[ \t]*\n─{8,}[ \t]*\n(?:[ \t]*\n)*─{8,}[ \t]*` + oldPiFooterTail
	oldPiWorkingRule   = `(?ms)^[ \t]*[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏][ \t]+(?:Working|Running|Retrying|Compacting context|Auto-compacting|Context overflow detected, Auto-compacting|Summarizing branch)\b[^\n]*\n[ \t]*\n─{8,}[ \t]*\n(?:[ \t]*\n)*─{8,}[ \t]*` + oldPiFooterTail

	oldPiFooterTail = `\n[^\n]*\n[^\n]*[ \t]*(?:\n[ \t]*)*\z`
	newPiFooterTail = `(?:\n[^\n]*){2,5}[ \t]*(?:\n[ \t]*)*\z`
)

// mergeTool returns user with any zero-value field filled from def.
//
// Shell is deliberately not among them. "terminal" is a plausible name for
// a hand-rolled agent block, and backfilling the flag onto one would take
// the user's own tool out of the pickers and refuse to prompt it, without
// saying so. A block is a shell only where its author wrote that.
func mergeTool(name string, user, def Tool) Tool {
	fill := func(dst *string, src string) {
		if *dst == "" {
			*dst = src
		}
	}
	fill(&user.Command, def.Command)
	fill(&user.ReviveCommand, def.ReviveCommand)
	fill(&user.PromptFlag, def.PromptFlag)
	fill(&user.PromptMode, def.PromptMode)
	fill(&user.SessionIDFlag, def.SessionIDFlag)
	fill(&user.ResumeByIDCommand, def.ResumeByIDCommand)
	fill(&user.ResumePickerCommand, def.ResumePickerCommand)
	fill(&user.ResumePickerKeys, def.ResumePickerKeys)
	fill(&user.ForkCommand, def.ForkCommand)
	fill(&user.SessionStore, def.SessionStore)
	fill(&user.MCP, def.MCP)
	fill(&user.StatusSource, def.StatusSource)
	fill(&user.DefaultStatus, def.DefaultStatus)
	fill(&user.ActivityCutoff, def.ActivityCutoff)
	fill(&user.TurnEnd, def.TurnEnd)
	fill(&user.ChromeLine, def.ChromeLine)
	fill(&user.BlockedLine, def.BlockedLine)
	fill(&user.TrailingNote, def.TrailingNote)
	fill(&user.MessageStart, def.MessageStart)
	fill(&user.InputPlaceholder, def.InputPlaceholder)
	fill(&user.UserEcho, def.UserEcho)
	fill(&user.BusyLine, def.BusyLine)
	fill(&user.LimitLine, def.LimitLine)
	fill(&user.DialogFooter, def.DialogFooter)
	fill(&user.InputPrefix, def.InputPrefix)
	fill(&user.ComposerPlaceholder, def.ComposerPlaceholder)
	if name == "claude" && user.BusyLine == busyLineAgentsOnly {
		user.BusyLine = def.BusyLine
	}
	if name == "command-code" {
		if user.TurnEnd == oldCmdTurnEnd {
			user.TurnEnd = def.TurnEnd
		}
		if user.ChromeLine == oldCmdChromeLine {
			user.ChromeLine = def.ChromeLine
		}
		for i, rule := range user.Rules {
			if rule.State != "working" || rule.Pattern != oldCmdWorking {
				continue
			}
			for _, current := range def.Rules {
				if current.State == "working" {
					user.Rules[i] = current
					break
				}
			}
		}
	}
	if name == "pi" {
		for i, rule := range user.Rules {
			switch rule.Pattern {
			case oldPiIdleRule, oldPiErrorRule, oldPiRateLimitRule, oldPiQuestionRule, oldPiWorkingRule:
				user.Rules[i].Pattern = strings.Replace(rule.Pattern, oldPiFooterTail, newPiFooterTail, 1)
			}
		}
	}
	if len(user.Rules) == 0 {
		user.Rules = def.Rules
	} else if name == "codex" {
		for i, rule := range user.Rules {
			if rule.State != "working" || rule.Pattern != `(?m)esc to interrupt\b` {
				continue
			}
			for _, current := range def.Rules {
				if current.State == "working" {
					user.Rules[i] = current
					break
				}
			}
		}
	}
	return user
}

func decodeInto(path string, cfg *Config) error {
	_, err := toml.DecodeFile(path, cfg)
	return err
}

// Default returns the built-in configuration without touching the filesystem.
func Default() (Config, error) {
	var cfg Config
	if _, err := toml.Decode(defaultConfig, &cfg); err != nil {
		return Config{}, err
	}
	cfg.applyDefaults()
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.PollInterval.Duration <= 0 {
		c.PollInterval.Duration = 2 * time.Second
	}
	if c.Tools == nil {
		c.Tools = map[string]Tool{}
	}
	for name, tool := range c.Tools {
		if tool.DefaultStatus == "" {
			tool.DefaultStatus = "idle"
			c.Tools[name] = tool
		}
	}
}

func (c Config) ToolNames() []string {
	names := make([]string, 0, len(c.Tools))
	for name := range c.Tools {
		names = append(names, name)
	}
	return names
}

// ShellTool returns the first shell block by name, making the choice stable
// when a user configures more than one.
func (c Config) ShellTool() (string, Tool, bool) {
	chosen := ""
	for name, tool := range c.Tools {
		if tool.Shell && (chosen == "" || name < chosen) {
			chosen = name
		}
	}
	if chosen == "" {
		return "", Tool{}, false
	}
	return chosen, c.Tools[chosen], true
}

func writeDefault(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultConfig), 0o644)
}

const defaultConfig = `poll_interval = "2s"

# The editor "o" opens a directory in, arguments allowed: "code -n", or
# "open -a 'Visual Studio Code'". Quotes group an argument that carries a
# space; the line is run directly, never through a shell. Left unset,
# Agent Manager takes $AGENT_MANAGER_EDITOR, then the first GUI editor on
# PATH (code, cursor, windsurf, zed, subl, idea), then $VISUAL or $EDITOR.
# editor = "code"

# Rules are matched top-down against the visible pane text (ANSI stripped);
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
chrome_line = "^\\s*[─q]{4,}.*$|^[\\s─q]*$"
blocked_line = "Interrupted ·"
# recap blocks ("※ recap: …") render below the turn-end summary
trailing_note = "^※"
# a question dialog draws its selected option on the composer's own row
# ("❯ 1. Spaces"), where a numbered draft would sit; this footer under it
# is what tells the two apart
dialog_footer = "(?m)^\\s*Enter to select\\b"
# background agents and shells keep running after the turn that spawned them
# ends, and the line saying so carries the same shape as a turn-end summary:
# "✻ Waiting for 2 background agents to finish" / "✻ Cooked for 4s · 2 shells
# still running"
busy_line = "^[✻✳✶✽✢·✦✧+*] (?:Waiting for \\d+ background agents? to finish|.*· \\d+ shells? still running)"
# a usage/rate-limit banner sits above the turn-end summary
limit_line = "(?m)You've hit your .+limit"
# every message and tool call opens on a bullet at the left edge; the
# glyph is ⏺ on current Claude Code and ● on older releases
message_start = "^[●⏺] "
# a submitted prompt echoes into the transcript on its own ❯ line
user_echo = "^❯ "
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
# a turn that ran commands closes with a "─ Worked for 12s ─" divider above
# the input box; purely conversational turns leave no divider and resolve
# through the quiet-region fallback instead
turn_end = "(?m)^─+ Worked for [\\dhms. ]+─"
chrome_line = "^\\s*─*\\s*$"
# every message and tool call opens on a "• " bullet
message_start = "^• "
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
activity_cutoff = "(?m)^\\s*│ ❯"
# turn summary above the input box. Grok prints a live "Worked for 1m20s"
# timer while subagents run; only the real end line gains "stop" (and usually
# "[hooks: N]"). Trailing period after the duration is optional.
turn_end = "(?m)^\\s*Worked for [\\dhms. ]+s\\.?(?:\\s|$).*\\bstop\\b"
# input-box borders plus the right-edge scrollbar block on overflow panes
chrome_line = "^\\s*[┃❙│─╭╮╰╯█]*\\s*$"
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
chrome_line = "^\\s*[╭╮╰╯│─▄▀█]*\\s*$|^\\s*\\? for shortcuts\\s*$|^\\s*press tab twice for more\\s*$|^\\s*Press Ctrl\\+O to show more lines.*$|(?i)^\\s*(auto-accept edits |plan |yolo )?\\S*tab\\S* to (accept edits|manual|plan|auto-accept edits)\\s*$"
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
# points at "hermes setup", which installs it.
mcp = "hermes"
default_status = "idle"
activity_cutoff = "(?m)^\\s*(?:\\S+\\s+)?[❯>$#›»→]\\s"
chrome_line = "^\\s*[─╭╮╰╯│]*\\s*$|^\\s*⚕ .*$"
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
# like streaming output when Agent Manager attaches or detaches.
activity_cutoff = "(?ms)\\A.*^─{8,}[ \\t]*$"
chrome_line = "^[ \\t]*─{8,}[ \\t]*$"
rules = [
  { state = "idle", pattern = "(?ms)^[ \\t]*Resumed session[ \\t]*\\n[ \\t]*\\n─{8,}[ \\t]*\\n(?:[ \\t]*\\n)*─{8,}[ \\t]*(?:\\n[^\\n]*){2,5}[ \\t]*(?:\\n[ \\t]*)*\\z" },
  { state = "waiting", pattern = "(?ms)^[ \\t]*Project trust[ \\t]*\\n.*\\n─{8,}[ \\t]*(?:\\n[ \\t]*)*\\z" },
  { state = "errored", pattern = "(?ms)^[ \\t]*Error:[^\\n]*(?:\\n[ \\t]+[^ \\t\\n][^\\n]*){0,8}\\n[ \\t]*\\n─{8,}[ \\t]*\\n(?:[ \\t]*\\n)*─{8,}[ \\t]*(?:\\n[^\\n]*){2,5}[ \\t]*(?:\\n[ \\t]*)*\\z" },
  { state = "errored", pattern = "(?ms)^[ \\t]*[^\\n]*rate limit reached[^\\n]*\\n[ \\t]*\\n─{8,}[ \\t]*\\n(?:[ \\t]*\\n)*─{8,}[ \\t]*(?:\\n[^\\n]*){2,5}[ \\t]*(?:\\n[ \\t]*)*\\z" },
  { state = "waiting", pattern = "(?ms)\\?[ \\t]*\\n[ \\t]*\\n─{8,}[ \\t]*\\n(?:[ \\t]*\\n)*─{8,}[ \\t]*(?:\\n[^\\n]*){2,5}[ \\t]*(?:\\n[ \\t]*)*\\z" },
  { state = "working", pattern = "(?ms)^[ \\t]*[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏][ \\t]+(?:Working|Running|Retrying|Compacting context|Auto-compacting|Context overflow detected, Auto-compacting|Summarizing branch)\\b[^\\n]*\\n[ \\t]*\\n─{8,}[ \\t]*\\n(?:[ \\t]*\\n)*─{8,}[ \\t]*(?:\\n[^\\n]*){2,5}[ \\t]*(?:\\n[ \\t]*)*\\z" },
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
