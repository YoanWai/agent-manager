package termbrowser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/testenv"
)

type recordingClient struct {
	*Client
	lastArgs []string
}

func fakeClient(t *testing.T, stdout, stderr []byte, exitCode int) *recordingClient {
	t.Helper()
	recorder := &recordingClient{Client: New()}
	recorder.run = func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		recorder.lastArgs = args
		return stdout, stderr, exitCode, nil
	}
	return recorder
}

func sleepingRun(delay time.Duration) func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	return func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		select {
		case <-time.After(delay):
			return nil, nil, 0, nil
		case <-ctx.Done():
			return nil, nil, -1, ctx.Err()
		}
	}
}

func fixture(name string) []byte {
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		panic(err)
	}
	return data
}

func TestListMapsEachBrowserToItsSession(t *testing.T) {
	client := fakeClient(t, fixture("ls.json"), nil, 0)
	browsers, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(browsers) != 2 || browsers[0].SessionID() != "b1b2b3b4" || browsers[1].SessionID() != "a1a2a3a4" {
		t.Fatalf("%+v", browsers)
	}
	tab, ok := browsers[1].ActiveTab()
	if !ok || tab.URL != "https://news.ycombinator.com/" || tab.Title != "Hacker News" {
		t.Fatalf("active tab %+v %v", tab, ok)
	}
	if got := client.lastArgs; !slices.Equal(got, []string{"ls", "--all", "--json"}) {
		t.Fatalf("args %q", got)
	}
}

func TestForSessionReportsNoBrowser(t *testing.T) {
	client := fakeClient(t, fixture("ls.json"), nil, 0)
	_, err := client.ForSession(context.Background(), "ffffffff")
	if !errors.Is(err, ErrNoBrowser) {
		t.Fatalf("err = %v", err)
	}
}

func TestActionTargetsTheKeyAndSplitsTheCommand(t *testing.T) {
	client := fakeClient(t, []byte("- link \"Learn more\" [ref=e1]\n"), nil, 0)
	result, err := client.Action(context.Background(), "96030-2", `fill @e3 "hello world"`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"action", "--browser", "96030-2", "--", "fill", "@e3", "hello world"}
	if !slices.Equal(client.lastArgs, want) || result.ExitCode != 0 || result.Stdout == "" {
		t.Fatalf("args %q result %+v", client.lastArgs, result)
	}
}

func TestActionReportsNonZeroExitAndTimeout(t *testing.T) {
	client := fakeClient(t, nil, []byte("error: tab_gone"), 1)
	result, _ := client.Action(context.Background(), "k", "get url")
	if result.ExitCode != 1 || result.Stderr != "error: tab_gone" {
		t.Fatalf("%+v", result)
	}
	slow := fakeClient(t, nil, nil, 0)
	slow.Timeout = 10 * time.Millisecond
	slow.run = sleepingRun(50 * time.Millisecond)
	result, _ = slow.Action(context.Background(), "k", "get url")
	if !result.TimedOut {
		t.Fatalf("%+v", result)
	}
}

func TestDoneReleasesTheBrowsersAgentSession(t *testing.T) {
	client := fakeClient(t, []byte(`{"ok":true}`), nil, 0)
	if err := client.Done(context.Background(), "96030-2"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"action", "--browser", "96030-2", "--", "close"}; !slices.Equal(client.lastArgs, want) {
		t.Fatalf("args %q, want %q", client.lastArgs, want)
	}
	failing := fakeClient(t, nil, []byte("no such browser"), 1)
	if err := failing.Done(context.Background(), "k"); err == nil || !strings.Contains(err.Error(), "no such browser") {
		t.Fatalf("a failed release = %v", err)
	}
	bounded := fakeClient(t, nil, nil, 0)
	bounded.run = func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Errorf("release ran with deadline %v %v, want 5 seconds at most", deadline, ok)
		}
		return nil, nil, 0, nil
	}
	if err := bounded.Done(context.Background(), "k"); err != nil {
		t.Fatal(err)
	}
}

func TestActionRefusesAnEmptyCommand(t *testing.T) {
	if _, err := fakeClient(t, nil, nil, 0).Action(context.Background(), "k", "  "); err == nil {
		t.Fatal("want error")
	}
}

// manualClock is a Now that moves only when the test advances it.
func manualClock(client *Client) func(time.Duration) {
	current := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	client.Now = func() time.Time { return current }
	return func(step time.Duration) { current = current.Add(step) }
}

func TestListCachedReusesAFreshResult(t *testing.T) {
	client := fakeClient(t, fixture("ls.json"), nil, 0)
	advance := manualClock(client.Client)
	runs := 0
	recordArgs := client.run
	client.run = func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		runs++
		return recordArgs(ctx, name, args...)
	}
	var browsers []Browser
	for range 3 {
		var err error
		browsers, err = client.ListCached(context.Background(), 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		advance(500 * time.Millisecond)
	}
	if runs != 1 {
		t.Fatalf("three reads inside the age ran ls %d times, want 1", runs)
	}
	if len(browsers) != 2 {
		t.Fatalf("%+v", browsers)
	}
	if tab, ok := browsers[1].ActiveTab(); !ok || !tab.AgentControlled {
		t.Fatalf("active tab %+v should carry agentControlled", tab)
	}
	advance(500 * time.Millisecond)
	if _, err := client.ListCached(context.Background(), 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if runs != 2 {
		t.Fatalf("a read at the age ran ls %d times in all, want 2", runs)
	}
}

// terminal-browser names a browser's pane only when its own tmux backend is
// up, which it starts from $TMUX, so a manager outside tmux has to point it
// at the server the panes live on.
func TestNewOnServerPointsTmuxAtTheServer(t *testing.T) {
	testenv.SkipPOSIXShell(t)
	t.Setenv("TMUX", "/tmp/outer/default,1,2")
	t.Setenv("TMUX_PANE", "%9")
	bin := filepath.Join(t.TempDir(), "terminal-browser")
	script := "#!/bin/sh\nprintf '%s|%s' \"$TMUX\" \"${TMUX_PANE-unset}\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	client := NewOnServer("/tmp/amtest/tmux-501/agentmgr")
	client.Bin = bin
	result, err := client.Action(context.Background(), "k", "get url")
	if err != nil {
		t.Fatal(err)
	}
	if want := "/tmp/amtest/tmux-501/agentmgr,0,0|unset"; result.Stdout != want {
		t.Fatalf("terminal-browser saw TMUX|TMUX_PANE = %q, want %q", result.Stdout, want)
	}
}

// A failing or hung ls is held for the same age as a good one, so a poll
// that pokes faster than the age cannot rerun it every pass.
func TestListCachedHoldsAFailureForItsAge(t *testing.T) {
	client := fakeClient(t, nil, []byte("boom"), 1)
	advance := manualClock(client.Client)
	runs := 0
	failing := client.run
	client.run = func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		runs++
		return failing(ctx, name, args...)
	}
	for range 2 {
		if _, err := client.ListCached(context.Background(), 2*time.Second); err == nil {
			t.Fatal("want the ls failure")
		}
		advance(time.Second - time.Millisecond)
	}
	if runs != 1 {
		t.Fatalf("two failing reads inside the age ran ls %d times, want 1", runs)
	}
	client.run = func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		runs++
		return fixture("ls.json"), nil, 0, nil
	}
	advance(2 * time.Millisecond)
	browsers, err := client.ListCached(context.Background(), 2*time.Second)
	if err != nil || len(browsers) != 2 || runs != 2 {
		t.Fatalf("a read past the age should run ls again: %+v %v, %d runs", browsers, err, runs)
	}
}

func TestFramesForPicksSharedMemoryOnlyForALocalGhostty(t *testing.T) {
	cases := []struct {
		termProgram string
		remote      bool
		want        string
	}{
		{"ghostty", false, "shm"},
		{"ghostty", true, ""},
		{"iTerm.app", false, ""},
		{"tmux", false, ""},
		{"", false, ""},
	}
	for _, c := range cases {
		if got := FramesFor(c.termProgram, c.remote); got != c.want {
			t.Errorf("FramesFor(%q, %v) = %q, want %q", c.termProgram, c.remote, got, c.want)
		}
	}
}
