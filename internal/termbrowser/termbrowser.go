package termbrowser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	binaryName     = "terminal-browser"
	sessionPrefix  = "am_"
	releaseTimeout = 5 * time.Second
)

var ErrNoBrowser = errors.New("no browser engine is running in that pane")

// FramesEnv picks how terminal-browser sends its frames to the terminal.
const FramesEnv = "TERMINAL_BROWSER_FRAMES"

// FramesFor answers terminal-browser's startup frame probe for it: a browser
// started on the manager's server has no terminal attached to answer, so it
// would fall back to inline frames, which tmux relays slowly. Ghostty on the
// same machine reads shared memory; everything else keeps the probe's answer.
func FramesFor(termProgram string, remote bool) string {
	if termProgram == "ghostty" && !remote {
		return "shm"
	}
	return ""
}

type Tab struct {
	ID              int    `json:"id"`
	URL             string `json:"url"`
	Title           string `json:"title"`
	Active          bool   `json:"active"`
	TargetID        string `json:"targetId"`
	AgentControlled bool   `json:"agentControlled"`
}

type Browser struct {
	Key     string `json:"key"`
	PID     int    `json:"pid"`
	CDPPort int    `json:"cdpPort"`
	Pane    struct {
		Tab  string `json:"tab"`
		Pane string `json:"pane"`
	} `json:"pane"`
	Tabs []Tab `json:"tabs"`
}

func (b Browser) ActiveTab() (Tab, bool) {
	for _, tab := range b.Tabs {
		if tab.Active {
			return tab, true
		}
	}
	return Tab{}, false
}

// PageText drops control and private use characters from page-supplied text,
// so a title can carry neither escape sequences nor image placeholder cells
// onto the manager's screen.
func PageText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Co, r) {
			return -1
		}
		return r
	}, text)
}

func (b Browser) SessionID() string {
	rest, hasPrefix := strings.CutPrefix(b.Pane.Tab, sessionPrefix)
	if !hasPrefix {
		return ""
	}
	id, _, hasWindow := strings.Cut(rest, ":")
	if !hasWindow {
		return ""
	}
	return id
}

type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	TimedOut bool
}

type Client struct {
	Bin     string
	Timeout time.Duration
	Now     func() time.Time
	run     func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error)

	cacheMu   sync.Mutex
	cached    []Browser
	cachedErr error
	cachedAt  time.Time
}

func New() *Client {
	return &Client{Bin: binaryName, Timeout: 60 * time.Second, Now: time.Now, run: commandRunner(nil)}
}

// NewOnServer points terminal-browser at the tmux server at socket. Its tmux
// backend starts only from $TMUX, and without that backend ls names no
// browser's pane.
func NewOnServer(socket string) *Client {
	client := New()
	client.run = commandRunner(serverEnv(socket))
	return client
}

func serverEnv(socket string) []string {
	inherited := os.Environ()
	env := make([]string, 0, len(inherited)+1)
	for _, entry := range inherited {
		name, _, _ := strings.Cut(entry, "=")
		if name == "TMUX" || name == "TMUX_PANE" {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "TMUX="+socket+",0,0")
}

func Installed() bool {
	_, err := exec.LookPath(binaryName)
	return err == nil
}

func (c *Client) List(ctx context.Context) ([]Browser, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	stdout, stderr, exitCode, err := c.run(ctx, c.Bin, "ls", "--all", "--json")
	if err != nil {
		return nil, fmt.Errorf("%s ls: %w", c.Bin, err)
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("%s ls exited %d: %s", c.Bin, exitCode, strings.TrimSpace(string(stderr)))
	}
	var listing struct {
		Browsers []Browser `json:"browsers"`
	}
	if err := json.Unmarshal(stdout, &listing); err != nil {
		return nil, fmt.Errorf("%s ls: decode: %w", c.Bin, err)
	}
	return listing.Browsers, nil
}

// ListCached holds a failed ls for maxAge too, so a hung engine costs one
// timeout per age rather than one per call.
func (c *Client) ListCached(ctx context.Context, maxAge time.Duration) ([]Browser, error) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if !c.cachedAt.IsZero() && c.Now().Sub(c.cachedAt) < maxAge {
		return c.cached, c.cachedErr
	}
	c.cached, c.cachedErr = c.List(ctx)
	c.cachedAt = c.Now()
	return c.cached, c.cachedErr
}

func (c *Client) ForSession(ctx context.Context, sessionID string) (Browser, error) {
	browsers, err := c.List(ctx)
	if err != nil {
		return Browser{}, err
	}
	for _, browser := range browsers {
		if sessionID != "" && browser.SessionID() == sessionID {
			return browser, nil
		}
	}
	return Browser{}, ErrNoBrowser
}

// Done runs close to end the agent-browser session terminal-browser keeps for
// the browser at key, which outlives the browser's pane otherwise.
func (c *Client) Done(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, releaseTimeout)
	defer cancel()
	_, stderr, exitCode, err := c.run(ctx, c.Bin, "action", "--browser", key, "--", "close")
	if err != nil {
		return fmt.Errorf("%s action close: %w", c.Bin, err)
	}
	if exitCode != 0 {
		return fmt.Errorf("%s action close exited %d: %s", c.Bin, exitCode, strings.TrimSpace(string(stderr)))
	}
	return nil
}

func (c *Client) Action(ctx context.Context, key, command string) (Result, error) {
	words, err := SplitCommand(command)
	if err != nil {
		return Result{}, err
	}
	if len(words) == 0 {
		return Result{}, errors.New("browser command is empty")
	}
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	args := append([]string{"action", "--browser", key, "--"}, words...)
	stdout, stderr, exitCode, err := c.run(ctx, c.Bin, args...)
	result := Result{Stdout: string(stdout), Stderr: string(stderr), ExitCode: exitCode}
	if errors.Is(err, context.DeadlineExceeded) {
		result.TimedOut = true
		return result, nil
	}
	return result, err
}

func commandRunner(env []string) func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	return func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		return runCommand(ctx, env, name, args...)
	}
}

func runCommand(ctx context.Context, env []string, name string, args ...string) ([]byte, []byte, int, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = env
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	configureBoundedCommand(command)
	// A child that inherits the pipes would otherwise keep Run blocked past the deadline.
	command.WaitDelay = 2 * time.Second
	runErr := command.Run()
	if errors.Is(runErr, exec.ErrWaitDelay) && command.ProcessState != nil {
		return stdout.Bytes(), stderr.Bytes(), command.ProcessState.ExitCode(), nil
	}
	if ctxErr := ctx.Err(); runErr != nil && ctxErr != nil {
		return stdout.Bytes(), stderr.Bytes(), -1, ctxErr
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return stdout.Bytes(), stderr.Bytes(), exitErr.ExitCode(), nil
	}
	if runErr != nil {
		return stdout.Bytes(), stderr.Bytes(), -1, runErr
	}
	return stdout.Bytes(), stderr.Bytes(), 0, nil
}
