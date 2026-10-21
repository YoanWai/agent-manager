package agentsession

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeCursorChat(t *testing.T, root, hashedPath, id, metaCwd string, created, updated int64) {
	t.Helper()
	dir := filepath.Join(root, "chats", cursorChatBucket(hashedPath), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	meta := cursorChatMeta{CreatedAtMs: created, UpdatedAtMs: updated, Cwd: metaCwd}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureCursorReadsTheChatCreatedAfterLaunch(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CURSOR_CONFIG_DIR", root)
	workspace := t.TempDir()
	launched := time.UnixMilli(1_700_000_000_000)
	oldID := "11111111-1111-4111-8111-111111111111"
	newID := "22222222-2222-4222-8222-222222222222"
	writeCursorChat(t, root, workspace, oldID, workspace, launched.Add(-time.Minute).UnixMilli(), launched.UnixMilli())
	writeCursorChat(t, root, workspace, newID, workspace, launched.Add(time.Second).UnixMilli(), launched.Add(2*time.Second).UnixMilli())
	writeCursorChat(t, root, workspace, "33333333-3333-4333-8333-333333333333", filepath.Join(workspace, "other"), launched.Add(time.Second).UnixMilli(), launched.Add(time.Second).UnixMilli())

	id, ok := Capture("cursor", workspace, launched, nil, "")
	if !ok || id != newID {
		t.Fatalf("Capture = %q %v, want %s", id, ok, newID)
	}
	id, ok = Capture("cursor", workspace, launched, map[string]bool{newID: true}, "")
	if ok {
		t.Fatalf("Capture of a claimed chat = %q", id)
	}
}

func TestCaptureCursorFindsAChatStoredUnderTheRealPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CURSOR_CONFIG_DIR", root)
	real := filepath.Join(t.TempDir(), "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved := resolvePath(link)
	id := "44444444-4444-4444-8444-444444444444"
	created := time.Now().UnixMilli()
	writeCursorChat(t, root, resolved, id, resolved, created, created)

	got, ok := Capture("cursor", link, time.UnixMilli(created-1000), nil, "")
	if !ok || got != id {
		t.Fatalf("Capture through the link = %q %v, want %s (resolved %s)", got, ok, id, resolved)
	}
}

func TestCaptureCursorWaitsUntilMetaExists(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CURSOR_CONFIG_DIR", root)
	workspace := t.TempDir()
	id := "55555555-5555-4555-8555-555555555555"
	dir := filepath.Join(root, "chats", cursorChatBucket(workspace), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := Capture("cursor", workspace, time.Now().Add(-time.Minute), nil, ""); ok {
		t.Fatal("Capture succeeded with no meta.json")
	}
}

func TestRecaptureCursorBindsTheChatThatMoved(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CURSOR_CONFIG_DIR", root)
	workspace := t.TempDir()
	kept := "66666666-6666-4666-8666-666666666666"
	moved := "77777777-7777-4777-8777-777777777777"
	writeCursorChat(t, root, workspace, kept, workspace, 1_000, 2_000)
	writeCursorChat(t, root, workspace, moved, workspace, 1_000, 2_000)
	snapshot, ok := Snapshot("cursor", workspace)
	if !ok {
		t.Fatal("Snapshot failed")
	}
	writeCursorChat(t, root, workspace, moved, workspace, 1_000, 3_000)
	id, ok := Recapture("cursor", workspace, snapshot, nil)
	if !ok || id != moved {
		t.Fatalf("Recapture = %q %v, want %s", id, ok, moved)
	}
	writeCursorChat(t, root, workspace, kept, workspace, 1_000, 4_000)
	if _, ok := Recapture("cursor", workspace, snapshot, nil); ok {
		t.Fatal("Recapture bound a chat when two had moved")
	}
}

func TestCursorConfigDirFollowsTheEnvThenXDGThenHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CURSOR_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if got, want := cursorConfigDir(), filepath.Join(home, ".cursor"); got != want {
		t.Fatalf("config dir = %q, want %q", got, want)
	}
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if got, want := cursorConfigDir(), filepath.Join(xdg, "cursor"); got != want {
		t.Fatalf("config dir = %q, want %q", got, want)
	}
	explicit := t.TempDir()
	t.Setenv("CURSOR_CONFIG_DIR", "  "+explicit+"  ")
	if got := cursorConfigDir(); got != explicit {
		t.Fatalf("config dir = %q, want %q", got, explicit)
	}
}

func TestCaptureCursorSkipsMalformedMetadata(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	t.Setenv("CURSOR_CONFIG_DIR", root)
	now := time.Now()
	good := "11111111-1111-4111-8111-111111111111"
	bad := "22222222-2222-4222-8222-222222222222"
	writeCursorChat(t, root, cwd, good, cwd, now.UnixMilli(), now.UnixMilli())
	writeCursorChat(t, root, cwd, bad, cwd, now.UnixMilli(), now.UnixMilli())
	for _, data := range []string{`{`, `{"createdAtMs":"broken"}`} {
		if err := os.WriteFile(filepath.Join(root, "chats", cursorChatBucket(cwd), bad, "meta.json"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if id, ok := Capture("cursor", cwd, now, nil, ""); !ok || id != good {
			t.Fatalf("malformed sibling blocked capture. Got %q %v", id, ok)
		}
		if ids, ok := Snapshot("cursor", cwd); !ok || len(ids) != 1 {
			t.Fatalf("malformed sibling blocked snapshot. Got %v %v", ids, ok)
		}
	}
}
