package execution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/termbrowser"
	"github.com/YoanWai/agent-manager/internal/testenv"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

const placeholderCell = "\U0010EEEE"

// fakeBrowserEngine is a terminal-browser whose ls reports one browser in
// sessionID's pane and appends a line to the returned file on every run.
func fakeBrowserEngine(t *testing.T, sessionID string) (*termbrowser.Client, string) {
	t.Helper()
	testenv.SkipPOSIXShell(t)
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs")
	listing := `{"browsers":[{"key":"1-1","pid":1,"pane":{"tab":"am_` + sessionID + `:@1","pane":"%1"},` +
		`"tabs":[{"id":1,"url":"https://example.org/docs","title":"Example Docs","active":true,"agentControlled":true}]}]}`
	script := "#!/bin/sh\necho run >> " + tmux.ShellQuote(runs) + "\nprintf '%s\\n' " + tmux.ShellQuote(listing) + "\n"
	bin := filepath.Join(dir, "terminal-browser")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	client := termbrowser.New()
	client.Bin = bin
	return client, runs
}

func countRuns(t *testing.T, path string) int {
	t.Helper()
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(content), "run\n")
}

func TestPollerReadsEngineAtMostEveryTwoSeconds(t *testing.T) {
	h := buildModel(t)
	createSession(t, h, "coder", t.TempDir(), "")
	client, runs := fakeBrowserEngine(t, "unused")
	h.poller.browsers = client
	for range 3 {
		h.applyCmd(t, nil)
	}
	if got := countRuns(t, runs); got != 0 {
		t.Fatalf("with no browser row listed the poller ran ls %d times, want 0", got)
	}

	createSessionOn(t, h, "docs", "browser", t.TempDir())
	var browserID string
	for _, sess := range h.sessionRows() {
		if sess.Tool == "browser" {
			browserID = sess.ID
		}
	}
	client, runs = fakeBrowserEngine(t, browserID)
	clock := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	client.Now = func() time.Time { return clock }
	h.poller.browsers = client
	var snapshot Snapshot
	step := func() {
		t.Helper()
		result := h.poller.Step()
		if result.Err != nil {
			t.Fatal(result.Err)
		}
		snapshot = result.Snapshot
	}
	for range 3 {
		step()
		clock = clock.Add(600 * time.Millisecond)
	}
	if got := countRuns(t, runs); got != 1 {
		t.Fatalf("three ticks inside two seconds ran ls %d times, want 1", got)
	}
	clock = clock.Add(200 * time.Millisecond)
	step()
	if got := countRuns(t, runs); got != 2 {
		t.Fatalf("a tick two seconds after the first read ran ls %d times in all, want 2", got)
	}
	if !snapshot.BrowsersRead {
		t.Fatal("the pass should report that it read the browser engines")
	}
	engine, ok := snapshot.Browsers[browserID]
	if !ok || engine.URL != "https://example.org/docs" || engine.Title != "Example Docs" || !engine.AgentDriven {
		t.Fatalf("engine for %s = %+v, %v", browserID, engine, ok)
	}
	stored, err := h.store.Get(browserID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.BrowserURL != "https://example.org/docs" {
		t.Fatalf("stored browser url = %q, want the active tab's", stored.BrowserURL)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := h.tmux.CapturePane(browserID)
		if err == nil && strings.Contains(pane, placeholderCell) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the browser pane never painted placeholder cells: %q", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}
	result := h.poller.Step()
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if line := result.Snapshot.PaneLines[browserID]; strings.Contains(line, placeholderCell) {
		t.Fatalf("a browser row's quote carries placeholder cells: %q", line)
	}
}
