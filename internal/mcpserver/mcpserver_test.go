package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

// The fake decides the input kind the way the real layer does, since the
// front is only allowed to report what it was handed.
func (f *fakeTerminalCommands) Send(_ string, id, command string, keys []string) (sessioncmd.TerminalInput, error) {
	f.sentID = id
	f.sentCommand = command
	f.sentKeys = append([]string(nil), keys...)
	sent := "keys"
	if strings.TrimSpace(command) != "" {
		sent = "command"
	}
	return sessioncmd.TerminalInput{TerminalID: id, Sent: sent}, f.err
}

func (f *fakeTerminalCommands) Read(_ string, id string) (sessioncmd.TerminalScreen, error) {
	f.readID = id
	return f.screen, f.err
}

func (f *fakeTerminalCommands) Close(_ string, id string) error {
	f.closedID = id
	return f.err
}

type fakeSessionCommands struct {
	listed        []sessioncmd.Session
	created       sessioncmd.Session
	screen        sessioncmd.SessionScreen
	groups        []sessioncmd.Group
	createdOpts   sessioncmd.CreateSessionOptions
	sentID        string
	sentMessage   string
	readID        string
	statusID      int64
	waitedID      string
	waitedUntil   []string
	waitedTimeout time.Duration
	tasks         []sessioncmd.Task
	taskTitle     string
	taskBody      string
	taskDeps      []string
	claimedTaskID string
	settledTaskID string
	reservedPaths []string
	reservedMode  string
	reservedTTL   time.Duration
	releasedPaths []string
	reservations  []sessioncmd.Reservation
	revivedID     string
	killedID      string
	archivedID    string
	archived      bool
	groupPath     string
	groupDir      string
	err           error
}

func (f *fakeSessionCommands) List(string) ([]sessioncmd.Session, error) {
	return f.listed, f.err
}

func (f *fakeSessionCommands) Create(_ string, opts sessioncmd.CreateSessionOptions) (sessioncmd.Session, error) {
	f.createdOpts = opts
	return f.created, f.err
}

func (f *fakeSessionCommands) Send(_ string, id, message string) (sessioncmd.SendResult, error) {
	f.sentID = id
	f.sentMessage = message
	return sessioncmd.SendResult{MessageID: 7, QueuePosition: 1, ManagerAwake: true}, f.err
}

func (f *fakeSessionCommands) Wait(_ context.Context, _, id string, until []string, timeout time.Duration) (sessioncmd.WaitResult, error) {
	f.waitedID = id
	f.waitedUntil = until
	f.waitedTimeout = timeout
	return sessioncmd.WaitResult{
		Session: sessioncmd.Session{ID: id, Name: "payments-retry", Tool: "claude", Status: "finished"},
		Reached: true,
		Waited:  "3s",
	}, f.err
}

func (f *fakeSessionCommands) MessageStatus(_ string, messageID int64) (sessioncmd.MessageState, error) {
	f.statusID = messageID
	return sessioncmd.MessageState{MessageID: messageID, SessionID: "a1b2c3d4", State: "delivered"}, f.err
}

func (f *fakeSessionCommands) Read(_ string, id string) (sessioncmd.SessionScreen, error) {
	f.readID = id
	return f.screen, f.err
}

func (f *fakeSessionCommands) Revive(_ string, id string) (sessioncmd.Session, error) {
	f.revivedID = id
	return f.created, f.err
}

func (f *fakeSessionCommands) Kill(_ string, id string) (sessioncmd.Session, error) {
	f.killedID = id
	return f.created, f.err
}

// The row an archive returns carries the state it landed in, which is what
// the front reads its verb off.
func (f *fakeSessionCommands) Archive(_ string, id string, archived bool) (sessioncmd.Session, error) {
	f.archivedID = id
	f.archived = archived
	updated := f.created
	updated.Archived = archived
	return updated, f.err
}

func (f *fakeSessionCommands) Tasks(string) ([]sessioncmd.Task, error) {
	return f.tasks, f.err
}

func (f *fakeSessionCommands) CreateTask(_ string, title, body string, dependsOn []string) (sessioncmd.Task, error) {
	f.taskTitle = title
	f.taskBody = body
	f.taskDeps = dependsOn
	return sessioncmd.Task{ID: "t1", Title: title, State: "pending"}, f.err
}

func (f *fakeSessionCommands) ClaimTask(_ string, taskID string) (sessioncmd.Task, error) {
	f.claimedTaskID = taskID
	return sessioncmd.Task{ID: "t1", Title: "fix retries", State: "in_progress", Mine: true}, f.err
}

func (f *fakeSessionCommands) FinishTask(_ string, taskID string) (sessioncmd.Task, error) {
	f.settledTaskID = taskID
	return sessioncmd.Task{ID: taskID, Title: "fix retries", State: "done"}, f.err
}

func (f *fakeSessionCommands) ReleaseTask(_ string, taskID string) (sessioncmd.Task, error) {
	f.settledTaskID = taskID
	return sessioncmd.Task{ID: taskID, Title: "fix retries", State: "pending"}, f.err
}

func (f *fakeSessionCommands) DeleteTask(_ string, taskID string) error {
	f.settledTaskID = taskID
	return f.err
}

func (f *fakeSessionCommands) Reserve(_ string, patterns []string, mode, note string, ttl time.Duration) (sessioncmd.ReserveResult, error) {
	f.reservedPaths = patterns
	f.reservedMode = mode
	f.reservedTTL = ttl
	return sessioncmd.ReserveResult{
		Reserved:  []sessioncmd.Reservation{{Pattern: patterns[0], Mode: "exclusive", Holder: "lead", ExpiresIn: "30m0s", Mine: true}},
		Conflicts: []sessioncmd.Reservation{{Pattern: "internal/store/store.go", Mode: "exclusive", Holder: "worker", ExpiresIn: "12m0s", Note: "adding a table"}},
	}, f.err
}

func (f *fakeSessionCommands) ReleaseFiles(_ string, patterns []string) (int, error) {
	f.releasedPaths = patterns
	return len(patterns), f.err
}

func (f *fakeSessionCommands) Reservations(string) ([]sessioncmd.Reservation, error) {
	return f.reservations, f.err
}

func (f *fakeSessionCommands) Groups(string) ([]sessioncmd.Group, error) {
	return f.groups, f.err
}

func (f *fakeSessionCommands) CreateGroup(_ string, path, directory string) (sessioncmd.Group, error) {
	f.groupPath = path
	f.groupDir = directory
	return sessioncmd.Group{Path: path, Directory: directory}, f.err
}

func (f *fakeSessionCommands) DeleteGroup(_ string, path string) (sessioncmd.GroupRemoval, error) {
	f.groupPath = path
	return sessioncmd.GroupRemoval{Removed: []string{path}, Moved: []string{"a1b2c3d4"}}, f.err
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
		"rename", "review",
		"list_terminals", "create_terminal", "send_terminal", "read_terminal", "close_terminal",
	} {
		if !names[want] {
			t.Fatalf("missing tool %q in %v", want, names)
		}
	}
}

// The errors this server returns tell an agent what to call next, and it
// can only call the tools this server registered.
func TestTheMCPVocabularyNamesRealTools(t *testing.T) {
	session := connect(t, t.TempDir(), "abc123")
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, tool := range tools.Tools {
		registered[tool.Name] = true
	}
	words := reflect.ValueOf(sessioncmd.MCPVocabulary())
	for index := range words.NumField() {
		phrase := words.Field(index).String()
		field := words.Type().Field(index).Name
		// A phrase may carry an argument, as archive_session does; the tool
		// is the first word of it.
		tool, _, _ := strings.Cut(phrase, " ")
		if !registered[tool] {
			t.Fatalf("%s names %q, which this server does not serve: %v", field, tool, registered)
		}
	}
}

// A schema tag is a literal, so raising a constant would leave the tools
// advertising the old ceiling while the shell front rewrote its own help.
func TestSchemaBoundsMatchTheirConstants(t *testing.T) {
	bounds := []struct {
		args  any
		field string
		want  string
	}{
		{reserveFilesArgs{}, "TTLM", fmt.Sprintf("default %d, maximum %d",
			int(sessioncmd.DefaultReservationTTL.Minutes()), int(sessioncmd.MaxReservationTTL.Minutes()))},
		{waitSessionArgs{}, "TimeoutS", fmt.Sprintf("default %d, maximum %d",
			int(sessioncmd.DefaultWaitTimeout.Seconds()), int(sessioncmd.MaxWaitTimeout.Seconds()))},
	}
	for _, bound := range bounds {
		field, ok := reflect.TypeOf(bound.args).FieldByName(bound.field)
		if !ok {
			t.Fatalf("%T has no field %s", bound.args, bound.field)
		}
		if schema := field.Tag.Get("jsonschema"); !strings.Contains(schema, bound.want) {
			t.Errorf("%T.%s describes itself as %q, which does not state %q", bound.args, bound.field, schema, bound.want)
		}
	}
}

func TestServerTeachesProactiveTerminalWorkflow(t *testing.T) {
	session := connect(t, t.TempDir(), "abc123")
	instructions := session.InitializeResult().Instructions
	for _, want := range []string{
		"without waiting to be asked",
		"SSH",
		"one-shot",
		"list_terminals",
		"create_terminal",
		"send_terminal",
		"read_terminal",
		"close_terminal",
		"reuse a running terminal",
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
		"list_terminals":  {"human-visible", "Reuse", "create_terminal"},
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
	session := connectServer(t, newServer(t.TempDir(), "abc123", "test", fake, &fakeSessionCommands{}))

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
	session := connectServer(t, newServer(t.TempDir(), "abc123", "test", fake, &fakeSessionCommands{}))
	text, isError := callText(t, session, "close_terminal", map[string]any{"terminal_id": "a1b2c3d4"})
	if isError || !strings.Contains(text, "closed terminal a1b2c3d4") {
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
	session := connectServer(t, newServer(t.TempDir(), "abc123", "test", fake, &fakeSessionCommands{}))

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
	session := connectServer(t, newServer(t.TempDir(), "abc123", "test", fake, &fakeSessionCommands{}))
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
	text, isError := callText(t, session, "review", map[string]any{"repo": repo})
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

	text, isError := callText(t, session, "review", map[string]any{"base": "main", "repo": repo})
	if isError || !strings.Contains(text, "main") {
		t.Fatalf("review_base = %q, isError=%v", text, isError)
	}
	mailbox := hooks.NewManager(configDir).ReviewBaseFile("abc123")
	content, err := os.ReadFile(mailbox)
	if err != nil || !strings.HasSuffix(string(content), "\nmain\n") {
		t.Fatalf("mailbox = %q, %v", content, err)
	}

	text, isError = callText(t, session, "review", map[string]any{"base": "auto", "repo": repo})
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
	if text, isError := callText(t, session, "review", map[string]any{"repo": t.TempDir()}); !isError {
		t.Fatalf("non-repo path should error, got %q", text)
	}
	if text, isError := callText(t, session, "review", map[string]any{"base": "nope-branch", "repo": gitRepo(t)}); !isError {
		t.Fatalf("unknown ref should error, got %q", text)
	}
	if text, isError := callText(t, session, "review", map[string]any{"mode": "bogus"}); !isError {
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
		text, isError := callText(t, session, "review", map[string]any{"mode": scope})
		if isError {
			t.Fatalf("review mode %q error: %q", scope, text)
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

func TestListsFleetTools(t *testing.T) {
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
		"list_sessions", "create_session", "read_session", "send_session",
		"revive_session", "kill_session", "archive_session",
		"list_groups", "create_group", "message_status", "wait_for_session",
		"task",
		"reserve_files", "release_files", "list_reservations",
	} {
		if !names[want] {
			t.Fatalf("missing tool %q in %v", want, names)
		}
	}
}

// A model already has a way to run work in parallel, and reads a list of
// sessions as that list unless the block says otherwise. What the tools
// reach is the user's machine, so the block names it: other CLIs, running
// whatever the user picked, outliving this conversation.
func TestServerTeachesThatSessionsAreOtherCLIsNotSubagents(t *testing.T) {
	session := connect(t, t.TempDir(), "abc123")
	instructions := session.InitializeResult().Instructions
	for _, want := range []string{
		"separate CLI processes",
		"never subagents of this conversation",
		"Codex",
	} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("server instructions do not teach %q:\n%s", want, instructions)
		}
	}
}

func TestServerTeachesDelegationWorkflow(t *testing.T) {
	session := connect(t, t.TempDir(), "abc123")
	instructions := session.InitializeResult().Instructions
	for _, want := range []string{
		"list_sessions",
		"create_session",
		"read_session",
		"send_session",
		"wait_for_session",
		"the task tool",
		"reserve_files",
		"worktree",
		"without waiting to be asked",
	} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("server instructions do not teach %q:\n%s", want, instructions)
		}
	}
}

// The instruction block is the whole discovery mechanism: with it emptied,
// a model offered these same tools reaches for its own subagents instead.
// Claude Code truncates it at 2048 characters, and a block that overruns
// loses its tail there silently, so the length is part of the contract.
func TestServerInstructionsSurviveTheClientLimit(t *testing.T) {
	const claudeCodeLimit = 2048
	session := connect(t, t.TempDir(), "abc123")
	instructions := session.InitializeResult().Instructions
	if len(instructions) >= claudeCodeLimit {
		t.Fatalf("server instructions are %d characters; Claude Code truncates at %d, dropping the tail", len(instructions), claudeCodeLimit)
	}
	// The safety paragraph is the tail, and the one thing no tool
	// description repeats.
	if !strings.Contains(instructions, "acts on the user's machine") {
		t.Fatalf("the instructions no longer say these tools act on the user's machine:\n%s", instructions)
	}
}

func TestSessionDescriptionsTeachWhenAndHowToChainTools(t *testing.T) {
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
		"list_sessions":    {"Call first", "create_session"},
		"create_session":   {"without waiting for the user", "worktree", "cannot see this conversation", "read_session"},
		"read_session":     {"after create_session", "current screen"},
		"send_session":     {"self-contained instruction", "read_session", "at rest", "another agent rather than from the user"},
		"message_status":   {"delivered", "queued"},
		"wait_for_session": {"instead of calling read_session in a loop", "timeout is a normal answer", "reached false"},
		"revive_session":   {"dead session"},
		"kill_session":     {"revive_session", "ask first"},
		"archive_session":  {"archived false"},
		"create_group":     {"list_groups", "parent"},
	} {
		for _, want := range wants {
			if !strings.Contains(descriptions[tool], want) {
				t.Errorf("%s description does not contain %q: %s", tool, want, descriptions[tool])
			}
		}
	}
}

func TestSessionToolsExposeStructuredResultsAndForwardArguments(t *testing.T) {
	group := "backend"
	worktree := true
	fake := &fakeSessionCommands{
		listed: []sessioncmd.Session{{
			ID: "a1b2c3d4", Name: "payments-retry", Tool: "claude", Group: group,
			Directory: "/work", Status: "working", Running: true, Self: true,
		}},
		created: sessioncmd.Session{
			ID: "e5f6a7b8", Name: "payments-retry-fix", Tool: "codex", Group: group,
			Directory: "/work/tree", Status: "starting", Running: true, Branch: "am/payments-retry-fix",
		},
		screen: sessioncmd.SessionScreen{
			Session: sessioncmd.Session{ID: "a1b2c3d4", Name: "payments-retry", Running: true},
			Output:  "tests passing",
		},
		groups: []sessioncmd.Group{{Path: group, Directory: "/work", Sessions: 2}},
	}
	session := connectServer(t, newServer(t.TempDir(), "abc123", "test", &fakeTerminalCommands{}, fake))

	listed := callTool(t, session, "list_sessions", map[string]any{})
	if listed.IsError || listed.StructuredContent == nil {
		t.Fatalf("list_sessions = %+v", listed)
	}
	if text, _ := callText(t, session, "list_sessions", map[string]any{}); !strings.Contains(text, "payments-retry") || !strings.Contains(text, "this session") {
		t.Fatalf("list text = %q", text)
	}

	created := callTool(t, session, "create_session", map[string]any{
		"name": "payments-retry-fix", "prompt": "fix the retry backoff",
		"tool": "codex", "group": group, "directory": "/work", "worktree": worktree,
	})
	if created.IsError || created.StructuredContent == nil {
		t.Fatalf("create_session = %+v", created)
	}
	opts := fake.createdOpts
	if opts.Name != "payments-retry-fix" || opts.Prompt != "fix the retry backoff" || opts.Tool != "codex" {
		t.Fatalf("create args = %+v", opts)
	}
	if opts.Group == nil || *opts.Group != group || opts.Directory != "/work" {
		t.Fatalf("create target = %+v", opts)
	}
	if opts.Worktree == nil || !*opts.Worktree {
		t.Fatalf("worktree flag = %v", opts.Worktree)
	}

	if text, isError := callText(t, session, "send_session", map[string]any{
		"session_id": "a1b2c3d4", "message": "rebase on main",
	}); isError || !strings.Contains(text, "queued message 7") {
		t.Fatalf("send_session = %q, isError=%v", text, isError)
	}
	if fake.sentID != "a1b2c3d4" || fake.sentMessage != "rebase on main" {
		t.Fatalf("send args = id %q message %q", fake.sentID, fake.sentMessage)
	}

	if text, isError := callText(t, session, "message_status", map[string]any{"message_id": 7}); isError || !strings.Contains(text, "delivered") {
		t.Fatalf("message_status = %q, isError=%v", text, isError)
	}
	if fake.statusID != 7 {
		t.Fatalf("message_status reached the layer with id %d", fake.statusID)
	}

	if text, isError := callText(t, session, "read_session", map[string]any{"session_id": "a1b2c3d4"}); isError || text != "tests passing" {
		t.Fatalf("read_session = %q, isError=%v", text, isError)
	}

	if text, isError := callText(t, session, "wait_for_session", map[string]any{
		"session_id": "a1b2c3d4", "until": []string{"finished"}, "timeout_s": 30,
	}); isError || !strings.Contains(text, "finished") {
		t.Fatalf("wait_for_session = %q, isError=%v", text, isError)
	}
	if fake.waitedID != "a1b2c3d4" || strings.Join(fake.waitedUntil, ",") != "finished" || fake.waitedTimeout != 30*time.Second {
		t.Fatalf("wait args = id %q until %v timeout %v", fake.waitedID, fake.waitedUntil, fake.waitedTimeout)
	}

	if _, isError := callText(t, session, "revive_session", map[string]any{"session_id": "a1b2c3d4"}); isError {
		t.Fatal("revive_session returned an error")
	}
	if fake.revivedID != "a1b2c3d4" {
		t.Fatalf("revive id = %q", fake.revivedID)
	}

	if _, isError := callText(t, session, "kill_session", map[string]any{"session_id": "a1b2c3d4"}); isError {
		t.Fatal("kill_session returned an error")
	}
	if fake.killedID != "a1b2c3d4" {
		t.Fatalf("kill id = %q", fake.killedID)
	}

	if text, _ := callText(t, session, "archive_session", map[string]any{"session_id": "a1b2c3d4"}); !strings.Contains(text, "archived") {
		t.Fatalf("archive text = %q", text)
	}
	if !fake.archived {
		t.Fatal("archive_session should default to archiving")
	}
	if text, _ := callText(t, session, "archive_session", map[string]any{"session_id": "a1b2c3d4", "archived": false}); !strings.Contains(text, "restored") {
		t.Fatalf("restore text = %q", text)
	}
	if fake.archived {
		t.Fatal("archived false should restore")
	}

	if text, _ := callText(t, session, "list_groups", map[string]any{}); !strings.Contains(text, "backend") {
		t.Fatalf("list_groups text = %q", text)
	}
	if _, isError := callText(t, session, "create_group", map[string]any{"path": "work/payments", "directory": "/work"}); isError {
		t.Fatal("create_group returned an error")
	}
	if fake.groupPath != "work/payments" || fake.groupDir != "/work" {
		t.Fatalf("create_group args = %q %q", fake.groupPath, fake.groupDir)
	}
}

func TestSessionToolAnnotationsDescribeLocalRisk(t *testing.T) {
	session := connect(t, t.TempDir(), "abc123")
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
	}
	// A rename is the change this test exists to catch, and reading the
	// annotations off a tool that is no longer there panics the package.
	for _, name := range []string{"list_sessions", "read_session", "list_groups", "kill_session", "create_session", "send_session"} {
		if tools[name] == nil {
			t.Fatalf("%s is not registered", name)
		}
	}
	for _, name := range []string{"list_sessions", "read_session", "list_groups"} {
		if annotations := tools[name].Annotations; annotations == nil || !annotations.ReadOnlyHint {
			t.Fatalf("%s annotations = %+v", name, annotations)
		}
		if tools[name].OutputSchema == nil {
			t.Fatalf("%s has no structured output schema", name)
		}
	}
	if annotations := tools["kill_session"].Annotations; annotations == nil || annotations.DestructiveHint == nil || !*annotations.DestructiveHint {
		t.Fatalf("kill annotations = %+v", annotations)
	}
	for _, name := range []string{"create_session", "send_session"} {
		annotations := tools[name].Annotations
		if annotations == nil || annotations.ReadOnlyHint || annotations.OpenWorldHint == nil || !*annotations.OpenWorldHint {
			t.Fatalf("%s annotations = %+v", name, annotations)
		}
	}
}

func TestSessionToolErrorsAreToolErrors(t *testing.T) {
	fake := &fakeSessionCommands{err: errors.New("session is not running")}
	session := connectServer(t, serverWithFakes(t, fake))
	for _, call := range []struct {
		name string
		args map[string]any
	}{
		{"list_sessions", map[string]any{}},
		{"create_session", map[string]any{"name": "x"}},
		{"read_session", map[string]any{"session_id": "a1"}},
		{"send_session", map[string]any{"session_id": "a1", "message": "hi"}},
		{"message_status", map[string]any{"message_id": 7}},
		{"wait_for_session", map[string]any{"session_id": "a1"}},
		{"task", map[string]any{"action": "list"}},
		{"task", map[string]any{"action": "create", "title": "x"}},
		{"task", map[string]any{"action": "claim"}},
		{"task", map[string]any{"action": "finish", "task_id": "t1"}},
		{"task", map[string]any{"action": "release", "task_id": "t1"}},
		{"task", map[string]any{"action": "delete", "task_id": "t1"}},
		{"reserve_files", map[string]any{"paths": []string{"a.go"}}},
		{"release_files", map[string]any{}},
		{"list_reservations", map[string]any{}},
		{"revive_session", map[string]any{"session_id": "a1"}},
		{"kill_session", map[string]any{"session_id": "a1"}},
		{"archive_session", map[string]any{"session_id": "a1"}},
		{"list_groups", map[string]any{}},
		{"create_group", map[string]any{"path": "work"}},
	} {
		text, isError := callText(t, session, call.name, call.args)
		if !isError || !strings.Contains(text, "not running") {
			t.Fatalf("%s = %q, isError=%v", call.name, text, isError)
		}
	}
}

func serverWithFakes(t *testing.T, sessions sessionCommands) *mcp.Server {
	t.Helper()
	return newServer(t.TempDir(), "abc123", "test", &fakeTerminalCommands{}, sessions)
}

func TestTaskToolsForwardArgumentsAndRenderTheList(t *testing.T) {
	fake := &fakeSessionCommands{
		tasks: []sessioncmd.Task{
			{ID: "t1", Title: "add the column", State: "done"},
			{ID: "t2", Title: "backfill it", State: "pending", DependsOn: []string{"t1"}, BlockedBy: []string{"t1"}, Blocked: true},
			{ID: "t3", Title: "verify", State: "in_progress", OwnerName: "worker", Mine: true},
		},
	}
	session := connectServer(t, serverWithFakes(t, fake))

	text, isError := callText(t, session, "task", map[string]any{"action": "list"})
	if isError {
		t.Fatalf("task list errored: %q", text)
	}
	for _, want := range []string{"add the column", "blocked on t1", "held by worker", "yours"} {
		if !strings.Contains(text, want) {
			t.Fatalf("task list text missing %q: %s", want, text)
		}
	}

	if _, isError := callText(t, session, "task", map[string]any{
		"action": "create", "title": "fix retries", "body": "see internal/retry", "depends_on": []string{"t1"},
	}); isError {
		t.Fatal("task create errored")
	}
	if fake.taskTitle != "fix retries" || fake.taskBody != "see internal/retry" || strings.Join(fake.taskDeps, ",") != "t1" {
		t.Fatalf("task create args = %q %q %v", fake.taskTitle, fake.taskBody, fake.taskDeps)
	}

	if _, isError := callText(t, session, "task", map[string]any{"action": "claim"}); isError {
		t.Fatal("claim with no id errored")
	}
	if fake.claimedTaskID != "" {
		t.Fatalf("claim-next should forward an empty id, got %q", fake.claimedTaskID)
	}
	if _, isError := callText(t, session, "task", map[string]any{"action": "claim", "task_id": "t2"}); isError {
		t.Fatal("claim errored")
	}
	if fake.claimedTaskID != "t2" {
		t.Fatalf("claim id = %q", fake.claimedTaskID)
	}
	for _, action := range []string{"finish", "release", "delete"} {
		fake.settledTaskID = ""
		if _, isError := callText(t, session, "task", map[string]any{"action": action, "task_id": "t3"}); isError {
			t.Fatalf("%s errored", action)
		}
		if fake.settledTaskID != "t3" {
			t.Fatalf("%s id = %q", action, fake.settledTaskID)
		}
	}

	if text, isError := callText(t, session, "task", map[string]any{"action": "bogus"}); !isError ||
		!strings.Contains(text, "unknown action") {
		t.Fatalf("an unknown action answered %q, isError=%v", text, isError)
	}
}

func TestReservationToolsReportConflictsWithoutRefusingTheLease(t *testing.T) {
	fake := &fakeSessionCommands{
		reservations: []sessioncmd.Reservation{
			{Pattern: "internal/ui/*.go", Mode: "exclusive", Holder: "worker", ExpiresIn: "20m0s", Note: "focus mode"},
		},
	}
	session := connectServer(t, serverWithFakes(t, fake))

	text, isError := callText(t, session, "reserve_files", map[string]any{
		"paths": []string{"internal/store/*.go"}, "mode": "exclusive", "note": "inbox table", "ttl_minutes": 45,
	})
	if isError {
		t.Fatalf("reserve_files errored: %q", text)
	}
	for _, want := range []string{"reserved internal/store/*.go", "conflicts with leases already held", "held by worker", "adding a table"} {
		if !strings.Contains(text, want) {
			t.Fatalf("reserve text missing %q: %s", want, text)
		}
	}
	if strings.Join(fake.reservedPaths, ",") != "internal/store/*.go" || fake.reservedMode != "exclusive" || fake.reservedTTL != 45*time.Minute {
		t.Fatalf("reserve args = %v %q %v", fake.reservedPaths, fake.reservedMode, fake.reservedTTL)
	}

	if text, _ := callText(t, session, "list_reservations", map[string]any{}); !strings.Contains(text, "focus mode") {
		t.Fatalf("list_reservations text = %q", text)
	}
	released := callTool(t, session, "release_files", map[string]any{"paths": []string{"internal/store/*.go", "internal/ui/*.go"}})
	if released.IsError {
		t.Fatalf("release_files = %+v", released)
	}
	// The count is what tells a caller how much of what it asked for it
	// actually held, and the shell front already hands it back as a number.
	if structured, ok := released.StructuredContent.(map[string]any); !ok || structured["released"] != float64(2) {
		t.Fatalf("release_files structured = %#v", released.StructuredContent)
	}
	if strings.Join(fake.releasedPaths, ",") != "internal/store/*.go,internal/ui/*.go" {
		t.Fatalf("release args = %v", fake.releasedPaths)
	}
}
