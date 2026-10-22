//go:build !windows

package conversation

import (
	"path/filepath"
	"testing"
	"time"
)

// Windows has no /proc, so no process's open files are listed there.
// Muse holds its current session's registry file or session lock open, and
// Antigravity its conversation's presence lock, so the one such file the
// agent holds names its conversation.
func TestHeldConversationNamesTheOneFileTheAgentHolds(t *testing.T) {
	root := t.TempDir()
	holdOpen(t, filepath.Join(root, "muse", "sessions", "01a1183b-cbc5.json"))
	holdOpen(t, filepath.Join(root, "antigravity-cli", "presence", "93f26d7d-480a.lock"))
	holdOpen(t, filepath.Join(root, "antigravity-cli", "conversations", "70b8e342.db"))
	var reader Reader
	for style, want := range map[string]string{"muse": "01a1183b-cbc5", "antigravity": "93f26d7d-480a"} {
		id, err := reader.Current(style, recordedNow(), time.Time{})
		if err != nil || id != want {
			t.Fatalf("%s = %q, %v; want %q", style, id, err, want)
		}
	}

	second := holdOpen(t, filepath.Join(root, "muse", "sessions", "01a1183b-4d90.json"))
	if id, err := reader.Current("muse", recordedNow(), time.Time{}); err != nil || id != "" {
		t.Fatalf("two held sessions = %q, %v; want none named", id, err)
	}
	second.Close()
	if id, err := reader.Current("muse", recordedNow(), time.Time{}); err != nil || id != "01a1183b-cbc5" {
		t.Fatalf("after closing one = %q, %v", id, err)
	}
	holdOpen(t, filepath.Join(root, "share", "muse", "sessions", "2026", "10", "08", "01a1183b-cbc5", ".session.lock"))
	if id, err := reader.Current("muse", recordedNow(), time.Time{}); err != nil || id != "01a1183b-cbc5" {
		t.Fatalf("registry entry and session lock of one session = %q, %v", id, err)
	}
}
func TestHeldConversationReadsMusesSessionLock(t *testing.T) {
	holdOpen(t, filepath.Join(t.TempDir(), "muse", "sessions", "2026", "10", "08", "01a11886-aac3", ".session.lock"))
	var reader Reader
	if id, err := reader.Current("muse", recordedNow(), time.Time{}); err != nil || id != "01a11886-aac3" {
		t.Fatalf("session lock = %q, %v", id, err)
	}
}
