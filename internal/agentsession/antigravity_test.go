package agentsession

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type agyConversation struct {
	id, workspaces, parent string
	modified               time.Time
}

// writeAntigravityStore lays out agy 1.2.14's store under a fake home: the
// summaries table as agy creates it, and its map from each directory to the
// conversation last started there.
func writeAntigravityStore(t *testing.T, lastStarted string, conversations ...agyConversation) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := antigravityRoot()
	if err := os.MkdirAll(filepath.Join(root, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if lastStarted != "" {
		if err := os.WriteFile(filepath.Join(root, "cache", "last_conversations.json"), []byte(lastStarted), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "conversation_summaries.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "CREATE TABLE `conversation_summaries` (`conversation_id` text,`title` text NOT NULL DEFAULT \"\",`preview` text NOT NULL DEFAULT \"\",`step_count` integer NOT NULL DEFAULT 0,`last_modified_time` datetime NOT NULL,`workspace_uris` text NOT NULL,`status` text NOT NULL DEFAULT \"\",`source` text NOT NULL DEFAULT \"\",`project_id` text NOT NULL DEFAULT \"\",`agent_name` text NOT NULL DEFAULT \"\",`parent_conversation_id` text NOT NULL DEFAULT \"\",`nesting_depth` integer NOT NULL DEFAULT 0,`battle_id` text NOT NULL DEFAULT \"\",`winning_conversation_id` text NOT NULL DEFAULT \"\",`not_fully_idle` numeric NOT NULL DEFAULT false,`killed` numeric NOT NULL DEFAULT false,`last_user_input_time` datetime NOT NULL,`last_user_input_step_index` integer NOT NULL DEFAULT -1,`app_data_dir` text NOT NULL DEFAULT \"\",`raw_summary` blob,`group_id` text NOT NULL DEFAULT \"\",PRIMARY KEY (`conversation_id`))"); err != nil {
		t.Fatal(err)
	}
	for _, c := range conversations {
		modified := "0001-01-01 00:00:00+00:00"
		if !c.modified.IsZero() {
			modified = c.modified.UTC().Format("2006-01-02 15:04:05.999999-07:00")
		}
		depth := 0
		if c.parent != "" {
			depth = 1
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO conversation_summaries (conversation_id, last_modified_time, workspace_uris, parent_conversation_id, nesting_depth, last_user_input_time) VALUES (?, ?, ?, ?, ?, ?)",
			c.id, modified, c.workspaces, c.parent, depth, modified); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAntigravityCaptureReadsTheSummariesIndex(t *testing.T) {
	cwd := t.TempDir()
	other := t.TempDir()
	launched := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	uri := func(dir string) string { return `["file://` + dir + `"]` }
	writeAntigravityStore(t, "",
		agyConversation{id: "before-launch", workspaces: uri(cwd), modified: launched.Add(-time.Hour)},
		agyConversation{id: "first-turn-running", workspaces: uri(cwd)},
		agyConversation{id: "elsewhere", workspaces: uri(other), modified: launched.Add(time.Second)},
		agyConversation{id: "added-dir", workspaces: `["file://` + cwd + `","file://` + other + `"]`, modified: launched.Add(time.Second)},
		agyConversation{id: "subagent", workspaces: uri(cwd), parent: "mine", modified: launched.Add(time.Second)},
		agyConversation{id: "mine", workspaces: uri(cwd), modified: launched.Add(2 * time.Second)},
		agyConversation{id: "sibling", workspaces: uri(cwd), modified: launched.Add(3 * time.Second)},
	)
	if id, ok := Capture("antigravity", cwd, launched, nil); !ok || id != "mine" {
		t.Fatalf("Capture = %q, %v, want the earliest finished turn in cwd", id, ok)
	}
	if id, ok := Capture("antigravity", cwd, launched, map[string]bool{"mine": true}); !ok || id != "sibling" {
		t.Fatalf("Capture with mine claimed = %q, %v", id, ok)
	}
	snapshot, ok := Snapshot("antigravity", cwd)
	if !ok || len(snapshot) != 3 {
		t.Fatalf("Snapshot = %v, %v, want before-launch, mine and sibling", snapshot, ok)
	}
	if id, ok := Recapture("antigravity", cwd, snapshot, nil); ok {
		t.Fatalf("Recapture bound %q with nothing moved since the snapshot", id)
	}
	snapshot["mine"] = launched.UnixNano()
	if id, ok := Recapture("antigravity", cwd, snapshot, nil); !ok || id != "mine" {
		t.Fatalf("Recapture = %q, %v, want the conversation that turned again", id, ok)
	}
}

// A conversation started behind the trust prompt records no workspace; agy's
// own directory map is what ties it to the directory it ran in.
func TestAntigravityCaptureFollowsTheDirectoryMapPastTheTrustPrompt(t *testing.T) {
	cwd := t.TempDir()
	launched := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	writeAntigravityStore(t, `{"`+cwd+`": "trusted-late", "/elsewhere": "other-trusted-late"}`,
		agyConversation{id: "other-trusted-late", modified: launched.Add(time.Second)},
		agyConversation{id: "trusted-late", modified: launched.Add(2 * time.Second)},
	)
	if id, ok := Capture("antigravity", cwd, launched, nil); !ok || id != "trusted-late" {
		t.Fatalf("Capture = %q, %v", id, ok)
	}
}

func TestAntigravityCaptureWithoutAStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if id, ok := Capture("antigravity", t.TempDir(), time.Now(), nil); ok {
		t.Fatalf("captured %q with no agy store", id)
	}
	if _, ok := Snapshot("antigravity", t.TempDir()); ok {
		t.Fatal("snapshot reported a store that does not exist")
	}
}
