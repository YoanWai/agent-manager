package config

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
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

func TestToolNamesAreSorted(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	for range 5 {
		if names := cfg.ToolNames(); !sort.StringsAreSorted(names) || len(names) != len(cfg.Tools) {
			t.Fatalf("ToolNames = %v", names)
		}
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

func TestMuseDefaultsOnLoad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[tools.claude]\ncommand = 'claude'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := cfg.Tools["muse"]
	if !ok || tool.Command != "muse" || tool.SessionStore != "muse" || tool.ResumeByIDCommand != "muse resume {id}" || tool.ResumePickerCommand != "muse resume" || tool.MCP != "none" {
		t.Fatalf("Muse defaults = %+v", tool)
	}
	if tool.SessionIDFlag != "" || tool.ForkCommand != "" || tool.PromptFlag != "" {
		t.Fatalf("unsupported Muse flags: %+v", tool)
	}
}

// A profile is the base tool under a new name with its arguments on every
// line that launches it, and nothing else of its own: the status rules are
// the base's, so a fix for the base's screen reaches the profile too.
func TestProfileIsTheBaseToolWithArgumentsOnEveryLaunchLine(t *testing.T) {
	dir := writeConfigText(t, `
[profiles.pi-sol]
tool = "pi"
args = ["--model", "openai-codex/gpt-6-sol:xhigh", "--thinking", "it's high"]
`)
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	profile, ok := cfg.Tools["pi-sol"]
	if !ok {
		t.Fatal("expected pi-sol tool")
	}
	base := cfg.Tools["pi"]
	suffix := ` '--model' 'openai-codex/gpt-6-sol:xhigh' '--thinking' 'it'\''s high'`
	for _, tc := range []struct{ field, base, got string }{
		{"command", base.Command, profile.Command},
		{"revive_command", base.ReviveCommand, profile.ReviveCommand},
		{"resume_by_id_command", base.ResumeByIDCommand, profile.ResumeByIDCommand},
		{"resume_picker_command", base.ResumePickerCommand, profile.ResumePickerCommand},
		{"fork_command", base.ForkCommand, profile.ForkCommand},
	} {
		if tc.base == "" {
			t.Fatalf("pi %s is empty; the test needs a base line to append to", tc.field)
		}
		if want := tc.base + suffix; tc.got != want {
			t.Errorf("pi-sol %s = %q, want %q", tc.field, tc.got, want)
		}
	}
	if !reflect.DeepEqual(profile.Rules, base.Rules) || profile.ActivityCutoff != base.ActivityCutoff || profile.SessionIDFlag != base.SessionIDFlag {
		t.Errorf("pi-sol reads its screen differently from pi:\n%+v\n%+v", profile, base)
	}
	if got := cfg.Profiles["pi-sol"]; got != "pi" {
		t.Errorf("Profiles[pi-sol] = %q, want pi", got)
	}
	if got := cfg.Tools["pi"].Command; got != "pi" {
		t.Errorf("the base tool changed: pi command = %q", got)
	}
}

// A base whose block leaves a launch line empty keeps it empty on the
// profile, so the fallbacks that read emptiness take the same path; and the
// MCP style keys on the base's name, since the profile's is nobody's.
func TestProfileKeepsTheBaseFallbacksAndMCPStyle(t *testing.T) {
	dir := writeConfigText(t, `
[profiles.claude-sonnet]
tool = "claude"
args = ["--model", "sonnet"]

[profiles.muse-quiet]
tool = "muse"
args = []
`)
	cfg, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if got := cfg.Tools["claude-sonnet"].MCP; got != "claude" {
		t.Errorf("claude-sonnet mcp = %q, want claude", got)
	}
	if got := cfg.Tools["muse-quiet"].MCP; got != cfg.Tools["muse"].MCP {
		t.Errorf("muse-quiet mcp = %q, want the base's %q", got, cfg.Tools["muse"].MCP)
	}
	if cfg.Tools["muse"].ForkCommand != "" {
		t.Fatal("the test needs a base without a fork command")
	}
	if got := cfg.Tools["muse-quiet"].ForkCommand; got != "" {
		t.Errorf("muse-quiet fork_command = %q, want empty like the base", got)
	}
	if got := cfg.Tools["muse-quiet"].Command; got != "muse" {
		t.Errorf("muse-quiet command = %q, want muse with no arguments", got)
	}
}

func TestLoadDirRefusesAProfileThatCannotLaunch(t *testing.T) {
	for _, tc := range []struct{ text, reason string }{
		{"[profiles.pi]\ntool = \"pi\"\n", `profile "pi" shadows the built-in tool`},
		{"[profiles.mine]\ntool = \"nope\"\n", `profile "mine": tool "nope" is not a CLI it supports; the CLIs are claude, codex, command-code`},
		{"[profiles.mine]\n", `profile "mine": tool "" is not a CLI`},
		{"[profiles.mine]\ntool = \"terminal\"\n", `profile "mine": tool "terminal" opens a shell`},
		{"[profiles.a]\ntool = \"pi\"\n[profiles.b]\ntool = \"a\"\n", `profile "b": tool "a" is not a CLI`},
	} {
		dir := writeConfigText(t, tc.text)
		_, err := LoadDir(dir)
		if err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: err = %v, want %q", tc.text, err, tc.reason)
		}
	}
}

// The built-in config and a file without profiles name none, and a
// [tools.*] block stays as ignored as before beside one.
func TestNoProfilesIsAnEmptyMap(t *testing.T) {
	def, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if len(def.Profiles) != 0 {
		t.Fatalf("Default profiles = %v", def.Profiles)
	}
	cfg, err := LoadDir(writeConfigText(t, "[tools.pi]\ncommand = \"x\"\n[profiles.p]\ntool = \"pi\"\n"))
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(cfg.Profiles) != 1 || cfg.IgnoredTools[0] != "pi" || cfg.Tools["p"].Command != "pi" {
		t.Fatalf("profiles = %v ignored = %v p = %q", cfg.Profiles, cfg.IgnoredTools, cfg.Tools["p"].Command)
	}
}
