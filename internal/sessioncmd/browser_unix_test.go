//go:build !windows

package sessioncmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/termbrowser"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/google/uuid"
)

func prependStub(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatalf("write %s stub: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// newStubTmuxBrowsers serves the prerequisite checks a tmux that reports
// version and never starts a server, so a refusal is proven to come first.
func newStubTmuxBrowsers(t *testing.T, version string) (*Browsers, string) {
	t.Helper()
	prependStub(t, "tmux", "#!/bin/sh\necho 'tmux "+version+"'\n")
	driver, err := tmux.NewWithSocket("ambrowsertest-" + uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("tmux driver: %v", err)
	}
	configDir := t.TempDir()
	browsers := newBrowsers(configDir, MCPVocabulary(), func() (*tmux.Driver, error) { return driver, nil }, &fakeBrowserEngine{})
	browsers.installed = func() bool { return true }
	return browsers, configDir
}

func assertNoSessionRows(t *testing.T, configDir string) {
	t.Helper()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer st.Close()
	sessions, err := st.ListSessions(true)
	if err != nil || len(sessions) != 0 {
		t.Fatalf("a refused create left rows %+v (%v)", sessions, err)
	}
}

func TestCreateBrowserRefusesOldTmux(t *testing.T) {
	browsers, configDir := newStubTmuxBrowsers(t, "3.2")
	_, err := browsers.Create("", CreateBrowserOptions{URL: "https://example.com"})
	if err == nil || !strings.Contains(err.Error(), "the browser needs tmux 3.3 or newer (you have 3.2)") {
		t.Fatalf("old tmux error = %v", err)
	}
	assertNoSessionRows(t, configDir)
}

func TestBrowserPrerequisitesPassOtherPlatformsThrough(t *testing.T) {
	original := browserGOOS
	t.Cleanup(func() { browserGOOS = original })
	for _, platform := range []string{"linux", "darwin"} {
		browserGOOS = platform
		browsers, _ := newStubTmuxBrowsers(t, "3.2")
		_, err := browsers.Create("", CreateBrowserOptions{})
		if err == nil || !strings.Contains(err.Error(), "the browser needs tmux 3.3 or newer (you have 3.2)") {
			t.Fatalf("%s reached error %v, want the tmux version check", platform, err)
		}
	}
}

func TestCreateBrowserRefusesWhenNotInstalled(t *testing.T) {
	browsers, configDir := newStubTmuxBrowsers(t, "3.4")
	browsers.installed = func() bool { return false }
	_, err := browsers.Create("", CreateBrowserOptions{})
	if err == nil || !strings.HasPrefix(err.Error(), "terminal-browser is not installed; install") {
		t.Fatalf("not installed error = %v", err)
	}
	var missing config.MissingToolError
	if !errors.As(err, &missing) || missing.Binary != "terminal-browser" {
		t.Fatalf("not installed error = %#v, want config.MissingToolError for terminal-browser", err)
	}
	assertNoSessionRows(t, configDir)
}

func TestCreateBrowserNestsNamesAndQuotesTheURL(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	prependStub(t, "terminal-browser", "#!/bin/sh\nprintf '%s\\n' \"$@\" > "+tmux.ShellQuote(argsFile)+"\nexec sleep 600\n")
	h := newBrowserHarness(t)
	h.browsers.installed = termbrowser.Installed
	if major, minor, err := h.driver.ServerVersion(); err != nil || !tmux.VersionAtLeast(major, minor, 3, 3) {
		t.Skipf("the browser needs tmux 3.3 or newer: %d.%d %v", major, minor, err)
	}

	const launchURL = "https://example.com/?q=it's&next=1;done"
	created, err := h.browsers.Create(h.caller.ID, CreateBrowserOptions{URL: launchURL})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "browser-"+h.caller.Name || created.ParentID != h.caller.ID || created.ParentName != h.caller.Name {
		t.Fatalf("created = %+v", created)
	}
	if created.URL != launchURL || !created.Running || created.Group != h.caller.Group {
		t.Fatalf("created = %+v", created)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatalf("stored browser: %v", err)
	}
	if stored.Tool != "browser" || stored.BrowserURL != launchURL {
		t.Fatalf("stored browser = %+v", stored)
	}

	waitForFile(t, argsFile, "open\n--no-merge\n"+launchURL+"\n")

	sibling, err := h.browsers.Create(created.ID, CreateBrowserOptions{})
	if err != nil {
		t.Fatalf("Create from a browser: %v", err)
	}
	if sibling.ParentID != h.caller.ID || sibling.Name != "browser-"+h.caller.Name+"-2" || sibling.URL != "" {
		t.Fatalf("browser opened from a browser = %+v, want a sibling under %s", sibling, h.caller.ID)
	}
}

// terminal-browser names a browser's pane only through its tmux backend,
// which starts from $TMUX, so the default engine must point it at the
// manager's own server even when the command runs outside tmux.
func TestBrowsersDefaultEngineRunsOnTheManagersServer(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "tmux")
	prependStub(t, "terminal-browser", "#!/bin/sh\nprintf '%s|%s' \"$TMUX\" \"${TMUX_PANE-unset}\" > "+tmux.ShellQuote(seen)+"\necho '{\"browsers\":[]}'\n")
	h := newBrowserHarness(t)
	h.browsers.engine = nil
	h.addBrowserRow(t, uuid.NewString()[:8], h.caller.ID, "https://example.com/")
	if _, err := h.browsers.List(h.caller.ID); err != nil {
		t.Fatalf("List: %v", err)
	}
	got, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("the default engine never ran terminal-browser: %v", err)
	}
	if want := h.driver.SocketPath() + ",0,0|unset"; string(got) != want {
		t.Fatalf("terminal-browser saw TMUX|TMUX_PANE = %q, want %q", got, want)
	}
}

func waitForFile(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var got []byte
	for time.Now().Before(deadline) {
		if got, _ = os.ReadFile(path); string(got) == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s holds %q, want %q", path, got, want)
}
