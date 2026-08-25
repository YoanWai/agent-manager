package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadWritesAndParsesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := writeDefault(path); err != nil {
		t.Fatalf("writeDefault: %v", err)
	}
	var cfg Config
	if err := decodeInto(path, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	cfg.applyDefaults()

	if cfg.PollInterval.Duration != 2*time.Second {
		t.Fatalf("poll interval = %v want 2s", cfg.PollInterval.Duration)
	}
	if _, ok := cfg.Tools["claude"]; !ok {
		t.Fatal("expected claude tool in default config")
	}
	if _, ok := cfg.Tools["opencode"]; !ok {
		t.Fatal("expected opencode tool in default config")
	}
	if _, ok := cfg.Tools["codex"]; !ok {
		t.Fatal("expected codex tool in default config")
	}
	if cfg.Tools["codex"].Command != "codex" {
		t.Fatalf("codex command = %q", cfg.Tools["codex"].Command)
	}
	if got := cfg.Tools["codex"].ReviveCommand; got != "codex resume --last" {
		t.Fatalf("codex revive_command = %q want \"codex resume --last\"", got)
	}
	if got := cfg.Tools["codex"].PromptFlag; got != "" {
		t.Fatalf("codex prompt_flag = %q want empty (positional prompt)", got)
	}
	if _, ok := cfg.Tools["grok"]; !ok {
		t.Fatal("expected grok tool in default config")
	}
	if cfg.Tools["grok"].Command != "grok" {
		t.Fatalf("grok command = %q", cfg.Tools["grok"].Command)
	}
	if got := cfg.Tools["grok"].ReviveCommand; got != "grok --continue" {
		t.Fatalf("grok revive_command = %q want \"grok --continue\"", got)
	}
	if got := cfg.Tools["grok"].PromptFlag; got != "" {
		t.Fatalf("grok prompt_flag = %q want empty (positional prompt)", got)
	}
	if _, ok := cfg.Tools["gemini"]; !ok {
		t.Fatal("expected gemini tool in default config")
	}
	if cfg.Tools["gemini"].Command != "gemini" {
		t.Fatalf("gemini command = %q", cfg.Tools["gemini"].Command)
	}
	if got := cfg.Tools["gemini"].ReviveCommand; got != "gemini --resume latest" {
		t.Fatalf("gemini revive_command = %q want \"gemini --resume latest\"", got)
	}
	if got := cfg.Tools["gemini"].PromptFlag; got != "" {
		t.Fatalf("gemini prompt_flag = %q want empty (positional prompt)", got)
	}
	if got := cfg.Tools["pi"].Command; got != "pi" {
		t.Fatalf("pi command = %q want pi", got)
	}
	if got := cfg.Tools["pi"].ReviveCommand; got != "pi --continue" {
		t.Fatalf("pi revive_command = %q want \"pi --continue\"", got)
	}
	if got := cfg.Tools["pi"].PromptFlag; got != "" {
		t.Fatalf("pi prompt_flag = %q want empty (positional prompt)", got)
	}
	if got := cfg.Tools["pi"].InputPrefix; got != "^" {
		t.Fatalf("pi input_prefix = %q want ^ (blank composer row)", got)
	}
	if got := cfg.Tools["opencode"].InputPrefix; got != `(?m)^\s*┃` {
		t.Fatalf("opencode input_prefix = %q want the gutter bar", got)
	}
	commandCode := cfg.Tools["command-code"]
	if commandCode.Command != "cmd" {
		t.Fatalf("command-code command = %q want cmd", commandCode.Command)
	}
	if got := commandCode.ReviveCommand; got != "cmd --continue" {
		t.Fatalf("command-code revive_command = %q want \"cmd --continue\"", got)
	}
	if commandCode.SessionStore != "command-code" || commandCode.ResumeByIDCommand != "cmd --session {id}" {
		t.Fatalf("command-code resume config = %+v", commandCode)
	}
	if got := commandCode.ForkCommand; got != "cmd --session {id} --fork-session --name {name}" {
		t.Fatalf("command-code fork_command = %q want \"cmd --session {id} --fork-session --name {name}\"", got)
	}
	if got := commandCode.PromptFlag; got != "" {
		t.Fatalf("command-code prompt_flag = %q want empty (positional prompt)", got)
	}
	if got := commandCode.TurnEnd; got != `^\s*✻ (?:Thought|Worked) for [\dhms. ]+.*$` {
		t.Fatalf("command-code turn_end = %q want the Thought/Worked shape", got)
	}
	if got := commandCode.ComposerPlaceholder; got != "Ask your question..." {
		t.Fatalf("command-code composer_placeholder = %q want the placeholder", got)
	}
	if got := commandCode.ActivityCutoff; got == "" {
		t.Fatalf("command-code activity_cutoff is empty; prompt delivery needs it")
	}
	if got := commandCode.MCP; got != "" {
		t.Fatalf("command-code mcp = %q want empty (style inferred from the tool key)", got)
	}
	if cfg.Tools["claude"].Command != "claude" {
		t.Fatalf("claude command = %q", cfg.Tools["claude"].Command)
	}
	if got := cfg.Tools["opencode"].PromptFlag; got != "--prompt" {
		t.Fatalf("opencode prompt_flag = %q want --prompt", got)
	}
	if got := cfg.Tools["claude"].PromptFlag; got != "" {
		t.Fatalf("claude prompt_flag = %q want empty (positional prompt)", got)
	}
	if got := cfg.Tools["claude"].ForkCommand; got != "claude --resume {id} --fork-session --session-id {new_id} --name {name}" {
		t.Fatalf("claude fork_command = %q", got)
	}
	if got := cfg.Tools["codex"].ForkCommand; got != "codex fork {id}" {
		t.Fatalf("codex fork_command = %q want \"codex fork {id}\"", got)
	}
	if got := cfg.Tools["opencode"].ForkCommand; got != "opencode --session {id} --fork" {
		t.Fatalf("opencode fork_command = %q want \"opencode --session {id} --fork\"", got)
	}
	if got := cfg.Tools["grok"].ForkCommand; got != "grok --resume {id} --fork-session --session-id {new_id}" {
		t.Fatalf("grok fork_command = %q want \"grok --resume {id} --fork-session --session-id {new_id}\"", got)
	}
	if got := cfg.Tools["pi"].ForkCommand; got != "pi --fork {id} --session-id {new_id}" {
		t.Fatalf("pi fork_command = %q want \"pi --fork {id} --session-id {new_id}\"", got)
	}
	if got := cfg.Tools["gemini"].ForkCommand; got != "gemini --session-file {session_file}" {
		t.Fatalf("gemini fork_command = %q want \"gemini --session-file {session_file}\"", got)
	}
	if got := cfg.Tools["gemini"].SessionStore; got != "gemini" {
		t.Fatalf("gemini session_store = %q want \"gemini\" (captures the fork's minted id)", got)
	}
	hermes := cfg.Tools["hermes"]
	if hermes.Command != "hermes --cli" {
		t.Fatalf("hermes command = %q", hermes.Command)
	}
	if hermes.PromptMode != "send" {
		t.Fatalf("hermes prompt_mode = %q want send", hermes.PromptMode)
	}
	if hermes.SessionStore != "hermes" || hermes.ResumeByIDCommand != "hermes --cli --resume {id}" {
		t.Fatalf("hermes resume config = %+v", hermes)
	}
	if hermes.MCP != "hermes" {
		t.Fatalf("hermes mcp = %q want hermes (sessions must carry the MCP tools)", hermes.MCP)
	}
}

func TestLoadDirWritesDefaultInRequestedDirectory(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if _, ok := cfg.Tools["terminal"]; !ok {
		t.Fatal("default terminal tool is missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); err != nil {
		t.Fatalf("config file: %v", err)
	}
}

func TestLoadDirUpgradesLegacyCodexWorkingRule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	legacy := `
[tools.codex]
command = "codex"
rules = [
  { state = "working", pattern = "(?m)esc to interrupt\\b" },
]
`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	rule := cfg.Tools["codex"].Rules[0]
	if rule.Pattern == `(?m)esc to interrupt\b` {
		t.Fatal("legacy Codex working rule was not upgraded")
	}
	if !strings.Contains(rule.Pattern, `\z`) {
		t.Fatalf("upgraded Codex working rule is not scoped to the activity-region tail: %q", rule.Pattern)
	}
}

func TestLoadDirUpgradesLegacyClaudeBusyLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	legacy := `
[tools.claude]
command = "claude"
busy_line = '` + busyLineAgentsOnly + `'
`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if !strings.Contains(cfg.Tools["claude"].BusyLine, "shells? still running") {
		t.Fatalf("legacy claude busy_line was not upgraded: %q", cfg.Tools["claude"].BusyLine)
	}
}

func TestLoadDirBackfillsLimitLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[tools.claude]\ncommand = \"claude\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if !strings.Contains(cfg.Tools["claude"].LimitLine, "You've hit your") {
		t.Fatalf("claude limit_line was not backfilled: %q", cfg.Tools["claude"].LimitLine)
	}
	if !strings.Contains(cfg.Tools["codex"].LimitLine, "You've hit your usage limit") {
		t.Fatalf("codex limit_line was not backfilled: %q", cfg.Tools["codex"].LimitLine)
	}
	if !strings.Contains(cfg.Tools["gemini"].LimitLine, "Usage limit reached") {
		t.Fatalf("gemini limit_line was not backfilled: %q", cfg.Tools["gemini"].LimitLine)
	}
	if !strings.Contains(cfg.Tools["opencode"].LimitLine, "limit reached") {
		t.Fatalf("opencode limit_line was not backfilled: %q", cfg.Tools["opencode"].LimitLine)
	}
	if !strings.Contains(cfg.Tools["grok"].LimitLine, "rate limit") {
		t.Fatalf("grok limit_line was not backfilled: %q", cfg.Tools["grok"].LimitLine)
	}
	if !strings.Contains(cfg.Tools["hermes"].LimitLine, "Rate limited") {
		t.Fatalf("hermes limit_line was not backfilled: %q", cfg.Tools["hermes"].LimitLine)
	}
}

func TestLoadDirBackfillsDialogFooter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[tools.claude]\ncommand = \"claude\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if !strings.Contains(cfg.Tools["claude"].DialogFooter, "Enter to select") {
		t.Fatalf("claude dialog_footer was not backfilled: %q", cfg.Tools["claude"].DialogFooter)
	}
}

func TestLoadDirPreservesCustomClaudeBusyLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	custom := `
[tools.claude]
command = "claude"
busy_line = "my own busy signal"
`
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if got := cfg.Tools["claude"].BusyLine; got != "my own busy signal" {
		t.Fatalf("claude busy_line = %q want the user's own pattern", got)
	}
}

func TestLoadDirPreservesCustomCodexWorkingRule(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	custom := `
[tools.codex]
command = "codex"
rules = [
  { state = "working", pattern = "my private status signal" },
]
`
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if got := cfg.Tools["codex"].Rules[0].Pattern; got != "my private status signal" {
		t.Fatalf("custom Codex working rule = %q", got)
	}
}

func TestShellToolUsesFlagAndStableName(t *testing.T) {
	cfg := Config{Tools: map[string]Tool{
		"terminal": {Command: "agent", Shell: false},
		"zsh":      {Command: "zsh", Shell: true},
		"bash":     {Command: "bash", Shell: true},
	}}
	name, tool, ok := cfg.ShellTool()
	if !ok || name != "bash" || tool.Command != "bash" {
		t.Fatalf("ShellTool = %q %+v %v, want bash", name, tool, ok)
	}
}

func TestDefaultWaitingRulesPrecedeWorking(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}

	for name, tool := range cfg.Tools {
		firstWorking := -1
		lastWaiting := -1
		for i, rule := range tool.Rules {
			switch rule.State {
			case "working":
				if firstWorking < 0 {
					firstWorking = i
				}
			case "waiting":
				lastWaiting = i
			}
		}
		if name == "claude" && (firstWorking < 0 || lastWaiting < 0) {
			t.Fatalf("claude defaults need both working and waiting rules: %#v", tool.Rules)
		}
		if firstWorking >= 0 && lastWaiting > firstWorking {
			t.Errorf("%s waiting rule at %d follows first working rule at %d", name, lastWaiting, firstWorking)
		}
	}
}

func TestBackfillToolDefaults(t *testing.T) {
	cfg := Config{Tools: map[string]Tool{
		"opencode": {Command: "opencode", ReviveCommand: "opencode --continue"},
	}}
	if err := cfg.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got := cfg.Tools["opencode"].PromptFlag; got != "--prompt" {
		t.Fatalf("opencode prompt_flag = %q want --prompt (backfilled)", got)
	}
	if got := cfg.Tools["opencode"].ReviveCommand; got != "opencode --continue" {
		t.Fatalf("opencode revive_command = %q want user value kept", got)
	}
	if _, ok := cfg.Tools["claude"]; !ok {
		t.Fatal("expected claude tool added from built-in defaults")
	}
}

// The arrow-unfocus fix needs per-tool input-line knowledge: pi declares
// its blank composer row and opencode its gutter bar. Older configs written
// before the field existed gain both on load, and a value the user wrote is
// kept.
func TestBackfillArrowUnfocusFields(t *testing.T) {
	user := `^\s*┃\s+mine`
	cfg := Config{Tools: map[string]Tool{
		"pi":       {Command: "pi"},
		"opencode": {Command: "opencode", InputPrefix: user},
	}}
	if err := cfg.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got := cfg.Tools["pi"].InputPrefix; got != "^" {
		t.Fatalf("pi input_prefix = %q want ^ (backfilled)", got)
	}
	if got := cfg.Tools["opencode"].InputPrefix; got != user {
		t.Fatalf("opencode input_prefix = %q want user value kept", got)
	}
	plain := Config{Tools: map[string]Tool{"opencode": {Command: "opencode"}}}
	if err := plain.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got := plain.Tools["opencode"].InputPrefix; got != `(?m)^\s*┃` {
		t.Fatalf("opencode input_prefix = %q want the gutter bar (backfilled)", got)
	}
}

// Left could never leave a focused terminal: the shell block declared no
// input marker, so the prompt head was unrecognisable. Older configs
// written before the field existed gain the generic prompt matcher on
// load, and a value the user wrote is kept.
func TestBackfillTerminalInputPrefix(t *testing.T) {
	cfg := Config{Tools: map[string]Tool{"terminal": {Shell: true}}}
	if err := cfg.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got := cfg.Tools["terminal"].InputPrefix; got != `(?m)^\s*(?:\S+\s+){0,3}[❯>$#›»→%➜]\s` {
		t.Fatalf("terminal input_prefix = %q want the generic prompt matcher (backfilled)", got)
	}
	user := `^\$ `
	kept := Config{Tools: map[string]Tool{"terminal": {Shell: true, InputPrefix: user}}}
	if err := kept.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got := kept.Tools["terminal"].InputPrefix; got != user {
		t.Fatalf("terminal input_prefix = %q want user value kept", got)
	}
}

func TestBackfillComposerPlaceholder(t *testing.T) {
	plain := Config{Tools: map[string]Tool{"command-code": {Command: "cmd"}}}
	if err := plain.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got := plain.Tools["command-code"].ComposerPlaceholder; got != "Ask your question..." {
		t.Fatalf("composer_placeholder = %q want backfilled", got)
	}
	mine := Config{Tools: map[string]Tool{"command-code": {Command: "cmd", ComposerPlaceholder: "Type here"}}}
	if err := mine.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got := mine.Tools["command-code"].ComposerPlaceholder; got != "Type here" {
		t.Fatalf("composer_placeholder = %q want user value kept", got)
	}
}

func TestBackfillHermesRequiresMCP(t *testing.T) {
	cfg := Config{Tools: map[string]Tool{
		"hermes": {Command: "hermes --cli"},
	}}
	if err := cfg.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got := cfg.Tools["hermes"].MCP; got != "hermes" {
		t.Fatalf("hermes mcp = %q want hermes", got)
	}
}

func TestBackfillHermesKeepsExplicitMCPOptOut(t *testing.T) {
	cfg := Config{Tools: map[string]Tool{
		"hermes": {Command: "hermes --cli", MCP: "none"},
	}}
	if err := cfg.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if got := cfg.Tools["hermes"].MCP; got != "none" {
		t.Fatalf("hermes mcp = %q want the explicit opt-out kept", got)
	}
}

func TestApplyDefaults(t *testing.T) {
	var cfg Config
	cfg.applyDefaults()
	if cfg.PollInterval.Duration != 2*time.Second {
		t.Fatalf("poll = %v", cfg.PollInterval.Duration)
	}
	if cfg.Tools == nil {
		t.Fatal("tools should be non-nil after defaults")
	}
}

func TestDefaultResumeByIDFields(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	// Tools that accept a chosen id launch with it and resume by it.
	for _, name := range []string{"claude", "grok", "gemini", "pi"} {
		tool := cfg.Tools[name]
		if tool.SessionIDFlag != "--session-id" {
			t.Fatalf("%s session_id_flag = %q want --session-id", name, tool.SessionIDFlag)
		}
		if tool.ResumeByIDCommand == "" || !strings.Contains(tool.ResumeByIDCommand, "{id}") {
			t.Fatalf("%s resume_by_id_command = %q want an {id} template", name, tool.ResumeByIDCommand)
		}
	}
	if got := cfg.Tools["pi"].ResumeByIDCommand; got != "pi --session {id}" {
		t.Fatalf("pi resume_by_id_command = %q want \"pi --session {id}\"", got)
	}
	// Tools that mint their own id declare a store to capture it from.
	for _, name := range []string{"codex", "opencode", "hermes", "command-code"} {
		tool := cfg.Tools[name]
		if tool.SessionStore != name {
			t.Fatalf("%s session_store = %q want %q", name, tool.SessionStore, name)
		}
		if tool.SessionIDFlag != "" {
			t.Fatalf("%s session_id_flag = %q want empty (no launch flag)", name, tool.SessionIDFlag)
		}
		if !strings.Contains(tool.ResumeByIDCommand, "{id}") {
			t.Fatalf("%s resume_by_id_command = %q want an {id} template", name, tool.ResumeByIDCommand)
		}
	}
	// The picker replaces the blind revive_command fallback for the tools
	// whose native picker is validated; the rest keep the fallback.
	for _, tc := range []struct{ name, want string }{
		{"claude", "claude --resume"},
		{"codex", "codex resume"},
		{"command-code", "cmd --resume"},
	} {
		if got := cfg.Tools[tc.name].ResumePickerCommand; got != tc.want {
			t.Fatalf("%s resume_picker_command = %q want %q", tc.name, got, tc.want)
		}
	}
	for _, name := range []string{"opencode", "grok", "gemini", "hermes", "pi"} {
		if got := cfg.Tools[name].ResumePickerCommand; got != "" {
			t.Fatalf("%s resume_picker_command = %q want empty (no default picker)", name, got)
		}
	}
}

func TestBackfillFillsResumeFields(t *testing.T) {
	cfg := Config{Tools: map[string]Tool{
		"claude":       {Command: "claude", ReviveCommand: "claude --continue"},
		"command-code": {Command: "cmd"},
	}}
	if err := cfg.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	tool := cfg.Tools["claude"]
	if tool.SessionIDFlag != "--session-id" {
		t.Fatalf("claude session_id_flag = %q want backfilled --session-id", tool.SessionIDFlag)
	}
	if tool.ResumeByIDCommand != "claude --resume {id}" {
		t.Fatalf("claude resume_by_id_command = %q want backfilled", tool.ResumeByIDCommand)
	}
	if tool.ResumePickerCommand != "claude --resume" {
		t.Fatalf("claude resume_picker_command = %q want backfilled \"claude --resume\"", tool.ResumePickerCommand)
	}
	if tool.ForkCommand == "" {
		t.Fatal("claude fork_command was not backfilled")
	}
	if got := cfg.Tools["command-code"].ForkCommand; got != "cmd --session {id} --fork-session --name {name}" {
		t.Fatalf("command-code fork_command = %q want backfilled", got)
	}
}

// A config.toml a #385-era release wrote carries the command-code patterns
// of its day verbatim, and backfill only touches zero fields. The stale
// shapes are rewritten to the current defaults, so the status fix reaches
// existing installations instead of only fresh files.
func TestMigratesStaleCommandCodePatterns(t *testing.T) {
	cfg := Config{Tools: map[string]Tool{
		"command-code": {
			Command:    "cmd",
			TurnEnd:    oldCmdTurnEnd,
			ChromeLine: oldCmdChromeLine,
			Rules:      []Rule{{State: "working", Pattern: oldCmdWorking}},
		},
	}}
	if err := cfg.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	tool := cfg.Tools["command-code"]
	if tool.TurnEnd == oldCmdTurnEnd {
		t.Fatal("stale turn_end was not migrated")
	}
	if !strings.Contains(tool.TurnEnd, "Thought") {
		t.Fatalf("migrated turn_end = %q, want the Thought/Worked shape", tool.TurnEnd)
	}
	if tool.ChromeLine == oldCmdChromeLine {
		t.Fatal("stale chrome_line was not migrated")
	}
	if !strings.Contains(tool.ChromeLine, "»") {
		t.Fatalf("migrated chrome_line = %q, want the » hint covered", tool.ChromeLine)
	}
	for _, rule := range tool.Rules {
		if rule.State == "working" && rule.Pattern == oldCmdWorking {
			t.Fatal("stale working rule was not migrated")
		}
	}
}

// A config.toml written before the pi footer widened carries rules that pin
// exactly two footer lines, so a pi extension drawing more keeps every rule
// from matching. The stale strings are rewritten to the current defaults;
// a rule the user edited by hand is kept as written.
func TestMigratesStalePiFooterRules(t *testing.T) {
	handEdited := `(?ms)my own waiting rule\z`
	cfg := Config{Tools: map[string]Tool{
		"pi": {
			Command: "pi",
			Rules: []Rule{
				{State: "idle", Pattern: oldPiIdleRule},
				{State: "errored", Pattern: oldPiErrorRule},
				{State: "errored", Pattern: oldPiRateLimitRule},
				{State: "waiting", Pattern: oldPiQuestionRule},
				{State: "working", Pattern: oldPiWorkingRule},
				{State: "waiting", Pattern: handEdited},
			},
		},
	}}
	if err := cfg.backfillToolDefaults(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	def, err := Default()
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	defaults := make(map[string]bool)
	for _, rule := range def.Tools["pi"].Rules {
		defaults[rule.Pattern] = true
	}
	tool := cfg.Tools["pi"]
	for _, rule := range tool.Rules[:5] {
		if !defaults[rule.Pattern] {
			t.Fatalf("stale %s rule was not rewritten to the current default: %q", rule.State, rule.Pattern)
		}
	}
	if got := tool.Rules[5].Pattern; got != handEdited {
		t.Fatalf("hand-edited rule = %q, want kept as written", got)
	}
}
