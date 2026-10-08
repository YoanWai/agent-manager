package mcpserver

import (
	"context"
	"strings"
	"testing"
)

// A CLI registers this server at user scope, so with no caller it is that
// CLI running outside Agent Manager, and no tool reaches the workspace:
// neither this machine's rows nor a connection's.
func TestServerWithNoCallerKeepsTheWorkspaceClosed(t *testing.T) {
	terminals, sessions := &fakeTerminalCommands{}, &fakeSessionCommands{}
	session := connectServer(t, newServer(t.TempDir(), "", "test", true, terminals, sessions, &fakeReporter{}))
	calls := []struct {
		name string
		args map[string]any
	}{
		{"rename", map[string]any{"name": "payments-fix"}},
		{"review", map[string]any{"mode": "staged"}},
		{"review_comment", map[string]any{"comment_id": "c1"}},
		{"list_sessions", map[string]any{}},
		{"create_session", map[string]any{"name": "x", "tool": "claude"}},
		{"create_session", map[string]any{"name": "x", "host": "box"}},
		{"read_session", map[string]any{"session_id": "a1"}},
		{"read_session", map[string]any{"session_id": "box::a1"}},
		{"send_session", map[string]any{"session_id": "a1", "message": "hi"}},
		{"send_session", map[string]any{"session_id": "box::a1", "message": "hi"}},
		{"message_status", map[string]any{"message_id": 7}},
		{"wait_for_session", map[string]any{"session_id": "a1"}},
		{"revive_session", map[string]any{"session_id": "a1"}},
		{"revive_session", map[string]any{"session_id": "box::a1"}},
		{"kill_session", map[string]any{"session_id": "a1"}},
		{"kill_session", map[string]any{"session_id": "box::a1"}},
		{"archive_session", map[string]any{"session_id": "a1"}},
		{"archive_session", map[string]any{"session_id": "box::a1"}},
		{"archive_self", map[string]any{}},
		{"kill_self", map[string]any{"cancel": true}},
		{"task", map[string]any{"action": "list"}},
		{"reserve_files", map[string]any{"paths": []string{"a.go"}}},
		{"release_files", map[string]any{}},
		{"list_reservations", map[string]any{}},
		{"list_groups", map[string]any{}},
		{"create_group", map[string]any{"path": "work"}},
		{"create_group", map[string]any{"path": "work", "host": "box"}},
		{"delete_group", map[string]any{"path": "work"}},
		{"delete_group", map[string]any{"path": "work", "host": "box"}},
		{"list_terminals", map[string]any{}},
		{"create_terminal", map[string]any{}},
		{"create_terminal", map[string]any{"host": "box"}},
		{"send_terminal", map[string]any{"terminal_id": "t1", "command": "echo hi"}},
		{"send_terminal", map[string]any{"terminal_id": "box::t1", "command": "echo hi"}},
		{"read_terminal", map[string]any{"terminal_id": "t1"}},
		{"read_terminal", map[string]any{"terminal_id": "box::t1"}},
		{"close_terminal", map[string]any{"terminal_id": "t1"}},
		{"close_terminal", map[string]any{"terminal_id": "box::t1"}},
		{"report_issue", map[string]any{"kind": "bug", "title": "t", "body": "b"}},
	}
	checked := map[string]bool{}
	for _, call := range calls {
		checked[call.name] = true
		text, isError := callText(t, session, call.name, call.args)
		if !isError || !strings.Contains(text, "not inside an Agent Manager session") {
			t.Errorf("%s %v with no caller = %q, isError=%v", call.name, call.args, text, isError)
		}
	}
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if !checked[tool.Name] {
			t.Errorf("%s is not checked with no caller", tool.Name)
		}
	}
	if terminals.sentID != "" || sessions.killedID != "" || sessions.revivedID != "" || sessions.archivedID != "" {
		t.Fatalf("a refused call still reached the workspace: sent %q, killed %q, revived %q, archived %q",
			terminals.sentID, sessions.killedID, sessions.revivedID, sessions.archivedID)
	}
}
