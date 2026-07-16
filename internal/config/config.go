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
	DefaultStatus  string `toml:"default_status"`
	ActivityCutoff string `toml:"activity_cutoff"`
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

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.Duration.String()), nil
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
	cfg.applyDefaults()
	return cfg, nil
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
# first match wins, default_status applies when nothing matches.
#
# activity_cutoff marks where the tool's input box begins: the text above
# its last match is the content region. When no rule matches but that
# region changed since the previous poll, the session counts as working
# (streaming output often renders without any spinner).

[tools.claude]
command = "claude"
default_status = "idle"
activity_cutoff = "(?m)^❯"
rules = [
  # active turn: "✳ Drizzling… (6s · thinking with medium effort)"
  { state = "working", pattern = "… \\(\\d+m?\\d*s? ·" },
  { state = "working", pattern = "esc to interrupt" },
  # selection dialogs (trust prompt, permission asks, questions) block on the user
  { state = "waiting", pattern = "Enter to confirm" },
  { state = "waiting", pattern = "(?m)^[ \\x{A0}]*❯[ \\x{A0}]+\\d+\\." },
  # an interrupted turn asks what to do next
  { state = "waiting", pattern = "Interrupted[^\\n]*\\s*\\n─+\\n❯" },
  # a turn that ends on a question line is blocked on the user even
  # without a selection menu; the summary must sit directly above the
  # prompt so questions from older turns cannot retrigger it
  { state = "waiting", pattern = "\\?\\s*\\n\\s*[✻✳✶✽✢] \\S+ for [\\dm.]+s\\s*\\n─+\\n❯" },
  # turn-end summary directly above the prompt = finished
  { state = "finished", pattern = "[✻✳✶✽✢] \\S+ for [\\dm.]+s\\s*\\n─+\\n❯" },
  { state = "errored", pattern = "(?im)^\\s*error:" },
]

[tools.opencode]
command = "opencode"
default_status = "idle"
activity_cutoff = "(?m)^\\s*╹"
rules = [
  { state = "errored", pattern = "(?i)requires more credits" },
  { state = "errored", pattern = "(?im)^\\s*error\\b" },
  # spinner row while running: "▣  Build · GLM-5.2" (a finished turn
  # gains a duration: "▣  Build · GLM-5.2 · 22.0s")
  { state = "working", pattern = "(?m)^\\s*▣ +[^·\\n]+· [^·\\n]+$" },
  { state = "working", pattern = "esc interrupt" },
  # newest turn ended on a question line = blocked on the user; the
  # duration row must sit directly above the input box
  { state = "waiting", pattern = "\\?\\s*\\n\\s*▣ [^\\n]*· [\\d][\\dm.]*s(\\n[ \\t]*(┃[^\\n]*)?)*\\n\\s*╹" },
  # a model row with a duration directly above the input box = finished
  { state = "finished", pattern = "▣ +[^\\n]+· [\\d][\\dm.]*s(\\n[ \\t]*(┃[^\\n]*)?)*\\n\\s*╹" },
  { state = "finished", pattern = "Ask anything" },
]
`
