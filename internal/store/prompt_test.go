package store

import (
	"testing"
	"time"
)

func TestPromptReceiptCannotCrossRelaunchOrSocketChange(t *testing.T) {
	for _, change := range []string{"relaunch", "socket"} {
		t.Run(change, func(t *testing.T) {
			st := newTestStore(t)
			if err := st.CreateSession(Session{ID: "target", Tool: "claude", Name: "target", CreatedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			original, err := st.Get("target")
			if err != nil {
				t.Fatal(err)
			}
			if err := st.SetLastPromptForLaunch(original, "first"); err != nil {
				t.Fatal(err)
			}
			if change == "relaunch" {
				if err := st.SetAgentLaunchedAt("target", time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := st.SetTmuxSocket("target", "replacement"); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.SetLastPromptForLaunch(original, "stale"); err == nil {
				t.Fatal("stale prompt receipt accepted")
			}
			current, err := st.Get("target")
			if err != nil {
				t.Fatal(err)
			}
			if current.LastPrompt != "first" {
				t.Fatalf("prompt overwritten: %q", current.LastPrompt)
			}
		})
	}
}

func TestPromptReceiptAcceptsLegacySecondPrecisionLaunch(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(Session{ID: "legacy", Tool: "claude", Name: "legacy"}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Unix()
	if _, err := st.db.Exec("UPDATE sessions SET created_at=?,agent_launched_at=? WHERE id='legacy'", old, old+1); err != nil {
		t.Fatal(err)
	}
	expected, err := st.Get("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetLastPromptForLaunch(expected, "legacy draft"); err != nil {
		t.Fatalf("compatible launch rejected: %v", err)
	}
}

func TestQuickSendReceiptUpdatesMetadataForSameIncarnation(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(Session{
		ID: "target", Tool: "claude", Name: "target", CreatedAt: time.Now(), TmuxSocket: "socket-a",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAcked("target", true); err != nil {
		t.Fatal(err)
	}
	expected, err := st.Get("target")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordQuickSendForLaunch(expected, "continue with the migration"); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get("target")
	if err != nil {
		t.Fatal(err)
	}
	if got.Acked {
		t.Fatal("quick-send receipt left the previous finished alert acknowledged")
	}
	if got.LastPrompt != "continue with the migration" {
		t.Fatalf("last prompt = %q", got.LastPrompt)
	}
}

func TestQuickSendReceiptCannotCrossIncarnationChange(t *testing.T) {
	for _, change := range []string{"creation", "relaunch", "socket"} {
		t.Run(change, func(t *testing.T) {
			st := newTestStore(t)
			if err := st.CreateSession(Session{
				ID: "target", Tool: "claude", Name: "target", CreatedAt: time.Now(), TmuxSocket: "socket-a",
			}); err != nil {
				t.Fatal(err)
			}
			if err := st.SetAcked("target", true); err != nil {
				t.Fatal(err)
			}
			if err := st.SetLastPrompt("target", "previous"); err != nil {
				t.Fatal(err)
			}
			expected, err := st.Get("target")
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "creation":
				if _, err := st.db.Exec(`UPDATE sessions SET created_at = ? WHERE id = ?`,
					encodeTime(expected.CreatedAt.Add(time.Second)), expected.ID); err != nil {
					t.Fatal(err)
				}
			case "relaunch":
				if err := st.SetAgentLaunchedAt("target", time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			case "socket":
				if err := st.SetTmuxSocket("target", "socket-b"); err != nil {
					t.Fatal(err)
				}
			}

			if err := st.RecordQuickSendForLaunch(expected, "stale"); err == nil {
				t.Fatal("stale quick-send receipt accepted")
			}
			current, err := st.Get("target")
			if err != nil {
				t.Fatal(err)
			}
			if !current.Acked || current.LastPrompt != "previous" {
				t.Fatalf("stale receipt mutated metadata: acked=%v prompt=%q", current.Acked, current.LastPrompt)
			}
		})
	}
}

func TestQuickSendReceiptAcceptsLegacySecondPrecisionIncarnation(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(Session{ID: "legacy", Tool: "claude", Name: "legacy"}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Unix()
	if _, err := st.db.Exec(
		`UPDATE sessions SET created_at = ?, agent_launched_at = ?, acked = 1 WHERE id = 'legacy'`, old, old+1,
	); err != nil {
		t.Fatal(err)
	}
	expected, err := st.Get("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordQuickSendForLaunch(expected, "legacy send"); err != nil {
		t.Fatalf("compatible incarnation rejected: %v", err)
	}
	got, err := st.Get("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if got.Acked || got.LastPrompt != "legacy send" {
		t.Fatalf("legacy receipt = acked %v, prompt %q", got.Acked, got.LastPrompt)
	}
}
