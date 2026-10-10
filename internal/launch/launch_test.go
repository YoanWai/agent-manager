package launch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/conversation"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

func TestPromptInjectsDirectiveOnlyForAutoNamedWithPrompt(t *testing.T) {
	withDirective := Prompt("", "build the api", true)
	if !strings.HasPrefix(withDirective, RenameDirective+"\n\n") || !strings.HasSuffix(withDirective, "build the api") {
		t.Fatalf("auto-named prompt should carry the directive, got %q", withDirective)
	}
	named := Prompt("", "build the api", false)
	if !strings.HasPrefix(named, RenameAvailableNote+"\n\n") || !strings.HasSuffix(named, "build the api") {
		t.Fatalf("custom-named prompt should note rename is optional later, got %q", named)
	}
	if strings.Contains(named, "Run rename only this once") || strings.HasPrefix(named, RenameDirective) {
		t.Fatalf("custom-named prompt must not force a rename, got %q", named)
	}
	if got := Prompt("", "", true); got != "" {
		t.Fatalf("promptless session should stay clean, got %q", got)
	}
	if got := Prompt("", "/compact keep the api notes", true); got != "/compact keep the api notes" {
		t.Fatalf("slash-command prompt should stay clean, got %q", got)
	}
	if got := Prompt("", "/compact keep the api notes", false); got != "/compact keep the api notes" {
		t.Fatalf("named slash-command prompt should stay clean, got %q", got)
	}
}

func TestWithPromptComposesPerToolStyle(t *testing.T) {
	flagged := config.Tool{Command: "opencode", PromptFlag: "--prompt"}
	if got := WithPrompt(flagged, flagged.Command, "do it"); got != "opencode --prompt 'do it'" {
		t.Fatalf("flagged compose = %q", got)
	}
	bare := config.Tool{Command: "cat"}
	if got := WithPrompt(bare, bare.Command, "do it"); got != "cat 'do it'" {
		t.Fatalf("bare compose = %q", got)
	}
	if got := WithPrompt(bare, bare.Command, ""); got != "cat" {
		t.Fatalf("empty prompt should leave the command untouched, got %q", got)
	}
	sent := config.Tool{Command: "hermes --cli", PromptMode: "send"}
	if got := WithPrompt(sent, sent.Command, "do it"); got != sent.Command {
		t.Fatalf("send-mode prompt changed launch command to %q", got)
	}
}

func TestAssembleRoutesPromptAndDirective(t *testing.T) {
	flagged := config.Tool{Command: "claude", PromptFlag: "-p", SessionIDFlag: "--session-id"}
	plan := Assemble("claude", flagged, "build the api", true, true)
	if !strings.HasPrefix(plan.Command, "claude -p '"+RenameDirective) {
		t.Fatalf("auto-named flagged command = %q", plan.Command)
	}
	if plan.AgentSessionID == "" || !strings.Contains(plan.Command, "--session-id "+plan.AgentSessionID) {
		t.Fatalf("command should carry the chosen session id, got %q", plan.Command)
	}
	if len(plan.PendingInputs) != 0 {
		t.Fatalf("an embeddable directive needs no pending input, got %v", plan.PendingInputs)
	}

	deferred := Assemble("claude", flagged, "/compact the notes", true, true)
	if len(deferred.PendingInputs) != 1 || deferred.PendingInputs[0] != DeferredRenameDirective {
		t.Fatalf("slash-command launch should defer the directive, got %v", deferred.PendingInputs)
	}

	sent := config.Tool{Command: "hermes", PromptMode: "send"}
	typed := Assemble("hermes", sent, "build the api", false, true)
	if typed.Command != "hermes" {
		t.Fatalf("send-mode command = %q", typed.Command)
	}
	if len(typed.PendingInputs) != 1 || !strings.HasSuffix(typed.PendingInputs[0], "build the api") {
		t.Fatalf("send-mode prompt should be typed in, got %v", typed.PendingInputs)
	}
	if typed.AgentSessionID != "" {
		t.Fatalf("a tool without a session-id flag must not get one, got %q", typed.AgentSessionID)
	}
}

func TestAssembleNotesCoordinationOnlyForToolsWithoutMCP(t *testing.T) {
	for _, mode := range []struct {
		proactive bool
		note      string
	}{
		{proactive: true, note: ProactiveCoordinationNote},
		{proactive: false, note: OnRequestCoordinationNote},
	} {
		noClient := config.Tool{Command: "pi", PromptFlag: "-p"}
		carried := Assemble("pi", noClient, "build the api", false, mode.proactive)
		if !strings.Contains(carried.Command, mode.note) {
			t.Fatalf("a tool with no MCP client should be pointed at the subcommands, got %q", carried.Command)
		}
		if len(carried.PendingInputs) != 0 {
			t.Fatalf("a note the prompt carried needs no pending input, got %v", carried.PendingInputs)
		}

		withClient := config.Tool{Command: "claude", PromptFlag: "-p"}
		if plan := Assemble("claude", withClient, "build the api", false, mode.proactive); strings.Contains(plan.Command, mode.note) {
			t.Fatalf("a tool whose MCP tool descriptions say this already must not repeat it, got %q", plan.Command)
		}

		commandCode := Assemble("command-code", config.Tool{Command: "cmd"}, "build the api", false, mode.proactive)
		if strings.Contains(commandCode.Command, mode.note) {
			t.Fatalf("command-code registers MCP on spawn, so it must not get the subcommand note, got %q", commandCode.Command)
		}

		// A slash command carries neither, so both queue, and the order is what
		// the agent reads: the directive ends on "Then continue.", and the note
		// is what it continues into.
		deferred := Assemble("pi", noClient, "/compact the notes", true, mode.proactive)
		if len(deferred.PendingInputs) != 2 ||
			deferred.PendingInputs[0] != DeferredRenameDirective || deferred.PendingInputs[1] != mode.note {
			t.Fatalf("a launch needing both should queue them in reading order, got %v", deferred.PendingInputs)
		}

		promptless := Assemble("pi", noClient, "", false, mode.proactive)
		if promptless.Command != noClient.Command {
			t.Fatalf("a promptless launch command should stay clean, got %q", promptless.Command)
		}
		if len(promptless.PendingInputs) != 1 || promptless.PendingInputs[0] != mode.note {
			t.Fatalf("a prompt that cannot carry the note should queue it, got %v", promptless.PendingInputs)
		}
	}
}

func TestReviveCommandResumesTheConversationItHeld(t *testing.T) {
	full := config.Tool{
		Command:           "claude",
		ReviveCommand:     "claude --continue",
		ResumeByIDCommand: "claude --resume {id}",
	}
	picker := full
	picker.ResumePickerCommand = "claude --resume"
	for _, tc := range []struct {
		name           string
		tool           config.Tool
		agentSessionID string
		want           string
	}{
		{"a captured id resumes that conversation", full, "abc-123", "claude --resume 'abc-123'"},
		{"no captured id falls back to the newest one", full, "", "claude --continue"},
		{"no captured id with a picker opens it", picker, "", "claude --resume"},
		{"a captured id wins over the picker", picker, "abc-123", "claude --resume 'abc-123'"},
		{"a tool with no revive of its own starts fresh", config.Tool{Command: "pi"}, "abc-123", "pi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReviveCommand(tc.tool, tc.agentSessionID); got != tc.want {
				t.Fatalf("ReviveCommand = %q, want %q", got, tc.want)
			}
		})
	}
}

// A conversation id is read from the agent CLI's own store, so it is not
// ours to trust. Spliced raw it would need no quote to break out of: a
// semicolon ends the resume command and starts whatever follows.
func TestReviveCommandQuotesAnIDThatSpellsAnotherCommand(t *testing.T) {
	tool := config.Tool{ResumeByIDCommand: "codex resume {id}"}
	for _, tc := range []struct{ name, id, want string }{
		{"a command after a semicolon", `abc; touch pwned`, `codex resume 'abc; touch pwned'`},
		{"an id closing the quote itself", `abc'; touch pwned; echo '`, `codex resume 'abc'\''; touch pwned; echo '\'''`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReviveCommand(tool, tc.id); got != tc.want {
				t.Fatalf("ReviveCommand = %q, want %q", got, tc.want)
			}
		})
	}
}

// The quoting has to hold against a real shell, not just against a string
// comparison: the revive command reaches one through tmux.
func TestReviveCommandKeepsACapturedIDOutOfTheShell(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "pwned")
	id := `abc; touch ` + marker

	command := ReviveCommand(config.Tool{ResumeByIDCommand: "echo resume {id}"}, id)
	driver, err := tmux.NewWithSocket("amrevivequote")
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	sessID := "revive-quote-probe"
	// Wide enough that the echoed id lands on one row, since a wrapped
	// capture would split the text the assertion looks for.
	if err := driver.Create(sessID, dir, command, map[string]string{}, 400, 24); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = driver.Kill(sessID) })

	echoed := "resume " + id
	deadline := time.Now().Add(10 * time.Second)
	var pane string
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			pane, _ = driver.CapturePane(sessID)
			t.Fatalf("the id ran as a shell command: %s was created; pane:\n%s", marker, strings.TrimSpace(pane))
		}
		pane, _ = driver.CapturePane(sessID)
		if strings.Contains(pane, echoed) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Echoing the whole id as one argument is what proves the command ran
	// and the shell read the id as text, rather than nothing having run.
	if !strings.Contains(pane, echoed) {
		t.Fatalf("the resume command never echoed the id as one argument; pane:\n%s", strings.TrimSpace(pane))
	}
}

func TestEnvironmentCarriesSessionIDAndHooks(t *testing.T) {
	manager := hooks.NewManager(t.TempDir())

	plain := config.Tool{Command: "cat"}
	command, env, err := Environment(manager, "plain", plain, plain.Command, "abcd1234", t.TempDir(), time.Time{})
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	if env[hooks.EnvSessionID] != "abcd1234" {
		t.Fatalf("plain tool env = %v, want session id", env)
	}
	if env[hooks.EnvStatusFile] != "" {
		t.Fatalf("a tool without claude hooks must not get a status file, got %q", env[hooks.EnvStatusFile])
	}
	if command != agentLaunch(manager, plain.Command) {
		t.Fatalf("a tool with no MCP style should launch untouched, got %q", command)
	}

	hooked := config.Tool{Command: "cat", StatusSource: hooks.StatusSourceClaude, MCP: "claude"}
	command, env, err = Environment(manager, "hooked", hooked, hooked.Command, "abcd1234", t.TempDir(), time.Time{})
	if err != nil {
		t.Fatalf("Environment hooked: %v", err)
	}
	if env[hooks.EnvSessionID] != "abcd1234" || env[hooks.EnvStatusFile] == "" {
		t.Fatalf("hooked tool env = %v, want session id and status file", env)
	}
	want := agentLaunch(manager, "cat --mcp-config "+tmux.ShellQuote(filepath.Join(manager.Dir(), "mcp-claude.json"))+" --settings "+tmux.ShellQuote(manager.SettingsFile("abcd1234")))
	if command != want {
		t.Fatalf("hooked command = %q, want this session's own settings", command)
	}
}

// agentLaunch is the line a row's first launch runs its agent with.
func agentLaunch(manager *hooks.Manager, command string) string {
	agentFile := manager.AgentFile("abcd1234")
	return "sh -c " + tmux.ShellQuote("export "+hooks.EnvAgentPID+"=$$; "+conversation.RecordCommand(agentFile, 0)+"; exec "+command) + "; " + conversation.EndedCommand(agentFile)
}

// The agent replaces the shell that exported and recorded the pid, so it
// runs under that pid whatever shell the line is typed into, the shell the
// line ran in marks the record once the agent has quit, and a shell-only
// launch stays a plain shell.
func TestEnvironmentRunsTheAgentUnderThePIDItExports(t *testing.T) {
	manager := hooks.NewManager(t.TempDir())
	probe := config.Tool{Command: "sh -c 'echo $$ $" + hooks.EnvAgentPID + "'", MCP: mcpreg.StyleNone}
	command, _, err := Environment(manager, "probe", probe, probe.Command, "abcd1234", t.TempDir(), time.Time{})
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	out, err := exec.Command("bash", "-c", "bash -c "+tmux.ShellQuote(command)).Output()
	if err != nil {
		t.Fatalf("run %q: %v", command, err)
	}
	pids := strings.Fields(string(out))
	if len(pids) != 2 || pids[0] != pids[1] {
		t.Fatalf("agent pid and exported pid = %q, want one pid twice", out)
	}
	if agent, found, err := conversation.ReadAgent(manager.AgentFile("abcd1234")); err != nil || !found || strconv.Itoa(agent.PID) != pids[0] || agent.Launch != 0 || !agent.Ended {
		t.Fatalf("recorded agent = %+v %v %v, want pid %s of launch 0, ended", agent, found, err, pids[0])
	}

	prompt := "it's \"quoted\" $HOME `date` \\n\nsecond line"
	echo := config.Tool{Command: `sh -c 'printf "%s" "$1"' sh`, MCP: mcpreg.StyleNone}
	command, _, err = Environment(manager, "echo", echo, WithPrompt(echo, echo.Command, prompt), "abcd1234", t.TempDir(), time.Time{})
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	if out, err := exec.Command("bash", "-c", command).Output(); err != nil || string(out) != prompt {
		t.Fatalf("the agent got prompt %q, %v; want %q", out, err, prompt)
	}

	shell := config.Tool{Command: "", MCP: mcpreg.StyleNone}
	if command, env, err := Environment(manager, "terminal", shell, shell.Command, "abcd1234", t.TempDir(), time.Time{}); err != nil || command != "" || env[hooks.EnvSessionID] != "abcd1234" {
		t.Fatalf("shell launch = %q, %v, %v; want no command and the session env", command, env, err)
	}
}

// The launch the agent is told it belongs to is the one its row is stamped
// with: 0 for a row's first launch, which leaves agent_launched_at unset.
func TestEnvironmentNamesTheLaunch(t *testing.T) {
	manager := hooks.NewManager(t.TempDir())
	plain := config.Tool{Command: "cat"}
	_, env, err := Environment(manager, "plain", plain, plain.Command, "abcd1234", t.TempDir(), time.Time{})
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	if env[hooks.EnvLaunch] != "0" {
		t.Fatalf("first launch env = %v, want launch 0", env)
	}
	launchedAt := time.Unix(1700000000, 123456789)
	_, env, err = Environment(manager, "plain", plain, plain.Command, "abcd1234", t.TempDir(), launchedAt)
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	if want := strconv.FormatInt(store.LaunchStamp(launchedAt), 10); env[hooks.EnvLaunch] != want {
		t.Fatalf("relaunch env = %v, want launch %s", env, want)
	}
}

func TestEnvironmentWiresTheSessionReport(t *testing.T) {
	t.Setenv("OPENCODE_TUI_CONFIG", "")
	manager := hooks.NewManager(t.TempDir())

	pi := config.Tool{Command: "cat", ModelArgs: "--model {model}", SessionReport: "pi", MCP: mcpreg.StyleNone}.WithChoice(config.Choice{Model: "big"})
	command, _, err := Environment(manager, "pi", pi, pi.Command+" --session-id abc", "abcd1234", t.TempDir(), time.Time{})
	if err != nil {
		t.Fatalf("Environment pi: %v", err)
	}
	if want := agentLaunch(manager, "cat --model 'big' --session-id abc -e "+tmux.ShellQuote(filepath.Join(manager.Dir(), "track-pi.ts"))); command != want {
		t.Fatalf("pi command = %q, want %q", command, want)
	}

	opencode := config.Tool{Command: "cat", SessionReport: "opencode", MCP: mcpreg.StyleNone}
	command, env, err := Environment(manager, "opencode", opencode, opencode.Command, "abcd1234", t.TempDir(), time.Time{})
	if err != nil {
		t.Fatalf("Environment opencode: %v", err)
	}
	if command != agentLaunch(manager, "cat") || env["OPENCODE_TUI_CONFIG"] != filepath.Join(manager.Dir(), "track-opencode-tui.json") {
		t.Fatalf("opencode launch = %q %v, want the TUI config in the environment", command, env)
	}

	t.Setenv("OPENCODE_TUI_CONFIG", "/users/own/tui.json")
	_, env, err = Environment(manager, "opencode", opencode, opencode.Command, "abcd1234", t.TempDir(), time.Time{})
	if err != nil {
		t.Fatalf("Environment opencode: %v", err)
	}
	if _, set := env["OPENCODE_TUI_CONFIG"]; set {
		t.Fatalf("a user's own OPENCODE_TUI_CONFIG must stay theirs, got %v", env)
	}
}

func TestEnvironmentReportsClaudeConversationsThroughItsSettings(t *testing.T) {
	manager := hooks.NewManager(t.TempDir())
	for _, tc := range []struct {
		report string
		want   bool
	}{{"claude", true}, {"none", false}} {
		tool := config.Tool{Command: "cat", StatusSource: hooks.StatusSourceClaude, MCP: mcpreg.StyleNone, SessionReport: tc.report}
		if _, _, err := Environment(manager, "claude", tool, tool.Command, "abcd1234", t.TempDir(), time.Unix(5, 0)); err != nil {
			t.Fatalf("Environment: %v", err)
		}
		raw, err := os.ReadFile(manager.SettingsFile("abcd1234"))
		if err != nil {
			t.Fatalf("read settings: %v", err)
		}
		if got := strings.Contains(string(raw), " track-conversation --tool claude --key session_id"); got != tc.want {
			t.Fatalf("session_report %q: settings report the conversation = %v, want %v:\n%s", tc.report, got, tc.want, raw)
		}
		if !strings.Contains(string(raw), `"`+hooks.EnvLaunch+`": "5000000000"`) {
			t.Fatalf("settings do not carry the launch:\n%s", raw)
		}
	}
}

func TestEnvironmentSetsGrokTerminalTheme(t *testing.T) {
	for _, source := range []string{"HOME", "GROK_HOME"} {
		t.Run(source, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("GROK_HOME", "")
			dir := filepath.Join(home, ".grok")
			if source == "GROK_HOME" {
				dir = t.TempDir()
				t.Setenv("GROK_HOME", dir)
			}
			grok := config.Tool{Command: "cat", MCP: mcpreg.StyleNone}
			if _, _, err := Environment(hooks.NewManager(t.TempDir()), "grok", grok, grok.Command, "abcd1234", t.TempDir(), time.Time{}); err != nil {
				t.Fatalf("Environment grok: %v", err)
			}
			written, err := os.ReadFile(filepath.Join(dir, "config.toml"))
			if err != nil {
				t.Fatalf("grok config: %v", err)
			}
			if !strings.Contains(string(written), "theme = \"terminal\"") || !strings.Contains(string(written), "terminal_theme = true") {
				t.Fatalf("grok config = %q", written)
			}
		})
	}
}

func TestEnvironmentKeepsGrokOffTheSharedLeader(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GROK_HOME", "")
	grok := config.Tool{Command: "cat", MCP: mcpreg.StyleNone}
	manager := hooks.NewManager(t.TempDir())
	command, _, err := Environment(manager, "grok", grok, "cat --resume abc", "abcd1234", t.TempDir(), time.Time{})
	if err != nil {
		t.Fatalf("Environment grok: %v", err)
	}
	if command != agentLaunch(manager, "cat --resume abc --no-leader") {
		t.Fatalf("grok command = %q, want every launch line to force a local agent", command)
	}
}

func TestAssembleCarriesTheDirectiveOverAPastedImagePath(t *testing.T) {
	flagged := config.Tool{Command: "claude", PromptFlag: "-p"}
	prompt := "/var/folders/_b/T/agent-manager-pastes/paste-268.png why is this session working?"
	plan := Assemble("claude", flagged, prompt, true, true)
	if len(plan.PendingInputs) != 0 {
		t.Fatalf("a prompt led by an image path is no slash command, got %v", plan.PendingInputs)
	}
	if !strings.Contains(plan.Command, RenameDirective) || !strings.Contains(plan.Command, prompt) {
		t.Fatalf("directive should ride the prompt, got %q", plan.Command)
	}
	if DirectiveEmbeddable("/compact") || DirectiveEmbeddable("/land-pr now") {
		t.Fatal("a slash command must still open its own message")
	}
}

func TestMuseLaunchAndRevive(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	tool := cfg.Tools["muse"]
	plan := Assemble("muse", tool, "fix the bug", false, true)
	// Muse carries the MCP tools, whose descriptions stand in for the note.
	if !strings.HasPrefix(plan.Command, "muse '") || strings.Contains(plan.Command, ProactiveCoordinationNote) || !strings.Contains(plan.Command, "fix the bug") {
		t.Fatalf("launch = %+v", plan)
	}
	if plan.AgentSessionID != "" || len(plan.PendingInputs) != 0 {
		t.Fatalf("unexpected session flag or deferred prompt: %+v", plan)
	}
	if got := ReviveCommand(tool, "session-id"); got != "muse resume 'session-id'" {
		t.Fatal(got)
	}
	if got := ReviveCommand(tool, ""); got != "muse resume" {
		t.Fatal(got)
	}
}

func TestAntigravityLaunchAndRevive(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	tool := cfg.Tools["antigravity"]
	plan := Assemble("antigravity", tool, "fix the bug", false, true)
	// agy carries the MCP tools, whose descriptions stand in for the note.
	if !strings.HasPrefix(plan.Command, "agy -i '") || !strings.Contains(plan.Command, "fix the bug") || strings.Contains(plan.Command, ProactiveCoordinationNote) {
		t.Fatalf("launch = %+v", plan)
	}
	if plan.AgentSessionID != "" || len(plan.PendingInputs) != 0 {
		t.Fatalf("unexpected session flag or deferred prompt: %+v", plan)
	}
	if got := ReviveCommand(tool, "0ab14c4d-1cb2-454a-b992-b622e29c0825"); got != "agy --conversation '0ab14c4d-1cb2-454a-b992-b622e29c0825'" {
		t.Fatal(got)
	}
	if got := ReviveCommand(tool, ""); got != "agy" {
		t.Fatal(got)
	}
}
