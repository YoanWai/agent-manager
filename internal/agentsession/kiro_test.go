package agentsession

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Printed by kiro-cli 3.1.0 right after the trust gate, before any prompt.
const kiroListAfterTrust = `[
  {
    "cwd": "/home/user/src/demo-project",
    "sessions": [
      {
        "sessionId": "sess_a92aea39-1bad-4faf-a546-793e09d35d3f",
        "source": "v3",
        "title": "New Session",
        "updatedAt": "2026-11-06T15:41:55.078Z",
        "executionTarget": "local",
        "status": "idle"
      }
    ],
    "complete": true
  }
]
`

// Printed by kiro-cli 3.1.0 while a tool approval was open.
const kiroListDuringApproval = `[{"cwd":"/home/user/src/demo-project","sessions":[{"sessionId":"sess_a92aea39-1bad-4faf-a546-793e09d35d3f","source":"v3","title":"List Directory Contents","updatedAt":"2026-11-06T15:42:16.283Z","executionTarget":"local","status":"idle"}],"complete":true}]
`

const kiroCapturedID = "sess_a92aea39-1bad-4faf-a546-793e09d35d3f"

func stubKiroList(t *testing.T, list func(cwd string) (string, error)) {
	t.Helper()
	saved := kiroSessionList
	kiroSessionList = func(cwd string, _ Env) ([]kiroWorkspace, error) {
		out, err := list(cwd)
		if err != nil {
			return nil, err
		}
		return parseKiroSessions([]byte(out))
	}
	t.Cleanup(func() { kiroSessionList = saved })
}

func inCwd(document, cwd string) string {
	return strings.ReplaceAll(document, "/home/user/src/demo-project", filepath.ToSlash(cwd))
}

func TestParseKiroSessionsReadsTheRealList(t *testing.T) {
	for name, document := range map[string]string{"after trust": kiroListAfterTrust, "during approval": kiroListDuringApproval} {
		workspaces, err := parseKiroSessions([]byte(document))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(workspaces) != 1 || workspaces[0].Cwd != "/home/user/src/demo-project" || len(workspaces[0].Sessions) != 1 {
			t.Fatalf("%s: workspaces = %+v", name, workspaces)
		}
		if session := workspaces[0].Sessions[0]; session.SessionID != kiroCapturedID || session.UpdatedAt.IsZero() {
			t.Fatalf("%s: session = %+v", name, session)
		}
	}
	if _, err := parseKiroSessions([]byte("[]")); err != nil {
		t.Fatalf("empty list before trust: %v", err)
	}
}

func TestParseKiroSessionsRefusesMalformedOutput(t *testing.T) {
	for _, out := range []string{"", "Not logged in", `{"cwd":"/x"}`, `[{"sessions":[{"sessionId":"s","updatedAt":"yesterday"}]}]`} {
		if _, err := parseKiroSessions([]byte(out)); err == nil {
			t.Fatalf("parseKiroSessions(%q) accepted it", out)
		}
	}
}

func TestCaptureKiroTakesTheSessionListedAfterLaunch(t *testing.T) {
	cwd := t.TempDir()
	stubKiroList(t, func(string) (string, error) { return inCwd(kiroListAfterTrust, cwd), nil })
	launched := time.Date(2026, 11, 6, 15, 41, 50, 0, time.UTC)
	if id, ok := Capture("kiro", cwd, launched, nil, ""); !ok || id != kiroCapturedID {
		t.Fatalf("Capture = %q %v, want %s", id, ok, kiroCapturedID)
	}
	if id, ok := Capture("kiro", cwd, launched, map[string]bool{kiroCapturedID: true}, ""); ok {
		t.Fatalf("Capture took a claimed session %q", id)
	}
	if id, ok := Capture("kiro", cwd, launched.Add(time.Hour), nil, ""); ok {
		t.Fatalf("Capture took a session older than the launch: %q", id)
	}
	if id, ok := Capture("kiro", filepath.Join(cwd, "other"), launched, nil, ""); ok {
		t.Fatalf("Capture took another directory's session: %q", id)
	}
}

func TestCaptureKiroPicksTheEarliestUnclaimedOfSeveral(t *testing.T) {
	cwd := t.TempDir()
	first, second := "sess_11111111-1111-4111-8111-111111111111", "sess_22222222-2222-4222-8222-222222222222"
	list := `[{"cwd":"` + filepath.ToSlash(cwd) + `","sessions":[` +
		`{"sessionId":"` + second + `","updatedAt":"2026-11-06T15:42:30.000Z"},` +
		`{"sessionId":"` + first + `","updatedAt":"2026-11-06T15:42:10.000Z"},` +
		`{"sessionId":"bad id; rm","updatedAt":"2026-11-06T15:42:00.000Z"}],"complete":true}]`
	stubKiroList(t, func(string) (string, error) { return list, nil })
	launched := time.Date(2026, 11, 6, 15, 42, 5, 0, time.UTC)
	if id, ok := Capture("kiro", cwd, launched, nil, ""); !ok || id != first {
		t.Fatalf("Capture = %q %v, want %s", id, ok, first)
	}
	if id, ok := Capture("kiro", cwd, launched, map[string]bool{first: true}, ""); !ok || id != second {
		t.Fatalf("Capture with the first claimed = %q %v, want %s", id, ok, second)
	}
}

func TestKiroListFailureCapturesNothing(t *testing.T) {
	cwd := t.TempDir()
	stubKiroList(t, func(string) (string, error) { return "", errors.New("kiro-cli: not logged in") })
	if id, ok := Capture("kiro", cwd, time.Time{}, nil, ""); ok {
		t.Fatalf("Capture = %q on a failed list", id)
	}
	if _, ok := Snapshot("kiro", cwd); ok {
		t.Fatal("Snapshot succeeded on a failed list")
	}
	if _, err := Saved("kiro", "none", kiroCapturedID, Launch{Cwd: cwd}); err == nil {
		t.Fatal("Saved reported no error on a failed list")
	}
}

func TestRecaptureKiroBindsTheSessionThatMoved(t *testing.T) {
	cwd := t.TempDir()
	document := kiroListAfterTrust
	stubKiroList(t, func(string) (string, error) { return inCwd(document, cwd), nil })
	snapshot, ok := Snapshot("kiro", cwd)
	if !ok || len(snapshot) != 1 {
		t.Fatalf("Snapshot = %v %v", snapshot, ok)
	}
	if id, ok := Recapture("kiro", cwd, snapshot, nil); ok {
		t.Fatalf("Recapture bound %q before it moved", id)
	}
	document = kiroListDuringApproval
	if id, ok := Recapture("kiro", cwd, snapshot, nil); !ok || id != kiroCapturedID {
		t.Fatalf("Recapture = %q %v, want %s", id, ok, kiroCapturedID)
	}
	if saved, err := Saved("kiro", "none", kiroCapturedID, Launch{Cwd: cwd}); err != nil || !saved {
		t.Fatalf("Saved = %v %v", saved, err)
	}
}

func TestKiroSessionListRunsInTheSessionDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake kiro-cli is a shell script")
	}
	bin := t.TempDir()
	script := `#!/bin/sh
[ "$*" = "chat --list-sessions -f json" ] || exit 2
printf '[{"cwd":"%s","sessions":[{"sessionId":"sess_a92aea39-1bad-4faf-a546-793e09d35d3f","updatedAt":"2026-11-06T15:41:55.078Z"}],"complete":true}]\n' "$(pwd -P)"
`
	if err := os.WriteFile(filepath.Join(bin, "kiro-cli"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	cwd := t.TempDir()
	launched := time.Date(2026, 11, 6, 15, 41, 50, 0, time.UTC)
	if id, ok := Capture("kiro", cwd, launched, nil, ""); !ok || id != kiroCapturedID {
		t.Fatalf("Capture = %q %v, want %s", id, ok, kiroCapturedID)
	}
}
