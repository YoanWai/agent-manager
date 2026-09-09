package agentsession

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeMuseSession(t *testing.T, root, relative, id, cwd string, created, modified time.Time) string {
	t.Helper()
	path := filepath.Join(root, relative, "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	record := map[string]any{
		"stream":      map[string]string{"kind": "session", "id": id},
		"recorded_at": created.UnixMicro(), "payload_type": "runtime.session.metadata",
		"payload": map[string]any{"record": map[string]string{"workspace_root": cwd}},
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	data = append([]byte("{\"retained_frame\":\"session_permission_transaction\",\"children\":[]}\n"), data...)
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMuseCaptureAndRecapture(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := museRoot()
	cwd := t.TempDir()
	now := time.Now().Truncate(time.Microsecond)
	writeMuseSession(t, root, "2026/09/09/old", "old", cwd, now.Add(-time.Hour), now)
	writeMuseSession(t, root, "2026/09/09/other", "other", t.TempDir(), now, now)
	writeMuseSession(t, root, "2026/09/09/first/subagent/child", "child", cwd, now, now)
	if id, ok := Capture("muse", cwd, now, nil); ok {
		t.Fatalf("captured stale, foreign, or child session %q", id)
	}
	path := writeMuseSession(t, root, "2026/09/09/first", "first", cwd, now, now)
	writeMuseSession(t, root, "2026/09/09/second", "second", cwd, now.Add(time.Second), now.Add(time.Second))
	if id, ok := Capture("muse", cwd, now, nil); !ok || id != "first" {
		t.Fatalf("Capture = %q, %v", id, ok)
	}
	if id, ok := Capture("muse", cwd, now, map[string]bool{"first": true}); !ok || id != "second" {
		t.Fatalf("claimed Capture = %q, %v", id, ok)
	}
	snapshot, ok := Snapshot("muse", cwd)
	if !ok || len(snapshot) != 3 {
		t.Fatalf("Snapshot = %v, %v", snapshot, ok)
	}
	if id, ok := Recapture("muse", cwd, snapshot, nil); ok {
		t.Fatalf("recaptured unchanged %q", id)
	}
	later := now.Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if id, ok := Recapture("muse", cwd, snapshot, nil); !ok || id != "first" {
		t.Fatalf("Recapture = %q, %v", id, ok)
	}
	if id, ok := Recapture("muse", cwd, snapshot, map[string]bool{"first": true}); ok {
		t.Fatalf("recaptured claimed %q", id)
	}
	writeMuseSession(t, root, "2026/09/09/third", "third", cwd, later, later)
	if id, ok := Recapture("muse", cwd, snapshot, nil); ok {
		t.Fatalf("recaptured ambiguous %q", id)
	}
}

func TestMuseMissingAndMalformedStore(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if _, ok := Snapshot("muse", "/work"); ok {
		t.Fatal("missing store accepted")
	}
	if _, ok := Recapture("muse", "/work", nil, nil); ok {
		t.Fatal("nil snapshot accepted")
	}
	for _, data := range []string{"", "not json\n", `{"payload_type":"runtime.session.metadata"}` + "\n"} {
		path := filepath.Join(t.TempDir(), "session.jsonl")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, _, ok := museMeta(path); ok {
			t.Fatalf("accepted %q", data)
		}
	}
}

func TestMuseRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	if got := museRoot(); got != filepath.Join(home, ".local", "share", "muse", "sessions") {
		t.Fatal(got)
	}
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	if got := museRoot(); got != filepath.Join(data, "muse", "sessions") {
		t.Fatal(got)
	}
}
