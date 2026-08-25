package launch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
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
	plan := Assemble("claude", flagged, "build the api", true)
	if !strings.HasPrefix(plan.Command, "claude -p '"+RenameDirective) {
		t.Fatalf("auto-named flagged command = %q", plan.Command)
	}
	if plan.AgentSessionID == "" || !strings.Contains(plan.Command, "--session-id "+plan.AgentSessionID) {
		t.Fatalf("command should carry the chosen session id, got %q", plan.Command)
	}
	if len(plan.PendingInputs) != 0 {
		t.Fatalf("an embeddable directive needs no pending input, got %v", plan.PendingInputs)
	}

	deferred := Assemble("claude", flagged, "/compact the notes", true)
	if len(deferred.PendingInputs) != 1 || deferred.PendingInputs[0] != DeferredRenameDirective {
		t.Fatalf("slash-command launch should defer the directive, got %v", deferred.PendingInputs)
	}

	sent := config.Tool{Command: "hermes", PromptMode: "send"}
	typed := Assemble("hermes", sent, "build the api", false)
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
	noClient := config.Tool{Command: "pi", PromptFlag: "-p"}
	carried := Assemble("pi", noClient, "build the api", false)
	if !strings.Contains(carried.Command, CoordinationNote) {
		t.Fatalf("a tool with no MCP client should be pointed at the subcommands, got %q", carried.Command)
	}
	if len(carried.PendingInputs) != 0 {
		t.Fatalf("a note the prompt carried needs no pending input, got %v", carried.PendingInputs)
	}

	withClient := config.Tool{Command: "claude", PromptFlag: "-p"}
	if plan := Assemble("claude", withClient, "build the api", false); strings.Contains(plan.Command, CoordinationNote) {
		t.Fatalf("a tool whose MCP tool descriptions say this already must not repeat it, got %q", plan.Command)
	}

	commandCode := Assemble("command-code", config.Tool{Command: "cmd"}, "build the api", false)
	if strings.Contains(commandCode.Command, CoordinationNote) {
		t.Fatalf("command-code registers MCP on spawn, so it must not get the subcommand note, got %q", commandCode.Command)
	}

	// A slash command carries neither, so both queue, and the order is what
	// the agent reads: the directive ends on "Then continue.", and the note
	// is what it continues into.
	deferred := Assemble("pi", noClient, "/compact the notes", true)
	if len(deferred.PendingInputs) != 2 ||
		deferred.PendingInputs[0] != DeferredRenameDirective || deferred.PendingInputs[1] != CoordinationNote {
		t.Fatalf("a launch needing both should queue them in reading order, got %v", deferred.PendingInputs)
	}

	promptless := Assemble("pi", noClient, "", false)
	if promptless.Command != noClient.Command {
		t.Fatalf("a promptless launch command should stay clean, got %q", promptless.Command)
	}
	if len(promptless.PendingInputs) != 1 || promptless.PendingInputs[0] != CoordinationNote {
		t.Fatalf("a prompt that cannot carry the note should queue it, got %v", promptless.PendingInputs)
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
	command, env, err := Environment(manager, "plain", plain, plain.Command, "abcd1234")
	if err != nil {
		t.Fatalf("Environment: %v", err)
	}
	if env[hooks.EnvSessionID] != "abcd1234" {
		t.Fatalf("plain tool env = %v, want session id", env)
	}
	if env[hooks.EnvStatusFile] != "" {
		t.Fatalf("a tool without claude hooks must not get a status file, got %q", env[hooks.EnvStatusFile])
	}
	if command != plain.Command {
		t.Fatalf("a tool with no MCP style should launch untouched, got %q", command)
	}

	hooked := config.Tool{Command: "cat", StatusSource: hooks.StatusSourceClaude, MCP: "claude"}
	command, env, err = Environment(manager, "hooked", hooked, hooked.Command, "abcd1234")
	if err != nil {
		t.Fatalf("Environment hooked: %v", err)
	}
	if env[hooks.EnvSessionID] != "abcd1234" || env[hooks.EnvStatusFile] == "" {
		t.Fatalf("hooked tool env = %v, want session id and status file", env)
	}
	if !strings.Contains(command, "--mcp-config '") || !strings.Contains(command, "--settings '") {
		t.Fatalf("hooked command = %q", command)
	}
}

func TestAssembleCarriesTheDirectiveOverAPastedImagePath(t *testing.T) {
	flagged := config.Tool{Command: "claude", PromptFlag: "-p"}
	prompt := "/var/folders/_b/T/agent-manager-pastes/paste-268.png why is this session working?"
	plan := Assemble("claude", flagged, prompt, true)
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
