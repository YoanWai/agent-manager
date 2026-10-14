package conversation

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// recordedNow is this test process standing in for an agent whose launch
// recorded it just now.
func recordedNow() Agent {
	return Agent{PID: os.Getpid(), Recorded: time.Now()}
}

func holdOpen(t *testing.T, path string) *os.File {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file
}

func TestHeldConversationOfAProcessThatIsGone(t *testing.T) {
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	var reader Reader
	if id, err := reader.Current("muse", Agent{PID: gone.Process.Pid}, time.Time{}); err != nil || id != "" {
		t.Fatalf("gone process = %q, %v; want nothing", id, err)
	}
}

// A pid recorded before this process started belonged to an agent that has
// since quit, so what this process holds is not that agent's conversation.
func TestHeldConversationOfAPIDReusedSinceTheRecord(t *testing.T) {
	holdOpen(t, filepath.Join(t.TempDir(), "muse", "sessions", "01a1183b-cbc5.json"))
	reused := Agent{PID: os.Getpid(), Recorded: time.Now().Add(-time.Hour)}
	var reader Reader
	if id, err := reader.Current("muse", reused, time.Time{}); err != nil || id != "" {
		t.Fatalf("reused pid = %q, %v; want nothing", id, err)
	}
}
