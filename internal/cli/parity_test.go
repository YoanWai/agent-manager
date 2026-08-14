package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/mcpserver"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The rest of this suite drives the subcommands against a fake layer, and
// the MCP suite does the same on its side, so a front that forwarded the
// wrong caller, dropped an argument or grew a sentence of its own would
// pass both. These tests run the two fronts over one real sessioncmd
// layer and one store. They stay on the shared task list, which is the
// part of that layer no tmux command touches.
type parityWorkspace struct {
	configDir string
	tasks     taskCommands
	files     fileCommands
	lead      store.Session
	worker    store.Session
}

func newParityWorkspace(t *testing.T) *parityWorkspace {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	configDir := t.TempDir()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	defer st.Close()
	commands := sessioncmd.NewSessions(configDir, sessioncmd.CLIVocabulary())
	workspace := &parityWorkspace{
		configDir: configDir,
		tasks:     commands,
		files:     commands,
		lead:      store.Session{ID: uuid.NewString()[:8], Name: "lead-agent", Tool: "claude", Cwd: configDir, Status: status.Idle},
		worker:    store.Session{ID: uuid.NewString()[:8], Name: "worker-agent", Tool: "claude", Cwd: configDir, Status: status.Idle},
	}
	for _, sess := range []store.Session{workspace.lead, workspace.worker} {
		if err := st.CreateSession(sess); err != nil {
			t.Fatalf("create session row: %v", err)
		}
	}
	return workspace
}

func (w *parityWorkspace) mcpText(t *testing.T, sessionID, tool string, args map[string]any) (string, bool) {
	t.Helper()
	result := w.mcpCall(t, sessionID, tool, args)
	var text strings.Builder
	for _, content := range result.Content {
		if written, ok := content.(*mcp.TextContent); ok {
			text.WriteString(written.Text)
		}
	}
	return text.String(), result.IsError
}

func (w *parityWorkspace) mcpCall(t *testing.T, sessionID, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := mcpserver.NewServer(w.configDir, sessionID, "test").Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatalf("serve: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "parity-client", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", tool, err)
	}
	return result
}

func cliText(t *testing.T, run func(*bytes.Buffer) error) string {
	t.Helper()
	out := &bytes.Buffer{}
	if err := run(out); err != nil {
		t.Fatalf("subcommand: %v", err)
	}
	return out.String()
}

// A task created from a shell reaches an agent holding only MCP tools, and
// what that agent does with it comes back to the shell.
func TestTheTwoFrontsShareOneTaskList(t *testing.T) {
	w := newParityWorkspace(t)

	created := cliText(t, func(out *bytes.Buffer) error {
		return runTaskCreate(out, w.tasks, []string{"wire the cli", "--body", "five files"}, w.lead.ID)
	})
	id := taskID(t, created)

	if text, isError := w.mcpText(t, w.worker.ID, "claim_task", map[string]any{"task_id": id}); isError ||
		!strings.Contains(text, "claimed wire the cli ("+id+") [in_progress]") {
		t.Fatalf("claim_task = %q, isError=%v", text, isError)
	}
	listed := cliText(t, func(out *bytes.Buffer) error {
		return runTaskList(out, w.tasks, nil, w.lead.ID)
	})
	if !strings.Contains(listed, "held by worker-agent") || strings.Contains(listed, "; yours") {
		t.Fatalf("the shell front does not see the MCP claim: %q", listed)
	}
	// The claim holds against the other front too, and says who has it.
	err := runTaskClaim(&bytes.Buffer{}, w.tasks, []string{id}, w.lead.ID)
	if err == nil || !strings.Contains(err.Error(), "already claimed by worker-agent") {
		t.Fatalf("claiming across fronts = %v", err)
	}

	finished := cliText(t, func(out *bytes.Buffer) error {
		return runTaskFinish(out, w.tasks, []string{id}, w.worker.ID)
	})
	if !strings.Contains(finished, "[done]") {
		t.Fatalf("finish = %q", finished)
	}
	if text, isError := w.mcpText(t, w.worker.ID, "list_tasks", map[string]any{}); isError ||
		!strings.Contains(text, "wire the cli ("+id+") [done]") {
		t.Fatalf("the MCP front does not see the shell's finish: %q, isError=%v", text, isError)
	}
}

// Same caller, same operation, same words: the sentences come from
// sessioncmd, and a front writing its own would drift from the other.
func TestTheTwoFrontsAnswerOneOperationIdentically(t *testing.T) {
	w := newParityWorkspace(t)
	created := cliText(t, func(out *bytes.Buffer) error {
		return runTaskCreate(out, w.tasks, []string{"wire the cli"}, w.lead.ID)
	})
	id := taskID(t, created)
	if _, err := w.tasks.ClaimTask(w.worker.ID, id); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	fromCLI := cliText(t, func(out *bytes.Buffer) error {
		return runTaskList(out, w.tasks, nil, w.lead.ID)
	})
	fromMCP, isError := w.mcpText(t, w.lead.ID, "list_tasks", map[string]any{})
	if isError {
		t.Fatalf("list_tasks errored: %q", fromMCP)
	}
	if strings.TrimSpace(fromCLI) != strings.TrimSpace(fromMCP) {
		t.Fatalf("the fronts describe one list differently:\ncli: %q\nmcp: %q", fromCLI, fromMCP)
	}

	cliRecord := cliText(t, func(out *bytes.Buffer) error {
		return runTaskList(out, w.tasks, []string{"--json"}, w.lead.ID)
	})
	var cliTasks []sessioncmd.Task
	if err := json.Unmarshal([]byte(cliRecord), &cliTasks); err != nil {
		t.Fatalf("task list --json is not JSON: %v (%q)", err, cliRecord)
	}
	structured := w.mcpCall(t, w.lead.ID, "list_tasks", map[string]any{}).StructuredContent
	encoded, err := json.Marshal(structured)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var mcpTasks struct {
		Tasks []sessioncmd.Task `json:"tasks"`
	}
	if err := json.Unmarshal(encoded, &mcpTasks); err != nil {
		t.Fatalf("structured content is not a task list: %v (%s)", err, encoded)
	}
	if !reflect.DeepEqual(cliTasks, mcpTasks.Tasks) {
		t.Fatalf("the fronts return different records:\ncli: %+v\nmcp: %+v", cliTasks, mcpTasks.Tasks)
	}

	// A refusal is what an agent reads most often, so it has to match too.
	cliRefusal := runTaskClaim(&bytes.Buffer{}, w.tasks, []string{id}, w.lead.ID)
	if cliRefusal == nil {
		t.Fatal("claiming a held task should be refused")
	}
	mcpRefusal, isError := w.mcpText(t, w.lead.ID, "claim_task", map[string]any{"task_id": id})
	if !isError {
		t.Fatalf("claim_task on a held task = %q, want a tool error", mcpRefusal)
	}
	if cliRefusal.Error() != mcpRefusal {
		t.Fatalf("the fronts refuse differently:\ncli: %q\nmcp: %q", cliRefusal, mcpRefusal)
	}
}

// A bound enforced on one front is a bound the other front does not have.
// Both take the lease duration in their own units, and an out-of-range one
// has to come back refused in the same words whichever front asked.
func TestTheTwoFrontsRefuseTheSameOutOfRangeTTL(t *testing.T) {
	w := newParityWorkspace(t)
	for _, out := range []struct {
		name    string
		ttl     string
		minutes int
	}{
		{"beyond the maximum", "9h", 540},
		{"negative", "-5m", -5},
	} {
		t.Run(out.name, func(t *testing.T) {
			fromCLI := runReserve(&bytes.Buffer{}, w.files, []string{"--ttl", out.ttl, "internal/store/store.go"}, w.lead.ID)
			if fromCLI == nil {
				t.Fatal("the shell front granted a lease outside the bound")
			}
			fromMCP, isError := w.mcpText(t, w.lead.ID, "reserve_files", map[string]any{
				"paths": []string{"internal/store/store.go"}, "ttl_minutes": out.minutes,
			})
			if !isError {
				t.Fatalf("the MCP front granted a lease outside the bound: %q", fromMCP)
			}
			if fromCLI.Error() != fromMCP {
				t.Fatalf("the fronts refuse differently:\ncli: %q\nmcp: %q", fromCLI, fromMCP)
			}
			if !strings.Contains(fromMCP, "outside 0 to "+sessioncmd.MaxReservationTTL.String()) {
				t.Fatalf("the refusal does not name the bound: %q", fromMCP)
			}
		})
	}
	// The bound refuses what is outside it and nothing else, on both fronts:
	// an extra check grown on one of them is the drift this file is here for.
	if err := runReserve(&bytes.Buffer{}, w.files, []string{"--ttl", "45m", "internal/store/store.go"}, w.lead.ID); err != nil {
		t.Fatalf("a ttl inside the bound was refused: %v", err)
	}
	granted, isError := w.mcpText(t, w.lead.ID, "reserve_files", map[string]any{
		"paths": []string{"internal/store/tasks.go"}, "ttl_minutes": 45,
	})
	if isError {
		t.Fatalf("the MCP front refused a ttl inside the bound: %q", granted)
	}
}

// An agent holds one front or the other: a session whose CLI carries no
// MCP client has the subcommands and nothing else. An error naming the
// words of the front the reader does not have sends it after a command it
// cannot run.
func TestEachFrontNamesTheCommandsItsCallerHas(t *testing.T) {
	w := newParityWorkspace(t)

	fromCLI := runTaskFinish(&bytes.Buffer{}, w.tasks, []string{"nosuch12"}, w.lead.ID)
	if fromCLI == nil {
		t.Fatal("finishing a task that does not exist should be refused")
	}
	if !strings.Contains(fromCLI.Error(), "agent-manager task list") {
		t.Fatalf("the shell front does not name a subcommand: %q", fromCLI)
	}
	if strings.Contains(fromCLI.Error(), "list_tasks") {
		t.Fatalf("the shell front names an MCP tool its caller cannot call: %q", fromCLI)
	}

	fromMCP, isError := w.mcpText(t, w.lead.ID, "finish_task", map[string]any{"task_id": "nosuch12"})
	if !isError {
		t.Fatalf("finish_task on a missing task = %q, want a tool error", fromMCP)
	}
	if !strings.Contains(fromMCP, "list_tasks") || strings.Contains(fromMCP, "agent-manager") {
		t.Fatalf("the MCP front does not speak in tools: %q", fromMCP)
	}
}

// Every spelling the shell front hands out has to be a command that
// actually dispatches, since the whole point is an agent running it.
func TestTheCLIVocabularyNamesRealSubcommands(t *testing.T) {
	table := Commands()
	verbs := map[string][]command{}
	for _, section := range sections() {
		for _, registered := range section.commands {
			verbs[registered.name] = registered.verbs
		}
	}
	words := reflect.ValueOf(sessioncmd.CLIVocabulary())
	for index := range words.NumField() {
		phrase := words.Field(index).String()
		field := words.Type().Field(index).Name
		parts := strings.Fields(phrase)
		if len(parts) < 2 || parts[0] != "agent-manager" {
			t.Fatalf("%s is %q, which is not a command an agent can run", field, phrase)
		}
		if table[parts[1]] == nil {
			t.Fatalf("%s names %q, which is not a subcommand", field, parts[1])
		}
		if len(parts) > 2 && !strings.HasPrefix(parts[2], "-") && !slices.ContainsFunc(verbs[parts[1]], func(verb command) bool {
			return verb.name == parts[2]
		}) {
			t.Fatalf("%s names %q, which %s has no verb for", field, parts[2], parts[1])
		}
	}
}

// taskID reads the id back out of the sentence a create printed, which is
// the only place either front hands one to its caller.
func taskID(t *testing.T, created string) string {
	t.Helper()
	opened := strings.Index(created, "(")
	closed := strings.Index(created, ")")
	if opened < 0 || closed < opened {
		t.Fatalf("create printed no task id: %q", created)
	}
	return created[opened+1 : closed]
}
