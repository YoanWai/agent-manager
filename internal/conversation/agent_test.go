package conversation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

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

func TestReadAgentWithoutARecord(t *testing.T) {
	if _, found, err := ReadAgent(filepath.Join(t.TempDir(), "none.agent")); found || err != nil {
		t.Fatalf("ReadAgent = %v %v, want nothing", found, err)
	}
	empty := filepath.Join(t.TempDir(), "empty.agent")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, found, err := ReadAgent(empty); found || err != nil {
		t.Fatalf("a record being written = %v %v, want nothing yet", found, err)
	}
	broken := filepath.Join(t.TempDir(), "broken.agent")
	if err := os.WriteFile(broken, []byte("x 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadAgent(broken); err == nil {
		t.Fatal("a broken record read as an agent")
	}
}

func TestReadAgentRejectsPIDsOutsideTheProcessIDRange(t *testing.T) {
	for _, pid := range []string{"0", "-1", "2147483648", "4294967297"} {
		path := filepath.Join(t.TempDir(), "row.agent")
		if err := os.WriteFile(path, []byte(pid+" 1 /dev/ttys001\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, found, err := ReadAgent(path); err == nil || found {
			t.Errorf("pid %s accepted: %v, %v", pid, found, err)
		}
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

// Once the agent has quit, the system can hand its pid to a process started
// since, which is not the agent.
func TestRunningTellsTheAgentFromAProcessThatReusedItsPID(t *testing.T) {
	later := exec.Command("sleep", "30")
	if err := later.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		later.Process.Kill()
		later.Wait()
	})
	for _, tc := range []struct {
		recorded time.Time
		want     bool
	}{
		{time.Now(), true},
		{time.Now().Add(-time.Minute), false},
	} {
		agent := Agent{PID: later.Process.Pid, Recorded: tc.recorded}
		if running, err := agent.Running(); err != nil || running != tc.want {
			t.Errorf("recorded %s before now = %v, %v; want %v", time.Since(tc.recorded).Round(time.Second), running, err, tc.want)
		}
	}
}
