package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

type Rule struct {
	State   string `toml:"state"`
	Pattern string `toml:"pattern"`
}

type Tool struct {
	Command       string `toml:"command"`
	ReviveCommand string `toml:"revive_command"`
	PromptFlag    string `toml:"prompt_flag"`
	// SessionIDFlag makes a new session launch with an id we choose (e.g.
	// claude/grok/pi "--session-id <uuid>"), so revive can later resume that
	// exact conversation deterministically.
	SessionIDFlag string `toml:"session_id_flag"`
	// ResumeByIDCommand resumes a specific conversation; "{id}" is replaced
	// with the session's agent id. Preferred over ReviveCommand, which only
	// resumes the working directory's most recent conversation.
	ResumeByIDCommand string `toml:"resume_by_id_command"`
	// ForkCommand creates a new conversation from an existing one. Templates
	// can use {id}, {new_id}, and {name}; Agent Manager quotes each value.
	ForkCommand string `toml:"fork_command"`
	// SessionStore names the built-in capturer that reads back the id a tool
	// minted itself when it has no SessionIDFlag ("codex" or "opencode").
	SessionStore string `toml:"session_store"`
	// MCP picks how the agent-manager MCP server is registered into this
	// tool's sessions: "claude", "codex", "opencode", "grok", "gemini" or
	// "none".
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
	// background agents. Matching it in the newest turn keeps a turn-end
	// summary from resolving to finished while that work runs.
	BusyLine string `toml:"busy_line"`
	Rules    []Rule `toml:"rules"`
}

type Config struct {
	PollInterval Duration        `toml:"poll_interval"`
	Tools        map[string]Tool `toml:"tools"`
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
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
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
		c.Tools[name] = mergeTool(user, def)
	}
	return nil
}

// mergeTool returns user with any zero-value field filled from def.
func mergeTool(user, def Tool) Tool {
	fill := func(dst *string, src string) {
		if *dst == "" {
			*dst = src
		}
	}
	fill(&user.Command, def.Command)
	fill(&user.ReviveCommand, def.ReviveCommand)
	fill(&user.PromptFlag, def.PromptFlag)
	fill(&user.SessionIDFlag, def.SessionIDFlag)
	fill(&user.ResumeByIDCommand, def.ResumeByIDCommand)
	fill(&user.ForkCommand, def.ForkCommand)
	fill(&user.SessionStore, def.SessionStore)
	fill(&user.StatusSource, def.StatusSource)
	fill(&user.DefaultStatus, def.DefaultStatus)
	fill(&user.ActivityCutoff, def.ActivityCutoff)
	fill(&user.TurnEnd, def.TurnEnd)
	fill(&user.ChromeLine, def.ChromeLine)
	fill(&user.BlockedLine, def.BlockedLine)
	fill(&user.TrailingNote, def.TrailingNote)
	fill(&user.BusyLine, def.BusyLine)
	if len(user.Rules) == 0 {
		user.Rules = def.Rules
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

func writeDefault(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultConfig), 0o644)
}

const defaultConfig = `poll_interval = "2s"

# Rules are matched top-down against the visible pane text (ANSI stripped);
# first match wins, except a matching waiting rule outranks a working match.
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
# background agents keep running after the turn that spawned them ends, and
# their wait line carries the same shape as a turn-end summary
busy_line = "^[✻✳✶✽✢·✦✧+*] Waiting for \\d+ background agents? to finish"
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
revive_command = "opencode --continue"
# opencode's positional argument is the project path, so the optional
# session prompt travels behind this flag
prompt_flag = "--prompt"
default_status = "idle"
activity_cutoff = "(?m)^\\s*╹"
turn_end = "^\\s*▣ +.+· [\\dhms. ]+\\s*$"
chrome_line = "^\\s*(┃.*)?$"
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
rules = [
  # bottom-pane dialogs (command approval, choice prompts, first-run trust)
  # select a numbered option and block on the user's answer
  { state = "waiting", pattern = "(?m)^\\s*›\\s+\\d+\\." },
  { state = "waiting", pattern = "(?m)Press enter to (confirm|continue)\\b" },
  { state = "waiting", pattern = "(?m)enter to submit answer\\b" },
  # active turn status line: "• Working (0s • esc to interrupt)" / "Analyzing"
  { state = "working", pattern = "(?m)esc to interrupt\\b" },
  { state = "errored", pattern = "(?m)You've hit your usage limit" },
  { state = "errored", pattern = "(?im)^\\s*■.*\\berror\\b" },
]

[tools.grok]
command = "grok"
session_id_flag = "--session-id"
resume_by_id_command = "grok --resume {id}"
fork_command = "grok --resume {id} --fork-session --session-id {new_id}"
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

[tools.pi]
command = "pi"
session_id_flag = "--session-id"
resume_by_id_command = "pi --session {id}"
fork_command = "pi --fork {id} --session-id {new_id}"
revive_command = "pi --continue"
# Pi shows a spinner for active work. A resting pane is a finished turn until
# the user acknowledges it; a resumed conversation is already acknowledged.
default_status = "finished"
# Start the activity region at the pane origin. Pane reflow then cannot look
# like streaming output when Agent Manager attaches or detaches.
activity_cutoff = "(?ms)\\A.*^─{8,}[ \\t]*$"
chrome_line = "^[ \\t]*─{8,}[ \\t]*$"
rules = [
  { state = "idle", pattern = "(?ms)^[ \\t]*Resumed session[ \\t]*\\n[ \\t]*\\n─{8,}[ \\t]*\\n(?:[ \\t]*\\n)*─{8,}[ \\t]*\\n[^\\n]*\\n[^\\n]*[ \\t]*(?:\\n[ \\t]*)*\\z" },
  { state = "waiting", pattern = "(?ms)^[ \\t]*Project trust[ \\t]*\\n.*\\n─{8,}[ \\t]*(?:\\n[ \\t]*)*\\z" },
  { state = "errored", pattern = "(?ms)^[ \\t]*Error:[^\\n]*(?:\\n[ \\t]+[^ \\t\\n][^\\n]*){0,8}\\n[ \\t]*\\n─{8,}[ \\t]*\\n(?:[ \\t]*\\n)*─{8,}[ \\t]*\\n[^\\n]*\\n[^\\n]*[ \\t]*(?:\\n[ \\t]*)*\\z" },
  { state = "waiting", pattern = "(?ms)\\?[ \\t]*\\n[ \\t]*\\n─{8,}[ \\t]*\\n(?:[ \\t]*\\n)*─{8,}[ \\t]*\\n[^\\n]*\\n[^\\n]*[ \\t]*(?:\\n[ \\t]*)*\\z" },
  { state = "working", pattern = "(?ms)^[ \\t]*[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏][ \\t]+(?:Working|Running|Retrying|Compacting context|Auto-compacting|Context overflow detected, Auto-compacting|Summarizing branch)\\b[^\\n]*\\n[ \\t]*\\n─{8,}[ \\t]*\\n(?:[ \\t]*\\n)*─{8,}[ \\t]*\\n[^\\n]*\\n[^\\n]*[ \\t]*(?:\\n[ \\t]*)*\\z" },
]
`
