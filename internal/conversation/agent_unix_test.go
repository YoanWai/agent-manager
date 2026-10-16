//go:build !windows

package conversation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/YoanWai/agent-manager/internal/tmux"
)

// The record names the process the launch line execs into, which is the
// agent.
func TestRecordCommandNamesTheProcessItExecsInto(t *testing.T) {
	path := filepath.Join(t.TempDir(), "row.agent")
	pidFile := filepath.Join(t.TempDir(), "pid")
	line := RecordCommand(path, 1700000000123) + "; exec sh -c 'echo $$ > " + pidFile + "'"
	if out, err := exec.Command("sh", "-c", line).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", line, err, out)
	}
	agent, found, err := ReadAgent(path)
	if err != nil || !found {
		t.Fatalf("ReadAgent = %+v %v %v", agent, found, err)
	}
	pid, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	if strconv.Itoa(agent.PID)+"\n" != string(pid) || agent.Launch != 1700000000123 || agent.TTY == "" {
		t.Fatalf("record = %+v, want pid %s and launch 1700000000123", agent, pid)
	}
}

// The shell that ran the launch line marks the record once the agent has
// quit.
func TestReadAgentSeesTheEndedMark(t *testing.T) {
	path := filepath.Join(t.TempDir(), "row.agent")
	line := RecordCommand(path, 7) + "; exec true"
	if out, err := exec.Command("sh", "-c", "sh -c "+tmux.ShellQuote(line)+"; "+EndedCommand(path)).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	agent, found, err := ReadAgent(path)
	if err != nil || !found || !agent.Ended || agent.Launch != 7 {
		t.Fatalf("ReadAgent = %+v %v %v, want launch 7 ended", agent, found, err)
	}
	if running, err := agent.Running(); err != nil || running {
		t.Fatalf("an ended agent runs = %v, %v", running, err)
	}
}
