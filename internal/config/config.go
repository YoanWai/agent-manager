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
	// claude/grok "--session-id <uuid>"), so revive can later resume that
	// exact conversation deterministically.
	SessionIDFlag string `toml:"session_id_flag"`
	// ResumeByIDCommand resumes a specific conversation; "{id}" is replaced
	// with the session's agent id. Preferred over ReviveCommand, which only
	// resumes the working directory's most recent conversation.
	ResumeByIDCommand string `toml:"resume_by_id_command"`
	// SessionStore names the built-in capturer that reads back the id a tool
	// minted itself when it has no SessionIDFlag ("codex" or "opencode").
	SessionStore string `toml:"session_store"`
	// MCP picks how the agent-manager MCP server is registered into this
	// tool's sessions: "claude", "codex", "opencode", "grok" or "none".
	// Empty uses the tool's config key when it names a known style.
	MCP            string `toml:"mcp"`
	StatusSource   string `toml:"status_source"`
	DefaultStatus  string `toml:"default_status"`
	ActivityCutoff string `toml:"activity_cutoff"`
	TurnEnd        string `toml:"turn_end"`
	ChromeLine     string `toml:"chrome_line"`
	BlockedLine    string `toml:"blocked_line"`
	TrailingNote   string `toml:"trailing_note"`
	Rules          []Rule `toml:"rules"`
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
	fill(&user.SessionStore, def.SessionStore)
	fill(&user.StatusSource, def.StatusSource)
	fill(&user.DefaultStatus, def.DefaultStatus)
	fill(&user.ActivityCutoff, def.ActivityCutoff)
	fill(&user.TurnEnd, def.TurnEnd)
	fill(&user.ChromeLine, def.ChromeLine)
	fill(&user.BlockedLine, def.BlockedLine)
	fill(&user.TrailingNote, def.TrailingNote)
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
# first match wins. When no rule matches, the newest turn decides:
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
rules = [
  # spinner row of an active turn, any duration format:
  # "✳ Drizzling… (6s · thinking)" / "✽ Zigzagging… (3m 18s · ↓ 1.4k tokens)"
  { state = "working", pattern = "(?m)^[✻✳✶✽✢·✦✧+*] \\S+… \\(" },
  { state = "working", pattern = "esc to interrupt" },
  # selection dialogs (trust prompt, permission asks, questions) block on the user
  { state = "waiting", pattern = "Enter to confirm" },
  { state = "waiting", pattern = "(?m)^[ \\x{A0}]*❯[ \\x{A0}]+\\d+\\." },
  { state = "errored", pattern = "(?im)^\\s*error:" },
]

[tools.opencode]
command = "opencode"
# opencode mints its own session id; capture it after launch and resume it
session_store = "opencode"
resume_by_id_command = "opencode --session {id}"
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
# fallback: resumes the most recent session for the working directory
revive_command = "grok --continue"
default_status = "idle"
activity_cutoff = "(?m)^\\s*│ ❯"
# turn summary above the input box, e.g. "Worked for 5.0s." / "Worked for 25s."
turn_end = "(?m)^\\s*Worked for [\\dhms. ]+s\\."
chrome_line = "^\\s*[┃❙│─╭╮╰╯]*\\s*$"
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
`
