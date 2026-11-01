package sessioncmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/termbrowser"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

type Browser struct {
	ID         string `json:"id" jsonschema:"managed browser session id"`
	Name       string `json:"name" jsonschema:"browser name shown in Agent Manager"`
	Group      string `json:"group" jsonschema:"group path holding the browser; empty is the root"`
	Directory  string `json:"directory" jsonschema:"directory the browser pane runs in"`
	Status     string `json:"status" jsonschema:"stored Agent Manager status"`
	Running    bool   `json:"running" jsonschema:"whether the browser currently has a live tmux pane"`
	ParentID   string `json:"parent_id" jsonschema:"id of the parent session when nested; empty when un-nested"`
	ParentName string `json:"parent_name" jsonschema:"name of the parent session when nested; empty when un-nested"`
	URL        string `json:"url" jsonschema:"url of the active tab, or the launch url until the engine reports one"`
	Title      string `json:"title" jsonschema:"title of the active tab; empty until the engine reports one"`
}

type CreateBrowserOptions struct {
	URL       string
	Group     *string
	Directory string
	Nest      *bool
}

type BrowserOutput struct {
	BrowserID string `json:"browser_id"`
	Command   string `json:"command"`
	Output    string `json:"output"`
	ExitCode  int    `json:"exit_code"`
}

const (
	browserCommandBudget = 60 * time.Second
	engineStartBudget    = 20 * time.Second
	engineStartPoll      = 500 * time.Millisecond
	browserReleaseBudget = 5 * time.Second
)

var browserGOOS = goruntime.GOOS

type browserEngine interface {
	List(ctx context.Context) ([]termbrowser.Browser, error)
	ForSession(ctx context.Context, sessionID string) (termbrowser.Browser, error)
	Action(ctx context.Context, key, command string) (termbrowser.Result, error)
	Done(ctx context.Context, key string) error
}

type Browsers struct {
	commands
	// engine stands in for terminal-browser in tests. Nil runs the real one
	// on the opened runtime's tmux server.
	engine          browserEngine
	installed       func() bool
	now             func() time.Time
	engineStartPoll time.Duration
}

var browserKind = childKind{
	noun:     "browser",
	listWord: func(words Vocabulary) string { return words.ListBrowsers },
	toolName: config.BrowserTool,
}

func NewBrowsers(configDir string, words Vocabulary) *Browsers {
	return newBrowsers(configDir, words, tmux.New, nil)
}

func NewBrowsersWithBackend(backend *Backend, words Vocabulary) *Browsers {
	if backend == nil {
		panic("session command backend is required")
	}
	return &Browsers{
		commands:        commands{words: words, backend: backend},
		installed:       termbrowser.Installed,
		now:             time.Now,
		engineStartPoll: engineStartPoll,
	}
}

func newBrowsers(configDir string, words Vocabulary, newDriver func() (*tmux.Driver, error), engine browserEngine) *Browsers {
	return &Browsers{
		commands:        commands{configDir: configDir, words: words, newDriver: newDriver, loadConfig: config.Default},
		engine:          engine,
		installed:       termbrowser.Installed,
		now:             time.Now,
		engineStartPoll: engineStartPoll,
	}
}

// engineOn reaches terminal-browser through the manager's tmux server. Its
// tmux backend starts only from $TMUX, and without it ls names no browser's
// pane, so a command typed outside tmux would find no browser.
func (b *Browsers) engineOn(driver *tmux.Driver) browserEngine {
	if b.engine != nil {
		return b.engine
	}
	return termbrowser.NewOnServer(driver.SocketPath())
}

// releaseBrowserAgent ends the agent-browser session of the browser in
// sessionID's pane before the pane goes. It is best effort by design: a
// browser whose engine is gone or never started has nothing to release, and
// ending the row must not wait on it past the budget.
func releaseBrowserAgent(engine browserEngine, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), browserReleaseBudget)
	defer cancel()
	engineBrowser, err := engine.ForSession(ctx, sessionID)
	if err != nil {
		return
	}
	_ = engine.Done(ctx, engineBrowser.Key)
}

func CheckBrowserPrerequisites(driver *tmux.Driver, installed func() bool) error {
	if browserGOOS == "windows" {
		return errors.New("the browser needs WSL on Windows; run agent-manager inside your WSL distro")
	}
	major, minor, err := driver.ServerVersion()
	if err != nil {
		return err
	}
	if !tmux.VersionAtLeast(major, minor, 3, 3) {
		return fmt.Errorf("the browser needs tmux 3.3 or newer (you have %d.%d)", major, minor)
	}
	if !installed() {
		return config.MissingToolError{Binary: "terminal-browser"}
	}
	return nil
}

func validBrowserURL(raw string) error {
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("url %q does not parse: %w", raw, err)
	}
	switch {
	case (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != "":
		return nil
	case parsed.Scheme == "file" && strings.HasPrefix(parsed.Path, "/"):
		return nil
	default:
		return fmt.Errorf("url %q must be an http:// or https:// address with a host, or a file:// url with an absolute path", raw)
	}
}

func browserFrom(terminal Terminal, pageURL string) Browser {
	return Browser{
		ID:         terminal.ID,
		Name:       terminal.Name,
		Group:      terminal.Group,
		Directory:  terminal.Directory,
		Status:     terminal.Status,
		Running:    terminal.Running,
		ParentID:   terminal.ParentID,
		ParentName: terminal.ParentName,
		URL:        pageURL,
	}
}

func (b *Browsers) List(sessionID string) ([]Browser, error) {
	runtime, err := b.open()
	if err != nil {
		return nil, err
	}
	defer runtime.Close()
	infos, rows, err := runtime.listChildren(browserKind, sessionID)
	if err != nil {
		return nil, err
	}
	browsers := make([]Browser, 0, len(infos))
	for index, info := range infos {
		browsers = append(browsers, browserFrom(info, rows[index].BrowserURL))
	}
	if len(browsers) == 0 {
		return browsers, nil
	}
	// The listing only refines what the store already holds, so an engine
	// that cannot answer leaves each browser on its launch url.
	reported, err := b.engineOn(runtime.driver).List(context.Background())
	if err != nil {
		return browsers, nil
	}
	activeTabs := make(map[string]termbrowser.Tab, len(reported))
	for _, engineBrowser := range reported {
		if tab, ok := engineBrowser.ActiveTab(); ok {
			activeTabs[engineBrowser.SessionID()] = tab
		}
	}
	for index := range browsers {
		if tab, ok := activeTabs[browsers[index].ID]; ok {
			browsers[index].URL = tab.URL
			browsers[index].Title = tab.Title
		}
	}
	return browsers, nil
}

func (b *Browsers) Create(sessionID string, opts CreateBrowserOptions) (Browser, error) {
	if err := validBrowserURL(opts.URL); err != nil {
		return Browser{}, err
	}
	runtime, err := b.open()
	if err != nil {
		return Browser{}, err
	}
	defer runtime.Close()
	caller, err := runtime.optionalCaller(sessionID)
	if err != nil {
		return Browser{}, err
	}
	if err := CheckBrowserPrerequisites(runtime.driver, b.installed); err != nil {
		return Browser{}, err
	}
	_, browser := runtime.cfg.Browser()
	launched, err := b.launchChild(runtime, caller, childLaunch{
		kind:       browserKind,
		command:    launch.BrowserCommand(browser.Command, opts.URL),
		browserURL: opts.URL,
		group:      opts.Group,
		directory:  opts.Directory,
		nest:       opts.Nest,
	})
	if err != nil {
		return Browser{}, err
	}
	info, err := runtime.info(launched, true)
	if err != nil {
		return Browser{}, err
	}
	return browserFrom(info, opts.URL), nil
}

func (b *Browsers) Command(sessionID, browserID, command string) (BrowserOutput, error) {
	runtime, err := b.open()
	if err != nil {
		return BrowserOutput{}, err
	}
	defer runtime.Close()
	browser, err := runtime.nestedChild(browserKind, sessionID, browserID)
	if err != nil {
		return BrowserOutput{}, err
	}
	if !runtime.driver.Exists(browser.ID) {
		return BrowserOutput{}, fmt.Errorf("browser %s is not running; revive it in Agent Manager first", browser.ID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), browserCommandBudget)
	defer cancel()
	engine := b.engineOn(runtime.driver)
	starting := b.engineMayBeStarting(browser)
	engineBrowser, err := b.engineFor(ctx, engine, browser, starting)
	if errors.Is(err, termbrowser.ErrNoBrowser) {
		if starting {
			return BrowserOutput{}, fmt.Errorf("browser %s is still starting; retry in a few seconds", browser.ID)
		}
		return BrowserOutput{}, fmt.Errorf("browser %s has no engine running; revive it in Agent Manager, or call %s for a new one", browser.ID, runtime.words.CreateBrowser)
	}
	if err != nil {
		return BrowserOutput{}, err
	}
	result, err := engine.Action(ctx, engineBrowser.Key, command)
	if err != nil {
		return BrowserOutput{}, err
	}
	if result.TimedOut {
		return BrowserOutput{}, errors.New("browser command timed out after 60s")
	}
	output := BrowserOutput{BrowserID: browser.ID, Command: command, Output: result.Stdout, ExitCode: result.ExitCode}
	if result.ExitCode != 0 {
		if result.Stderr != "" {
			output.Output = result.Stderr
		}
		return output, fmt.Errorf("browser command exited %d: %s", result.ExitCode, strings.TrimSpace(output.Output))
	}
	return output, nil
}

func (b *Browsers) engineMayBeStarting(browser store.Session) bool {
	return browser.Status == status.Starting || b.now().Sub(browser.LaunchTime()) < engineStartBudget
}

// engineFor waits out Electron's startup on a starting browser, which
// terminal-browser does not list until its engine is up, and then the first
// page, which a cold engine shows as about:blank for a few seconds.
func (b *Browsers) engineFor(ctx context.Context, engine browserEngine, browser store.Session, starting bool) (termbrowser.Browser, error) {
	engineBrowser, err := engine.ForSession(ctx, browser.ID)
	if !starting {
		return engineBrowser, err
	}
	deadline := b.now().Add(engineStartBudget)
	for stillLoading(engineBrowser, err, browser.BrowserURL) && b.now().Before(deadline) {
		select {
		case <-ctx.Done():
			return termbrowser.Browser{}, ctx.Err()
		case <-time.After(b.engineStartPoll):
		}
		engineBrowser, err = engine.ForSession(ctx, browser.ID)
	}
	return engineBrowser, err
}

// stillLoading holds a browser opened without a URL to its first listing,
// since a blank tab is its real state.
func stillLoading(engineBrowser termbrowser.Browser, err error, launchURL string) bool {
	if errors.Is(err, termbrowser.ErrNoBrowser) {
		return true
	}
	if err != nil || launchURL == "" {
		return false
	}
	tab, _ := engineBrowser.ActiveTab()
	return tab.URL == "" || tab.URL == "about:blank"
}

func (b *Browsers) Close(sessionID, browserID string) error {
	return b.closeChild(browserKind, sessionID, browserID, func(runtime *runtime, browser store.Session) {
		releaseBrowserAgent(b.engineOn(runtime.driver), browser.ID)
	})
}
