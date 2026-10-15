package notify

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestCommandEnvContract(t *testing.T) {
	event := Event{ID: "sess-1", Session: "deploy", Tool: "claude", Dir: "/src/api-worktrees/am-x", Branch: "am/fix-login"}
	for kind, want := range map[Kind][2]string{
		Waiting:  {"waiting", "◆ Waiting for your input"},
		Finished: {"finished", "● Finished"},
		Errored:  {"errored", "✕ Errored"},
	} {
		event.Kind = kind
		got := CommandEnv(event)
		expected := []string{
			"AM_NOTIFY_KIND=" + want[0],
			"AM_SESSION_ID=sess-1",
			"AM_SESSION_NAME=deploy",
			"AM_TOOL=claude",
			"AM_CWD=/src/api-worktrees/am-x",
			"AM_BRANCH=am/fix-login",
			"AM_TITLE=deploy · claude",
			"AM_BODY=" + want[1],
		}
		if !slices.Equal(got, expected) {
			t.Fatalf("%s env = %q, want %q", want[0], got, expected)
		}
	}
	if env := CommandEnv(Event{ID: "x"}); env != nil {
		t.Fatalf("an event of no kind should have no env, got %q", env)
	}
}

func shellOutFile(t *testing.T) (dir, out string) {
	t.Helper()
	dir = t.TempDir()
	return dir, filepath.Join(dir, "out")
}

func TestRunCommandSkipsABlankCommand(t *testing.T) {
	defer func(previous func([]string, string) (string, error)) { runShell = previous }(runShell)
	ran := false
	runShell = func([]string, string) (string, error) {
		ran = true
		return "", nil
	}
	RunCommand("  ", t.TempDir(), Event{ID: "sess-1", Kind: Waiting})
	if ran {
		t.Fatal("a blank command should not reach the shell")
	}
}

func TestCommandLogStartsOverPastItsLimit(t *testing.T) {
	dir, _ := shellOutFile(t)
	path := filepath.Join(dir, CommandLog)
	if err := os.WriteFile(path, []byte(strings.Repeat("x", commandLogLimit+1)), 0o644); err != nil {
		t.Fatal(err)
	}
	RunCommand(`exit 1`, dir, Event{ID: "sess-1", Session: "s", Kind: Waiting})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > commandLogLimit {
		t.Fatalf("log is %d bytes, want it started over below %d", info.Size(), commandLogLimit)
	}
}

func TestCappedBufferTakesEveryWrite(t *testing.T) {
	buffer := &cappedBuffer{limit: 4}
	for _, chunk := range []string{"ab", "cdef", "gh"} {
		if n, err := buffer.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("Write(%q) = %d, %v; want every byte taken", chunk, n, err)
		}
	}
	if got := buffer.String(); got != "abcd" {
		t.Fatalf("kept %q, want abcd", got)
	}
}

func TestLogCommandFailureNamesTheEventAndCause(t *testing.T) {
	dir := t.TempDir()
	LogCommandFailure(dir, Event{ID: "sess-1", Kind: Waiting}, errors.New("reading the notify command: database is closed"))
	log, err := os.ReadFile(filepath.Join(dir, CommandLog))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"waiting", "sess-1", "database is closed"} {
		if !strings.Contains(string(log), want) {
			t.Fatalf("log %q does not name %q", log, want)
		}
	}
}

// Deliveries log from goroutines of their own; every line lands whole.
func TestLogCommandFailureFromConcurrentDeliveries(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			LogCommandFailure(dir, Event{ID: fmt.Sprintf("sess-%d", i), Kind: Errored}, errors.New("exit status 1"))
		}()
	}
	wg.Wait()
	log, err := os.ReadFile(filepath.Join(dir, CommandLog))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(log), "exit status 1\n"); lines != 20 {
		t.Fatalf("log holds %d whole lines, want 20:\n%s", lines, log)
	}
}

// The new-command banner names no session, so a click brings the manager
// forward without moving its cursor, and it shows the command itself.
func TestShowCommandShowsTheCommandOnABanner(t *testing.T) {
	defer restore()()
	rec := plainMac(t)
	var posted []string
	macPost = func(sessionID, subtitle, body, sound string) error {
		posted = []string{sessionID, subtitle, body, sound}
		return nil
	}
	ShowCommand("curl -d \"$AM_BODY\"\n ntfy.sh/topic")
	want := []string{"", "New notify command", `curl -d "$AM_BODY" ntfy.sh/topic`, "Glass"}
	if !slices.Equal(posted, want) {
		t.Fatalf("helper fields = %q, want %q", posted, want)
	}
	if len(rec.calls()) != 0 {
		t.Fatalf("nothing else should run once the helper posted, got %v", rec.calls())
	}
}

func TestShowCommandCutsALongCommand(t *testing.T) {
	defer restore()()
	plainMac(t)
	var body string
	macPost = func(_, _, text, _ string) error {
		body = text
		return nil
	}
	ShowCommand(strings.Repeat("x", commandBannerRunes+50))
	if runes := []rune(body); len(runes) != commandBannerRunes || !strings.HasSuffix(body, "…") {
		t.Fatalf("banner body is %d runes, want %d ending in …", len(runes), commandBannerRunes)
	}
}

func TestShowCommandReachesGhostty(t *testing.T) {
	defer restore()()
	plainMac(t)
	getenv = func(key string) string {
		if key == "TERM_PROGRAM" {
			return "ghostty"
		}
		return ""
	}
	var emitted []string
	emitSeq = func(seq string) error {
		emitted = append(emitted, seq)
		return nil
	}
	ShowCommand("say done")
	if !slices.Equal(emitted, []string{"\x1b]777;notify;agent-manager;say done — New notify command\a"}) {
		t.Fatalf("want one OSC 777 sequence, got %q", emitted)
	}
}
