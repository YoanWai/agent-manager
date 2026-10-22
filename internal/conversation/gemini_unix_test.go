//go:build !windows

package conversation

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// The newest log record the agent's own processes wrote names its
// conversation. A gemini started from shell mode, in a process group of its
// own, logs to the same file and is skipped, as are metrics.
func TestGeminiReadsTheAgentsNewestConversationAndEmptiesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "row.otel")
	shellMode := exec.Command("sleep", "60")
	shellMode.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := shellMode.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		shellMode.Process.Kill()
		shellMode.Wait()
	})
	appendFile(t, path, telemetryLog(os.Getpid(), "first")+telemetryLog(os.Getpid(), "cleared")+telemetryMetric+telemetryLog(shellMode.Process.Pid, "shell-mode"))
	var reader Reader
	id := ""
	if err := reader.FollowTelemetry(path, os.Getpid(), func(current string) (bool, error) {
		id = current
		return true, nil
	}); err != nil || id != "cleared" {
		t.Fatalf("FollowTelemetry = %q, %v; want cleared", id, err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Fatalf("telemetry after done = %v %v, want empty", info, err)
	}
	if err := reader.FollowTelemetry(path, os.Getpid(), func(string) (bool, error) {
		t.Error("empty telemetry reported a conversation")
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Windows refuses to remove a file another handle holds open, so a newer
// launch cannot replace the file under a reader there.
func TestTelemetryCleanupDoesNotTruncateANewerLaunchFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "row.otel")
	appendFile(t, path, telemetryLog(os.Getpid(), "old"))
	newLaunch := telemetryLog(os.Getpid(), "new")
	reader := &Reader{}
	err := reader.FollowTelemetry(path, os.Getpid(), func(string) (bool, error) {
		if err := os.Remove(path); err != nil {
			return false, err
		}
		return true, os.WriteFile(path, []byte(newLaunch), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != newLaunch {
		t.Fatalf("new launch telemetry = %q, %v", data, err)
	}
}
