//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The detached process outlives the call, writes only to the log, and
// runs in a session of its own, so the shell that started it can hang up.
func TestStartDetachedLogsToAPrivateFileInANewSession(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), ServeLog)
	if err := os.WriteFile(logPath, []byte("earlier run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pid, err := startDetached("/bin/sh", []string{"-c", `read line; echo "stdin=[$line] pgid=$(ps -o pgid= -p $$ | tr -d ' ') pid=$$"; echo oops >&2`}, logPath)
	if err != nil {
		t.Fatalf("startDetached: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("pid = %d", pid)
	}
	var log string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		if log = string(raw); strings.Contains(log, "oops") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.HasPrefix(log, "earlier run\n") {
		t.Fatalf("the log was not appended to: %q", log)
	}
	if !strings.Contains(log, "stdin=[] ") || !strings.Contains(log, "oops") {
		t.Fatalf("stdin or stderr went elsewhere: %q", log)
	}
	if want := "pid=" + strconv.Itoa(pid); !strings.Contains(log, want) {
		t.Fatalf("log %q does not come from pid %d", log, pid)
	}
	if !strings.Contains(log, "pgid="+strconv.Itoa(pid)+" ") {
		t.Fatalf("the process does not lead a session and process group of its own: %q", log)
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("log mode = %v", info.Mode().Perm())
	}

	fresh := filepath.Join(t.TempDir(), ServeLog)
	if _, err := startDetached("/bin/sh", []string{"-c", "true"}, fresh); err != nil {
		t.Fatalf("startDetached: %v", err)
	}
	if info, err := os.Stat(fresh); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("a new log was created as %v (%v)", info.Mode().Perm(), err)
	}
}
