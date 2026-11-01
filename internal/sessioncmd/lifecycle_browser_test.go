package sessioncmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/testenv"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/google/uuid"
)

const browserPageURL = "https://example.com/page?a=1&b=two words"

type browserLifecycleHarness struct {
	*sessionHarness
	lifecycle *Lifecycle
	launches  string
}

// newBrowserLifecycleHarness stands a terminal-browser in for the real one,
// by absolute path so no shell profile can put the real binary first. Each
// launch writes its arguments as one line, every argument closed by a bar,
// then waits the way a browser does.
func newBrowserLifecycleHarness(t *testing.T) *browserLifecycleHarness {
	t.Helper()
	testenv.SkipPOSIXShell(t)
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	launches := filepath.Join(dir, "launches")
	standIn := filepath.Join(dir, "terminal-browser")
	script := "#!/bin/sh\n{ printf '%s|' \"$(basename \"$0\")\" \"$@\"; printf '\\n'; } >> " + tmux.ShellQuote(launches) + "\nexec cat\n"
	if err := os.WriteFile(standIn, []byte(script), 0o755); err != nil {
		t.Fatalf("write the terminal-browser stand-in: %v", err)
	}
	h := newSessionHarness(t)
	const builtinCommand = `command = "terminal-browser open --no-merge"`
	if !strings.Contains(sessionConfig, builtinCommand) {
		t.Fatalf("the test tools no longer declare %s", builtinCommand)
	}
	h.sessions.loadConfig = testConfigLoader(t, strings.Replace(sessionConfig, builtinCommand, `command = "`+standIn+` open --no-merge"`, 1))
	lifecycle, err := NewLifecycle(harnessRuntime(t, h))
	if err != nil {
		t.Fatal(err)
	}
	return &browserLifecycleHarness{sessionHarness: h, lifecycle: lifecycle, launches: launches}
}

// TestKillAndDeleteReleaseABrowsersAgentSession covers the shared path the
// manager's kill, archive, restart and delete and the kill_session tool take.
func TestKillAndDeleteReleaseABrowsersAgentSession(t *testing.T) {
	h := newBrowserLifecycleHarness(t)
	engine := &fakeBrowserEngine{browsers: fixtureBrowsers(t)}
	h.lifecycle.browserEngine = engine
	browser := store.Session{ID: fixtureHackerNewsSession, Name: "browser-row", Tool: "browser", Cwd: h.caller.Cwd, Group: h.caller.Group, Status: status.Dead, ParentID: h.caller.ID}
	if err := h.store.CreateSession(browser); err != nil {
		t.Fatal(err)
	}
	agent := store.Session{ID: fixtureBrowserSession, Name: "agent-row", Tool: "claude", Cwd: h.caller.Cwd, Group: h.caller.Group, Status: status.Dead}
	if err := h.store.CreateSession(agent); err != nil {
		t.Fatal(err)
	}
	paneAlive := false
	engine.onDone = func(string) { paneAlive = h.driver.Exists(browser.ID) }

	h.shellPane(t, browser)
	h.shellPane(t, agent)
	if _, err := h.lifecycle.Kill(agent); err != nil {
		t.Fatalf("Kill agent: %v", err)
	}
	if len(engine.doneKeys) != 0 {
		t.Fatalf("killing an agent released %q", engine.doneKeys)
	}
	if _, err := h.lifecycle.Kill(browser); err != nil {
		t.Fatalf("Kill browser: %v", err)
	}
	if len(engine.doneKeys) != 1 || engine.doneKeys[0] != fixtureHackerNewsBrowserID || !paneAlive {
		t.Fatalf("kill released %q with the pane alive %t", engine.doneKeys, paneAlive)
	}

	h.shellPane(t, browser)
	paneAlive = false
	if _, err := h.lifecycle.DeleteForHuman(DeleteSelection{Sessions: []store.Session{browser}}); err != nil {
		t.Fatalf("Delete browser: %v", err)
	}
	if len(engine.doneKeys) != 2 || !paneAlive {
		t.Fatalf("delete released %q with the pane alive %t", engine.doneKeys, paneAlive)
	}

	engine.browsers = nil
	stale := store.Session{ID: "c1c2c3c4", Name: "browser-stale", Tool: "browser", Cwd: h.caller.Cwd, Group: h.caller.Group, Status: status.Dead, ParentID: h.caller.ID}
	if err := h.store.CreateSession(stale); err != nil {
		t.Fatal(err)
	}
	h.shellPane(t, stale)
	if _, err := h.lifecycle.Kill(stale); err != nil {
		t.Fatalf("Kill a browser with no engine: %v", err)
	}
	if h.driver.Exists(stale.ID) || len(engine.doneKeys) != 2 {
		t.Fatalf("a browser with no engine: pane alive %t, released %q", h.driver.Exists(stale.ID), engine.doneKeys)
	}
}

func (h *browserLifecycleHarness) row(t *testing.T, tool, url string) store.Session {
	t.Helper()
	sess := store.Session{
		ID:         uuid.NewString()[:8],
		Name:       tool + "-row",
		Tool:       tool,
		Cwd:        h.caller.Cwd,
		Group:      h.caller.Group,
		Status:     status.Dead,
		ParentID:   h.caller.ID,
		BrowserURL: url,
	}
	if err := h.store.CreateSession(sess); err != nil {
		t.Fatalf("create %s row: %v", tool, err)
	}
	return sess
}

// shellPane opens the row's pane on a bare shell, as a pane looks once the
// program it ran has exited.
func (h *browserLifecycleHarness) shellPane(t *testing.T, sess store.Session) {
	t.Helper()
	if err := h.driver.Create(sess.ID, sess.Cwd, "", nil, 80, 24); err != nil {
		t.Fatalf("create pane: %v", err)
	}
	if err := h.store.UpdateStatus(sess.ID, status.Idle); err != nil {
		t.Fatal(err)
	}
}

func browserLaunchLine(url string) string {
	if url == "" {
		return "terminal-browser|open|--no-merge|"
	}
	return "terminal-browser|open|--no-merge|" + url + "|"
}

func (h *browserLifecycleHarness) waitForLaunches(t *testing.T, want ...string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	expected := strings.Join(want, "\n")
	var got string
	for time.Now().Before(deadline) {
		content, _ := os.ReadFile(h.launches)
		if got = strings.TrimRight(string(content), "\n"); got == expected {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("terminal-browser launches:\n%s\nwant:\n%s", got, expected)
}

func TestReviveBrowserReopensItsURL(t *testing.T) {
	for name, url := range map[string]string{"with-url": browserPageURL, "without-url": ""} {
		t.Run(name, func(t *testing.T) {
			h := newBrowserLifecycleHarness(t)
			sess := h.row(t, "browser", url)
			result, err := h.lifecycle.Revive(sess, PaneSize{Width: 80, Height: 24})
			if err != nil {
				t.Fatalf("Revive: %v", err)
			}
			if result.ReusedPane || result.Session.BrowserURL != url {
				t.Fatalf("revive result %+v, want a new pane keeping url %q", result, url)
			}
			h.waitForLaunches(t, browserLaunchLine(url))
			stored, err := h.store.Get(sess.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.BrowserURL != url || stored.Status != status.Starting {
				t.Fatalf("stored row after revive: url %q status %q", stored.BrowserURL, stored.Status)
			}
		})
	}
}

func TestRelaunchInPaneAcceptsABrowser(t *testing.T) {
	h := newBrowserLifecycleHarness(t)
	browser := h.row(t, "browser", browserPageURL)
	h.shellPane(t, browser)
	result, err := h.lifecycle.Revive(browser, PaneSize{Width: 80, Height: 24})
	if err != nil {
		t.Fatalf("Revive a browser whose pane is on its shell: %v", err)
	}
	if !result.ReusedPane {
		t.Fatalf("revive result %+v, want the browser started in its own pane", result)
	}
	h.waitForLaunches(t, browserLaunchLine(browserPageURL))

	terminal := h.row(t, "terminal", "")
	h.shellPane(t, terminal)
	if _, err := h.lifecycle.Revive(terminal, PaneSize{Width: 80, Height: 24}); err == nil || !strings.Contains(err.Error(), "its pane is already open") {
		t.Fatalf("revive of a live terminal = %v, want the refusal", err)
	}
}

func TestRelaunchInPaneTypesTheBrowserCommand(t *testing.T) {
	h := newBrowserLifecycleHarness(t)
	browser := h.row(t, "browser", browserPageURL)
	h.shellPane(t, browser)
	cfg, err := h.sessions.loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	relaunched, err := RelaunchInPane(h.driver, h.store, h.lifecycle.runtime.Hooks, browser.ID, cfg.Tools["browser"].Launch())
	if err != nil {
		t.Fatalf("RelaunchInPane: %v", err)
	}
	if relaunched.BrowserURL != browserPageURL {
		t.Fatalf("relaunched row url = %q, want %q", relaunched.BrowserURL, browserPageURL)
	}
	h.waitForLaunches(t, browserLaunchLine(browserPageURL))
}

func TestRestartBrowserReopensItsURL(t *testing.T) {
	const movedURL = "https://example.org/moved"
	for _, pane := range []string{"running", "shell", "dead"} {
		t.Run(pane, func(t *testing.T) {
			h := newBrowserLifecycleHarness(t)
			sess := h.row(t, "browser", browserPageURL)
			var want []string
			switch pane {
			case "running":
				cfg, err := h.sessions.loadConfig()
				if err != nil {
					t.Fatal(err)
				}
				if err := h.store.Delete(sess.ID); err != nil {
					t.Fatal(err)
				}
				tool := cfg.Tools["browser"].Launch()
				if _, err := h.lifecycle.Launch(LaunchRequest{Session: sess, Tool: tool, BaseCommand: launch.BrowserCommand(tool.Command, sess.BrowserURL), Pane: PaneSize{Width: 80, Height: 24}}); err != nil {
					t.Fatalf("Launch: %v", err)
				}
				want = append(want, browserLaunchLine(browserPageURL))
				h.waitForLaunches(t, want...)
			case "shell":
				h.shellPane(t, sess)
			}
			if err := h.store.SetBrowserURL(sess.ID, movedURL); err != nil {
				t.Fatal(err)
			}
			result, err := h.lifecycle.Restart(sess, PaneSize{Width: 80, Height: 24})
			if err != nil {
				t.Fatalf("Restart: %v", err)
			}
			if result.Conversation != "" || result.Session.RetiredAgentSessionID != "" || result.Session.BrowserURL != movedURL {
				t.Fatalf("restart result %+v, want no conversation and the stored url", result)
			}
			if result.ReusedPane != (pane == "shell") {
				t.Fatalf("restart reused pane = %v on a %s pane", result.ReusedPane, pane)
			}
			want = append(want, browserLaunchLine(movedURL))
			h.waitForLaunches(t, want...)
		})
	}
}
