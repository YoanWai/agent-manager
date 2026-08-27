package sessioncmd

import (
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
)

// waitForAgentGone waits for the pane to hold nothing but its shell, and
// for that to still be true on a second reading: the launch script's own
// exit leaves a process visible for a moment after the agent is done.
func waitForAgentGone(t *testing.T, driver *tmux.Driver, sessID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	quiet := 0
	for time.Now().Before(deadline) {
		running, err := AgentRunning(driver, sessID)
		if err == nil && !running {
			quiet++
			if quiet == 2 {
				return
			}
			time.Sleep(25 * time.Millisecond)
			continue
		}
		quiet = 0
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("session %s never came back to its shell", sessID)
}
