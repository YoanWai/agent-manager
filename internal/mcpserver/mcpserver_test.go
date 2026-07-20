package mcpserver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func connect(t *testing.T, configDir, sessionID string) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := NewServer(configDir, sessionID, "test")
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

func callText(t *testing.T, session *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", tool, err)
	}
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
	for _, want := range []string{"rename", "review_repo", "review_base"} {
		if !names[want] {
			t.Fatalf("missing tool %q in %v", want, names)
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

	noSession := connect(t, configDir, "")
	if text, isError := callText(t, noSession, "rename", map[string]any{"name": "x"}); !isError || !strings.Contains(text, "AGENT_MANAGER_SESSION_ID") {
		t.Fatalf("missing session id should error, got %q", text)
	}
}
