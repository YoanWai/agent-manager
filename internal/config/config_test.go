package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

func TestDefaultDefinesEveryShippedTool(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
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
	if got := cfg.Tools["grok"].ActivityCutoff; got != `(?m)^(?:\s*│ )?❯` {
		t.Fatalf("grok activity_cutoff = %q want boxed-or-minimal composer", got)
	}
	if !strings.Contains(cfg.Tools["grok"].ChromeLine, "Help improve Grok") {
		t.Fatalf("grok chrome_line missing the opt-in card: %q", cfg.Tools["grok"].ChromeLine)
	}
	if got := cfg.Tools["grok"].TrailingNote; got != "^Worked for " {
		t.Fatalf("grok trailing_note = %q want the duration line", got)
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

// The file a first run leaves behind holds what the manager reads from it
// and nothing else. Tools are not in it, because they are not read from it.
func TestFirstRunWritesAStarterFileThatDeclaresNoTools(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	written, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("config file: %v", err)
	}
	if strings.Contains(string(written), "[tools.") {
		t.Fatalf("the starter file should declare no tools:\n%s", written)
	}
	if len(cfg.IgnoredTools) != 0 {
		t.Fatalf("a fresh file ignores nothing, got %v", cfg.IgnoredTools)
	}
	if _, ok := cfg.Tools["terminal"]; !ok {
		t.Fatal("the built-in terminal tool is missing")
	}
	if cfg.PollInterval.Duration != 2*time.Second {
		t.Fatalf("poll interval = %v want 2s", cfg.PollInterval.Duration)
	}
}

// A file written by an older release carries every block it shipped that
// day; they define nothing now, and their names feed the notice.
func TestToolBlocksInTheFileAreIgnoredAndReported(t *testing.T) {
	dir := writeConfigText(t, `
[tools.claude]
command = "not-claude"
activity_cutoff = "nonsense"

[tools.mine]
command = "mine"
`)
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	builtin, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if got, want := cfg.Tools["claude"].Command, builtin.Tools["claude"].Command; got != want {
		t.Fatalf("claude command = %q, want the built-in %q", got, want)
	}
	if got, want := cfg.Tools["claude"].ActivityCutoff, builtin.Tools["claude"].ActivityCutoff; got != want {
		t.Fatalf("claude activity_cutoff = %q, want the built-in %q", got, want)
	}
	if _, ok := cfg.Tools["mine"]; ok {
		t.Fatal("a block the binary does not ship must not become a tool")
	}
	if len(cfg.IgnoredTools) != 2 || cfg.IgnoredTools[0] != "claude" || cfg.IgnoredTools[1] != "mine" {
		t.Fatalf("ignored blocks = %v, want claude and mine", cfg.IgnoredTools)
	}
}

// A hand-edit that no longer fits the Tool shape is as inert as a
// well-formed block, and an empty table still gets named.
func TestAnIgnoredToolBlockCannotFailTheLoad(t *testing.T) {
	dir := writeConfigText(t, `
poll_interval = "3s"

[tools.claude]
shell = "yes"
rules = "not an array"

[tools.empty]

[keybindings.session]
review = "ctrl+g"
`)
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if got := cfg.IgnoredTools; len(got) != 2 || got[0] != "claude" || got[1] != "empty" {
		t.Fatalf("ignored blocks = %v, want claude and empty", got)
	}
	if cfg.PollInterval.Duration != 3*time.Second {
		t.Fatalf("poll interval = %v, want the file's 3s", cfg.PollInterval.Duration)
	}
	if got := cfg.SessionKeys.Binding(keybind.Review).Label(); got != "ctrl+g" {
		t.Fatalf("review key = %q, want the file's ctrl+g", got)
	}
	if cfg.Tools["claude"].Shell {
		t.Fatal("the built-in claude block is not a shell")
	}
}

// Nothing types into a pane it cannot read, so every agent CLI has to mark
// where its input box is. The shell is exempt: nothing types into it.
func TestEveryAgentToolMarksItsInputBox(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	for name, tool := range cfg.Tools {
		if tool.Shell {
			continue
		}
		if tool.ActivityCutoff == "" {
			t.Errorf("tool %q declares no activity_cutoff", name)
		}
	}
}

func TestTheBinaryShipsOneShell(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	shells := 0
	for _, tool := range cfg.Tools {
		if tool.Shell {
			shells++
		}
	}
	if shells != 1 {
		t.Fatalf("shell tools = %d, want exactly one", shells)
	}
	name, tool := cfg.ShellTool()
	if name != "terminal" || tool.Command != "" {
		t.Fatalf("ShellTool = %q %+v, want the terminal block on $SHELL", name, tool)
	}
}

func TestShellToolUsesFlagAndStableName(t *testing.T) {
	cfg := Config{Tools: map[string]Tool{
		"terminal": {Command: "agent", Shell: false},
		"zsh":      {Command: "zsh", Shell: true},
		"bash":     {Command: "bash", Shell: true},
	}}
	name, tool := cfg.ShellTool()
	if name != "bash" || tool.Command != "bash" {
		t.Fatalf("ShellTool = %q %+v, want bash", name, tool)
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
		{"grok", "grok"},
		{"gemini", "gemini -i /resume"},
		{"hermes", "hermes --cli sessions browse"},
		{"pi", "pi --resume"},
	} {
		if got := cfg.Tools[tc.name].ResumePickerCommand; got != tc.want {
			t.Fatalf("%s resume_picker_command = %q want %q", tc.name, got, tc.want)
		}
	}
	// opencode's picker only exists inside the running TUI, so its default
	// pairs the bare launch with the shortcut the manager types at the
	// composer instead of a command-line picker.
	if got := cfg.Tools["opencode"].ResumePickerCommand; got != "opencode" {
		t.Fatalf("opencode resume_picker_command = %q want \"opencode\"", got)
	}
	if got := cfg.Tools["opencode"].ResumePickerKeys; got != "/sessions" {
		t.Fatalf("opencode resume_picker_keys = %q want \"/sessions\"", got)
	}
	for _, name := range []string{"claude", "codex", "command-code", "grok", "gemini", "hermes", "pi"} {
		if got := cfg.Tools[name].ResumePickerKeys; got != "" {
			t.Fatalf("%s resume_picker_keys = %q want empty", name, got)
		}
	}
}

func TestPiActivityCutoffReadsTheSpinnerBorder(t *testing.T) {
	def, err := Default()
	if err != nil {
		t.Fatalf("default: %v", err)
	}
	cutoff := regexp.MustCompile(def.Tools["pi"].ActivityCutoff)
	for row, want := range map[string]bool{
		"──────────────────────────────────────────────────": true,
		"── ⠧ Working ─────────────────────────────────────": true,
		"── ⠹ Compacting context ─────────────────────────":  true,
		"─── ↑ 2 more ─────────────────────────────────────": false,
		"─── ↓ 1 more ─────────────────────────────────────": false,
		"a draft that trails off ────────────":               false,
		"⠧ Working": false,
	} {
		if got := cutoff.MatchString(row); got != want {
			t.Errorf("activity_cutoff on %q = %v, want %v", row, got, want)
		}
	}
	pane := "output\n── ⠧ Working ──────────\n\n──────────────────\n~\n$0.000"
	if loc := cutoff.FindStringIndex(pane); loc == nil || loc[0] != 0 || pane[loc[1]:] != "\n~\n$0.000" {
		t.Fatalf("whole-pane cutoff = %v on %q, want one match from the origin to the bottom rule", loc, pane)
	}
}

func writeConfigText(t *testing.T, text string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(text), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return dir
}

func TestLoadDirReadsTheKeyTables(t *testing.T) {
	dir := writeConfigText(t, `
[keybindings.session]
detach = ["f9", "alt+q"]
review = "none"

[keybindings.list]
new_session = "N"
quit = "none"
`)
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	keys := cfg.SessionKeys
	if got := keys.Binding(keybind.Detach).Label(); got != "f9 / alt+q" {
		t.Errorf("detach = %q", got)
	}
	if got := keys.Binding(keybind.Review).Label(); got != "" {
		t.Errorf("review none should be off, got %q", got)
	}
	if got := keys.Binding(keybind.Editor).Label(); got != "f3" {
		t.Errorf("editor left out should take the default, got %q", got)
	}
	if got, _ := cfg.ListKeys.ActionFor("N"); got != keybind.NewSession {
		t.Errorf("N should open a new session, got %q", got)
	}
	if _, bound := cfg.ListKeys.ActionFor("q"); bound {
		t.Error("quit none should leave q unbound")
	}
	if got, _ := cfg.ListKeys.ActionFor("?"); got != keybind.Help {
		t.Errorf("an action left out keeps its key, got %q", got)
	}
}

// A config that names no keys, the generated one included, binds what the
// manager always bound.
func TestKeyTableDefaultsWhenTheFileNamesNone(t *testing.T) {
	cfg, err := LoadDir(t.TempDir())
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	def, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	for name, keys := range map[string]Config{"generated": cfg, "built-in": def} {
		if !keys.SessionKeys.Equal(keybind.DefaultSession()) {
			t.Errorf("%s: session keys = %q", name, sessionLabels(keys.SessionKeys))
		}
		if !keys.ListKeys.Equal(keybind.DefaultList()) {
			t.Errorf("%s: list keys are not the defaults", name)
		}
	}
}

func TestLoadDirRefusesAKeyTableThatCannotWork(t *testing.T) {
	for _, tc := range []struct{ table, text, reason string }{
		{"session", `editor = "ctrl+i"`, "ctrl+i is tab"},
		{"session", `editor = "o"`, `"o" is a plain key, which reaches the agent`},
		{"session", `detach = "none"`, "detach needs at least one key"},
		{"list", `settings = "none"`, "settings needs at least one key"},
		{"list", `quit = "esc"`, "stays as it is"},
		{"list", `detach = "f9"`, `no action named "detach"`},
	} {
		dir := writeConfigText(t, "[keybindings."+tc.table+"]\n"+tc.text+"\n")
		_, err := LoadDir(dir)
		if err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s %s: err = %v, want %q", tc.table, tc.text, err, tc.reason)
		}
	}
}
