package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
)

func TestEnsureSettingsWritesValidHookJSON(t *testing.T) {
	manager := NewManager(t.TempDir())
	path, err := manager.EnsureSettings()
	if err != nil {
		t.Fatalf("EnsureSettings: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var parsed struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("settings is not valid JSON: %v", err)
	}
	events := []string{"UserPromptSubmit", "PreToolUse", "PostToolUse", "Notification", "Stop", "SessionStart", "SessionEnd"}
	if len(parsed.Hooks) != len(events) {
		t.Fatalf("hooks has %d events, want %d: %v", len(parsed.Hooks), len(events), parsed.Hooks)
	}
	guard := `[ -z "$` + EnvStatusFile + `" ] ||`
	for _, event := range events {
		matchers, ok := parsed.Hooks[event]
		if !ok {
			t.Fatalf("event %s missing from settings", event)
		}
		for _, matcher := range matchers {
			for _, hook := range matcher.Hooks {
				if hook.Type != "command" {
					t.Fatalf("event %s hook type = %q, want command", event, hook.Type)
				}
				if !strings.Contains(hook.Command, guard) {
					t.Fatalf("event %s command lacks env guard: %q", event, hook.Command)
				}
			}
		}
	}
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		if got := parsed.Hooks[event][0].Matcher; got != "*" {
			t.Fatalf("%s matcher = %q, want *", event, got)
		}
	}
}

func TestEnsureSettingsIdempotent(t *testing.T) {
	manager := NewManager(t.TempDir())
	first, err := manager.EnsureSettings()
	if err != nil {
		t.Fatalf("first EnsureSettings: %v", err)
	}
	info, err := os.Stat(first)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	second, err := manager.EnsureSettings()
	if err != nil {
		t.Fatalf("second EnsureSettings: %v", err)
	}
	if first != second {
		t.Fatalf("paths differ: %q vs %q", first, second)
	}
	again, err := os.Stat(second)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.ModTime().Equal(again.ModTime()) {
		t.Fatal("unchanged settings should not be rewritten")
	}
}

func TestStatusFilePath(t *testing.T) {
	configDir := t.TempDir()
	manager := NewManager(configDir)
	want := filepath.Join(configDir, "hooks", "abcd1234.status")
	if got := manager.StatusFile("abcd1234"); got != want {
		t.Fatalf("StatusFile = %q, want %q", got, want)
	}
}

func TestReadWhitelist(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(manager.StatusFile("x")), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeStatus := func(content string) {
		t.Helper()
		if err := os.WriteFile(manager.StatusFile("x"), []byte(content), 0o644); err != nil {
			t.Fatalf("write status: %v", err)
		}
	}

	for _, valid := range []string{status.Working, status.Waiting, status.Finished, status.Idle} {
		writeStatus(valid)
		got, ok := manager.Read("x")
		if !ok || got != valid {
			t.Fatalf("Read(%q) = %q, %v; want value, true", valid, got, ok)
		}
	}

	writeStatus("working\n")
	if got, ok := manager.Read("x"); !ok || got != status.Working {
		t.Fatalf("trailing newline should trim to working, got %q, %v", got, ok)
	}

	for _, invalid := range []string{"garbage", "", "dead"} {
		writeStatus(invalid)
		if got, ok := manager.Read("x"); ok {
			t.Fatalf("Read(%q) accepted %q, want rejection", invalid, got)
		}
	}

	if _, ok := manager.Read("no-such-session"); ok {
		t.Fatal("missing file should not read ok")
	}
}

func TestReadNameNormalizes(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(manager.NameFile("x")), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeName := func(content string) {
		t.Helper()
		if err := os.WriteFile(manager.NameFile("x"), []byte(content), 0o644); err != nil {
			t.Fatalf("write name: %v", err)
		}
	}

	writeName("  fix   auth\nbug \n")
	if got, found := manager.ReadName("x"); !found || got != "fix auth bug" {
		t.Fatalf("ReadName = %q, %v; want squashed line, true", got, found)
	}

	writeName("   \n\t")
	if got, found := manager.ReadName("x"); !found || got != "" {
		t.Fatalf("whitespace file: got %q, %v; want empty name with found=true", got, found)
	}

	writeName(strings.Repeat("é", maxNameLength+20))
	got, _ := manager.ReadName("x")
	if runes := []rune(got); len(runes) != maxNameLength {
		t.Fatalf("long name should cap at %d runes, got %d", maxNameLength, len(runes))
	}

	if _, found := manager.ReadName("no-such-session"); found {
		t.Fatal("missing name file should not be found")
	}

	if err := manager.RemoveName("x"); err != nil {
		t.Fatalf("RemoveName: %v", err)
	}
	if err := manager.RemoveName("x"); err != nil {
		t.Fatalf("second RemoveName should be a no-op: %v", err)
	}
	if _, found := manager.ReadName("x"); found {
		t.Fatal("removed name file should not be found")
	}
}

func TestRemoveIdempotent(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(manager.StatusFile("x")), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(manager.StatusFile("x"), []byte(status.Working), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := manager.Remove("x"); err != nil {
		t.Fatalf("first Remove: %v", err)
	}
	if err := manager.Remove("x"); err != nil {
		t.Fatalf("second Remove should be a no-op: %v", err)
	}
}
