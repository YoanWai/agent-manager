package conversation

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
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

// A record gemini is still writing waits for a later poll, and what gemini
// wrote after the read is never emptied away.
func TestGeminiKeepsWhatItHasNotReadWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "row.otel")
	whole := telemetryLog(os.Getpid(), "first")
	partial := telemetryLog(os.Getpid(), "second")
	appendFile(t, path, whole+partial[:len(partial)/2])
	var reader Reader
	id, wholeRecord, err := reader.geminiConversation([]byte(whole+partial[:len(partial)/2]), os.Getpid())
	if err != nil || id != "first" || wholeRecord {
		t.Fatalf("partial telemetry = %q, %v, %v; want first", id, wholeRecord, err)
	}
	appendFile(t, path, partial[len(partial)/2:])
	if err := reader.FollowTelemetry(path, os.Getpid(), func(id string) (bool, error) {
		if id != "second" {
			t.Errorf("reported %q, want second", id)
		}
		appendFile(t, path, telemetryMetric)
		return true, nil
	}); err != nil {
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
	id, whole, err := reader.geminiConversation([]byte(broken[len(broken)/2:]+telemetryLog(os.Getpid(), "after")), os.Getpid())
	if err != nil || id != "after" || !whole {
		t.Fatalf("broken telemetry = %q, %v, %v; want after", id, whole, err)
	}
}

// A /clear logs the new conversation before it holds anything, and gemini
// deletes it on exit if nothing follows, so only a later record moves the
// row there.
func TestGeminiCountsAClearedConversationFromItsFirstRecordAfterTheSwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "row.otel")
	appendFile(t, path, telemetryLog(os.Getpid(), "before")+telemetryEvent(os.Getpid(), "cleared", "gemini_cli.slash_command"))
	var reader Reader
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if id, _, err := reader.geminiConversation(data, os.Getpid()); err != nil || id != "before" {
		t.Fatalf("after /clear alone = %q, %v; want the conversation from before", id, err)
	}
	appendFile(t, path, telemetryLog(os.Getpid(), "cleared"))
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if id, _, err := reader.geminiConversation(data, os.Getpid()); err != nil || id != "cleared" {
		t.Fatalf("after a prompt = %q, %v; want the cleared conversation", id, err)
	}
}

func TestTelemetryReadersSerializeReportingAndCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "row.otel")
	appendFile(t, path, telemetryLog(os.Getpid(), "first"))
	reading := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	second := make(chan error, 1)
	reported := make(chan string, 1)
	go func() {
		reader := &Reader{}
		first <- reader.FollowTelemetry(path, os.Getpid(), func(string) (bool, error) {
			close(reading)
			<-release
			return true, nil
		})
	}()
	<-reading
	appendFile(t, path, telemetryLog(os.Getpid(), "second"))
	go func() {
		reader := &Reader{}
		second <- reader.FollowTelemetry(path, os.Getpid(), func(id string) (bool, error) {
			reported <- id
			return true, nil
		})
	}()
	id := ""
	select {
	case id = <-reported:
		t.Errorf("second reader reported %q before the first finished", id)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if id == "" {
		id = <-reported
	}
	if id != "second" {
		t.Fatalf("second reader reported %q, want second", id)
	}
}

func TestTelemetryKeepsRecordsUntilTheyAreStored(t *testing.T) {
	for _, reportErr := range []error{nil, errors.New("store unavailable")} {
		path := filepath.Join(t.TempDir(), "row.otel")
		data := telemetryLog(os.Getpid(), "pending")
		appendFile(t, path, data)
		reader := &Reader{}
		err := reader.FollowTelemetry(path, os.Getpid(), func(string) (bool, error) {
			return false, reportErr
		})
		if !errors.Is(err, reportErr) {
			t.Fatalf("FollowTelemetry = %v, want %v", err, reportErr)
		}
		if got, err := os.ReadFile(path); err != nil || string(got) != data {
			t.Fatalf("unreported telemetry = %q, %v", got, err)
		}
	}
}

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
