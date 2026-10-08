package agentsession

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeOmpSession writes a session file the way omp 18 does: a fixed-width
// title slot record, then the session header, then the conversation.
func writeOmpSession(t *testing.T, dir, id, cwd string, created, modified time.Time) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	title, err := json.Marshal(map[string]any{"type": "title", "v": 1, "title": "", "updatedAt": created.Format(time.RFC3339Nano), "pad": strings.Repeat(" ", 120)})
	if err != nil {
		t.Fatal(err)
	}
	header, err := json.Marshal(map[string]any{"type": "session", "version": 3, "id": id, "timestamp": created.UTC().Format("2006-01-02T15:04:05.000Z"), "cwd": cwd})
	if err != nil {
		t.Fatal(err)
	}
	path := ompSessionPath(dir, id, created)
	body := string(title) + "\n" + string(header) + "\n" + `{"type":"message","message":{"role":"assistant"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeOmpBreadcrumb writes the file omp keeps per terminal: the cwd, the
// session file, then "fresh" until that file is written.
func writeOmpBreadcrumb(t *testing.T, tty, cwd, sessionFile string, fresh bool, modified time.Time) {
	t.Helper()
	dir := filepath.Join(filepath.Dir(ompRoot()), "terminal-sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := cwd + "\n" + sessionFile + "\n"
	if fresh {
		body += "fresh\n"
	}
	body += "cwdstat 44 455492\n"
	path := filepath.Join(dir, strings.ReplaceAll(strings.TrimPrefix(tty, "/dev/"), "/", "-"))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
}

// Two sessions share a directory and the later launch answers first: the
// earlier one must not take that conversation. Each pane's breadcrumb names
// its own session file, and a fresh one (nothing written yet) binds nothing.
// ompSessionPath names a session file the way omp does:
// <ISO time with : and . as ->_<id>.jsonl.
func ompSessionPath(dir, id string, created time.Time) string {
	stamp := strings.NewReplacer(":", "-", ".", "-").Replace(created.UTC().Format("2006-01-02T15:04:05.000Z"))
	return filepath.Join(dir, stamp+"_"+id+".jsonl")
}

func TestOmpCaptureReadsThePaneBreadcrumb(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	root := ompRoot()
	cwd := t.TempDir()
	here := filepath.Join(root, "-project")
	now := time.Now().Truncate(time.Millisecond)
	const (
		firstID  = "01a0e200-0000-7000-8000-000000000004"
		secondID = "01a0e200-0000-7000-8000-000000000005"
	)
	secondFile := writeOmpSession(t, here, secondID, cwd, now.Add(time.Second), now.Add(2*time.Second))
	firstFile := ompSessionPath(here, firstID, now)
	writeOmpBreadcrumb(t, "/dev/pts/3", cwd, firstFile, true, now)
	writeOmpBreadcrumb(t, "/dev/pts/4", cwd, secondFile, false, now.Add(2*time.Second))
	if id, ok := Capture("omp", cwd, now, nil, "/dev/pts/3"); ok {
		t.Fatalf("captured %q while this pane's session is unwritten", id)
	}
	if id, ok := Capture("omp", cwd, now, nil, "/dev/pts/4"); !ok || id != secondID {
		t.Fatalf("second pane Capture = %q, %v", id, ok)
	}
	if got := writeOmpSession(t, here, firstID, cwd, now, now.Add(3*time.Second)); got != firstFile {
		t.Fatalf("session file %q want %q", got, firstFile)
	}
	writeOmpBreadcrumb(t, "/dev/pts/3", cwd, firstFile, false, now.Add(3*time.Second))
	if id, ok := Capture("omp", cwd, now, nil, "/dev/pts/3"); !ok || id != firstID {
		t.Fatalf("first pane Capture = %q, %v", id, ok)
	}
	if id, ok := Capture("omp", cwd, now, map[string]bool{firstID: true}, "/dev/pts/3"); ok {
		t.Fatalf("captured claimed %q", id)
	}
}

func TestOmpCaptureRefusesAStaleOrForeignBreadcrumb(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	root := ompRoot()
	cwd := t.TempDir()
	now := time.Now().Truncate(time.Millisecond)
	const id = "01a0e200-0000-7000-8000-000000000006"
	file := writeOmpSession(t, filepath.Join(root, "-project"), id, cwd, now, now)
	for _, tc := range []struct {
		name, tty, crumbCwd string
		modified            time.Time
	}{
		// a tty number the kernel handed to an earlier, finished pane,
		// even one that closed a moment before this launch
		{"left by an earlier pane", "/dev/pts/5", cwd, now.Add(-time.Hour)},
		{"left by a pane that just closed", "/dev/pts/5", cwd, now.Add(-2 * time.Second)},
		{"another directory", "/dev/pts/6", t.TempDir(), now},
	} {
		writeOmpBreadcrumb(t, tc.tty, tc.crumbCwd, file, false, tc.modified)
		if got, ok := Capture("omp", cwd, now, nil, tc.tty); ok {
			t.Fatalf("%s: captured %q", tc.name, got)
		}
	}
	for _, tty := range []string{"", "pts/7", "/dev/pts/8"} {
		if got, ok := Capture("omp", cwd, now, nil, tty); ok {
			t.Fatalf("tty %q without a breadcrumb: captured %q", tty, got)
		}
	}
}

// A pane launched now can open a conversation from before the launch; its
// breadcrumb then names that older session, which is the one to bind.
func TestOmpCaptureAcceptsAnOlderSessionOpenedInThisLaunch(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	cwd := t.TempDir()
	now := time.Now().Truncate(time.Millisecond)
	const id = "01a0e200-0000-7000-8000-000000000007"
	file := writeOmpSession(t, filepath.Join(ompRoot(), "-project"), id, cwd, now.Add(-24*time.Hour), now.Add(time.Second))
	writeOmpBreadcrumb(t, "/dev/pts/10", cwd, file, false, now.Add(time.Second))
	if got, ok := Capture("omp", cwd, now, nil, "/dev/pts/10"); !ok || got != id {
		t.Fatalf("Capture = %q, %v", got, ok)
	}
}

func TestOmpCaptureResolvesSymlinkedCwd(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	now := time.Now().Truncate(time.Millisecond)
	const id = "01a0e200-0000-7000-8000-00000000000a"
	file := writeOmpSession(t, filepath.Join(ompRoot(), "-real"), id, resolvePath(real), now, now)
	writeOmpBreadcrumb(t, "/dev/pts/9", resolvePath(real), file, false, now)
	if got, ok := Capture("omp", link, now, nil, "/dev/pts/9"); !ok || got != id {
		t.Fatalf("Capture via symlink = %q, %v", got, ok)
	}
}

func TestOmpSnapshotAndRecapture(t *testing.T) {
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	root := ompRoot()
	if root != filepath.Join(agentDir, "sessions") {
		t.Fatalf("ompRoot = %q", root)
	}
	cwd := t.TempDir()
	here := filepath.Join(root, "-project")
	now := time.Now().Truncate(time.Millisecond)
	const (
		oldID   = "01a0e200-0000-7000-8000-000000000001"
		otherID = "01a0e200-0000-7000-8000-000000000002"
		childID = "01a0e200-0000-7000-8000-000000000003"
		firstID = "01a0e200-0000-7000-8000-000000000004"
	)
	writeOmpSession(t, here, oldID, cwd, now.Add(-time.Hour), now)
	writeOmpSession(t, filepath.Join(root, "-elsewhere"), otherID, t.TempDir(), now, now)
	// a subagent log in a session's artifact directory shares the cwd
	writeOmpSession(t, filepath.Join(here, "2026-09-27T09-44-11-661Z_"+oldID), childID, cwd, now, now)
	path := writeOmpSession(t, here, firstID, cwd, now, now)
	snapshot, ok := Snapshot("omp", cwd)
	if !ok || len(snapshot) != 2 {
		t.Fatalf("Snapshot = %v, %v", snapshot, ok)
	}
	if id, ok := Recapture("omp", cwd, snapshot, nil); ok {
		t.Fatalf("Recapture without new activity = %q", id)
	}
	later := now.Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if id, ok := Recapture("omp", cwd, snapshot, nil); !ok || id != firstID {
		t.Fatalf("Recapture = %q, %v", id, ok)
	}
}

func TestOmpMetaSkipsMalformedHeaders(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"no-header":  `{"type":"title","v":1}` + "\n" + `{"type":"message"}` + "\n",
		"bad-id":     `{"type":"title","v":1}` + "\n" + `{"type":"session","id":"../x","timestamp":"2026-09-27T09:44:11.661Z","cwd":"/p"}` + "\n",
		"bad-stamp":  `{"type":"title","v":1}` + "\n" + `{"type":"session","id":"abc","timestamp":"yesterday","cwd":"/p"}` + "\n",
		"no-cwd":     `{"type":"title","v":1}` + "\n" + `{"type":"session","id":"abc","timestamp":"2026-09-27T09:44:11.661Z"}` + "\n",
		"not-json":   "garbage\nmore garbage\n",
		"empty-file": "",
	} {
		path := filepath.Join(dir, name+".jsonl")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		id, cwd, _, err := ompMeta(path)
		if err != nil || id != "" || cwd != "" {
			t.Fatalf("%s: ompMeta = %q, %q, %v", name, id, cwd, err)
		}
	}
	path := filepath.Join(dir, "header-first.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session","id":"abc","timestamp":"2026-09-27T09:44:11.661Z","cwd":"/p"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if id, cwd, _, err := ompMeta(path); err != nil || id != "abc" || cwd != "/p" {
		t.Fatalf("header on line 1: ompMeta = %q, %q, %v", id, cwd, err)
	}
}
