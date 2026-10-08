package mcpserver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
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
