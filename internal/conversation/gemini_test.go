package conversation

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func telemetryLog(pid int, sessionID string) string {
	return telemetryEvent(pid, sessionID, "gemini_cli.user_prompt")
}

func telemetryEvent(pid int, sessionID, event string) string {
	return fmt.Sprintf(`{
  "hrTime": [
    1791409522,
    876000000
  ],
  "resource": {
    "_rawAttributes": [
      [
        "host.name",
        "here"
      ],
      [
        "process.pid",
        %d
      ],
      [
        "session.id",
        "launch-session"
      ]
    ]
  },
  "attributes": {
    "session.id": %q,
    "event.name": %q
  },
  "_body": "{\n  \"nested\": 1\n}"
}
`, pid, sessionID, event)
}

const telemetryMetric = `{
  "descriptor": {
    "name": "gemini_cli.session.count"
  },
  "dataPoints": [
    {
      "attributes": {
        "session.id": "older-session"
      }
    }
  ]
}
`

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
	id, done, err := reader.Current("gemini", Agent{PID: os.Getpid()}, path)
	if err != nil || id != "cleared" || done == nil {
		t.Fatalf("Current = %q, %v, %v; want cleared and a done", id, done != nil, err)
	}
	if err := done(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Fatalf("telemetry after done = %v %v, want empty", info, err)
	}
	if id, done, err := reader.Current("gemini", Agent{PID: os.Getpid()}, path); err != nil || id != "" || done != nil {
		t.Fatalf("empty telemetry = %q, %v, %v", id, done != nil, err)
	}
}

// A record gemini is still writing waits for a later poll, and what gemini
// wrote after the read is never emptied away.
func TestGeminiKeepsWhatItHasNotReadWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "row.otel")
	whole := telemetryLog(os.Getpid(), "first")
	partial := telemetryLog(os.Getpid(), "second")
	appendFile(t, path, whole+partial[:len(partial)/2])
	var reader Reader
	id, done, err := reader.Current("gemini", Agent{PID: os.Getpid()}, path)
	if err != nil || id != "first" || done != nil {
		t.Fatalf("Current = %q, %v, %v; want first and nothing to empty", id, done != nil, err)
	}
	appendFile(t, path, partial[len(partial)/2:])
	id, done, err = reader.Current("gemini", Agent{PID: os.Getpid()}, path)
	if err != nil || id != "second" || done == nil {
		t.Fatalf("Current = %q, %v, %v; want second", id, done != nil, err)
	}
	appendFile(t, path, telemetryMetric)
	if err := done(); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || len(data) != len(whole)+len(partial)+len(telemetryMetric) {
		t.Fatalf("a write after the read was emptied away: %d bytes, %v", len(data), err)
	}
}

// A record cut short by an earlier empty is skipped rather than stalling
// every later read.
func TestGeminiSkipsABrokenRecordBeforeWholeOnes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "row.otel")
	broken := telemetryLog(os.Getpid(), "lost")
	appendFile(t, path, broken[len(broken)/2:]+telemetryLog(os.Getpid(), "after"))
	var reader Reader
	id, done, err := reader.Current("gemini", Agent{PID: os.Getpid()}, path)
	if err != nil || id != "after" || done == nil {
		t.Fatalf("Current = %q, %v, %v; want after", id, done != nil, err)
	}
}

// A /clear logs the new conversation before it holds anything, and gemini
// deletes it on exit if nothing follows, so only a later record moves the
// row there.
func TestGeminiCountsAClearedConversationFromItsFirstRecordAfterTheSwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "row.otel")
	appendFile(t, path, telemetryLog(os.Getpid(), "before")+telemetryEvent(os.Getpid(), "cleared", "gemini_cli.slash_command"))
	var reader Reader
	if id, _, err := reader.Current("gemini", Agent{PID: os.Getpid()}, path); err != nil || id != "before" {
		t.Fatalf("after /clear alone = %q, %v; want the conversation from before", id, err)
	}
	appendFile(t, path, telemetryLog(os.Getpid(), "cleared"))
	if id, _, err := reader.Current("gemini", Agent{PID: os.Getpid()}, path); err != nil || id != "cleared" {
		t.Fatalf("after a prompt = %q, %v; want the cleared conversation", id, err)
	}
}
