package tmux

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/keybind"
)

const prefix = "am_"

// defaultSocket is the private tmux server name the manager runs every agent
// on. A dedicated -L socket keeps agent sessions off the user's default
// socket, where a shell tmux, a `go test`, or a stray kill-server would
// otherwise share a server with the live agents and take them all down at once.
const defaultSocket = "agentmgr"

// requestOption is the global tmux user option the in-session bindings set
// on their way out, naming what the manager should do with the session they
// just detached from.
const requestOption = "@am_request"

// pinnedPrefixOption marks a prefix the manager set, the only kind unpinPrefix removes.
const pinnedPrefixOption = "@am_pinned_prefix"

const (
	RequestReview = "review"
	RequestEditor = "editor"
)

type Driver struct {
	bin    string
	socket string

	attachSizeLargest atomic.Bool
	paneTheme         atomic.Pointer[PaneTheme]
	paneThemePush     sync.Mutex
	socketPath        atomic.Pointer[string]
	sessionKeys       atomic.Pointer[keybind.Table]
}

func (d *Driver) SetSessionKeys(keys keybind.Table) {
	d.sessionKeys.Store(&keys)
}

func (d *Driver) currentSessionKeys() keybind.Table {
	if keys := d.sessionKeys.Load(); keys != nil {
		return *keys
	}
	return keybind.DefaultSession()
}

func New() (*Driver, error) {
	return NewWithSocket(defaultSocket)
}

// NewWithSocket builds a driver bound to a named tmux server. Tests pass an
// isolated socket so their sessions never collide with the default socket or
// with live agents on the production socket.
func NewWithSocket(socket string) (*Driver, error) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return nil, fmt.Errorf("tmux not found on PATH: %w\n%s", err, deps.Hint("tmux"))
	}
	return &Driver{bin: bin, socket: socket}, nil
}

func (d *Driver) SocketName() string {
	return d.socket
}

// SocketPath is the file the server this driver talks to listens on. The
// -L name does not identify a server on its own: tmux resolves it under
// TMUX_TMPDIR, so two managers started with different values of that
// variable drive different servers under one socket name, each blind to
// the other's sessions.
func (d *Driver) SocketPath() string {
	if cached := d.socketPath.Load(); cached != nil {
		return *cached
	}
	out, err := d.run("display-message", "-p", "#{socket_path}")
	if err != nil {
		return socketPathFromEnv(d.socket)
	}
	path := strings.TrimSpace(out)
	if path == "" {
		return socketPathFromEnv(d.socket)
	}
	d.socketPath.Store(&path)
	return path
}

// socketPathFromEnv rebuilds what tmux resolves -L to, without asking a
// server that may not be running. tmux reports the path with its symlinks
// resolved, so this does too and the two agree once a server exists.
func socketPathFromEnv(socket string) string {
	dir := "/tmp"
	// tmux skips a TMUX_TMPDIR it cannot resolve.
	if custom := os.Getenv("TMUX_TMPDIR"); custom != "" {
		if _, err := os.Stat(custom); err == nil {
			dir = custom
		}
	}
	// tmux takes a relative TMUX_TMPDIR from its own working directory and
	// reports the resolved path, so the same absolute form is what a session
	// has to be stamped with for a later poll to recognise it.
	if absolute, err := filepath.Abs(dir); err == nil {
		dir = absolute
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return filepath.Join(dir, fmt.Sprintf("tmux-%d", os.Getuid()), socket)
}

func sessionName(id string) string {
	return prefix + id
}

// windowTarget addresses the window the agent's pane lives in, the
// session's first, which is not the session's current window once anyone
// opens a second one inside it.
func windowTarget(id string) string {
	return sessionName(id) + ":^"
}

// PaneTarget addresses the pane the agent itself runs in. A bare session
// name does not: tmux resolves that to whichever pane is active, and an
// agent is free to split the window and hand focus to the new pane, as
// Claude Code's agent teams do when they run a teammate beside their
// leader. Reading or typing through a session-wide target then lands on
// the teammate. ":^" pins the session's first window for the same reason,
// and EnsureBindings pins pane-base-index so ".0" is the agent's pane on
// any user's tmux config.
func PaneTarget(id string) string {
	return windowTarget(id) + ".0"
}

// tmux requires -L <socket> before the command word.
func (d *Driver) args(a ...string) []string {
	return append([]string{"-L", d.socket}, a...)
}

func (d *Driver) run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), driverCommandTimeout)
	defer cancel()
	return d.runContext(ctx, args...)
}

const driverCommandTimeout = 5 * time.Second

func (d *Driver) runContext(ctx context.Context, args ...string) (string, error) {
	return d.runContextStarted(ctx, nil, args...)
}

func (d *Driver) runContextStarted(ctx context.Context, started func(), args ...string) (string, error) {
	release, err := enterGateContext(ctx, d.socket, false)
	if err != nil {
		return "", err
	}
	defer release()
	cmd := exec.CommandContext(ctx, d.bin, d.args(args...)...)
	configureBoundedCommand(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
	}
	if started != nil {
		started()
	}
	err = cmd.Wait()
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			err = contextErr
		}
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

// commandList joins commands into the single invocation tmux takes for a
// whole list, where any argument ending in ";" ends one command and starts
// the next, losing that character: a value that has to keep a trailing
// semicolon writes it as \;. tmux stops at the first command that fails and
// exits non-zero, so the list reports a failure the way a run per command
// did.
func commandList(commands ...[]string) []string {
	var args []string
	for _, command := range commands {
		if len(args) > 0 {
			args = append(args, ";")
		}
		args = append(args, command...)
	}
	return args
}

// SendCommand runs one tmux command from already-separated arguments,
// for a command such as if-shell whose branch is itself a command and so
// cannot survive the whitespace split SendRaw does.
func (d *Driver) SendCommand(args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("tmux command is empty")
	}
	_, err := d.run(args...)
	return err
}

// A missing tmux server means no request rather than an error: the
// manager outlives the sessions it opens.
func (d *Driver) PendingRequest() (string, error) {
	out, err := exec.Command(d.bin, d.args("show-option", "-gqv", requestOption)...).CombinedOutput()
	if err != nil {
		if noServer(string(out)) {
			return "", nil
		}
		return "", fmt.Errorf("tmux show-option %s: %w: %s", requestOption, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// ClearRequest unsets the marker so a request is carried out once.
func (d *Driver) ClearRequest() error {
	_, err := d.run("set-option", "-gu", requestOption)
	return err
}

func (d *Driver) Kill(id string) error {
	if !d.Exists(id) {
		os.Remove(launchScriptPath(id))
		return nil
	}
	_, err := d.run("kill-session", "-t", sessionName(id))
	os.Remove(launchScriptPath(id))
	return err
}

func (d *Driver) Exists(id string) bool {
	exists, _ := d.SessionExists(id)
	return exists
}

// SessionExists distinguishes a confirmed missing session/server from a tmux
// transport failure. Callers making destructive cleanup decisions must use
// this typed result instead of treating every command error as absence.
func (d *Driver) SessionExists(id string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), driverCommandTimeout)
	defer cancel()
	return d.SessionExistsContext(ctx, id)
}

// ExistsContext checks liveness without leaving an unbounded tmux child behind
// when its caller is shutting down.
func (d *Driver) ExistsContext(ctx context.Context, id string) bool {
	exists, _ := d.SessionExistsContext(ctx, id)
	return exists
}

func (d *Driver) SessionExistsContext(ctx context.Context, id string) (bool, error) {
	_, err := d.runContext(ctx, "has-session", "-t", sessionName(id))
	if err == nil {
		return true, nil
	}
	if noServer(err.Error()) || strings.Contains(err.Error(), "can't find session") {
		return false, nil
	}
	return false, err
}

// noServer recognizes both messages tmux prints when no server is up:
// "no server running on <socket>" and, on Linux since 3.4, "error
// connecting to <socket> (No such file or directory)".
func noServer(out string) bool {
	return strings.Contains(out, "no server running") ||
		strings.Contains(out, "error connecting to")
}
