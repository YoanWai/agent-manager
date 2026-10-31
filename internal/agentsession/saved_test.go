package agentsession

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	writeFile(t, filepath.Join(grokDir, "sessions", url.QueryEscape(cwd), "conv-grok", "summary.json"), `{"info":{"id":"conv-grok","cwd":`+jsonString(cwd)+`}}`, time.Now())
	writeFile(t, filepath.Join(piDir, "sessions", piCwdKey(cwd), "2026-10-29T00-00-00_conv-pi.jsonl"), "{}\n", time.Now())
	writeFile(t, filepath.Join(codexDir, "sessions", "rollout-1.jsonl"), `{"type":"session_meta","payload":{"session_id":"conv-codex","cwd":`+jsonString(cwd)+`}}`+"\n", time.Now())
	env := Env{"CLAUDE_CONFIG_DIR": claudeDir, "GROK_HOME": grokDir, "PI_CODING_AGENT_DIR": piDir, "CODEX_HOME": codexDir}

	for _, tc := range []struct{ store, report, id string }{
		{"", "claude", "conv-claude"},
		{"", "grok", "conv-grok"},
		{"", "pi", "conv-pi"},
		{"codex", "codex", "conv-codex"},
	} {
		if saved, err := Saved(tc.store, tc.report, tc.id, Launch{Cwd: cwd, Env: env}); err != nil || !saved {
			t.Errorf("%s in its own directory = %v %v, want saved", tc.id, saved, err)
		}
		if saved, err := Saved(tc.store, tc.report, tc.id, Launch{Cwd: elsewhere, Env: env}); err != nil || saved {
			t.Errorf("%s in another directory = %v %v, want not saved", tc.id, saved, err)
		}
		if saved, err := Saved(tc.store, tc.report, "conv-missing", Launch{Cwd: cwd, Env: env}); err != nil || saved {
			t.Errorf("a missing %s conversation = %v %v, want not saved", tc.report, saved, err)
		}
		if saved, _ := Saved(tc.store, tc.report, tc.id, Launch{Cwd: cwd}); saved {
			t.Errorf("%s found without the launch environment that points at its store", tc.id)
		}
	}
	writeFile(t, filepath.Join(grokDir, "sessions", "x", "conv-torn", "summary.json"), "{", time.Now())
	if _, err := Saved("", "grok", "conv-torn", Launch{Cwd: cwd, Env: env}); err == nil {
		t.Error("a summary.json grok left unreadable reported no error")
	}
	if _, err := Saved("", "none", "conv-1", Launch{Cwd: cwd, Env: env}); !errors.Is(err, ErrNoStore) {
		t.Fatalf("a tool with no store reader = %v, want ErrNoStore", err)
	}
}

func TestSavedReadsTheHomeAndProfileTheLaunchSelects(t *testing.T) {
	for _, name := range []string{"HERMES_HOME", "GEMINI_CLI_HOME", "CLAUDE_CONFIG_DIR"} {
		t.Setenv(name, "")
	}
	cwd := resolvePath(t.TempDir())
	launchHome, geminiHome, hermesHome := t.TempDir(), t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(launchHome, ".claude", "projects", claudeProjectSlug(cwd), "conv-claude.jsonl"), "{}\n", time.Now())
	writeFile(t, filepath.Join(geminiHome, ".gemini", "tmp", "w", "chats", "session-1.jsonl"), `{"sessionId":"conv-gemini","projectHash":"`+geminiProjectHash(cwd)+`"}`+"\n", time.Now())
	writeHermesStoreAt(t, filepath.Join(hermesHome, "profiles", "work", "state.db"), hermesRow{id: "conv-hermes", source: "cli", cwd: cwd, started: time.Now()})
	homeVariable := "HOME"
	if runtime.GOOS == "windows" {
		homeVariable = "USERPROFILE"
	}
	for _, tc := range []struct {
		store, report, id string
		launch            Launch
	}{
		{"", "claude", "conv-claude", Launch{Cwd: cwd, Env: Env{homeVariable: launchHome}}},
		{"gemini", "gemini", "conv-gemini", Launch{Cwd: cwd, Env: Env{"GEMINI_CLI_HOME": geminiHome}}},
		{"hermes", "hermes", "conv-hermes", Launch{Cwd: cwd, Env: Env{"HERMES_HOME": hermesHome}, Profile: "work"}},
	} {
		if saved, err := Saved(tc.store, tc.report, tc.id, tc.launch); err != nil || !saved {
			t.Errorf("%s under %+v = %v %v, want saved", tc.id, tc.launch, saved, err)
		}
	}
	if saved, _ := Saved("hermes", "hermes", "conv-hermes", Launch{Cwd: cwd, Env: Env{"HERMES_HOME": hermesHome}}); saved {
		t.Error("a hermes conversation saved under profile work was found under the default profile")
	}
}

func TestEnvCommandRunsTheExecutableOnTheLaunchPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in executable is a POSIX script")
	}
	launchBin, managerBin := t.TempDir(), t.TempDir()
	for dir, says := range map[string]string{launchBin: "launch", managerBin: "manager"} {
		writeFile(t, filepath.Join(dir, "opencode"), "#!/bin/sh\necho "+says+"\n", time.Now())
		if err := os.Chmod(filepath.Join(dir, "opencode"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", managerBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := Env{"PATH": launchBin}.command(context.Background(), "opencode").Output()
	if err != nil || strings.TrimSpace(string(out)) != "launch" {
		t.Fatalf("command ran %q (%v), want the launch PATH's opencode", out, err)
	}
	if out, err := (Env{"PATH": "relative" + string(os.PathListSeparator) + t.TempDir()}).command(context.Background(), "opencode").Output(); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("a launch PATH without opencode ran %q (%v), want not found", out, err)
	}
}
