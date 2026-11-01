package sessioncmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/termbrowser"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/google/uuid"
)

const (
	fixtureBrowserSession      = "b1b2b3b4"
	fixtureHackerNewsSession   = "a1a2a3a4"
	fixtureHackerNewsBrowserID = "96030-2"
)

type fakeBrowserEngine struct {
	browsers        []termbrowser.Browser
	listErr         error
	result          termbrowser.Result
	actionKey       string
	actionCommand   string
	startupFailures int
	blankReads      int
	forSessionCalls int
	actionCalls     int
	onForSession    func()
	doneKeys        []string
	onDone          func(key string)
}

func (engine *fakeBrowserEngine) List(context.Context) ([]termbrowser.Browser, error) {
	return engine.browsers, engine.listErr
}

func (engine *fakeBrowserEngine) ForSession(_ context.Context, sessionID string) (termbrowser.Browser, error) {
	engine.forSessionCalls++
	if engine.onForSession != nil {
		engine.onForSession()
	}
	if engine.forSessionCalls <= engine.startupFailures {
		return termbrowser.Browser{}, termbrowser.ErrNoBrowser
	}
	for _, browser := range engine.browsers {
		if browser.SessionID() != sessionID {
			continue
		}
		if engine.forSessionCalls <= engine.startupFailures+engine.blankReads {
			browser.Tabs = []termbrowser.Tab{{ID: 1, URL: "about:blank", Active: true}}
		}
		return browser, nil
	}
	return termbrowser.Browser{}, termbrowser.ErrNoBrowser
}

func (engine *fakeBrowserEngine) Done(_ context.Context, key string) error {
	engine.doneKeys = append(engine.doneKeys, key)
	if engine.onDone != nil {
		engine.onDone(key)
	}
	return nil
}

func (engine *fakeBrowserEngine) Action(_ context.Context, key, command string) (termbrowser.Result, error) {
	engine.actionCalls++
	engine.actionKey, engine.actionCommand = key, command
	return engine.result, nil
}

func fixtureBrowsers(t *testing.T) []termbrowser.Browser {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "termbrowser", "testdata", "ls.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var listing struct {
		Browsers []termbrowser.Browser `json:"browsers"`
	}
	if err := json.Unmarshal(data, &listing); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return listing.Browsers
}

type browserHarness struct {
	*terminalHarness
	engine   *fakeBrowserEngine
	browsers *Browsers
}

func newBrowserHarness(t *testing.T) *browserHarness {
	t.Helper()
	terminals := newTerminalHarness(t)
	engine := &fakeBrowserEngine{browsers: fixtureBrowsers(t)}
	browsers := newBrowsers(terminals.configDir, MCPVocabulary(), func() (*tmux.Driver, error) { return terminals.driver, nil }, engine)
	browsers.installed = func() bool { return true }
	return &browserHarness{terminalHarness: terminals, engine: engine, browsers: browsers}
}

func (h *browserHarness) addAgent(t *testing.T, name string) store.Session {
	t.Helper()
	agent := store.Session{
		ID:     uuid.NewString()[:8],
		Name:   name,
		Tool:   "claude",
		Cwd:    h.caller.Cwd,
		Group:  h.caller.Group,
		Status: status.Idle,
	}
	if err := h.store.CreateSession(agent); err != nil {
		t.Fatalf("create agent %s: %v", name, err)
	}
	return agent
}

// addBrowserRow stands a browser row with a live pane under parent, so the
// fixture's engine listing lines up with it by session id.
func (h *browserHarness) addBrowserRow(t *testing.T, id, parentID, launchURL string) store.Session {
	t.Helper()
	browser := store.Session{
		ID:         id,
		Name:       "browser-" + id,
		Tool:       "browser",
		Cwd:        h.caller.Cwd,
		Group:      h.caller.Group,
		Status:     status.Idle,
		ParentID:   parentID,
		BrowserURL: launchURL,
	}
	if err := h.driver.Create(browser.ID, browser.Cwd, "", nil, 80, 24); err != nil {
		t.Fatalf("create browser pane: %v", err)
	}
	if err := h.store.CreateSession(browser); err != nil {
		t.Fatalf("create browser row: %v", err)
	}
	return browser
}

func TestValidBrowserURL(t *testing.T) {
	for _, test := range []struct {
		url    string
		accept bool
	}{
		{"", true},
		{"http://x", true},
		{"https://x/y", true},
		{"file:///tmp/a.html", true},
		{"javascript:alert(1)", false},
		{"not a url", false},
		{"ftp://x", false},
		{"https:", false},
		{"file:relative", false},
	} {
		err := validBrowserURL(test.url)
		if accepted := err == nil; accepted != test.accept {
			t.Errorf("validBrowserURL(%q) = %v, want accept %t", test.url, err, test.accept)
		}
	}
}

func TestBrowserPrerequisitesRefuseNativeWindows(t *testing.T) {
	original := browserGOOS
	t.Cleanup(func() { browserGOOS = original })
	browserGOOS = "windows"
	err := CheckBrowserPrerequisites(nil, func() bool {
		t.Fatal("the refusal should come before the install check")
		return false
	})
	if err == nil || err.Error() != "the browser needs WSL on Windows; run agent-manager inside your WSL distro" {
		t.Fatalf("native Windows error = %v", err)
	}
}

func TestCreateBrowserRejectsABadURL(t *testing.T) {
	browsers := newBrowsers(t.TempDir(), MCPVocabulary(), func() (*tmux.Driver, error) {
		t.Fatal("a bad url reached tmux")
		return nil, nil
	}, &fakeBrowserEngine{})
	for _, bad := range []string{"javascript:alert(1)", "not a url", "ftp://example.com/file"} {
		if _, err := browsers.Create("", CreateBrowserOptions{URL: bad}); err == nil || !strings.Contains(err.Error(), "url") {
			t.Fatalf("url %q error = %v", bad, err)
		}
	}
}

func TestBrowserCommandTargetsTheCallersBrowserOnly(t *testing.T) {
	h := newBrowserHarness(t)
	other := h.addAgent(t, "other-agent")
	h.addBrowserRow(t, fixtureHackerNewsSession, h.caller.ID, "")
	h.addBrowserRow(t, fixtureBrowserSession, other.ID, "")
	h.engine.result = termbrowser.Result{Stdout: "https://news.ycombinator.com/\n"}

	if _, err := h.browsers.Command(h.caller.ID, fixtureBrowserSession, "get url"); err == nil || !strings.Contains(err.Error(), "not nested under this session") {
		t.Fatalf("commanding another session's browser = %v", err)
	}
	if h.engine.actionKey != "" {
		t.Fatalf("a refused command still reached the engine with key %q", h.engine.actionKey)
	}
	output, err := h.browsers.Command(h.caller.ID, fixtureHackerNewsSession, "get url")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if h.engine.actionKey != fixtureHackerNewsBrowserID || h.engine.actionCommand != "get url" {
		t.Fatalf("engine got key %q command %q", h.engine.actionKey, h.engine.actionCommand)
	}
	want := BrowserOutput{BrowserID: fixtureHackerNewsSession, Command: "get url", Output: "https://news.ycombinator.com/\n"}
	if output != want {
		t.Fatalf("output = %+v, want %+v", output, want)
	}
}

// stepClock stands in for the wall clock and the poll wait, moving time on by
// the poll interval at every engine read so a twenty second wait runs at once.
func (h *browserHarness) stepClock(start time.Time) {
	current := start
	h.browsers.now = func() time.Time { return current }
	h.browsers.engineStartPoll = time.Millisecond
	h.engine.onForSession = func() { current = current.Add(500 * time.Millisecond) }
}

func TestBrowserCommandWithoutEngineNamesCreateBrowser(t *testing.T) {
	h := newBrowserHarness(t)
	h.engine.browsers = nil
	h.addBrowserRow(t, fixtureHackerNewsSession, h.caller.ID, "")
	h.stepClock(time.Now().Add(time.Hour))
	_, err := h.browsers.Command(h.caller.ID, fixtureHackerNewsSession, "get url")
	if err == nil || !strings.Contains(err.Error(), "has no engine running") || !strings.Contains(err.Error(), MCPVocabulary().CreateBrowser) {
		t.Fatalf("no engine error = %v", err)
	}
	if h.engine.forSessionCalls != 1 {
		t.Fatalf("an old browser read the engine %d times, want 1", h.engine.forSessionCalls)
	}
}

func TestBrowserCommandWaitsForAYoungEngine(t *testing.T) {
	h := newBrowserHarness(t)
	h.addBrowserRow(t, fixtureHackerNewsSession, h.caller.ID, "")
	h.stepClock(time.Now())
	h.engine.startupFailures = 2
	h.engine.result = termbrowser.Result{Stdout: "https://news.ycombinator.com/\n"}
	if _, err := h.browsers.Command(h.caller.ID, fixtureHackerNewsSession, "get url"); err != nil {
		t.Fatalf("Command on a starting engine: %v", err)
	}
	if h.engine.forSessionCalls != 3 {
		t.Fatalf("read the engine %d times, want 3", h.engine.forSessionCalls)
	}
	if h.engine.actionKey != fixtureHackerNewsBrowserID {
		t.Fatalf("engine got key %q", h.engine.actionKey)
	}
}

func TestBrowserCommandWaitsForTheFirstPage(t *testing.T) {
	h := newBrowserHarness(t)
	h.addBrowserRow(t, fixtureHackerNewsSession, h.caller.ID, "https://news.ycombinator.com/")
	h.stepClock(time.Now())
	h.engine.blankReads = 2
	h.engine.onForSession = chain(h.engine.onForSession, func() {
		if h.engine.actionCalls != 0 {
			t.Errorf("the command ran before read %d", h.engine.forSessionCalls)
		}
	})
	if _, err := h.browsers.Command(h.caller.ID, fixtureHackerNewsSession, "snapshot -i"); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if h.engine.forSessionCalls != 3 || h.engine.actionCalls != 1 {
		t.Fatalf("read the engine %d times and ran %d commands, want 3 and 1", h.engine.forSessionCalls, h.engine.actionCalls)
	}
}

func TestBrowserCommandWithoutALaunchURLRunsOnTheFirstListing(t *testing.T) {
	h := newBrowserHarness(t)
	h.addBrowserRow(t, fixtureHackerNewsSession, h.caller.ID, "")
	h.stepClock(time.Now())
	h.engine.blankReads = 2
	if _, err := h.browsers.Command(h.caller.ID, fixtureHackerNewsSession, "snapshot -i"); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if h.engine.forSessionCalls != 1 || h.engine.actionCalls != 1 {
		t.Fatalf("read the engine %d times and ran %d commands, want 1 and 1", h.engine.forSessionCalls, h.engine.actionCalls)
	}
}

func chain(first, second func()) func() {
	return func() {
		first()
		second()
	}
}

func TestBrowserCommandReportsAnEngineStillStarting(t *testing.T) {
	h := newBrowserHarness(t)
	h.engine.browsers = nil
	h.addBrowserRow(t, fixtureHackerNewsSession, h.caller.ID, "")
	h.stepClock(time.Now())
	started := time.Now()
	_, err := h.browsers.Command(h.caller.ID, fixtureHackerNewsSession, "get url")
	want := "browser " + fixtureHackerNewsSession + " is still starting; retry in a few seconds"
	if err == nil || err.Error() != want {
		t.Fatalf("engine that never came up = %v, want %q", err, want)
	}
	if h.engine.forSessionCalls < 2 || h.engine.actionKey != "" {
		t.Fatalf("read the engine %d times and acted with key %q", h.engine.forSessionCalls, h.engine.actionKey)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("the wait took %v on the injected clock", elapsed)
	}
}

func TestBrowserCommandReportsExitAndTimeout(t *testing.T) {
	h := newBrowserHarness(t)
	h.addBrowserRow(t, fixtureHackerNewsSession, h.caller.ID, "")

	h.engine.result = termbrowser.Result{Stdout: "partial", Stderr: "error: tab_gone\n", ExitCode: 1}
	output, err := h.browsers.Command(h.caller.ID, fixtureHackerNewsSession, "click @e9")
	if err == nil || err.Error() != "browser command exited 1: error: tab_gone" {
		t.Fatalf("non-zero exit error = %v", err)
	}
	if output.ExitCode != 1 || output.Output != "error: tab_gone\n" || output.BrowserID != fixtureHackerNewsSession {
		t.Fatalf("non-zero exit output = %+v", output)
	}

	h.engine.result = termbrowser.Result{Stdout: "only stdout", ExitCode: 2}
	output, err = h.browsers.Command(h.caller.ID, fixtureHackerNewsSession, "click @e9")
	if err == nil || output.Output != "only stdout" || output.ExitCode != 2 {
		t.Fatalf("stdout fallback = %+v, %v", output, err)
	}

	h.engine.result = termbrowser.Result{ExitCode: -1, TimedOut: true}
	if _, err := h.browsers.Command(h.caller.ID, fixtureHackerNewsSession, "wait 999"); err == nil || err.Error() != "browser command timed out after 60s" {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestBrowserCommandRefusesAStoppedBrowser(t *testing.T) {
	h := newBrowserHarness(t)
	browser := h.addBrowserRow(t, fixtureHackerNewsSession, h.caller.ID, "")
	if err := h.driver.Kill(browser.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.browsers.Command(h.caller.ID, browser.ID, "get url"); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("stopped browser error = %v", err)
	}
}

func TestListBrowsersCarriesURLAndTitleFromTheEngine(t *testing.T) {
	h := newBrowserHarness(t)
	other := h.addAgent(t, "other-agent")
	h.addBrowserRow(t, fixtureHackerNewsSession, h.caller.ID, "https://launch.example/")
	unknown := h.addBrowserRow(t, "c1c2c3c4", other.ID, "https://stored.example/")

	listed, err := h.browsers.List(h.caller.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byID := make(map[string]Browser, len(listed))
	for _, browser := range listed {
		byID[browser.ID] = browser
	}
	if len(listed) != 2 {
		t.Fatalf("listed = %+v, want both browsers and no agents", listed)
	}
	reported := byID[fixtureHackerNewsSession]
	if reported.URL != "https://news.ycombinator.com/" || reported.Title != "Hacker News" {
		t.Fatalf("engine-reported browser = %+v", reported)
	}
	if reported.ParentID != h.caller.ID || reported.ParentName != h.caller.Name || !reported.Running {
		t.Fatalf("engine-reported browser parent = %+v", reported)
	}
	stored := byID[unknown.ID]
	if stored.URL != "https://stored.example/" || stored.Title != "" || stored.ParentName != other.Name {
		t.Fatalf("browser the engine does not know = %+v", stored)
	}

	h.engine.listErr = errors.New("terminal-browser ls exited 1")
	listed, err = h.browsers.List(h.caller.ID)
	if err != nil {
		t.Fatalf("List with a failing engine: %v", err)
	}
	for _, browser := range listed {
		if browser.ID == fixtureHackerNewsSession && browser.URL != "https://launch.example/" {
			t.Fatalf("failing engine left url %q, want the stored launch url", browser.URL)
		}
	}
}

func TestCloseBrowserDeletesItAndRefusesAnotherSessionsBrowser(t *testing.T) {
	h := newBrowserHarness(t)
	other := h.addAgent(t, "other-agent")
	own := h.addBrowserRow(t, fixtureHackerNewsSession, h.caller.ID, "")
	foreign := h.addBrowserRow(t, fixtureBrowserSession, other.ID, "")

	if err := h.browsers.Close(h.caller.ID, foreign.ID); err == nil || !strings.Contains(err.Error(), "not nested under this session") {
		t.Fatalf("closing another session's browser = %v", err)
	}
	if !h.driver.Exists(foreign.ID) {
		t.Fatal("a refused close killed the pane")
	}
	if err := h.browsers.Close(h.caller.ID, own.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := h.store.Get(own.ID); err == nil {
		t.Fatal("browser row still present")
	}
	if h.driver.Exists(own.ID) {
		t.Fatal("browser pane still live")
	}
}

func TestCloseBrowserReleasesTheAgentSessionBeforeTheKill(t *testing.T) {
	h := newBrowserHarness(t)
	own := h.addBrowserRow(t, fixtureHackerNewsSession, h.caller.ID, "")
	paneAliveAtRelease := false
	h.engine.onDone = func(string) { paneAliveAtRelease = h.driver.Exists(own.ID) }
	if err := h.browsers.Close(h.caller.ID, own.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(h.engine.doneKeys) != 1 || h.engine.doneKeys[0] != fixtureHackerNewsBrowserID {
		t.Fatalf("released keys %q, want [%s]", h.engine.doneKeys, fixtureHackerNewsBrowserID)
	}
	if !paneAliveAtRelease {
		t.Fatal("the release ran after the pane was killed")
	}
	if h.driver.Exists(own.ID) {
		t.Fatal("browser pane still live")
	}
}

func TestCloseBrowserWithoutAnEngineStillCloses(t *testing.T) {
	h := newBrowserHarness(t)
	h.engine.browsers = nil
	own := h.addBrowserRow(t, fixtureHackerNewsSession, h.caller.ID, "")
	if err := h.browsers.Close(h.caller.ID, own.ID); err != nil {
		t.Fatalf("Close with no engine: %v", err)
	}
	if len(h.engine.doneKeys) != 0 {
		t.Fatalf("released keys %q with no engine listed", h.engine.doneKeys)
	}
	if _, err := h.store.Get(own.ID); err == nil || h.driver.Exists(own.ID) {
		t.Fatal("the browser outlived a close with no engine")
	}
}

func TestBrowserToolsRefuseATerminalId(t *testing.T) {
	h := newBrowserHarness(t)
	terminal, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("Create terminal: %v", err)
	}
	if _, err := h.browsers.Command(h.caller.ID, terminal.ID, "get url"); err == nil || !strings.Contains(err.Error(), "not a browser") {
		t.Fatalf("terminal id at a browser tool = %v", err)
	}
	if _, err := h.browsers.Command(h.caller.ID, "", "get url"); err == nil || !strings.Contains(err.Error(), "browser_id is empty; call list_browsers") {
		t.Fatalf("empty browser id = %v", err)
	}
	if _, err := h.browsers.Command(h.caller.ID, "ffffffff", "get url"); err == nil || !strings.Contains(err.Error(), "browser ffffffff does not exist; call list_browsers") {
		t.Fatalf("missing browser id = %v", err)
	}
}

func TestTerminalToolsRefuseABrowserId(t *testing.T) {
	h := newBrowserHarness(t)
	browser := h.addBrowserRow(t, fixtureHackerNewsSession, h.caller.ID, "")
	if _, err := h.terminals.Send(h.caller.ID, browser.ID, "pwd", nil); err == nil || !strings.Contains(err.Error(), "not a terminal") {
		t.Fatalf("browser id at a terminal tool = %v", err)
	}
	listed, err := h.terminals.List(h.caller.ID)
	if err != nil {
		t.Fatalf("List terminals: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("terminal list carries a browser: %+v", listed)
	}
}

func TestAgentToolsRefuseABrowserTheWayTheyRefuseATerminal(t *testing.T) {
	h := newSessionHarness(t)
	browserID := uuid.NewString()[:8]
	if err := h.store.CreateSession(store.Session{
		ID: browserID, Name: "browser-calling-agent", Tool: "browser", Cwd: h.caller.Cwd,
		Group: h.caller.Group, Status: status.Idle, ParentID: h.caller.ID,
	}); err != nil {
		t.Fatalf("create browser row: %v", err)
	}
	const refusal = "is not an agent; use the terminal or browser tools for it"
	if _, err := h.sessions.Send(h.caller.ID, browserID, "hello"); err == nil || !strings.Contains(err.Error(), refusal) {
		t.Fatalf("send at a browser = %v", err)
	}
	if _, err := h.sessions.Read(h.caller.ID, browserID); err == nil || !strings.Contains(err.Error(), refusal) {
		t.Fatalf("read at a browser = %v", err)
	}
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "browser"}); err == nil || !strings.Contains(err.Error(), "create_browser") {
		t.Fatalf("create_session with the browser tool = %v", err)
	}
	listed, err := h.sessions.List(h.caller.ID)
	if err != nil {
		t.Fatalf("List sessions: %v", err)
	}
	for _, session := range listed {
		if session.ID == browserID {
			t.Fatalf("session list carries the browser: %+v", session)
		}
	}
}
