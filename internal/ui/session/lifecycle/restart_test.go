package lifecycle

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/google/uuid"
)

// Restart has to hold for every CLI the manager ships with, not just the one
// the fake tools stand in for: it launches each tool the way a brand new
// session does, and never reaches for a resume, continue or fork command.
func TestRestartLaunchIsAFreshStartForEveryShippedTool(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CLIs) < 6 {
		t.Fatalf("built-in tools = %d, expected every shipped CLI", len(cfg.CLIs))
	}
	for name, tool := range cfg.CLIs {
		command, agentSessionID := RestartLaunch(tool)
		if !strings.HasPrefix(command, tool.Command) {
			t.Errorf("%s: restart command %q does not start from its launch command %q", name, command, tool.Command)
		}
		rest := strings.TrimPrefix(command, tool.Command)
		for _, resume := range []string{"resume", "continue", "fork", "--session ", "--last"} {
			if strings.Contains(rest, resume) {
				t.Errorf("%s: restart command %q carries %q", name, command, resume)
			}
		}
		if tool.SessionIDFlag == "" {
			// Some tools mint their own id; the poller captures it.
			if agentSessionID != "" || rest != "" {
				t.Errorf("%s: restart handed an id to a tool that mints its own: %q", name, command)
			}
			if tool.SessionStore == "" {
				t.Errorf("%s: no session_id_flag and no session_store leaves restart with no conversation id at all", name)
			}
			continue
		}
		if _, err := uuid.Parse(agentSessionID); err != nil {
			t.Errorf("%s: restart conversation id %q is not a uuid: %v", name, agentSessionID, err)
		}
		if want := " " + tool.SessionIDFlag + " " + agentSessionID; rest != want {
			t.Errorf("%s: restart command tail = %q, want %q", name, rest, want)
		}
		if _, second := RestartLaunch(tool); second == agentSessionID {
			t.Errorf("%s: two restarts reused conversation id %q", name, agentSessionID)
		}
	}
}
