package mcpserver

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

func TestMessageLogToolsForwardTheirArguments(t *testing.T) {
	fake := &fakeSessionCommands{}
	session := connectServer(t, newServer(t.TempDir(), "abc123", "test", true, &fakeTerminalCommands{}, fake, &fakeReporter{}))

	text, isError := callText(t, session, "list_messages", map[string]any{"session_id": "e5f6a7b8", "queued": true, "limit": 5})
	if isError || !strings.Contains(text, `- 7 "lead" (a1b2c3d4) -> "worker" (e5f6a7b8)`) || !strings.Contains(text, "held at position 1: rebase\n    sitting on a dialog") {
		t.Fatalf("list_messages = %q, isError=%v", text, isError)
	}
	if fake.listedOpts != (sessioncmd.MessagesOptions{Session: "e5f6a7b8", QueuedOnly: true, Limit: 5}) {
		t.Fatalf("list_messages reached the layer with %+v", fake.listedOpts)
	}
	listed := callTool(t, session, "list_messages", map[string]any{})
	if listed.IsError || listed.StructuredContent == nil {
		t.Fatalf("list_messages = %+v", listed)
	}
	if fake.listedOpts != (sessioncmd.MessagesOptions{Limit: sessioncmd.DefaultMessagesLimit}) {
		t.Fatalf("an omitted limit reached the layer as %+v", fake.listedOpts)
	}

	text, isError = callText(t, session, "read_message", map[string]any{"message_id": 9})
	if isError || !strings.HasPrefix(text, `message 9 from "lead" (a1b2c3d4) to "worker" (e5f6a7b8)`) || !strings.HasSuffix(text, "\n\nrebase on main") {
		t.Fatalf("read_message = %q, isError=%v", text, isError)
	}
	if fake.readMessageID != 9 {
		t.Fatalf("read_message reached the layer with id %d", fake.readMessageID)
	}

	if _, isError := callText(t, session, "send_session", map[string]any{"session_id": "a1b2c3d4", "message": "rebase on main", "subject": "rebase"}); isError || fake.sentSubject != "rebase" {
		t.Fatalf("send_session subject reached the layer as %q, isError=%v", fake.sentSubject, isError)
	}
}

// The log is this host's store; a host::id names a session another one keeps.
func TestListMessagesRefusesARemoteSession(t *testing.T) {
	fake := &fakeSessionCommands{}
	session := connectServer(t, newServer(t.TempDir(), "abc123", "test", true, &fakeTerminalCommands{}, fake, &fakeReporter{}))
	text, isError := callText(t, session, "list_messages", map[string]any{"session_id": "gpu::r1"})
	if !isError || text != "list_messages works on this host's sessions only" {
		t.Fatalf("list_messages on a remote ref = %q, isError=%v", text, isError)
	}
	if fake.listedOpts != (sessioncmd.MessagesOptions{}) {
		t.Fatalf("a remote ref reached the local layer: %+v", fake.listedOpts)
	}
}
