package config

import (
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sessionreport"
)

func TestDefaultDefinesEveryShippedTool(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
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
	if !cfg.Tools["grok"].FitsHeight {
		t.Fatal("grok fits_height = false want true")
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

// A report names the session's tool by its reporter, so a reporting tool's
// style is its own name. Every agent says how it reports, or that it cannot.
func TestEveryAgentToolDeclaresHowItReportsItsConversation(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	for name, tool := range cfg.Tools {
		switch {
		case tool.Shell && tool.SessionReport != "":
			t.Errorf("shell tool %q declares session_report %q", name, tool.SessionReport)
		case tool.Shell:
		case !slices.Contains(sessionreport.Styles, tool.SessionReport):
			t.Errorf("tool %q declares session_report %q, not one of %v", name, tool.SessionReport, sessionreport.Styles)
		case tool.SessionReport != sessionreport.StyleNone && tool.SessionReport != name:
			t.Errorf("tool %q reports as %q", name, tool.SessionReport)
		}
	}
}

// A launch execs its line so the agent runs under the pid the launch
// exports, which holds only for a line that is one command.
func TestEveryLaunchLineIsOneCommand(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	for name, tool := range cfg.Tools {
		for _, line := range []string{tool.Command, tool.ReviveCommand, tool.ResumeByIDCommand, tool.ResumePickerCommand, tool.ForkCommand} {
			if strings.ContainsAny(line, ";&|\n`$()<>") {
				t.Errorf("tool %q launches with %q, which is not one command", name, line)
			}
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
	for _, name := range []string{"codex", "opencode", "hermes", "command-code", "antigravity", "omp"} {
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
		{"hermes", "hermes --cli {choice} sessions browse"},
		{"pi", "pi --resume"},
		{"omp", "omp --resume"},
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
	// agy -i /resume would hand "/resume" to the model as a prompt.
	if got := cfg.Tools["antigravity"].ResumePickerCommand; got != "agy" {
		t.Fatalf("antigravity resume_picker_command = %q want \"agy\"", got)
	}
	if got := cfg.Tools["antigravity"].ResumePickerKeys; got != "/resume" {
		t.Fatalf("antigravity resume_picker_keys = %q want \"/resume\"", got)
	}
	for _, name := range []string{"claude", "codex", "command-code", "grok", "gemini", "hermes", "pi", "omp"} {
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

func TestMuseDefaults(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := cfg.Tools["muse"]
	if !ok || tool.Command != "muse" || tool.SessionStore != "muse" || tool.ResumeByIDCommand != "muse resume {id}" || tool.ResumePickerCommand != "muse resume" || tool.MCP != "" {
		t.Fatalf("Muse defaults = %+v", tool)
	}
	if tool.ForkKeys != "/fork" || tool.ForkCommand != "muse resume {new_id}" {
		t.Fatalf("Muse fork = %q then %q", tool.ForkKeys, tool.ForkCommand)
	}
	if tool.SessionIDFlag != "" || tool.PromptFlag != "" {
		t.Fatalf("unsupported Muse flags: %+v", tool)
	}
}

// A choice puts its flags on every line that launches the tool, quoted for
// the shell, and changes nothing else: the status rules stay the tool's.
func TestWithChoiceFlagsEveryLaunchLine(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	base := cfg.Tools["pi"]
	chosen := base.WithChoice(Choice{Model: "openai-codex/gpt-6-sol", Effort: "it's high"})
	suffix := ` --model 'openai-codex/gpt-6-sol' --thinking 'it'\''s high'`
	for _, tc := range []struct{ field, base, got string }{
		{"command", base.Command, chosen.Command},
		{"revive_command", base.ReviveCommand, chosen.ReviveCommand},
		{"resume_by_id_command", base.ResumeByIDCommand, chosen.ResumeByIDCommand},
		{"resume_picker_command", base.ResumePickerCommand, chosen.ResumePickerCommand},
		{"fork_command", base.ForkCommand, chosen.ForkCommand},
	} {
		if tc.base == "" {
			t.Fatalf("pi %s is empty; the test needs a line to flag", tc.field)
		}
		if want := tc.base + suffix; tc.got != want {
			t.Errorf("%s = %q, want %q", tc.field, tc.got, want)
		}
	}
	if !reflect.DeepEqual(chosen.Rules, base.Rules) || chosen.ActivityCutoff != base.ActivityCutoff || chosen.SessionIDFlag != base.SessionIDFlag {
		t.Errorf("the choice changed how pi's screen is read:\n%+v\n%+v", chosen, base)
	}
}

// Hermes takes the profile, the provider with its model, and the effort, in
// that order; its session browser takes them ahead of the subcommand, and
// the fork line it does not have stays empty.
func TestWithChoicePlacesFlagsAtTheChoiceMark(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	chosen := cfg.Tools["hermes"].WithChoice(Choice{Provider: "xai-oauth", Model: "grok-4.6", Effort: "high", Profile: "work"})
	flags := ` -p 'work' --provider 'xai-oauth' -m 'grok-4.6' --reasoning 'high'`
	if want := "hermes --cli" + flags; chosen.Command != want {
		t.Errorf("command = %q, want %q", chosen.Command, want)
	}
	if want := "hermes --cli" + flags + " sessions browse"; chosen.ResumePickerCommand != want {
		t.Errorf("resume_picker_command = %q, want %q", chosen.ResumePickerCommand, want)
	}
	if chosen.ForkCommand != "" {
		t.Errorf("fork_command = %q, want empty like hermes's own", chosen.ForkCommand)
	}
	for line, want := range map[string]string{
		"cli {choice}":     "cli -m 'x'",
		"cli{choice} sub":  "cli -m 'x' sub",
		"cli {choice} sub": "cli -m 'x' sub",
	} {
		if got := placeChoice(line, " -m 'x'"); got != want {
			t.Errorf("placeChoice(%q) = %q, want %q", line, got, want)
		}
	}
}

// With nothing chosen every line launches as the tool ships it, the choice
// mark gone.
func TestWithAnEmptyChoiceLaunchesAsShipped(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	for name, tool := range cfg.Tools {
		plain := tool.WithChoice(Choice{})
		for _, line := range []string{plain.Command, plain.ReviveCommand, plain.ResumeByIDCommand, plain.ResumePickerCommand, plain.ForkCommand} {
			if strings.Contains(line, "{choice}") || strings.Contains(line, "  ") {
				t.Errorf("%s launches %q", name, line)
			}
		}
	}
	if got := cfg.Tools["hermes"].WithChoice(Choice{}).ResumePickerCommand; got != "hermes --cli sessions browse" {
		t.Errorf("hermes resume_picker_command = %q", got)
	}
}

// Every CLI that can be asked for its models says how, and takes the model
// it answers with; the ones asked for effort levels take an effort too.
func TestEveryCatalogToolTakesWhatItLists(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	for name, tool := range cfg.Tools {
		if tool.Catalog == "" {
			if tool.ModelArgs != "" || tool.EffortArgs != "" || tool.ProfileArgs != "" {
				t.Errorf("%s takes a choice nothing can list", name)
			}
			continue
		}
		if tool.CatalogCommand == "" || !strings.Contains(tool.ModelArgs, "{model}") {
			t.Errorf("%s catalog %q command %q model_args %q", name, tool.Catalog, tool.CatalogCommand, tool.ModelArgs)
		}
		if tool.EffortArgs != "" && !strings.Contains(tool.EffortArgs, "{effort}") {
			t.Errorf("%s effort_args = %q", name, tool.EffortArgs)
		}
		if tool.ProfileArgs != "" && !strings.Contains(tool.ProfileArgs, "{profile}") {
			t.Errorf("%s profile_args = %q", name, tool.ProfileArgs)
		}
	}
}

func TestOmpDefaults(t *testing.T) {
	cfg, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := cfg.Tools["omp"]
	if !ok || tool.Command != "omp" || tool.SessionStore != "omp" || tool.ResumeByIDCommand != "omp --resume {id}" || tool.ReviveCommand != "omp --continue" {
		t.Fatalf("omp defaults = %+v", tool)
	}
	// omp has no flag that picks the session id or forks from the command
	// line, and takes its startup prompt as a positional argument.
	if tool.SessionIDFlag != "" || tool.ForkCommand != "" || tool.ForkKeys != "" || tool.PromptFlag != "" {
		t.Fatalf("unsupported omp flags: %+v", tool)
	}
	if tool.MCP != "" || tool.DefaultStatus != "finished" || tool.InputPrefix == "" {
		t.Fatalf("omp status defaults = %+v", tool)
	}
}
