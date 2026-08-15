package mcpserver

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeTerminalCommands struct {
	listed      []sessioncmd.Terminal
	created     sessioncmd.Terminal
	screen      sessioncmd.TerminalScreen
	createdOpts sessioncmd.CreateTerminalOptions
	sentID      string
	sentCommand string
	sentKeys    []string
	readID      string
	closedID    string
	err         error
}

func (f *fakeTerminalCommands) List(string) ([]sessioncmd.Terminal, error) {
	return f.listed, f.err
}

func (f *fakeTerminalCommands) Create(_ string, opts sessioncmd.CreateTerminalOptions) (sessioncmd.Terminal, error) {
	f.createdOpts = opts
	return f.created, f.err
}

func (f *fakeTerminalCommands) Send(_ string, id, command string, keys []string) error {
	f.sentID = id
	f.sentCommand = command
	f.sentKeys = append([]string(nil), keys...)
	return f.err
}

func (f *fakeTerminalCommands) Read(_ string, id string) (sessioncmd.TerminalScreen, error) {
	f.readID = id
	return f.screen, f.err
}

func (f *fakeTerminalCommands) Close(_ string, id string) error {
	f.closedID = id
	return f.err
}

func connect(t *testing.T, configDir, sessionID string) *mcp.ClientSession {
	t.Helper()
	return connectServer(t, NewServer(configDir, sessionID, "test"))
}

func connectServer(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", tool, err)
	}
	return result
}

func callText(t *testing.T, session *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	result := callTool(t, session, tool, args)
	var text strings.Builder
	for _, content := range result.Content {
		if tc, ok := content.(*mcp.TextContent); ok {
			text.WriteString(tc.Text)
		}
	}
	return text.String(), result.IsError
}

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func TestListsAllTools(t *testing.T) {
	session := connect(t, t.TempDir(), "abc123")
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{
		"rename", "review_repo", "review_base", "review_mode",
		"list_terminals", "create_terminal", "send_terminal", "read_terminal", "close_terminal",
	} {
		if !names[want] {
			t.Fatalf("missing tool %q in %v", want, names)
		}
	}
}

func TestServerTeachesProactiveTerminalWorkflow(t *testing.T) {
	session := connect(t, t.TempDir(), "abc123")
	instructions := session.InitializeResult().Instructions
	for _, want := range []string{
		"SSH",
		"one-shot",
		"list_terminals",
		"create_terminal",
		"close_terminal",
		"nests under this session",
	} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("server instructions do not teach %q:\n%s", want, instructions)
		}
	}
}

func TestTerminalDescriptionsTeachWhenAndHowToChainTools(t *testing.T) {
	session := connect(t, t.TempDir(), "abc123")
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	descriptions := map[string]string{}
	for _, tool := range listed.Tools {
		descriptions[tool.Name] = tool.Description
	}
	for tool, wants := range map[string][]string{
		"list_terminals":  {"Call proactively", "Reuse", "create_terminal"},
		"create_terminal": {"SSH", "one-shot", "nests", "close_terminal"},
		"close_terminal":  {"finished", "kills", "SSH"},
		"send_terminal":   {"create_terminal", "read_terminal", "executes on the user's machine"},
		"read_terminal":   {"after send_terminal", "monitor ongoing work"},
	} {
		for _, want := range wants {
			if !strings.Contains(descriptions[tool], want) {
				t.Errorf("%s description does not contain %q: %s", tool, want, descriptions[tool])
			}
		}
	}
}

func TestTerminalToolsExposeStructuredResultsAndForwardArguments(t *testing.T) {
	group := "backend"
	fake := &fakeTerminalCommands{
		listed: []sessioncmd.Terminal{{
			ID: "a1b2c3d4", Name: "terminal-a1b2", Group: group,
			Directory: "/work", Status: "idle", Running: true,
		}},
		created: sessioncmd.Terminal{
			ID: "e5f6a7b8", Name: "terminal-e5f6", Group: group,
			Directory: "/tmp", Status: "starting", Running: true,
		},
		screen: sessioncmd.TerminalScreen{
			Terminal: sessioncmd.Terminal{ID: "a1b2c3d4", Name: "terminal-a1b2", Running: true},
			Output:   "build complete",
		},
	}
	session := connectServer(t, newServer(t.TempDir(), "abc123", "test", fake))

	listed := callTool(t, session, "list_terminals", map[string]any{})
	if listed.IsError || listed.StructuredContent == nil {
		t.Fatalf("list_terminals = %+v", listed)
	}
	if text, _ := callText(t, session, "list_terminals", map[string]any{}); !strings.Contains(text, "terminal-a1b2") {
		t.Fatalf("list text = %q", text)
	}

	created := callTool(t, session, "create_terminal", map[string]any{"group": group, "directory": "/tmp"})
	if created.IsError || created.StructuredContent == nil {
		t.Fatalf("create_terminal = %+v", created)
	}
	if fake.createdOpts.Group == nil || *fake.createdOpts.Group != group || fake.createdOpts.Directory != "/tmp" {
		t.Fatalf("create args = %+v", fake.createdOpts)
	}

	if text, isError := callText(t, session, "send_terminal", map[string]any{
		"terminal_id": "a1b2c3d4", "command": "go test ./...",
	}); isError || !strings.Contains(text, "sent command") {
		t.Fatalf("send command = %q, isError=%v", text, isError)
	}
	if fake.sentID != "a1b2c3d4" || fake.sentCommand != "go test ./..." || len(fake.sentKeys) != 0 {
		t.Fatalf("send command args = id %q command %q keys %v", fake.sentID, fake.sentCommand, fake.sentKeys)
	}

	if _, isError := callText(t, session, "send_terminal", map[string]any{
		"terminal_id": "a1b2c3d4", "keys": []string{"C-c", "Enter"},
	}); isError {
		t.Fatal("send keys returned an error")
	}
	if fake.sentCommand != "" || strings.Join(fake.sentKeys, ",") != "C-c,Enter" {
		t.Fatalf("send key args = command %q keys %v", fake.sentCommand, fake.sentKeys)
	}

	if text, isError := callText(t, session, "read_terminal", map[string]any{"terminal_id": "a1b2c3d4"}); isError || text != "build complete" {
		t.Fatalf("read = %q, isError=%v", text, isError)
	}
	if fake.readID != "a1b2c3d4" {
		t.Fatalf("read id = %q", fake.readID)
	}
}

func TestCloseTerminalForwardsID(t *testing.T) {
	fake := &fakeTerminalCommands{}
	session := connectServer(t, newServer(t.TempDir(), "abc123", "test", fake))
	if text, isError := callText(t, session, "close_terminal", map[string]any{"terminal_id": "a1b2c3d4"}); isError {
		t.Fatalf("close_terminal = %q, isError=%v", text, isError)
	}
	if fake.closedID != "a1b2c3d4" {
		t.Fatalf("closed id = %q", fake.closedID)
	}
}

func TestCreateTerminalForwardsNest(t *testing.T) {
	fake := &fakeTerminalCommands{
		created: sessioncmd.Terminal{ID: "e5f6a7b8", Name: "terminal-e5f6"},
	}
	session := connectServer(t, newServer(t.TempDir(), "abc123", "test", fake))

	if created := callTool(t, session, "create_terminal", map[string]any{}); created.IsError {
		t.Fatalf("create_terminal no args = %+v", created)
	}
	if fake.createdOpts.Nest != nil {
		t.Fatalf("omitted nest = %+v, want nil", fake.createdOpts.Nest)
	}

	if created := callTool(t, session, "create_terminal", map[string]any{"nest": false}); created.IsError {
		t.Fatalf("create_terminal nest false = %+v", created)
	}
	if fake.createdOpts.Nest == nil || *fake.createdOpts.Nest {
		t.Fatalf("nest false = %+v", fake.createdOpts.Nest)
	}
}

func TestTerminalToolAnnotationsDescribeLocalRisk(t *testing.T) {
	session := connect(t, t.TempDir(), "abc123")
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
	}
	for _, name := range []string{"list_terminals", "read_terminal"} {
		if annotations := tools[name].Annotations; annotations == nil || !annotations.ReadOnlyHint || annotations.OpenWorldHint == nil || *annotations.OpenWorldHint {
			t.Fatalf("%s annotations = %+v", name, annotations)
		}
		if tools[name].OutputSchema == nil {
			t.Fatalf("%s has no structured output schema", name)
		}
	}
	if annotations := tools["create_terminal"].Annotations; annotations == nil || annotations.DestructiveHint == nil || *annotations.DestructiveHint {
		t.Fatalf("create annotations = %+v", annotations)
	}
	if annotations := tools["send_terminal"].Annotations; annotations == nil || annotations.DestructiveHint == nil || !*annotations.DestructiveHint || annotations.OpenWorldHint == nil || !*annotations.OpenWorldHint {
		t.Fatalf("send annotations = %+v", annotations)
	}
	if tool := tools["close_terminal"]; tool == nil {
		t.Fatal("missing close_terminal")
	} else if annotations := tool.Annotations; annotations == nil || annotations.DestructiveHint == nil || !*annotations.DestructiveHint {
		t.Fatalf("close annotations = %+v", annotations)
	}
}

func TestTerminalToolErrorsAreToolErrors(t *testing.T) {
	fake := &fakeTerminalCommands{err: errors.New("terminal is not running")}
	session := connectServer(t, newServer(t.TempDir(), "abc123", "test", fake))
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"list_terminals", map[string]any{}},
		{"create_terminal", map[string]any{}},
		{"send_terminal", map[string]any{"terminal_id": "a1", "command": "pwd"}},
		{"read_terminal", map[string]any{"terminal_id": "a1"}},
		{"close_terminal", map[string]any{"terminal_id": "a1"}},
	} {
		text, isError := callText(t, session, call.name, call.args)
		if !isError || !strings.Contains(text, "not running") {
			t.Fatalf("%s = %q, isError=%v", call.name, text, isError)
		}
	}
}

func TestRenameWritesMailbox(t *testing.T) {
	configDir := t.TempDir()
	session := connect(t, configDir, "abc123")
	text, isError := callText(t, session, "rename", map[string]any{"name": "fix-auth-bug"})
	if isError || !strings.Contains(text, "fix-auth-bug") {
		t.Fatalf("rename = %q, isError=%v", text, isError)
	}
	content, err := os.ReadFile(hooks.NewManager(configDir).NameFile("abc123"))
	if err != nil || string(content) != "fix-auth-bug" {
		t.Fatalf("mailbox = %q, %v", content, err)
	}
}

func TestReviewRepoWritesMailbox(t *testing.T) {
	configDir := t.TempDir()
	repo := gitRepo(t)
	session := connect(t, configDir, "abc123")
	text, isError := callText(t, session, "review_repo", map[string]any{"path": repo})
	if isError {
		t.Fatalf("review_repo error: %q", text)
	}
	content, err := os.ReadFile(hooks.NewManager(configDir).ReviewRepoFile("abc123"))
	if err != nil {
		t.Fatal(err)
	}
	resolvedRepo, _ := filepath.EvalSymlinks(repo)
	resolvedGot, _ := filepath.EvalSymlinks(strings.TrimSpace(string(content)))
	if resolvedGot != resolvedRepo {
		t.Fatalf("mailbox repo = %q, want %q", resolvedGot, resolvedRepo)
	}
}

func TestReviewBaseAndAutoClear(t *testing.T) {
	configDir := t.TempDir()
	repo := gitRepo(t)
	session := connect(t, configDir, "abc123")

	text, isError := callText(t, session, "review_base", map[string]any{"ref": "main", "repo_path": repo})
	if isError || !strings.Contains(text, "main") {
		t.Fatalf("review_base = %q, isError=%v", text, isError)
	}
	mailbox := hooks.NewManager(configDir).ReviewBaseFile("abc123")
	content, err := os.ReadFile(mailbox)
	if err != nil || !strings.HasSuffix(string(content), "\nmain\n") {
		t.Fatalf("mailbox = %q, %v", content, err)
	}

	text, isError = callText(t, session, "review_base", map[string]any{"ref": "auto", "repo_path": repo})
	if isError || !strings.Contains(text, "cleared") {
		t.Fatalf("clear = %q, isError=%v", text, isError)
	}
	content, err = os.ReadFile(mailbox)
	if err != nil || !strings.HasSuffix(string(content), "\n\n") {
		t.Fatalf("cleared mailbox = %q, %v", content, err)
	}
}

func TestBadInputsReturnToolErrors(t *testing.T) {
	configDir := t.TempDir()

	session := connect(t, configDir, "abc123")
	if text, isError := callText(t, session, "rename", map[string]any{"name": "  "}); !isError {
		t.Fatalf("empty name should error, got %q", text)
	}
	if text, isError := callText(t, session, "review_repo", map[string]any{"path": t.TempDir()}); !isError {
		t.Fatalf("non-repo path should error, got %q", text)
	}
	if text, isError := callText(t, session, "review_base", map[string]any{"ref": "nope-branch", "repo_path": gitRepo(t)}); !isError {
		t.Fatalf("unknown ref should error, got %q", text)
	}
	if text, isError := callText(t, session, "review_mode", map[string]any{"scope": "bogus"}); !isError {
		t.Fatalf("unknown scope should error, got %q", text)
	}

	noSession := connect(t, configDir, "")
	if text, isError := callText(t, noSession, "rename", map[string]any{"name": "x"}); !isError || !strings.Contains(text, "AGENT_MANAGER_SESSION_ID") {
		t.Fatalf("missing session id should error, got %q", text)
	}
}

func TestReviewModeWritesMailbox(t *testing.T) {
	configDir := t.TempDir()
	session := connect(t, configDir, "abc123")

	for _, scope := range []string{"uncommitted", "branch", "last_commit", "staged"} {
		text, isError := callText(t, session, "review_mode", map[string]any{"scope": scope})
		if isError {
			t.Fatalf("review_mode(%q) error: %q", scope, text)
		}
		content, err := os.ReadFile(hooks.NewManager(configDir).ReviewScopeFile("abc123"))
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(content)); got != scope {
			t.Fatalf("mailbox scope = %q, want %q", got, scope)
		}
	}
}
