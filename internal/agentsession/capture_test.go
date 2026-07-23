package agentsession

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func codexRollout(sessionID, cwd string) string {
	return `{"timestamp":"2026-07-18T14:36:08.127Z","type":"session_meta","payload":{"session_id":"` +
		sessionID + `","cwd":"` + cwd + `"}}` + "\n" +
		`{"timestamp":"2026-07-18T14:36:09Z","type":"event_msg","payload":{}}` + "\n"
}

func TestCaptureCodexPicksSessionAfterLaunchInCwd(t *testing.T) {
	root := t.TempDir()
	launch := time.Now()
	// An older conversation in the same cwd predates the launch: not ours.
	writeFile(t, filepath.Join(root, "2026/07/18/rollout-old.jsonl"),
		codexRollout("old-uuid", "/repo"), launch.Add(-time.Hour))
	// A conversation in a different cwd started after launch: not ours.
	writeFile(t, filepath.Join(root, "2026/07/18/rollout-other.jsonl"),
		codexRollout("other-uuid", "/elsewhere"), launch.Add(time.Second))
	// Ours: same cwd, written just after launch.
	writeFile(t, filepath.Join(root, "2026/07/18/rollout-ours.jsonl"),
		codexRollout("ours-uuid", "/repo"), launch.Add(2*time.Second))

	id, ok := captureCodex(root, "/repo", launch, map[string]bool{})
	if !ok || id != "ours-uuid" {
		t.Fatalf("got id=%q ok=%v, want ours-uuid true", id, ok)
	}
}

func TestCaptureCodexSkipsClaimed(t *testing.T) {
	root := t.TempDir()
	launch := time.Now()
	writeFile(t, filepath.Join(root, "a/rollout-1.jsonl"),
		codexRollout("first-uuid", "/repo"), launch.Add(time.Second))
	writeFile(t, filepath.Join(root, "a/rollout-2.jsonl"),
		codexRollout("second-uuid", "/repo"), launch.Add(2*time.Second))

	// first-uuid already belongs to another session, so the earliest
	// unclaimed match wins instead.
	id, ok := captureCodex(root, "/repo", launch, map[string]bool{"first-uuid": true})
	if !ok || id != "second-uuid" {
		t.Fatalf("got id=%q ok=%v, want second-uuid true", id, ok)
	}
}

func TestCaptureCodexNoMatch(t *testing.T) {
	root := t.TempDir()
	launch := time.Now()
	writeFile(t, filepath.Join(root, "a/rollout-1.jsonl"),
		codexRollout("x", "/other"), launch.Add(time.Second))
	if id, ok := captureCodex(root, "/repo", launch, map[string]bool{}); ok {
		t.Fatalf("expected no match, got %q", id)
	}
}

type ocMeta struct {
	dir     string
	created time.Time
}

// stubOpencode replaces the opencode CLI seams with in-memory data for the
// duration of a test and returns a restore function.
func stubOpencode(t *testing.T, ids []string, metas map[string]ocMeta) {
	t.Helper()
	listSaved, metaSaved := opencodeListIDs, opencodeSessionMeta
	opencodeListIDs = func(string) ([]string, bool) { return ids, true }
	opencodeSessionMeta = func(_, id string) (string, time.Time, bool) {
		m, ok := metas[id]
		return m.dir, m.created, ok
	}
	t.Cleanup(func() { opencodeListIDs, opencodeSessionMeta = listSaved, metaSaved })
}

func TestCaptureOpencodePicksSessionAfterLaunchInCwd(t *testing.T) {
	launch := time.Now()
	stubOpencode(t, []string{"ses_ours", "ses_other", "ses_old"}, map[string]ocMeta{
		// An older conversation in the same cwd predates the launch: not ours.
		"ses_old": {"/repo", launch.Add(-time.Hour)},
		// A conversation in a different cwd started after launch: not ours.
		"ses_other": {"/elsewhere", launch.Add(time.Second)},
		// Ours: same cwd, created just after launch.
		"ses_ours": {"/repo", launch.Add(2 * time.Second)},
	})

	id, ok := captureOpencode("/repo", launch, map[string]bool{})
	if !ok || id != "ses_ours" {
		t.Fatalf("got id=%q ok=%v, want ses_ours true", id, ok)
	}
}

func TestCaptureOpencodeSkipsClaimed(t *testing.T) {
	launch := time.Now()
	stubOpencode(t, []string{"ses_1", "ses_2"}, map[string]ocMeta{
		"ses_1": {"/repo", launch.Add(time.Second)},
		"ses_2": {"/repo", launch.Add(2 * time.Second)},
	})

	// ses_1 already belongs to another session, so the earliest unclaimed
	// match wins instead.
	id, ok := captureOpencode("/repo", launch, map[string]bool{"ses_1": true})
	if !ok || id != "ses_2" {
		t.Fatalf("got id=%q ok=%v, want ses_2 true", id, ok)
	}
}

func TestParseOpencodeExportReadsDirectoryAndTime(t *testing.T) {
	out := []byte("Exporting session: ses_x\n" +
		`{"info":{"id":"ses_x","directory":"/repo","time":{"created":1784385368000}}}`)
	dir, created, ok := parseOpencodeExport(out)
	if !ok || dir != "/repo" || created.UnixMilli() != 1784385368000 {
		t.Fatalf("got dir=%q created=%v ok=%v", dir, created, ok)
	}
}

func TestCaptureUnknownStore(t *testing.T) {
	if _, ok := Capture("weird", "/repo", time.Now(), map[string]bool{}); ok {
		t.Fatal("unknown store should not match")
	}
}
