package conversation

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHermesConversationReadsTheTerminalsCrumb(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HERMES_HOME", home)
	cwd := t.TempDir()
	launched := time.Unix(1791410000, 0)
	writeCrumb := func(name, sessionID, dir string, written time.Time) {
		t.Helper()
		content := fmt.Sprintf(`{"session_id": %q, "cwd": %q, "ts": %f}`, sessionID, dir, float64(written.UnixNano())/1e9)
		path := filepath.Join(home, "terminal-sessions", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(tty string, until time.Time) string {
		t.Helper()
		id, err := HermesConversation(tty, launched, until, cwd)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	writeCrumb("tty-dev-ttys029", "20261008_005341_0d388a", cwd, launched.Add(time.Minute))
	writeCrumb("tty-dev-pts-3", "20261008_005342_2cbb4e", cwd, launched.Add(time.Minute))
	if id := read("/dev/ttys029", time.Time{}); id != "20261008_005341_0d388a" {
		t.Fatalf("macOS terminal = %q", id)
	}
	if id := read("/dev/pts/3", time.Time{}); id != "20261008_005342_2cbb4e" {
		t.Fatalf("Linux terminal = %q", id)
	}
	if id := read("/dev/ttys029", launched.Add(30*time.Second)); id != "" {
		t.Fatalf("a crumb written after the row died = %q", id)
	}
	if id := read("not a tty", time.Time{}); id != "" {
		t.Fatalf("no terminal = %q", id)
	}
	writeCrumb("tty-dev-ttys029", "before", cwd, launched.Add(-time.Second))
	if id := read("/dev/ttys029", time.Time{}); id != "" {
		t.Fatalf("a crumb from before the launch = %q", id)
	}
	writeCrumb("tty-dev-ttys029", "elsewhere", t.TempDir(), launched.Add(time.Minute))
	if id := read("/dev/ttys029", time.Time{}); id != "" {
		t.Fatalf("a crumb for another directory = %q", id)
	}
}
