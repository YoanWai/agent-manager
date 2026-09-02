package hooks

import (
	"encoding/json"
	"errors"
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
	events := []string{"UserPromptSubmit", "PreToolUse", "PostToolUse", "Notification", "Stop", "StopFailure", "SessionStart", "SessionEnd"}
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
	notification := parsed.Hooks["Notification"][0]
	if notification.Matcher != blockingNotifications {
		t.Fatalf("Notification matcher = %q, want %q", notification.Matcher, blockingNotifications)
	}
	// the idle reminder, auth success and background agents finishing all
	// arrive as Notification too, and the matcher is what keeps them out
	for _, unwanted := range []string{"idle_prompt", "auth_success", "agent_completed"} {
		if strings.Contains(notification.Matcher, unwanted) {
			t.Fatalf("Notification matcher %q subscribes to %s", notification.Matcher, unwanted)
		}
	}
	if command := notification.Hooks[0].Command; !strings.Contains(command, "printf "+status.Waiting) || strings.Contains(command, "grep") {
		t.Fatalf("Notification command = %q, want a plain %s write", command, status.Waiting)
	}
	if got := parsed.Hooks["SessionStart"][0].Matcher; got != "startup|resume|clear" {
		t.Fatalf("SessionStart matcher = %q, want startup|resume|clear", got)
	}
	stopFailure := parsed.Hooks["StopFailure"][0]
	if stopFailure.Matcher != limitStopFailures {
		t.Fatalf("StopFailure matcher = %q, want %q", stopFailure.Matcher, limitStopFailures)
	}
	if command := stopFailure.Hooks[0].Command; !strings.Contains(command, "printf "+status.Errored) {
		t.Fatalf("StopFailure command = %q, want a plain %s write", command, status.Errored)
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

	for _, valid := range []string{status.Working, status.Waiting, status.Finished, status.Idle, status.Errored} {
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

func TestReviewRepoMailbox(t *testing.T) {
	manager := NewManager(t.TempDir())
	if _, found := manager.ReadReviewRepo("abc"); found {
		t.Fatal("no mailbox should exist yet")
	}
	path := manager.ReviewRepoFile("abc")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("  /repos/alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, found := manager.ReadReviewRepo("abc")
	if !found || root != "/repos/alpha" {
		t.Fatalf("read = %q, %v; want /repos/alpha, true", root, found)
	}
	if err := manager.RemoveReviewRepo("abc"); err != nil {
		t.Fatal(err)
	}
	if _, found := manager.ReadReviewRepo("abc"); found {
		t.Fatal("mailbox should be gone after removal")
	}
}

func TestReviewBaseMailbox(t *testing.T) {
	manager := NewManager(t.TempDir())
	if _, _, found := manager.ReadReviewBase("abc"); found {
		t.Fatal("no mailbox should exist yet")
	}
	path := manager.ReviewBaseFile("abc")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("  /repos/alpha\nmain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, ref, found := manager.ReadReviewBase("abc")
	if !found || root != "/repos/alpha" || ref != "main" {
		t.Fatalf("read = %q, %q, %v; want /repos/alpha, main, true", root, ref, found)
	}
	// A clear writes the root with an empty ref line.
	if err := os.WriteFile(path, []byte("/repos/alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, ref, found = manager.ReadReviewBase("abc")
	if !found || root != "/repos/alpha" || ref != "" {
		t.Fatalf("cleared read = %q, %q, %v; want /repos/alpha, empty, true", root, ref, found)
	}
	if err := manager.RemoveReviewBase("abc"); err != nil {
		t.Fatal(err)
	}
	if _, _, found := manager.ReadReviewBase("abc"); found {
		t.Fatal("mailbox should be gone after removal")
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

func TestNameResultRoundTrips(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(manager.Dir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, _, _, found := manager.ReadNameResult("x"); found {
		t.Fatal("no result should be found before the poller answers")
	}

	if err := manager.WriteNameResult("x", "fix auth  bug", "fix auth bug", nil); err != nil {
		t.Fatalf("WriteNameResult: %v", err)
	}
	requested, applied, refusal, found := manager.ReadNameResult("x")
	if !found || refusal != nil || requested != "fix auth  bug" || applied != "fix auth bug" {
		t.Fatalf("ReadNameResult = %q, %q, %v, %v; want the asked and applied names", requested, applied, refusal, found)
	}

	if err := manager.WriteNameResult("x", "taken", "", errors.New("branch already exists: am/taken")); err != nil {
		t.Fatalf("WriteNameResult refusal: %v", err)
	}
	requested, applied, refusal, found = manager.ReadNameResult("x")
	if !found || refusal == nil || refusal.Error() != "branch already exists: am/taken" || requested != "taken" || applied != "" {
		t.Fatalf("ReadNameResult = %q, %q, %v, %v; want the refusal", requested, applied, refusal, found)
	}

	if err := manager.RemoveNameResult("x"); err != nil {
		t.Fatalf("RemoveNameResult: %v", err)
	}
	if _, _, _, found := manager.ReadNameResult("x"); found {
		t.Fatal("removed result should not be found")
	}
	if err := manager.RemoveNameResult("x"); err != nil {
		t.Fatalf("second RemoveNameResult should be a no-op: %v", err)
	}
}

// A file this package did not write is no answer: reading one as a
// rename that happened is the false success this mailbox exists to stop.
func TestReadNameResultRejectsAForeignFile(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(manager.Dir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, content := range []string{"", "renam", "fix auth bug", "ok\nfix auth bug\nfix auth bug"} {
		if err := os.WriteFile(manager.NameResultFile("x"), []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if requested, applied, refusal, found := manager.ReadNameResult("x"); found {
			t.Fatalf("content %q read as an answer: %q, %q, %v", content, requested, applied, refusal)
		}
	}
}

// The poller consumes the rename it applied, and leaves one written
// while it was working for its next pass.
func TestRemoveNameIfUnchanged(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(manager.Dir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(manager.NameFile("x"), []byte("second name"), 0o644); err != nil {
		t.Fatalf("write name: %v", err)
	}
	if err := manager.RemoveNameIfUnchanged("x", "first name"); err != nil {
		t.Fatalf("RemoveNameIfUnchanged: %v", err)
	}
	if name, found := manager.ReadName("x"); !found || name != "second name" {
		t.Fatalf("a newer name was consumed by the older rename: %q, %v", name, found)
	}
	if err := manager.RemoveNameIfUnchanged("x", "second name"); err != nil {
		t.Fatalf("RemoveNameIfUnchanged: %v", err)
	}
	if _, found := manager.ReadName("x"); found {
		t.Fatal("the applied name should be consumed")
	}
	if err := manager.RemoveNameIfUnchanged("x", "second name"); err != nil {
		t.Fatalf("removing a name that is gone should be a no-op: %v", err)
	}
}
