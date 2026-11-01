package mcpserver

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMoveSessionForwardsTheTargetAndGroup(t *testing.T) {
	fake := &fakeSessionCommands{}
	session := connectServer(t, serverWithFakes(t, fake))

	result := callTool(t, session, "move_session", map[string]any{"session_id": "a1b2c3d4", "group": "repos/api"})
	if result.IsError {
		t.Fatalf("move_session errored: %+v", result.Content)
	}
	if fake.movedID != "a1b2c3d4" || fake.groupPath != "repos/api" {
		t.Fatalf("move_session reached the layer with %q into %q", fake.movedID, fake.groupPath)
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok || structured["group"] != "repos/api" || structured["from"] != "backend" {
		t.Fatalf("structured = %#v", result.StructuredContent)
	}
	if text := result.Content[0].(*mcp.TextContent).Text; text != "moved session worker (id a1b2c3d4) from backend to repos/api; 1 terminal(s) went with it: t1" {
		t.Fatalf("text = %q", text)
	}
}

// The root is a destination of its own, so an agent that forgot the group
// is told so rather than unfiling the row.
func TestMoveSessionRequiresAGroup(t *testing.T) {
	fake := &fakeSessionCommands{}
	session := connectServer(t, serverWithFakes(t, fake))

	text, isError := callText(t, session, "move_session", map[string]any{"session_id": "a1b2c3d4"})
	if !isError || !strings.Contains(text, "group") {
		t.Fatalf("move_session without group = %q, isError=%v", text, isError)
	}
	if fake.movedID != "" {
		t.Fatalf("the layer was reached with %q", fake.movedID)
	}
	if _, isError := callText(t, session, "move_session", map[string]any{"session_id": "a1b2c3d4", "group": ""}); isError || fake.groupPath != "" || fake.movedID != "a1b2c3d4" {
		t.Fatalf("an empty group did not reach the layer as the root: %q %q", fake.movedID, fake.groupPath)
	}
}
