package conversation

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

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
