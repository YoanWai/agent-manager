package mcpserver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLegacyProtocolClientsKeepTextResultsAndMutationRefusals(t *testing.T) {
	for _, version := range []string{"2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25"} {
		t.Run(version, func(t *testing.T) {
			ctx := context.Background()
			commands := &fakeSessionCommands{listed: []sessioncmd.Session{{ID: "beef", Name: "worker", Tool: "codex", Status: "finished"}}}
			server := newServer(t.TempDir(), "cafe", "test", true, &fakeTerminalCommands{}, commands, &fakeReporter{})
			serverTransport, clientTransport := mcp.NewInMemoryTransports()
			serverSession, err := server.Connect(ctx, serverTransport, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { serverSession.Close() })
			client := mcp.NewClient(&mcp.Implementation{Name: "legacy-contract-test", Version: "test"}, nil)
			session, err := client.Connect(ctx, clientTransport, &mcp.ClientSessionOptions{ProtocolVersion: version})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { session.Close() })
			if negotiated := session.InitializeResult().ProtocolVersion; negotiated != version {
				t.Fatalf("negotiated %s, want %s", negotiated, version)
			}
			listed := callTool(t, session, "list_sessions", map[string]any{})
			if listed.IsError || len(listed.Content) == 0 {
				t.Fatalf("list result = %+v", listed)
			}
			text, ok := listed.Content[0].(*mcp.TextContent)
			if !ok || !strings.Contains(text.Text, "worker") {
				t.Fatalf("legacy text = %+v", listed.Content)
			}
			malformed := callTool(t, session, "archive_session", map[string]any{"session_id": 123})
			if !malformed.IsError || commands.archivedID != "" {
				t.Fatalf("malformed call dispatched: %+v, target=%s", malformed, commands.archivedID)
			}
			commands.err = errors.New("bound backend unavailable")
			refused := callTool(t, session, "archive_session", map[string]any{"session_id": "beef"})
			if !refused.IsError || commands.archivedID != "beef" {
				t.Fatalf("refusal = %+v, target=%s", refused, commands.archivedID)
			}
			text, ok = refused.Content[0].(*mcp.TextContent)
			if !ok || !strings.Contains(text.Text, "bound backend unavailable") {
				t.Fatalf("refusal text = %+v", refused.Content)
			}
		})
	}
}

type recordingArchiveOwner struct {
	request sessioncmd.ArchiveRequest
}

func (o *recordingArchiveOwner) Archive(request sessioncmd.ArchiveRequest) (sessioncmd.Session, error) {
	o.request = request
	return sessioncmd.Session{ID: request.TargetID, Name: "worker", Archived: request.Archived}, nil
}

func TestArchiveOwnerServerKeepsTheExplicitDurableOwner(t *testing.T) {
	owner := &recordingArchiveOwner{}
	session := connectServer(t, NewServerWithArchiveOwner(t.TempDir(), "cafe", "test", false, owner))
	text, isError := callText(t, session, "archive_session", map[string]any{"session_id": "beef"})
	if isError || !strings.Contains(text, "archived") {
		t.Fatalf("archive = %q, isError=%v", text, isError)
	}
	if owner.request.CallerID != "cafe" || owner.request.TargetID != "beef" || !owner.request.Archived {
		t.Fatalf("owner request = %+v", owner.request)
	}
	if instructions := session.InitializeResult().Instructions; !strings.Contains(instructions, "when the user asks") {
		t.Fatalf("archive-owner server lost on-request mode:\n%s", instructions)
	}
}

func TestBackendServerKeepsTheExplicitCoordinationMode(t *testing.T) {
	configDir := t.TempDir()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	backend, err := sessioncmd.BorrowBackend(sessioncmd.Runtime{
		Store: st, Driver: new(tmux.Driver), Hooks: hooks.NewManager(configDir),
		Snapshot: func(string, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	session := connectServer(t, NewServerWithBackend(configDir, "cafe", "test", false, backend))
	if instructions := session.InitializeResult().Instructions; !strings.Contains(instructions, "when the user asks") {
		t.Fatalf("backend server lost on-request mode:\n%s", instructions)
	}
}

func TestServerRunnersReturnCoordinationModeReadErrors(t *testing.T) {
	configDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(configDir, "state.db"), 0o755); err != nil {
		t.Fatal(err)
	}
	owner := &recordingArchiveOwner{}
	plainErr := Run(configDir, "cafe", "test")
	ownerErr := RunWithArchiveOwner(configDir, "cafe", "test", owner)
	if plainErr == nil || ownerErr == nil {
		t.Fatalf("coordination errors: plain=%v archive-owner=%v", plainErr, ownerErr)
	}
	if plainErr.Error() != ownerErr.Error() {
		t.Fatalf("coordination errors differ: plain=%q archive-owner=%q", plainErr, ownerErr)
	}

	backendDir := t.TempDir()
	st, err := store.Open(filepath.Join(backendDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	backend, err := sessioncmd.BorrowBackend(sessioncmd.Runtime{
		Store: st, Driver: new(tmux.Driver), Hooks: hooks.NewManager(backendDir),
		Snapshot: func(string, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := RunWithBackend(backendDir, "cafe", "test", backend); err == nil {
		t.Fatal("backend runner swallowed a coordination mode read error")
	}
}
