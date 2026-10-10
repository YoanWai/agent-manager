package sessioncmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

func geminiRecord(pid int, conversation string) string {
	return fmt.Sprintf(`{
  "hrTime": [1791409522, 876000000],
  "resource": {"_rawAttributes": [["process.pid", %d]]},
  "attributes": {"session.id": %q, "event.name": "gemini_cli.user_prompt"}
}
`, pid, conversation)
}

// The follower records what gemini last logged and empties the file while
// the agent runs, and reads once more after it quits, so a switch logged
// just before the agent ended still moves the row.
func TestFollowTelemetryRecordsAndEmptiesUntilTheAgentQuits(t *testing.T) {
	_, st := conversationStore(t)
	if err := st.CreateSession(store.Session{ID: "gem12345", Name: "gem12345", Tool: "gemini", Cwd: t.TempDir(), Status: status.Idle}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "gem12345.otel")
	if err := os.WriteFile(file, []byte(geminiRecord(os.Getpid(), "started-one")), 0o644); err != nil {
		t.Fatal(err)
	}
	polls := 0
	alive := func(int) bool {
		polls++
		switch polls {
		case 1:
			return true
		case 2:
			if got := storedConversation(t, st, "gem12345"); got != "started-one" {
				t.Errorf("after the first poll the row is on %q", got)
			}
			if info, err := os.Stat(file); err != nil || info.Size() != 0 {
				t.Errorf("telemetry after the first poll = %v %v, want empty", info, err)
			}
			if err := os.WriteFile(file, []byte(geminiRecord(os.Getpid(), "cleared-two")), 0o644); err != nil {
				t.Error(err)
			}
		}
		return false
	}
	if err := followTelemetry(st, "gem12345", file, 0, os.Getpid(), time.Millisecond, alive); err != nil {
		t.Fatal(err)
	}
	if polls != 2 {
		t.Fatalf("polled %d times, want one while running and a last one after", polls)
	}
	if got := storedConversation(t, st, "gem12345"); got != "cleared-two" {
		t.Fatalf("after the agent quit the row is on %q, want the last logged conversation", got)
	}
}

func TestAnOlderTelemetryFollowerLeavesTheRelaunchFileAlone(t *testing.T) {
	_, st := conversationStore(t)
	const row = "gem12345"
	if err := st.CreateSession(store.Session{ID: row, Name: "gemini", Tool: "gemini", Cwd: t.TempDir(), Status: status.Idle}); err != nil {
		t.Fatal(err)
	}
	launchedAt := time.Now()
	if err := st.SetAgentLaunchedAt(row, launchedAt); err != nil {
		t.Fatal(err)
	}
	manager := hooks.NewManager(t.TempDir())
	if err := os.MkdirAll(manager.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	oldFile := manager.TelemetryFile(row, 0)
	newFile := manager.TelemetryFile(row, store.LaunchStamp(launchedAt))
	newRecord := geminiRecord(os.Getpid(), "new-conversation")
	if err := os.WriteFile(oldFile, []byte(geminiRecord(os.Getpid(), "old-conversation")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte(newRecord), 0o644); err != nil {
		t.Fatal(err)
	}
	ended := func(int) bool { return false }
	if err := followTelemetry(st, row, oldFile, 0, os.Getpid(), time.Millisecond, ended); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(newFile); err != nil || string(data) != newRecord {
		t.Fatalf("old follower consumed the relaunch's telemetry: %q, %v", data, err)
	}
	if got := storedConversation(t, st, row); got != "" {
		t.Fatalf("old follower moved the relaunched row to %q", got)
	}
	if err := followTelemetry(st, row, newFile, store.LaunchStamp(launchedAt), os.Getpid(), time.Millisecond, ended); err != nil {
		t.Fatal(err)
	}
	if got := storedConversation(t, st, row); got != "new-conversation" {
		t.Fatalf("new follower reported %q", got)
	}
}

// Gemini's relaunched child logs on after the agent's own pid is killed
// alone, so the follower stays while anything is left in the group.
func TestGroupAliveWhileAnyProcessIsLeftInTheGroup(t *testing.T) {
	leader := exec.Command("sleep", "60")
	leader.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := leader.Start(); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "60")
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: leader.Process.Pid}
	if err := child.Start(); err != nil {
		leader.Process.Kill()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		leader.Process.Kill()
		child.Process.Kill()
		leader.Wait()
		child.Wait()
	})
	group := leader.Process.Pid
	if !groupAlive(group) {
		t.Fatal("a group with its leader and a child reads as gone")
	}
	leader.Process.Kill()
	leader.Wait()
	if !groupAlive(group) {
		t.Fatal("a group whose leader was killed alone reads as gone while its child runs")
	}
	child.Process.Kill()
	child.Wait()
	if groupAlive(group) {
		t.Fatal("an empty group reads as alive")
	}
}
