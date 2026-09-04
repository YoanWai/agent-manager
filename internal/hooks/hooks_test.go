package hooks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

	writeName(NameRequest(testRequest, "  fix   auth\nbug \n"))
	if request, got, found := manager.ReadName("x"); !found || got != "fix auth bug" || request != testRequest {
		t.Fatalf("ReadName = %q, %q, %v; want the request and the squashed line", request, got, found)
	}

	writeName(NameRequest(testRequest, "   \n\t"))
	if _, got, found := manager.ReadName("x"); !found || got != "" {
		t.Fatalf("whitespace file: got %q, %v; want empty name with found=true", got, found)
	}

	writeName(NameRequest(testRequest, strings.Repeat("é", maxNameLength+20)))
	_, got, _ := manager.ReadName("x")
	if runes := []rune(got); len(runes) != maxNameLength {
		t.Fatalf("long name should cap at %d runes, got %d", maxNameLength, len(runes))
	}

	if _, _, found := manager.ReadName("no-such-session"); found {
		t.Fatal("missing name file should not be found")
	}

	if err := manager.RemoveName("x"); err != nil {
		t.Fatalf("RemoveName: %v", err)
	}
	if err := manager.RemoveName("x"); err != nil {
		t.Fatalf("second RemoveName should be a no-op: %v", err)
	}
	if _, _, found := manager.ReadName("x"); found {
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

const (
	testRequest  = "00000000000000a1"
	otherRequest = "00000000000000b2"
)

// A rename file is written by agents, so a first line that is not a
// request this package issued must never reach a result path.
func TestReadNameRejectsARequestItDidNotIssue(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(manager.Dir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cases := []struct {
		label    string
		content  string
		wantName string
	}{
		{"empty request", "\nfix auth bug", "fix auth bug"},
		{"slash", "../../escape\nfix auth bug", "../../escape fix auth bug"},
		{"traversal", "..\nfix auth bug", ".. fix auth bug"},
		{"not hex", "not-a-request\nfix auth bug", "not-a-request fix auth bug"},
		{"too short", testRequest[1:] + "\nfix auth bug", testRequest[1:] + " fix auth bug"},
		{"one line", "fix auth bug", "fix auth bug"},
	}
	for _, testCase := range cases {
		t.Run(testCase.label, func(t *testing.T) {
			if err := os.WriteFile(manager.NameFile("x"), []byte(testCase.content), 0o644); err != nil {
				t.Fatalf("write name: %v", err)
			}
			request, name, found := manager.ReadName("x")
			if !found || request != "" || name != testCase.wantName {
				t.Fatalf("ReadName = %q, %q, %v; want no request and the whole file as the name", request, name, found)
			}
			request, name, found, err := manager.ClaimName("x")
			if err != nil || !found || request != "" || name != testCase.wantName {
				t.Fatalf("ClaimName = %q, %q, %v, %v", request, name, found, err)
			}
			if err := manager.ReleaseName("x"); err != nil {
				t.Fatalf("ReleaseName: %v", err)
			}
		})
	}
}

func TestNameResultRoundTrips(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(manager.Dir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, found, err := manager.ReadNameResult("x", testRequest); found || err != nil {
		t.Fatalf("before the poller answers: found=%v err=%v", found, err)
	}

	if err := manager.WriteNameResult("x", testRequest, "fix auth  bug", "fix auth bug", nil); err != nil {
		t.Fatalf("WriteNameResult: %v", err)
	}
	verdict, found, err := manager.ReadNameResult("x", testRequest)
	if err != nil || !found || verdict.Refusal != nil || verdict.Requested != "fix auth  bug" || verdict.Applied != "fix auth bug" {
		t.Fatalf("ReadNameResult = %+v, %v, %v; want the asked and applied names", verdict, found, err)
	}

	if err := manager.WriteNameResult("x", testRequest, "taken", "", errors.New("branch already exists: am/taken")); err != nil {
		t.Fatalf("WriteNameResult refusal: %v", err)
	}
	verdict, found, err = manager.ReadNameResult("x", testRequest)
	if err != nil || !found || verdict.Refusal == nil || verdict.Refusal.Error() != "branch already exists: am/taken" || verdict.Requested != "taken" || verdict.Applied != "" {
		t.Fatalf("ReadNameResult = %+v, %v, %v; want the refusal", verdict, found, err)
	}

	if err := manager.RemoveNameResult("x", testRequest); err != nil {
		t.Fatalf("RemoveNameResult: %v", err)
	}
	if _, found, err := manager.ReadNameResult("x", testRequest); found || err != nil {
		t.Fatalf("removed result: found=%v err=%v", found, err)
	}
	if err := manager.RemoveNameResult("x", testRequest); err != nil {
		t.Fatalf("second RemoveNameResult should be a no-op: %v", err)
	}
}

// Two renames for one session are answered apart, so neither reads nor
// removes the answer meant for the other.
func TestNameResultsAreKeptApartPerRequest(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(manager.Dir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := manager.WriteNameResult("x", testRequest, "first name", "first name", nil); err != nil {
		t.Fatalf("write first: %v", err)
	}
	if err := manager.WriteNameResult("x", otherRequest, "second name", "second name", nil); err != nil {
		t.Fatalf("write second: %v", err)
	}

	if err := manager.RemoveNameResult("x", otherRequest); err != nil {
		t.Fatalf("RemoveNameResult: %v", err)
	}
	verdict, found, err := manager.ReadNameResult("x", testRequest)
	if err != nil || !found || verdict.Applied != "first name" {
		t.Fatalf("the other rename took this answer with it: %+v, %v, %v", verdict, found, err)
	}

	if err := manager.SweepNameResults(time.Now().Add(NameResultLifetime)); err != nil {
		t.Fatalf("SweepNameResults: %v", err)
	}
	if _, found, _ := manager.ReadNameResult("x", testRequest); found {
		t.Fatal("an answer nobody came back for should be swept")
	}
}

// A sweep must never take an answer from a caller still reading for it,
// and must collect the ones left by sessions that are gone.
func TestSweepNameResultsKeepsFreshAnswers(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(manager.Dir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := manager.WriteNameResult("fresh", testRequest, "a name", "a name", nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := manager.WriteNameResult("stale", otherRequest, "another name", "another name", nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	old := time.Now().Add(-NameResultLifetime - time.Minute)
	if err := os.Chtimes(manager.NameResultFile("stale", otherRequest), old, old); err != nil {
		t.Fatalf("age the answer: %v", err)
	}

	if err := manager.SweepNameResults(time.Now()); err != nil {
		t.Fatalf("SweepNameResults: %v", err)
	}
	if _, found, err := manager.ReadNameResult("fresh", testRequest); !found || err != nil {
		t.Fatalf("the sweep took an answer a caller could still be reading: found=%v err=%v", found, err)
	}
	if _, found, _ := manager.ReadNameResult("stale", otherRequest); found {
		t.Fatal("an answer past its lifetime should be gone")
	}
}

// A file this package did not write is no answer: reading one as a
// rename that happened is the false success this mailbox exists to stop.
func TestReadNameResultRejectsAForeignFile(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(manager.Dir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, content := range []string{"", "renam", "fix auth bug", "ok\nfix auth bug\nfix auth bug", "renamed\nfix auth bug", "renamed\n\nfix auth bug", "renamed\nfix auth bug\n"} {
		if err := os.WriteFile(manager.NameResultFile("x", testRequest), []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if verdict, found, err := manager.ReadNameResult("x", testRequest); found || err != nil {
			t.Fatalf("content %q read as an answer: %+v, %v", content, verdict, err)
		}
	}
}

// A claim takes the pending rename out of the mailbox in one step, so a
// rename written while it is being applied waits for the next claim
// instead of being consumed with it.
func TestClaimNameLeavesALaterRenameForTheNextClaim(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(manager.Dir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, _, found, err := manager.ClaimName("x"); found || err != nil {
		t.Fatalf("ClaimName with nothing pending = %v, %v", found, err)
	}
	if err := os.WriteFile(manager.NameFile("x"), []byte(NameRequest(testRequest, "first name")), 0o644); err != nil {
		t.Fatalf("write name: %v", err)
	}

	request, name, found, err := manager.ClaimName("x")
	if err != nil || !found || name != "first name" || request != testRequest {
		t.Fatalf("ClaimName = %q, %q, %v, %v", request, name, found, err)
	}
	if _, _, found := manager.ReadName("x"); found {
		t.Fatal("a claimed rename must leave the mailbox free")
	}
	if err := os.WriteFile(manager.NameFile("x"), []byte(NameRequest(otherRequest, "second name")), 0o644); err != nil {
		t.Fatalf("write second name: %v", err)
	}

	// A claim the manager did not finish is picked up again ahead of it.
	request, again, found, err := manager.ClaimName("x")
	if err != nil || !found || again != "first name" || request != testRequest {
		t.Fatalf("re-claim = %q, %q, %v, %v; want the unfinished claim", request, again, found, err)
	}
	if err := manager.ReleaseName("x"); err != nil {
		t.Fatalf("ReleaseName: %v", err)
	}
	request, next, found, err := manager.ClaimName("x")
	if err != nil || !found || next != "second name" || request != otherRequest {
		t.Fatalf("next claim = %q, %q, %v, %v; want the rename written meanwhile", request, next, found, err)
	}
	if err := manager.ReleaseName("x"); err != nil {
		t.Fatalf("ReleaseName: %v", err)
	}
	if _, _, found, _ := manager.ClaimName("x"); found {
		t.Fatal("a released claim leaves nothing pending")
	}
}

// Two renames for one session write at the same time, so neither may
// publish the other's content or fail on a staging file it does not own.
func TestWriteWholeSurvivesConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abc123.name")
	var wg sync.WaitGroup
	for _, content := range []string{"first name", "second name"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				if err := WriteWhole(path, content); err != nil {
					t.Errorf("WriteWhole(%q): %v", content, err)
					return
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Errorf("read: %v", err)
					return
				}
				if got := string(raw); got != "first name" && got != "second name" {
					t.Errorf("read a torn file: %q", got)
					return
				}
			}
		}()
	}
	wg.Wait()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("staging files were left behind: %v", entries)
	}
}

// A mailbox that cannot be read is not the same as no answer yet: the
// waiting command must hear about it rather than report a rename queued.
func TestReadNameResultSurfacesAReadFailure(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(manager.NameResultFile("x", testRequest), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, found, err := manager.ReadNameResult("x", testRequest); err == nil || found {
		t.Fatalf("unreadable result = found %v, err %v; want the failure", found, err)
	}
}

// The claim tells a caller its rename is being applied, so a claim the
// manager took from a file carrying no request of ours reports no
// request rather than a made-up one.
func TestClaimedRequestReportsWhatWasClaimed(t *testing.T) {
	manager := NewManager(t.TempDir())
	if err := os.MkdirAll(manager.Dir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if request, held, err := manager.ClaimedRequest("x"); held || request != "" || err != nil {
		t.Fatalf("with nothing claimed = %q, %v, %v", request, held, err)
	}

	for _, testCase := range []struct {
		label   string
		content string
		want    string
	}{
		{"a request we issued", NameRequest(testRequest, "fix auth bug"), testRequest},
		{"a file carrying no request", "fix auth bug", ""},
	} {
		t.Run(testCase.label, func(t *testing.T) {
			if err := os.WriteFile(manager.NameFile("x"), []byte(testCase.content), 0o644); err != nil {
				t.Fatalf("write name: %v", err)
			}
			if _, _, found, err := manager.ClaimName("x"); err != nil || !found {
				t.Fatalf("ClaimName = %v, %v", found, err)
			}
			request, held, err := manager.ClaimedRequest("x")
			if err != nil || !held || request != testCase.want {
				t.Fatalf("ClaimedRequest = %q, %v, %v; want %q", request, held, err, testCase.want)
			}
			if err := manager.ReleaseName("x"); err != nil {
				t.Fatalf("ReleaseName: %v", err)
			}
		})
	}
}
