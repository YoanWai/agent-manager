package agentsession

import (
	"errors"
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

func TestSavedFindsAConversationOnlyWhereTheToolKeepsIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"CLAUDE_CONFIG_DIR", "GROK_HOME", "PI_CODING_AGENT_DIR", "PI_CODING_AGENT_SESSION_DIR", "CODEX_HOME"} {
		t.Setenv(name, "")
	}
	cwd, elsewhere := resolvePath(t.TempDir()), resolvePath(t.TempDir())
	claudeDir, grokDir, piDir, codexDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(claudeDir, "projects", claudeProjectSlug(cwd), "conv-claude.jsonl"), "{}\n", time.Now())
	writeFile(t, filepath.Join(grokDir, "sessions", url.QueryEscape(cwd), "conv-grok", "summary.json"), `{"info":{"id":"conv-grok","cwd":"`+cwd+`"}}`, time.Now())
	writeFile(t, filepath.Join(piDir, "sessions", piCwdKey(cwd), "2026-10-29T00-00-00_conv-pi.jsonl"), "{}\n", time.Now())
	writeFile(t, filepath.Join(codexDir, "sessions", "rollout-1.jsonl"), `{"type":"session_meta","payload":{"session_id":"conv-codex","cwd":"`+cwd+`"}}`+"\n", time.Now())
	env := Env{"CLAUDE_CONFIG_DIR": claudeDir, "GROK_HOME": grokDir, "PI_CODING_AGENT_DIR": piDir, "CODEX_HOME": codexDir}

	for _, tc := range []struct{ store, report, id string }{
		{"", "claude", "conv-claude"},
		{"", "grok", "conv-grok"},
		{"", "pi", "conv-pi"},
		{"codex", "codex", "conv-codex"},
	} {
		if saved, err := Saved(tc.store, tc.report, cwd, tc.id, env); err != nil || !saved {
			t.Errorf("%s in its own directory = %v %v, want saved", tc.id, saved, err)
		}
		if saved, err := Saved(tc.store, tc.report, elsewhere, tc.id, env); err != nil || saved {
			t.Errorf("%s in another directory = %v %v, want not saved", tc.id, saved, err)
		}
		if saved, err := Saved(tc.store, tc.report, cwd, "conv-missing", env); err != nil || saved {
			t.Errorf("a missing %s conversation = %v %v, want not saved", tc.report, saved, err)
		}
		if saved, _ := Saved(tc.store, tc.report, cwd, tc.id, nil); saved {
			t.Errorf("%s found without the launch environment that points at its store", tc.id)
		}
	}
	if _, err := Saved("", "none", cwd, "conv-1", env); !errors.Is(err, ErrNoStore) {
		t.Fatalf("a tool with no store reader = %v, want ErrNoStore", err)
	}
}
