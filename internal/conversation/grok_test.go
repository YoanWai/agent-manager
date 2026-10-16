package conversation

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func appendFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

// The last sid each pid wrote is its conversation, read as the log grows,
// a line still being written waits for its end, and a log that starts over
// is read from the top.
func TestGrokFollowsTheLastConversationEachProcessLogged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_HOME", home)
	log := filepath.Join(home, "logs", "unified.jsonl")
	var reader Reader
	current := func(pid int) string {
		t.Helper()
		id, err := reader.Current("grok", Agent{PID: pid, Ended: true, Recorded: time.Now()}, time.Time{})
		if err != nil {
			t.Fatalf("Current(%d) = %q, %v", pid, id, err)
		}
		return id
	}
	if id := current(88360); id != "" {
		t.Fatalf("no log = %q", id)
	}
	appendFile(t, log, `{"ts":"2026-10-08T00:00:00Z","pid":88360,"sid":"a1","msg":"session created"}
{"ts":"2026-10-08T00:00:00Z","pid":88394,"sid":"b1","msg":"session created"}
{"pid":88360,"msg":"no session"}
not json
`)
	if a, b := current(88360), current(88394); a != "a1" || b != "b1" {
		t.Fatalf("conversations = %q %q, want a1 b1", a, b)
	}
	appendFile(t, log, `{"ts":"2026-10-08T00:00:00Z","pid":88360,"sid":"a2","msg":"session.load.done"}`)
	if id := current(88360); id != "a1" {
		t.Fatalf("a line still being written moved the pid to %q", id)
	}
	appendFile(t, log, "\n")
	if id := current(88360); id != "a2" {
		t.Fatalf("after the switch = %q, want a2", id)
	}
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	appendFile(t, log, `{"ts":"2026-10-08T00:00:00Z","pid":88394,"sid":"b2"}`+"\n")
	if a, b := current(88360), current(88394); a != "" || b != "b2" {
		t.Fatalf("after the log started over = %q %q, want none and b2", a, b)
	}
}

func grokRecord(pid int, id string, at time.Time) string {
	return fmt.Sprintf("{\"ts\":%q,\"pid\":%d,\"sid\":%q}\n", at.Format(time.RFC3339Nano), pid, id)
}

func TestGrokOnlyReadsRecordsFromTheAgentsLifetime(t *testing.T) {
	t.Setenv("GROK_HOME", t.TempDir())
	path, err := grokLogPath()
	if err != nil {
		t.Fatal(err)
	}
	since := time.Now().Add(-time.Minute)
	ended := since.Add(30 * time.Second)
	agent := Agent{PID: 88360, Ended: true, Recorded: ended}
	appendFile(t, path, grokRecord(agent.PID, "previous-process", since.Add(-time.Second))+
		grokRecord(agent.PID, "own-conversation", since.Add(time.Second))+
		grokRecord(agent.PID, "reused-process", ended.Add(time.Second)))
	reader := &Reader{}
	if id, err := reader.Current("grok", agent, since); err != nil || id != "own-conversation" {
		t.Fatalf("ended agent conversation = %q, %v", id, err)
	}
	appendFile(t, path, grokRecord(agent.PID, "later-process", ended.Add(2*time.Second)))
	if id, err := reader.Current("grok", agent, since); err != nil || id != "own-conversation" {
		t.Fatalf("after pid reuse = %q, %v", id, err)
	}
	newSince := ended.Add(time.Second)
	later := Agent{PID: agent.PID, Ended: true, Recorded: ended.Add(3 * time.Second)}
	if id, err := reader.Current("grok", later, newSince); err != nil || id != "later-process" {
		t.Fatalf("new launch conversation = %q, %v", id, err)
	}
}

func TestGrokDoesNotReadAnUnmarkedAgentWhosePIDWasReused(t *testing.T) {
	t.Setenv("GROK_HOME", t.TempDir())
	path, err := grokLogPath()
	if err != nil {
		t.Fatal(err)
	}
	appendFile(t, path, grokRecord(os.Getpid(), "unrelated", time.Now()))
	agent := Agent{PID: os.Getpid(), Recorded: time.Now().Add(-time.Hour)}
	if id, err := (&Reader{}).Current("grok", agent, agent.Recorded); err != nil || id != "" {
		t.Fatalf("reused pid conversation = %q, %v", id, err)
	}
}
