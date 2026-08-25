package sessioncmd

import (
	"strings"
	"testing"
)

func TestFormatSessionCarriesTheCapturedConversationID(t *testing.T) {
	withID := Session{Name: "worker", ID: "abcd1234", Tool: "claude", Directory: "/repo", AgentSessionID: "conv-42"}
	if got := FormatSession(withID); !strings.Contains(got, "(conversation conv-42)") {
		t.Fatalf("a captured id should name its conversation, got %q", got)
	}
	withoutID := withID
	withoutID.AgentSessionID = ""
	if got := FormatSession(withoutID); strings.Contains(got, "(conversation") {
		t.Fatalf("a session with no captured id must not name a conversation, got %q", got)
	}
}
