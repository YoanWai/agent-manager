package agentsession

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type hermesRow struct {
	id, source, cwd string
	started         time.Time
}

func writeHermesStore(t *testing.T, rows ...hermesRow) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		source TEXT NOT NULL,
		cwd TEXT,
		started_at REAL NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO sessions (id, source, cwd, started_at) VALUES (?, ?, ?, ?)`,
			row.id, row.source, row.cwd, float64(row.started.UnixNano())/float64(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

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

func TestCaptureHermesPicksSessionAfterLaunchInCwd(t *testing.T) {
	launch := time.Now()
	path := writeHermesStore(t,
		hermesRow{"old", "cli", "/repo", launch.Add(-time.Hour)},
		hermesRow{"just-before", "cli", "/repo", launch.Add(-time.Second)},
		hermesRow{"other", "cli", "/elsewhere", launch.Add(time.Second)},
		hermesRow{"tui", "tui", "/repo", launch.Add(time.Second)},
		hermesRow{"ours", "cli", "/repo", launch.Add(2 * time.Second)},
	)

	id, ok := captureHermes(path, "/repo", launch, map[string]bool{})
	if !ok || id != "ours" {
		t.Fatalf("got id=%q ok=%v, want ours true", id, ok)
	}
}

func TestCaptureHermesSkipsClaimed(t *testing.T) {
	launch := time.Now()
	path := writeHermesStore(t,
		hermesRow{"first", "cli", "/repo", launch.Add(time.Second)},
		hermesRow{"second", "cli", "/repo", launch.Add(2 * time.Second)},
	)

	id, ok := captureHermes(path, "/repo", launch, map[string]bool{"first": true})
	if !ok || id != "second" {
		t.Fatalf("got id=%q ok=%v, want second true", id, ok)
	}
}

func TestHermesStateDBFollowsHermesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HERMES_HOME", home)
	if got := hermesStateDB(); got != filepath.Join(home, "state.db") {
		t.Fatalf("hermesStateDB() = %q", got)
	}
}

func TestHermesStateDBFollowsStickyProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HERMES_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "active_profile"), []byte("coder\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "profiles", "coder", "state.db")
	if got := hermesStateDB(); got != want {
		t.Fatalf("hermesStateDB() = %q want %q", got, want)
	}
}

func TestHermesStateDBKeepsExplicitProfileHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "profiles", "research")
	t.Setenv("HERMES_HOME", home)
	if got := hermesStateDB(); got != filepath.Join(home, "state.db") {
		t.Fatalf("hermesStateDB() = %q", got)
	}
}

// geminiSessionFixture is the header line of a gemini session file; the
// capturer and resolver only read this first line.
func geminiSessionFixture(sessionID, projectHash string) string {
	return `{"sessionId":"` + sessionID + `","projectHash":"` + projectHash +
		`","startTime":"2026-08-06T00:00:00.000Z","lastUpdated":"2026-08-06T00:00:00.000Z","kind":"main"}` + "\n"
}

func TestCaptureGeminiPicksSessionAfterLaunchInCwd(t *testing.T) {
	root := t.TempDir()
	launch := time.Now()
	oursHash := geminiProjectHash("/repo")
	otherHash := geminiProjectHash("/elsewhere")
	// An older conversation in the same project predates the launch: not ours.
	writeFile(t, filepath.Join(root, "proj/chats/session-1-old.jsonl"),
		geminiSessionFixture("old-uuid", oursHash), launch.Add(-time.Hour))
	// A conversation in a different project started after launch: not ours.
	writeFile(t, filepath.Join(root, "proj/chats/session-2-other.jsonl"),
		geminiSessionFixture("other-uuid", otherHash), launch.Add(time.Second))
	// Ours: matching project hash, written just after launch.
	writeFile(t, filepath.Join(root, "proj/chats/session-3-ours.jsonl"),
		geminiSessionFixture("ours-uuid", oursHash), launch.Add(2*time.Second))

	id, ok := captureGemini(root, "/repo", launch, map[string]bool{})
	if !ok || id != "ours-uuid" {
		t.Fatalf("got id=%q ok=%v, want ours-uuid true", id, ok)
	}
}

func TestCaptureGeminiSkipsClaimed(t *testing.T) {
	root := t.TempDir()
	launch := time.Now()
	hash := geminiProjectHash("/repo")
	writeFile(t, filepath.Join(root, "p/chats/session-1.jsonl"),
		geminiSessionFixture("first-uuid", hash), launch.Add(time.Second))
	writeFile(t, filepath.Join(root, "p/chats/session-2.jsonl"),
		geminiSessionFixture("second-uuid", hash), launch.Add(2*time.Second))

	// first-uuid already belongs to another session, so the earliest
	// unclaimed match wins instead.
	id, ok := captureGemini(root, "/repo", launch, map[string]bool{"first-uuid": true})
	if !ok || id != "second-uuid" {
		t.Fatalf("got id=%q ok=%v, want second-uuid true", id, ok)
	}
}

func TestGeminiSessionFileInResolvesByID(t *testing.T) {
	root := t.TempDir()
	id := "aaaa1111-2222-3333-4444-555566667777"
	path := filepath.Join(root, "proj/chats/session-1786000000000-aaaa1111.jsonl")
	writeFile(t, path, geminiSessionFixture(id, geminiProjectHash("/repo")), time.Now())

	got, err := geminiSessionFileIn(root, id)
	if err != nil || got != path {
		t.Fatalf("got %q err=%v, want %q", got, err, path)
	}

	if _, err := geminiSessionFileIn(root, "bbbb9999-0000-0000-0000-000000000000"); err == nil {
		t.Fatal("expected an error for an unknown conversation id")
	}
}

// Only gemini keeps a conversation in a file a fork can load. Any other
// store must be refused rather than read through the gemini layout.
func TestSessionFileRefusesStoresWithoutAFile(t *testing.T) {
	for _, sessionStore := range []string{"", "codex", "opencode", "hermes", "weird"} {
		if SupportsSessionFile(sessionStore) {
			t.Errorf("SupportsSessionFile(%q) = true", sessionStore)
		}
		if _, err := SessionFile(sessionStore, "some-conversation"); err == nil {
			t.Errorf("SessionFile(%q, ...) resolved a path", sessionStore)
		}
	}
	if !SupportsSessionFile("gemini") {
		t.Error(`SupportsSessionFile("gemini") = false`)
	}
}
