package agentsession

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type hermesRow struct {
	id, source, cwd string
	started         time.Time
	// activity is last_activity_at; zero leaves it NULL, so the activity
	// reads as the session start until its first heartbeat.
	activity time.Time
}

func writeHermesStore(t *testing.T, rows ...hermesRow) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		source TEXT NOT NULL,
		cwd TEXT,
		started_at REAL NOT NULL,
		last_activity_at REAL
	);
	CREATE TABLE messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		timestamp REAL NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		var activity any
		if !row.activity.IsZero() {
			activity = float64(row.activity.UnixNano()) / float64(time.Second)
		}
		if _, err := db.ExecContext(context.Background(), `INSERT INTO sessions (id, source, cwd, started_at, last_activity_at) VALUES (?, ?, ?, ?, ?)`,
			row.id, row.source, row.cwd, float64(row.started.UnixNano())/float64(time.Second), activity); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// setHermesActivity stamps a session's activity heartbeat, as a resumed
// session's first turn does.
func setHermesActivity(t *testing.T, path, id string, at time.Time) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `UPDATE sessions SET last_activity_at = ? WHERE id = ?`,
		float64(at.UnixNano())/float64(time.Second), id); err != nil {
		t.Fatal(err)
	}
}

// addHermesMessage appends a message row, the other half of hermes's
// activity signal for a session whose heartbeat has not stamped yet.
func addHermesMessage(t *testing.T, path, sessionID string, at time.Time) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO messages (session_id, timestamp) VALUES (?, ?)`,
		sessionID, float64(at.UnixNano())/float64(time.Second)); err != nil {
		t.Fatal(err)
	}
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

func commandCodeSession(sessionID, cwd string) string {
	return `{"type":"session","id":"` + sessionID + `","cwd":"` + cwd +
		`","timestamp":"2026-08-22T19:22:37.338Z","version":3}` + "\n" +
		`{"role":"user","content":[{"type":"text","text":"hi"}]}` + "\n"
}

// A forked transcript keeps the parent's id and mints its own under
// sessionId, the shape cmd 1.32.2 writes for `--fork-session`.
func commandCodeForkSession(parentID, sessionID, cwd string) string {
	return `{"type":"session","version":3,"id":"` + parentID +
		`","timestamp":"2026-08-25T17:03:30.026Z","cwd":"` + cwd +
		`","sessionId":"` + sessionID + `"}` + "\n" +
		`{"role":"user","content":[{"type":"text","text":"hi"}]}` + "\n"
}

func TestCaptureCommandCodeForkPrefersSessionID(t *testing.T) {
	root := t.TempDir()
	launch := time.Now()
	writeFile(t, filepath.Join(root, "a", "fork.jsonl"),
		commandCodeForkSession("parent-uuid", "fork-uuid", "/repo"), launch.Add(time.Second))

	id, ok := captureCommandCode(root, "/repo", launch, map[string]bool{"parent-uuid": true})
	if !ok || id != "fork-uuid" {
		t.Fatalf("got id=%q ok=%v, want fork-uuid true", id, ok)
	}
}

func TestCaptureCommandCodePicksSessionAfterLaunchInCwd(t *testing.T) {
	root := t.TempDir()
	launch := time.Now()
	// An older conversation in the same cwd predates the launch: not ours.
	writeFile(t, filepath.Join(root, "old-project", "old.jsonl"),
		commandCodeSession("old-uuid", "/repo"), launch.Add(-time.Hour))
	// A conversation in a different cwd started after launch: not ours.
	writeFile(t, filepath.Join(root, "other-project", "other.jsonl"),
		commandCodeSession("other-uuid", "/elsewhere"), launch.Add(time.Second))
	// Ours: same cwd, written just after launch.
	writeFile(t, filepath.Join(root, "repo-project", "ours.jsonl"),
		commandCodeSession("ours-uuid", "/repo"), launch.Add(2*time.Second))

	id, ok := captureCommandCode(root, "/repo", launch, map[string]bool{})
	if !ok || id != "ours-uuid" {
		t.Fatalf("got id=%q ok=%v, want ours-uuid true", id, ok)
	}
}

func TestCaptureCommandCodeSkipsClaimed(t *testing.T) {
	root := t.TempDir()
	launch := time.Now()
	writeFile(t, filepath.Join(root, "a", "1.jsonl"),
		commandCodeSession("first-uuid", "/repo"), launch.Add(time.Second))
	writeFile(t, filepath.Join(root, "a", "2.jsonl"),
		commandCodeSession("second-uuid", "/repo"), launch.Add(2*time.Second))

	// first-uuid already belongs to another session, so the earliest
	// unclaimed match wins instead.
	id, ok := captureCommandCode(root, "/repo", launch, map[string]bool{"first-uuid": true})
	if !ok || id != "second-uuid" {
		t.Fatalf("got id=%q ok=%v, want second-uuid true", id, ok)
	}
}

func TestCaptureCommandCodeNoMatch(t *testing.T) {
	root := t.TempDir()
	launch := time.Now()
	writeFile(t, filepath.Join(root, "a", "1.jsonl"),
		commandCodeSession("x", "/other"), launch.Add(time.Second))
	if id, ok := captureCommandCode(root, "/repo", launch, map[string]bool{}); ok {
		t.Fatalf("expected no match, got %q", id)
	}
}

func TestCaptureCommandCodeSkipsMalformedTranscripts(t *testing.T) {
	root := t.TempDir()
	launch := time.Now()
	// A corrupt first line does not parse: not ours.
	writeFile(t, filepath.Join(root, "a", "broken.jsonl"),
		`{"type":"session","id":`+"\n", launch.Add(time.Second))
	// A record that is not a session header does not count either.
	writeFile(t, filepath.Join(root, "a", "legacy.jsonl"),
		`{"type":"message","id":"legacy-uuid","cwd":"/repo"}`+"\n", launch.Add(2*time.Second))
	// Ours: a valid session record written after launch.
	writeFile(t, filepath.Join(root, "a", "ours.jsonl"),
		commandCodeSession("ours-uuid", "/repo"), launch.Add(3*time.Second))

	id, ok := captureCommandCode(root, "/repo", launch, map[string]bool{})
	if !ok || id != "ours-uuid" {
		t.Fatalf("got id=%q ok=%v, want ours-uuid true", id, ok)
	}
}

type ocMeta struct {
	dir     string
	created time.Time
	updated time.Time
}

// stubOpencode replaces the opencode CLI seams with in-memory data for the
// duration of a test and returns a restore function.
func stubOpencode(t *testing.T, ids []string, metas map[string]ocMeta) {
	t.Helper()
	listSaved, metaSaved := opencodeListIDs, opencodeSessionMeta
	opencodeListIDs = func(string) ([]string, bool) { return ids, true }
	opencodeSessionMeta = func(_, id string) (string, time.Time, time.Time, bool) {
		m, ok := metas[id]
		// A conversation never reopened keeps its creation time as its
		// update time, so capture tests need no updated field.
		if m.updated.IsZero() {
			m.updated = m.created
		}
		return m.dir, m.created, m.updated, ok
	}
	t.Cleanup(func() { opencodeListIDs, opencodeSessionMeta = listSaved, metaSaved })
}

// stubOpencodeJSON substitutes the `session list --format json` output
// snapshot and recapture read.
func stubOpencodeJSON(t *testing.T, entries []opencodeListEntry) {
	t.Helper()
	saved := opencodeSessionListJSON
	opencodeSessionListJSON = func(string) ([]opencodeListEntry, bool) { return entries, true }
	t.Cleanup(func() { opencodeSessionListJSON = saved })
}

func TestCaptureOpencodePicksSessionAfterLaunchInCwd(t *testing.T) {
	launch := time.Now()
	stubOpencode(t, []string{"ses_ours", "ses_other", "ses_old"}, map[string]ocMeta{
		// An older conversation in the same cwd predates the launch: not ours.
		"ses_old": {dir: "/repo", created: launch.Add(-time.Hour)},
		// A conversation in a different cwd started after launch: not ours.
		"ses_other": {dir: "/elsewhere", created: launch.Add(time.Second)},
		// Ours: same cwd, created just after launch.
		"ses_ours": {dir: "/repo", created: launch.Add(2 * time.Second)},
	})

	id, ok := captureOpencode("/repo", launch, map[string]bool{})
	if !ok || id != "ses_ours" {
		t.Fatalf("got id=%q ok=%v, want ses_ours true", id, ok)
	}
}

func TestCaptureOpencodeSkipsClaimed(t *testing.T) {
	launch := time.Now()
	stubOpencode(t, []string{"ses_1", "ses_2"}, map[string]ocMeta{
		"ses_1": {dir: "/repo", created: launch.Add(time.Second)},
		"ses_2": {dir: "/repo", created: launch.Add(2 * time.Second)},
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
		`{"info":{"id":"ses_x","directory":"/repo","time":{"created":1784385368000,"updated":1784388968000}}}`)
	dir, created, updated, ok := parseOpencodeExport(out)
	if !ok || dir != "/repo" || created.UnixMilli() != 1784385368000 || updated.UnixMilli() != 1784388968000 {
		t.Fatalf("got dir=%q created=%v updated=%v ok=%v", dir, created, updated, ok)
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
		hermesRow{id: "old", source: "cli", cwd: "/repo", started: launch.Add(-time.Hour)},
		hermesRow{id: "just-before", source: "cli", cwd: "/repo", started: launch.Add(-time.Second)},
		hermesRow{id: "other", source: "cli", cwd: "/elsewhere", started: launch.Add(time.Second)},
		hermesRow{id: "tui", source: "tui", cwd: "/repo", started: launch.Add(time.Second)},
		hermesRow{id: "ours", source: "cli", cwd: "/repo", started: launch.Add(2 * time.Second)},
	)

	id, ok := captureHermes(path, "/repo", launch, map[string]bool{})
	if !ok || id != "ours" {
		t.Fatalf("got id=%q ok=%v, want ours true", id, ok)
	}
}

func TestCaptureHermesSkipsClaimed(t *testing.T) {
	launch := time.Now()
	path := writeHermesStore(t,
		hermesRow{id: "first", source: "cli", cwd: "/repo", started: launch.Add(time.Second)},
		hermesRow{id: "second", source: "cli", cwd: "/repo", started: launch.Add(2 * time.Second)},
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

// Recapture returns the conversation a resumed session picked, and only
// when exactly one store entry answers the relaunch: a resumed session
// replays an existing conversation rather than minting one, so a shared cwd
// can hold several touched entries and none of them may be guessed from.

func TestRecaptureCodexBindsOnlyWhatOutranTheSnapshot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	base := time.Now().Add(-time.Hour)
	// Written one second before the relaunch: what the snapshot sees, and
	// the exact shape of the stale-binding bug — it must not bind untouched.
	writeFile(t, filepath.Join(root, "sessions", "2026/07/18/rollout-pre.jsonl"),
		codexRollout("pre-uuid", "/repo"), base)
	snapshot, ok := Snapshot("codex", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	if id, ok := Recapture("codex", "/repo", snapshot, map[string]bool{}); ok {
		t.Fatalf("a conversation that merely predates the relaunch must not bind, got %q", id)
	}
	// The picker's choice turns again: its rollout outruns the snapshot.
	writeFile(t, filepath.Join(root, "sessions", "2026/07/18/rollout-pre.jsonl"),
		codexRollout("pre-uuid", "/repo"), base.Add(10*time.Second))
	id, ok := Recapture("codex", "/repo", snapshot, map[string]bool{})
	if !ok || id != "pre-uuid" {
		t.Fatalf("got id=%q ok=%v, want pre-uuid true", id, ok)
	}
}

func TestRecaptureCodexMintedAfterSnapshotBinds(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	base := time.Now().Add(-time.Hour)
	writeFile(t, filepath.Join(root, "sessions", "a/rollout-old.jsonl"),
		codexRollout("old-uuid", "/repo"), base)
	snapshot, ok := Snapshot("codex", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	// A restart mints a fresh conversation: unseen by the snapshot, it
	// qualifies without needing an advance.
	writeFile(t, filepath.Join(root, "sessions", "a/rollout-new.jsonl"),
		codexRollout("new-uuid", "/repo"), base.Add(time.Second))
	id, ok := Recapture("codex", "/repo", snapshot, map[string]bool{})
	if !ok || id != "new-uuid" {
		t.Fatalf("got id=%q ok=%v, want new-uuid true", id, ok)
	}
}

func TestRecaptureCodexRefusesTwoThatOutranTheSnapshot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	base := time.Now().Add(-time.Hour)
	writeFile(t, filepath.Join(root, "sessions", "a/rollout-1.jsonl"),
		codexRollout("first-uuid", "/repo"), base)
	writeFile(t, filepath.Join(root, "sessions", "a/rollout-2.jsonl"),
		codexRollout("second-uuid", "/repo"), base)
	snapshot, ok := Snapshot("codex", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	writeFile(t, filepath.Join(root, "sessions", "a/rollout-1.jsonl"),
		codexRollout("first-uuid", "/repo"), base.Add(10*time.Second))
	writeFile(t, filepath.Join(root, "sessions", "a/rollout-2.jsonl"),
		codexRollout("second-uuid", "/repo"), base.Add(20*time.Second))

	if id, ok := Recapture("codex", "/repo", snapshot, map[string]bool{}); ok {
		t.Fatalf("expected no match for two candidates, got %q", id)
	}
}

func TestRecaptureRefusesWithoutSnapshot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	writeFile(t, filepath.Join(root, "sessions", "a/rollout-1.jsonl"),
		codexRollout("some-uuid", "/repo"), time.Now())
	// A nil snapshot means the relaunch predates snapshot capture; recapture
	// must refuse rather than guess from a bare cutoff.
	if id, ok := Recapture("codex", "/repo", nil, map[string]bool{}); ok {
		t.Fatalf("expected no match without a snapshot, got %q", id)
	}
}

func TestSnapshotCodexRecordsEachCwdConversation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	// Whole-second base, so the filesystem's own mtime granularity cannot
	// round the value the assertion compares against.
	base := time.Now().Add(-time.Hour).Truncate(time.Second)
	writeFile(t, filepath.Join(root, "sessions", "a/rollout-1.jsonl"),
		codexRollout("first-uuid", "/repo"), base)
	writeFile(t, filepath.Join(root, "sessions", "a/rollout-2.jsonl"),
		codexRollout("second-uuid", "/other"), base.Add(time.Second))

	snapshot, ok := Snapshot("codex", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	if len(snapshot) != 1 || snapshot["first-uuid"] != base.UnixNano() {
		t.Fatalf("got %v, want only first-uuid at %d", snapshot, base.UnixNano())
	}
}

// An empty store is a real pre-launch state, not a failure: any conversation
// that appears after it qualifies.
func TestRecaptureBindsAfterAnEmptySnapshot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	snapshot := map[string]int64{}
	writeFile(t, filepath.Join(root, "sessions", "a/rollout-1.jsonl"),
		codexRollout("fresh-uuid", "/repo"), time.Now())

	id, ok := Recapture("codex", "/repo", snapshot, map[string]bool{})
	if !ok || id != "fresh-uuid" {
		t.Fatalf("got id=%q ok=%v, want fresh-uuid true", id, ok)
	}
}

func TestRecaptureCommandCodeBindsOnlyWhatOutranTheSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := commandCodeRoot()
	base := time.Now().Add(-time.Hour)
	writeFile(t, filepath.Join(root, "old-project", "old.jsonl"),
		commandCodeSession("old-uuid", "/repo"), base)
	snapshot, ok := Snapshot("command-code", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	if id, ok := Recapture("command-code", "/repo", snapshot, map[string]bool{}); ok {
		t.Fatalf("a conversation that merely predates the relaunch must not bind, got %q", id)
	}
	// The picked conversation's transcript appends on the resumed turn.
	writeFile(t, filepath.Join(root, "old-project", "old.jsonl"),
		commandCodeSession("old-uuid", "/repo"), base.Add(10*time.Second))
	id, ok := Recapture("command-code", "/repo", snapshot, map[string]bool{})
	if !ok || id != "old-uuid" {
		t.Fatalf("got id=%q ok=%v, want old-uuid true", id, ok)
	}
}

func TestRecaptureCommandCodeRefusesTwoThatOutranTheSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := commandCodeRoot()
	base := time.Now().Add(-time.Hour)
	writeFile(t, filepath.Join(root, "a", "1.jsonl"),
		commandCodeSession("first-uuid", "/repo"), base)
	writeFile(t, filepath.Join(root, "a", "2.jsonl"),
		commandCodeSession("second-uuid", "/repo"), base)
	snapshot, ok := Snapshot("command-code", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	writeFile(t, filepath.Join(root, "a", "1.jsonl"),
		commandCodeSession("first-uuid", "/repo"), base.Add(10*time.Second))
	writeFile(t, filepath.Join(root, "a", "2.jsonl"),
		commandCodeSession("second-uuid", "/repo"), base.Add(20*time.Second))

	if id, ok := Recapture("command-code", "/repo", snapshot, map[string]bool{}); ok {
		t.Fatalf("expected no match for two candidates, got %q", id)
	}
}

func TestRecaptureGeminiBindsOnlyWhatOutranTheSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := geminiRoot()
	base := time.Now().Add(-time.Hour)
	oursHash := geminiProjectHash("/repo")
	otherHash := geminiProjectHash("/elsewhere")
	writeFile(t, filepath.Join(root, "proj/chats/session-1-old.jsonl"),
		geminiSessionFixture("old-uuid", oursHash), base)
	writeFile(t, filepath.Join(root, "proj/chats/session-2-other.jsonl"),
		geminiSessionFixture("other-uuid", otherHash), base)
	snapshot, ok := Snapshot("gemini", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	if id, ok := Recapture("gemini", "/repo", snapshot, map[string]bool{}); ok {
		t.Fatalf("a conversation that merely predates the relaunch must not bind, got %q", id)
	}
	writeFile(t, filepath.Join(root, "proj/chats/session-1-old.jsonl"),
		geminiSessionFixture("old-uuid", oursHash), base.Add(10*time.Second))
	id, ok := Recapture("gemini", "/repo", snapshot, map[string]bool{})
	if !ok || id != "old-uuid" {
		t.Fatalf("got id=%q ok=%v, want old-uuid true", id, ok)
	}
}

func TestRecaptureGeminiRefusesTwoThatOutranTheSnapshot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := geminiRoot()
	base := time.Now().Add(-time.Hour)
	hash := geminiProjectHash("/repo")
	writeFile(t, filepath.Join(root, "p/chats/session-1.jsonl"),
		geminiSessionFixture("first-uuid", hash), base)
	writeFile(t, filepath.Join(root, "p/chats/session-2.jsonl"),
		geminiSessionFixture("second-uuid", hash), base)
	snapshot, ok := Snapshot("gemini", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	writeFile(t, filepath.Join(root, "p/chats/session-1.jsonl"),
		geminiSessionFixture("first-uuid", hash), base.Add(10*time.Second))
	writeFile(t, filepath.Join(root, "p/chats/session-2.jsonl"),
		geminiSessionFixture("second-uuid", hash), base.Add(20*time.Second))

	if id, ok := Recapture("gemini", "/repo", snapshot, map[string]bool{}); ok {
		t.Fatalf("expected no match for two candidates, got %q", id)
	}
}

func TestSnapshotOpencodeRecordsTheListUpdateTimes(t *testing.T) {
	stubOpencodeJSON(t, []opencodeListEntry{
		{ID: "ses_ours", Directory: "/repo", Updated: 3000},
		{ID: "ses_other", Directory: "/elsewhere", Updated: 2000},
	})
	snapshot, ok := Snapshot("opencode", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	if len(snapshot) != 1 || snapshot["ses_ours"] != 3000*int64(time.Millisecond) {
		t.Fatalf("got %v, want only ses_ours at %d", snapshot, 3000*int64(time.Millisecond))
	}
}

func TestRecaptureOpencodeBindsOnlyWhatOutranTheSnapshot(t *testing.T) {
	stubOpencodeJSON(t, []opencodeListEntry{
		{ID: "ses_ours", Directory: "/repo", Updated: 1000},
		{ID: "ses_other", Directory: "/elsewhere", Updated: 2000},
	})
	snapshot, ok := Snapshot("opencode", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	if id, ok := Recapture("opencode", "/repo", snapshot, map[string]bool{}); ok {
		t.Fatalf("a conversation that merely predates the relaunch must not bind, got %q", id)
	}
	// Picking the conversation reopens it: info.time.updated advances.
	stubOpencodeJSON(t, []opencodeListEntry{
		{ID: "ses_ours", Directory: "/repo", Updated: 3000},
		{ID: "ses_other", Directory: "/elsewhere", Updated: 2000},
	})
	id, ok := Recapture("opencode", "/repo", snapshot, map[string]bool{})
	if !ok || id != "ses_ours" {
		t.Fatalf("got id=%q ok=%v, want ses_ours true", id, ok)
	}
}

func TestRecaptureOpencodeRefusesTwoThatOutranTheSnapshot(t *testing.T) {
	stubOpencodeJSON(t, []opencodeListEntry{
		{ID: "ses_1", Directory: "/repo", Updated: 1000},
		{ID: "ses_2", Directory: "/repo", Updated: 1000},
	})
	snapshot, ok := Snapshot("opencode", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	stubOpencodeJSON(t, []opencodeListEntry{
		{ID: "ses_1", Directory: "/repo", Updated: 3000},
		{ID: "ses_2", Directory: "/repo", Updated: 4000},
	})
	if id, ok := Recapture("opencode", "/repo", snapshot, map[string]bool{}); ok {
		t.Fatalf("expected no match for two resumed conversations, got %q", id)
	}
}

// hermes's post-resume signal is its activity columns, not a file mtime.
func TestRecaptureHermesBindsTheConversationThatTurnedAgain(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	path := writeHermesStore(t,
		hermesRow{id: "old", source: "cli", cwd: "/repo", started: base},
	)
	t.Setenv("HERMES_HOME", filepath.Dir(path))
	snapshot, ok := Snapshot("hermes", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	if id, ok := Recapture("hermes", "/repo", snapshot, map[string]bool{}); ok {
		t.Fatalf("an untouched conversation must not bind, got %q", id)
	}
	// The resumed session's first turn stamps the activity heartbeat.
	setHermesActivity(t, path, "old", base.Add(10*time.Second))
	id, ok := Recapture("hermes", "/repo", snapshot, map[string]bool{})
	if !ok || id != "old" {
		t.Fatalf("got id=%q ok=%v, want old true", id, ok)
	}
}

func TestRecaptureHermesRefusesTwoThatTurnedAgain(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	path := writeHermesStore(t,
		hermesRow{id: "first", source: "cli", cwd: "/repo", started: base},
		hermesRow{id: "second", source: "cli", cwd: "/repo", started: base},
	)
	t.Setenv("HERMES_HOME", filepath.Dir(path))
	snapshot, ok := Snapshot("hermes", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	setHermesActivity(t, path, "first", base.Add(10*time.Second))
	setHermesActivity(t, path, "second", base.Add(20*time.Second))

	if id, ok := Recapture("hermes", "/repo", snapshot, map[string]bool{}); ok {
		t.Fatalf("expected no match for two candidates, got %q", id)
	}
}

// A session whose heartbeat never stamped still signals its resumed turn
// through a fresh message row, which the COALESCE picks over started_at.
func TestRecaptureHermesBindsOnANewMessageWithoutAHeartbeat(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	path := writeHermesStore(t,
		hermesRow{id: "old", source: "cli", cwd: "/repo", started: base},
	)
	t.Setenv("HERMES_HOME", filepath.Dir(path))
	snapshot, ok := Snapshot("hermes", "/repo")
	if !ok {
		t.Fatal("snapshot failed")
	}
	if id, ok := Recapture("hermes", "/repo", snapshot, map[string]bool{}); ok {
		t.Fatalf("an untouched conversation must not bind, got %q", id)
	}
	addHermesMessage(t, path, "old", base.Add(10*time.Second))
	id, ok := Recapture("hermes", "/repo", snapshot, map[string]bool{})
	if !ok || id != "old" {
		t.Fatalf("got id=%q ok=%v, want old true", id, ok)
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
