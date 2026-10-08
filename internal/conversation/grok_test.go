package conversation

import (
	"os"
	"path/filepath"
	"testing"
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
		id, done, err := reader.Current("grok", Agent{PID: pid}, "")
		if err != nil || done != nil {
			t.Fatalf("Current(%d) = %q, %v", pid, id, err)
		}
		return id
	}
	if id := current(88360); id != "" {
		t.Fatalf("no log = %q", id)
	}
	appendFile(t, log, `{"pid":88360,"sid":"a1","msg":"session created"}
{"pid":88394,"sid":"b1","msg":"session created"}
{"pid":88360,"msg":"no session"}
not json
`)
	if a, b := current(88360), current(88394); a != "a1" || b != "b1" {
		t.Fatalf("conversations = %q %q, want a1 b1", a, b)
	}
	appendFile(t, log, `{"pid":88360,"sid":"a2","msg":"session.load.done"}`)
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
	appendFile(t, log, `{"pid":88394,"sid":"b2"}`+"\n")
	if a, b := current(88360), current(88394); a != "" || b != "b2" {
		t.Fatalf("after the log started over = %q %q, want none and b2", a, b)
	}
}
