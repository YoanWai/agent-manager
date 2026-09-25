package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/YoanWai/agent-manager/internal/keybind"
)

type Rule struct {
	State   string `toml:"state"`
	Pattern string `toml:"pattern"`
}

type Tool struct {
	Command string `toml:"command"`
	// Shell marks the tool that opens a plain shell rather than an agent
	// CLI: it is what T spawns, it stays out of the CLI pickers, and the
	// keys that write into a pane refuse it, since a sentence typed at a
	// shell is a command. Carried by the flag, never by the name.
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
	// "gemini", "hermes", "command-code" or "muse").
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
	// ChromeBlock marks a row that owns the rows drawn straight under it,
	// up to the next blank row. A quote steps over the whole block, which
	// covers frame rows whose wording varies or wraps.
	ChromeBlock string `toml:"chrome_block"`
	// BusyLine marks work that outlives the turn which started it, such as
	// background agents. Matching it in the newest turn keeps a turn-end
	// summary from resolving to finished while that work runs.
	BusyLine string `toml:"busy_line"`
	// LimitLine is a usage or rate-limit banner. Matching it in the newest
	// turn is errored even when a turn-end summary or a limit dialog would
	// otherwise settle the turn.
	LimitLine string `toml:"limit_line"`
	// MessageStart marks the first line of a message the tool prints, so a
	// caller quoting the last reply starts at its beginning rather than
	// its tail. Tools without one quote the newest content line instead.
	MessageStart string `toml:"message_start"`
	// BlinkingMarker is the message_start glyph a tool blinks on a step
	// still running. Its off frame captures that cell as a styled blank,
	// which a read of the pane fills back in with this glyph.
	BlinkingMarker string `toml:"blinking_marker"`
	// ToolResult marks the row a tool call's result is drawn under, which
	// a copied reply leaves out. Narrower than chrome_line, which every
	// caller drops: the row quote keeps these.
	ToolResult string `toml:"tool_result"`
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
	// BusyFooter is a line the tool draws under its activity cutoff only
	// while a turn runs. A working rule that matches without it belongs to
	// a turn that died before printing its end marker, which is errored.
	BusyFooter string `toml:"busy_footer"`
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
	Editor string `toml:"editor"`
	// Tools is what the binary ships. The file is never decoded for it, so a
	// [tools.<name>] block left there cannot fail the load.
	Tools        map[string]Tool `toml:"-"`
	IgnoredTools []string        `toml:"-"`
	Keybindings  Keybindings     `toml:"keybindings"`
	SessionKeys  keybind.Table   `toml:"-"`
	ListKeys     keybind.Table   `toml:"-"`
}

type Keybindings struct {
	Session map[string]keybind.Binding `toml:"session"`
	List    map[string]keybind.Binding `toml:"list"`
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
		if err := writeStarter(path); err != nil {
			return Config{}, err
		}
	}
	var cfg Config
	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	builtin, err := Default()
	if err != nil {
		return Config{}, err
	}
	cfg.IgnoredTools = declaredTools(meta)
	cfg.Tools = builtin.Tools
	cfg.applyDefaults()
	if err := cfg.resolveKeys(); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// declaredTools reads the [tools.<name>] headers off the key list, since
// nothing under them is decoded.
func declaredTools(meta toml.MetaData) []string {
	var names []string
	for _, key := range meta.Keys() {
		if len(key) == 2 && key[0] == "tools" {
			names = append(names, key[1])
		}
	}
	sort.Strings(names)
	return names
}

// Default returns the built-in configuration without touching the filesystem.
func Default() (Config, error) {
	var shipped struct {
		Tools map[string]Tool `toml:"tools"`
	}
	if _, err := toml.Decode(builtinTools, &shipped); err != nil {
		return Config{}, err
	}
	cfg := Config{Tools: shipped.Tools}
	cfg.applyDefaults()
	if err := cfg.resolveKeys(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) resolveKeys() error {
	session, err := keybind.SessionTable(c.Keybindings.Session)
	if err != nil {
		return err
	}
	list, err := keybind.ListTable(c.Keybindings.List)
	if err != nil {
		return err
	}
	c.SessionKeys, c.ListKeys = session, list
	return nil
}

func (c Config) keys(scope string) keybind.Table {
	if scope == keybind.ScopeList {
		return c.ListKeys
	}
	return c.SessionKeys
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

// ToolNames lists the configured tools by name, so a sentence naming them
// reads the same on every run.
func (c Config) ToolNames() []string {
	names := make([]string, 0, len(c.Tools))
	for name := range c.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ShellTool returns the shell tool, by name so the pick is stable. The
// binary ships exactly one, so a loaded config always has it.
func (c Config) ShellTool() (string, Tool) {
	chosen := ""
	for name, tool := range c.Tools {
		if tool.Shell && (chosen == "" || name < chosen) {
			chosen = name
		}
	}
	return chosen, c.Tools[chosen]
}

func writeStarter(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(starterConfig), 0o644)
}
