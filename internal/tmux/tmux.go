package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
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

// rootKeysOption is the user option where psmux servers record the root
// keys the manager bound, so the next run can take them off.
const rootKeysOption = "@am_root_keys"

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

// PaneTheme is the text and background colors agent panes are rendered on.
// The manager knows them — it paints every capture with them and sets the
// terminal to them for a full-screen attach — but an agent inside a pane
// cannot discover them: these sessions run on a server whose only client is
// in control mode, so there is no terminal to answer an OSC 10 or 11 color
// query, and the environment carries no COLORFGBG either. Declaring them on
// the server hands an auto-detecting agent the answer the manager already
// renders, instead of leaving it to guess.
type PaneTheme struct {
	Foreground string // "#rrggbb"; tmux answers pane OSC 10 queries with it
	Background string // "#rrggbb"; tmux answers pane OSC 11 queries with it
	ColorFgBg  string // "fg;bg" color indexes for agents reading COLORFGBG
}

// paneThemeArgs is the option pair as a tmux command list. window-style and
// the environment are both server-global: every managed session lives on
// this socket and wants the same answer, and a global option also reaches
// windows a user opens inside a session later.
func paneThemeArgs(t PaneTheme) []string {
	return []string{
		"set-option", "-g", "window-style", "fg=" + t.Foreground + ",bg=" + t.Background, ";",
		"set-environment", "-g", "COLORFGBG", t.ColorFgBg,
	}
}

// PublishPaneTheme records the pane colors so Create hands them to new
// sessions. It only stores the value; PushPaneTheme sends it to a running
// server. Recording is synchronous so a session created right after a theme
// change still opens on the chosen background.
func (d *Driver) PublishPaneTheme(t PaneTheme) {
	d.paneTheme.Store(&t)
}

// PushPaneTheme sends the recorded pane theme to a running server. It always
// pushes the latest published value under a lock, so concurrent pushes are
// latest-wins: whichever runs last writes the current theme rather than an
// older one it was spawned for. A server with no sessions exits immediately,
// so there is nothing to push to before the first session exists; Create
// re-applies the recorded theme in the same command list as its new-session,
// which is also what keeps the option set before the agent process can query
// it.
func (d *Driver) PushPaneTheme() error {
	d.paneThemePush.Lock()
	defer d.paneThemePush.Unlock()
	theme := d.paneTheme.Load()
	if theme == nil {
		return nil
	}
	return d.eachServer(func(id string) error {
		out, err := d.output(append(sessionRoute(id), paneThemeArgs(*theme)...)...)
		if err != nil {
			if noServer(string(out)) {
				return nil
			}
			return fmt.Errorf("tmux set pane theme: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	})
}

func New() (*Driver, error) {
	return NewWithSocket(defaultSocket)
}

// NewWithSocket builds a driver bound to a named tmux server. Tests pass an
// isolated socket so their sessions never collide with the default socket or
// with live agents on the production socket.
func NewWithSocket(socket string) (*Driver, error) {
	bin, err := exec.LookPath(Binary)
	if err != nil {
		return nil, fmt.Errorf("%s not found on PATH: %w\n%s", Binary, err, deps.Hint(Binary))
	}

	return &Driver{bin: bin, socket: socket}, nil
}

// Version probes the multiplexer's own version on the manager's socket,
// which the socket isolation invariant requires of every invocation, even
// one that opens no server.
func Version() (string, error) {
	d, err := New()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, d.bin, d.args("-V")...)
	cmd.Env = commandEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (d *Driver) SocketName() string {
	return d.socket
}
func sessionName(id string) string {
	return prefix + id
}

// windowTarget addresses the window the agent's pane lives in, the
// session's first, which is not the session's current window once anyone
// opens a second one inside it.
func windowTarget(id string) string {
	return sessionName(id) + ":" + firstWindow
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

// command builds one invocation of the multiplexer on this driver's socket.
func (d *Driver) command(args ...string) *exec.Cmd {
	cmd := exec.Command(d.bin, d.args(args...)...)
	cmd.Env = commandEnv()
	return cmd
}

// output runs a command list and returns everything it printed. tmux takes
// the list in one invocation; psmux's command line reads only one command,
// so there each runs on its own, stopping at the first that fails the way
// tmux does.
func (d *Driver) output(args ...string) ([]byte, error) {
	commands := splitCommandList(args)
	if len(commands) == 1 {
		return d.command(commands[0]...).CombinedOutput()
	}
	var out []byte
	for _, command := range commands {
		data, err := d.command(command...).CombinedOutput()
		out = append(out, data...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func (d *Driver) run(args ...string) (string, error) {
	release, err := enterGate(d.socket, false)
	if err != nil {
		return "", err
	}
	defer release()
	out, err := d.output(args...)
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// runIn runs a command on the server that hosts session id. tmux has one
// server for every session, so this is run; psmux runs a server per
// session, and a command without a target goes to whichever it finds.
func (d *Driver) runIn(id string, args ...string) (string, error) {
	return d.run(append(sessionRoute(id), args...)...)
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

// afterCreateThemeLoad runs between Create loading the pane theme and writing
// it, while Create holds the push lock. Nil in production; a test sets it to
// drive a push against the held lock and prove the write ordering.
var afterCreateThemeLoad func()

func (d *Driver) Create(id, cwd, command string, env map[string]string, width, height int) error {
	name := sessionName(id)
	// Hold the push lock across loading the theme and the command that
	// writes it, so a concurrent PushPaneTheme cannot land a newer theme
	// between the load and the write and be clobbered by this stale one.
	d.paneThemePush.Lock()
	theme := d.paneTheme.Load()
	var colorFgBg string
	if theme != nil {
		colorFgBg = theme.ColorFgBg
	}
	if afterCreateThemeLoad != nil {
		afterCreateThemeLoad()
	}
	// Launch through a script. Typing the full line with send-keys
	// truncates around 1024 bytes, which breaks long first prompts
	// mid-path. A script has no practical length limit, and handing the
	// pane to the user's shell afterwards matches "type into a shell"
	// (pane stays up).
	var scriptPath string
	if command != "" {
		var err error
		scriptPath, err = writeLaunchScript(id, env, command, colorFgBg)
		if err != nil {
			d.paneThemePush.Unlock()
			return err
		}
	}
	runErr := d.startSession(id, cwd, width, height, scriptPath, theme)
	d.paneThemePush.Unlock()
	if runErr != nil {
		if scriptPath != "" {
			os.Remove(scriptPath)
		}
		return runErr
	}
	if err := d.installSessionUX(name); err != nil {
		_ = d.Kill(id)
		return err
	}
	return nil
}

func sortedKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// relaunchHint lands in the pane the moment the agent exits, which is where
// the user is looking when they wonder how to get it back.
const relaunchHint = "agent-manager: agent exited - press v in Agent Manager to relaunch it here."

func (d *Driver) installSessionUX(name string) error {
	if err := d.ensureBindingsIn(strings.TrimPrefix(name, prefix)); err != nil {
		return err
	}
	// A new session carries no pin to take off, so only a chosen prefix needs work here.
	if keys := d.tmuxPrefixKeys(); len(keys) > 0 {
		if err := d.pinPrefix(name, keys); err != nil {
			return err
		}
	}
	if err := d.styleStatusBar(name); err != nil {
		return err
	}
	_, err := d.run("set-option", "-t", name, "status-left", "")
	return err
}

func (d *Driver) tmuxPrefixKeys() []keybind.Key {
	return d.currentSessionKeys().Binding(keybind.TmuxPrefix).Keys()
}

// Set per session, so unsetting hands back the server-wide prefix tmux.conf sets.
func (d *Driver) pinPrefix(name string, keys []keybind.Key) error {
	secondary := "None"
	if len(keys) > 1 {
		secondary = keys[1].Tmux()
	}
	_, err := d.run(commandList(
		[]string{"set-option", "-t", name, "prefix", keys[0].Tmux()},
		[]string{"set-option", "-t", name, "prefix2", secondary},
		[]string{"set-option", "-t", name, pinnedPrefixOption, "on"},
	)...)
	return err
}

func (d *Driver) unpinPrefix(name string) error {
	pinned, err := d.run("show-options", "-q", "-v", "-t", name, pinnedPrefixOption)
	if err != nil {
		return err
	}
	if strings.TrimSpace(pinned) == "" {
		return nil
	}
	_, err = d.run(commandList(
		[]string{"set-option", "-u", "-t", name, "prefix"},
		[]string{"set-option", "-u", "-t", name, "prefix2"},
		[]string{"set-option", "-u", "-t", name, pinnedPrefixOption},
	)...)
	return err
}

// Tmux answers an un-overridden session option with an empty string rather than the global option.
func (d *Driver) resolvedOption(name, option string) (string, error) {
	value, err := d.run("show-options", "-t", name, "-v", option)
	if err != nil {
		return "", err
	}
	if value = strings.TrimSpace(value); value != "" {
		return value, nil
	}
	global, err := d.runIn(strings.TrimPrefix(name, prefix), "show-options", "-g", "-v", option)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(global), nil
}

// styleStatusBar sets a session's status bar chrome, leaving status-left (the
// name label) untouched so re-styling a live session keeps its label.
func (d *Driver) styleStatusBar(name string) error {
	primary, err := d.resolvedOption(name, "prefix")
	if err != nil {
		return err
	}
	secondary, err := d.resolvedOption(name, "prefix2")
	if err != nil {
		return err
	}
	options := [][]string{
		{"set-option", "-t", name, "status", "on"},
		// The default status-right-length of 40 truncates the hints, so widen it
		// to fit the whole footer, including the configured-prefix fallback.
		{"set-option", "-t", name, "status-right-length", "100"},
		{"set-option", "-t", name, "status-right", attachStatusRight(primary, secondary, d.currentSessionKeys())},
		{"set-option", "-t", name, "status-style", "bg=colour236,fg=colour249"},
		// hide the "0:windowname*" window list; it reads as noise here
		{"set-option", "-t", name, "window-status-format", hiddenWindowStatus},
		{"set-option", "-t", name, "window-status-current-format", hiddenWindowStatus},
		// mouse on so tmux handles scrollback per-session instead of the
		// terminal emulator, whose buffer carries content from prior attaches.
		{"set-option", "-t", name, "mouse", "on"},
	}
	_, err = d.run(commandList(options...)...)
	return err
}

// attachStatusRight is the session footer: the keys the manager keeps,
// and every way back. A detach key the inner prefix shadows is left off,
// since the prefix takes it first, and the prefix itself follows with d.
func attachStatusRight(primary, secondary string, keys keybind.Table) string {
	parts := []string{"agent-manager"}
	if label := titledLabel(keys.Binding(keybind.Review)); label != "" {
		parts = append(parts, label+" = review")
	}
	if label := titledLabel(keys.Binding(keybind.Editor)); label != "" {
		parts = append(parts, label+" = editor")
	}
	var exits []string
	for _, key := range keys.Binding(keybind.Detach).Keys() {
		if key.Tmux() != primary && key.Tmux() != secondary {
			exits = append(exits, titled(key))
		}
	}
	for _, candidate := range []string{primary, secondary} {
		if candidate != "" && candidate != "None" {
			exits = append(exits, candidate+" d")
			break
		}
	}
	parts = append(parts, strings.Join(exits, " / ")+" = back")
	return " " + strings.Join(parts, " · ") + " "
}

func titledLabel(binding keybind.Binding) string {
	names := make([]string, 0, len(binding.Keys()))
	for _, key := range binding.Keys() {
		names = append(names, titled(key))
	}
	return strings.Join(names, " / ")
}

func titled(key keybind.Key) string {
	name := key.Tea()
	return strings.ToUpper(name[:1]) + name[1:]
}

// ownedBindingTest is the session-name check every root binding the
// manager installs on tmux carries, so that on the next run its own
// bindings can be told from the ones the user's tmux.conf put on this
// server. psmux records its keys instead (rootKeysOption).
const ownedBindingTest = "#{m:" + prefix + "*,#{session_name}}"

// EnsureBindings installs the server-wide setup every managed session
// relies on: the in-session key bindings, and the pane numbering
// PaneTarget addresses. A tmux config that starts panes at 1 is the
// dangerous case for the numbering, because tmux answers a target whose
// index does not exist with the active pane rather than an error, so
// every capture and keystroke would silently follow the focused pane.
//
// The bindings an earlier run installed come off first, whatever keys
// that run was configured with: a key the user moved or turned off would
// otherwise stay bound until the server restarts.
//
// psmux keeps options and bindings per session server, so there the setup
// goes to every managed session's server in turn.
func (d *Driver) EnsureBindings() error {
	return d.eachServer(d.ensureBindingsIn)
}

// ensureBindingsIn installs the setup on the server hosting session id.
func (d *Driver) ensureBindingsIn(id string) error {
	stale, err := d.ownedRootBindings(id)
	if err != nil {
		return err
	}
	keys := d.currentSessionKeys()
	commands := [][]string{
		{"set-window-option", "-g", "pane-base-index", "0"},
	}
	for _, key := range stale {
		commands = append(commands, []string{"unbind-key", "-T", "root", key})
	}
	var bound []string
	bind := func(binding keybind.Binding, action []string) {
		for _, key := range binding.Keys() {
			commands = append(commands, rootBinding(key, action...))
			bound = append(bound, key.Tmux())
		}
	}
	bind(keys.Binding(keybind.Detach), []string{"detach-client"})
	bind(keys.Binding(keybind.Review), d.requestAction(id, RequestReview))
	bind(keys.Binding(keybind.Editor), d.requestAction(id, RequestEditor))
	commands = append(commands, recordRootBindings(bound)...)
	// Restore the standard fallback when the prefix shadows a direct binding.
	commands = append(commands, []string{"bind-key", "-T", "prefix", "d", bindingArg("detach-client")})
	// list-keys starts a server that exits again straight away when it has
	// no sessions, so the list can find none. Nothing needs the setup then:
	// Create runs this again once a session brings the server up.
	if _, err = d.runIn(id, commandList(commands...)...); err != nil && noServer(err.Error()) {
		return nil
	}
	return err
}

// RefreshChrome re-applies the prefix and status bar chrome, keeping the session's name label.
func (d *Driver) RefreshChrome(id string) error {
	name := sessionName(id)
	var err error
	if keys := d.tmuxPrefixKeys(); len(keys) > 0 {
		err = d.pinPrefix(name, keys)
	} else {
		err = d.unpinPrefix(name)
	}
	if err != nil {
		return err
	}
	return d.styleStatusBar(name)
}

// SendText delivers text into the session's pane and presses Enter, so the
// agent inside receives it as a user message.
func (d *Driver) SendText(id, text string) error {
	return d.pasteAndEnter(id, text)
}

// SendKeys delivers exact tmux key names to a session. Keeping each key as
// its own argv entry avoids routing agent-supplied input through a shell.
func (d *Driver) SendKeys(id string, keys ...string) error {
	args := []string{"send-keys", "-t", PaneTarget(id), "--"}
	_, err := d.run(append(args, keys...)...)
	return err
}

// Paste delivers text into the session's pane without submitting it. The
// focus path uses this for clipboard pastes: sending the bytes as raw
// keystrokes would turn every newline into an Enter press and submit the
// agent's prompt mid-paste.
func (d *Driver) Paste(id, text string) error {
	return d.paste(id, text)
}

var pasteSeq atomic.Uint64

// Only a pane that never echoes what it reads waits out echoWait; an agent
// redraws a paste in tens of milliseconds, even mid-launch.
const (
	echoWait = time.Second
	echoPoll = 25 * time.Millisecond
)

// pasteAndEnter holds the Enter until the pane has drawn the paste. Both
// writes reach one pty, and a pane too busy to read between them takes the
// carriage return as part of the bracketed paste rather than as a submit,
// stranding the message in the composer.
func (d *Driver) pasteAndEnter(id, text string) error {
	target := PaneTarget(id)
	before, baseline := d.capturePlain(target)
	if err := d.paste(id, text); err != nil {
		return err
	}
	if baseline != nil {
		// Without a baseline, text already on screen reads as the new paste,
		// so the pane gets the whole window to draw it rather than a match.
		time.Sleep(echoWait)
	} else {
		d.awaitPasteEcho(target, before, text)
	}
	_, err := d.run("send-keys", "-t", target, "Enter")
	return err
}

// A pane that draws the paste some other way, as a collapsed placeholder or
// not at all, is released at the cap and submits the way it did before.
func (d *Driver) awaitPasteEcho(target, before, text string) {
	opening := MessageOpening(text)
	if opening == "" {
		return
	}
	was := strings.Count(before, opening)
	deadline := time.Now().Add(echoWait)
	for {
		if pane, err := d.capturePlain(target); err == nil && strings.Count(pane, opening) > was {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(echoPoll)
	}
}

// MessageOpening is the slice of a message to look for in a pane: its first
// line with anything on it, cut short because a composer wraps a long line
// and would split any longer match. A message that opens on a blank line
// still has to be waited for, so the blank lines are skipped rather than
// answered with nothing to match.
func MessageOpening(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if runes := []rune(line); len(runes) > 16 {
			line = strings.TrimSpace(string(runes[:16]))
		}
		return line
	}
	return ""
}

// paste loads text into a tmux buffer and pastes it into the session's pane.
// tmux send-keys silently stops around 1024 bytes; load-buffer does not.
func (d *Driver) paste(id, text string) error {
	target := PaneTarget(id)
	file, err := os.CreateTemp("", "am-paste-*")
	if err != nil {
		return fmt.Errorf("paste temp file: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.WriteString(text); err != nil {
		file.Close()
		return fmt.Errorf("paste temp write: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("paste temp close: %w", err)
	}
	// tmux buffers are server-wide, and every agent's MCP process pastes too.
	// psmux keeps them per session server, so each step goes to this one's.
	buf := fmt.Sprintf("am_paste_%d_%d", os.Getpid(), pasteSeq.Add(1))
	if _, err := d.runIn(id, "load-buffer", "-b", buf, path); err != nil {
		return err
	}
	// Preserve bracketed-paste boundaries when the pane application requests
	// them. Codex uses paste-burst detection without these markers and can
	// consume the immediately following Enter as part of the paste, leaving
	// the prompt in its composer instead of submitting it.
	if _, err := d.runIn(id, "paste-buffer", "-p", "-d", "-b", buf, "-t", target); err != nil {
		_, _ = d.runIn(id, "delete-buffer", "-b", buf)
		return err
	}
	return nil
}

// SendRaw runs one pre-assembled tmux command line. The focus path builds
// send-keys commands from fixed tokens and hex codes, so whitespace
// splitting is exact; nothing quoted ever rides through here.
func (d *Driver) SendRaw(command string) error {
	_, err := d.run(strings.Fields(command)...)
	return err
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

// SetLabel puts the session's name and group path in the status bar's
// left side, replacing the hidden window list.
func (d *Driver) SetLabel(id, label string) error {
	name := sessionName(id)
	if _, err := d.run("set-option", "-t", name, "status-left-length", "80"); err != nil {
		return err
	}
	_, err := d.run("set-option", "-t", name, "status-left", " "+sanitizeFormat(label)+" ")
	return err
}

// sanitizeFormat neutralizes tmux format expansion in user-supplied text.
// Status bars expand #(shell command) and friends, so a session named
// "#(cmd)" would otherwise execute when the bar renders. tmux escapes
// a literal # as ##. Control characters are dropped.
func sanitizeFormat(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return strings.ReplaceAll(s, "#", "##")
}

// A missing tmux server means no request rather than an error: the
// manager outlives the sessions it opens. id names the session the user
// just detached from; psmux keeps the marker on that session's server.
func (d *Driver) PendingRequest(id string) (string, error) {
	out, err := d.command(append(sessionRoute(id), "show-option", "-gqv", requestOption)...).CombinedOutput()
	if err != nil {
		if noServer(string(out)) {
			return "", nil
		}
		return "", fmt.Errorf("tmux show-option %s: %w: %s", requestOption, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// ClearRequest unsets the marker so a request is carried out once.
func (d *Driver) ClearRequest(id string) error {
	_, err := d.runIn(id, "set-option", "-gu", requestOption)
	return err
}

func (d *Driver) AttachCommand(id string) *exec.Cmd {
	return d.command("attach-session", "-t", sessionName(id))
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
	err := d.command("has-session", "-t", sessionName(id)).Run()
	return err == nil
}

// CapturePane returns the visible pane content with ANSI escapes intact
// (-e), so previews keep the session's real colors. Strip before regex use.
func (d *Driver) CapturePane(id string) (string, error) {
	return d.run("capture-pane", "-p", "-e", "-t", PaneTarget(id))
}

// CapturePaneHistory captures the pane with up to `lines` history rows
// above the visible ones, for extractions whose anchor — a message
// bullet, a prompt echo — can scroll off the visible screen.
func (d *Driver) CapturePaneHistory(id string, lines int) (string, error) {
	return d.run("capture-pane", "-p", "-e", "-S", "-"+strconv.Itoa(lines), "-t", PaneTarget(id))
}

// capturePlain drops the escapes CapturePane keeps, which an application is
// free to write partway through a line, breaking a match on the text.
func (d *Driver) capturePlain(target string) (string, error) {
	return d.run("capture-pane", "-p", "-t", target)
}

// Resize pins a detached session's window to the given dimensions so its
// preview capture fits the manager's preview panel. resize-window forces
// window-size to manual, which is what keeps the detached window fixed;
// PrepareAttach flips it back to auto before a client attaches so the
// window fills the terminal instead of leaving a dotted overlay gap.
func (d *Driver) Resize(id string, width, height int) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	panes, windowWidth, windowHeight, paneWidth, paneHeight, err := d.geometry(id)
	if err != nil {
		return err
	}
	// A window the agent split shares its geometry with the teammate panes,
	// so pinning it to the box leaves the pane the preview draws at a
	// fraction of the panel, with the rest of the panel blank. Sizing the
	// window to the box plus what the teammates already hold, then pinning
	// the agent's pane to the box, gives the preview its pane at full size
	// and hands the teammates back their share of the axis the split
	// divides, whether the box grew or shrank -- which is what keeps a Codex
	// teammate's scrollback (#369). The other axis belongs to the window, so
	// every pane follows the box there.
	split := panes > 1 && pinsSplitPane
	if split {
		windowWidth = width + (windowWidth - paneWidth)
		windowHeight = height + (windowHeight - paneHeight)
	} else {
		windowWidth, windowHeight = width, height
	}
	if _, err := d.run("resize-window", "-t", windowTarget(id),
		"-x", strconv.Itoa(windowWidth), "-y", strconv.Itoa(windowHeight)); err != nil {
		return err
	}
	if !split {
		return nil
	}
	_, err = d.run("resize-pane", "-t", PaneTarget(id), "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
	return err
}

// geometry reports how many panes share the agent's window, the window's
// size and the agent pane's own, which is what a resize needs to tell the
// teammates' share of the window from the pane the preview draws.
func (d *Driver) geometry(id string) (panes, windowWidth, windowHeight, paneWidth, paneHeight int, err error) {
	out, err := d.run("display-message", "-p", "-t", PaneTarget(id),
		"#{window_panes} #{window_width} #{window_height} #{pane_width} #{pane_height}")
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}
	line := strings.TrimSpace(out)
	if _, err := fmt.Sscanf(line, "%d %d %d %d %d",
		&panes, &windowWidth, &windowHeight, &paneWidth, &paneHeight); err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("tmux geometry %q for session %s: %w", line, id, err)
	}
	return panes, windowWidth, windowHeight, paneWidth, paneHeight, nil
}

// PrepareAttach restores automatic window sizing so the session fills the
// attaching client and tracks terminal resizes while attached. Without it,
// the manual size Resize pinned for the preview would leave the client's
// extra columns painted with tmux's out-of-bounds dotted overlay.
// "latest" needs tmux 3.1 (issue #114, Ubuntu 20.04 ships 3.0a); a server
// that rejects it gets "largest", which sizes to the single attaching
// client the same way. The rejection is the server's own verdict, so this
// stays correct when client binary and running server versions diverge.
func (d *Driver) PrepareAttach(id string) error {
	if d.attachSizeLargest.Load() {
		_, err := d.run("set-window-option", "-t", sessionName(id), "window-size", "largest")
		return err
	}
	_, err := d.run("set-window-option", "-t", sessionName(id), "window-size", "latest")
	if err != nil && strings.Contains(err.Error(), "unknown value") {
		d.attachSizeLargest.Store(true)
		_, err = d.run("set-window-option", "-t", sessionName(id), "window-size", "largest")
	}
	return err
}

// Cursor reports where the session's caret sits in its visible pane, in
// cells from the top left. A capture carries no cursor, so a caller that
// has to tell an empty prompt from a half-written line asks tmux for it.
func (d *Driver) Cursor(id string) (int, int, error) {
	out, err := d.run("display-message", "-p", "-t", PaneTarget(id), "#{cursor_x},#{cursor_y}")
	if err != nil {
		return 0, 0, err
	}
	column, row, ok := strings.Cut(strings.TrimSpace(out), ",")
	if !ok {
		return 0, 0, fmt.Errorf("tmux reported no cursor for session %s: %q", id, out)
	}
	x, err := strconv.Atoi(column)
	if err != nil {
		return 0, 0, fmt.Errorf("tmux cursor column %q for session %s: %w", column, id, err)
	}
	y, err := strconv.Atoi(row)
	if err != nil {
		return 0, 0, fmt.Errorf("tmux cursor row %q for session %s: %w", row, id, err)
	}
	return x, y, nil
}

func (d *Driver) PanePID(id string) (int, error) {
	out, err := d.run("display-message", "-p", "-t", PaneTarget(id), "#{pane_pid}")
	if err != nil {
		return 0, err
	}
	line := strings.TrimSpace(out)
	if line == "" {
		return 0, fmt.Errorf("no pane for session %s", id)
	}
	return strconv.Atoi(line)
}

// PaneCurrentPath is where the session's pane sits now, which follows any
// cd the shell or the agent made since launch, unlike the directory the
// session was created in.
func (d *Driver) PaneCurrentPath(id string) (string, error) {
	out, err := d.run("display-message", "-p", "-t", PaneTarget(id), "#{pane_current_path}")
	if err != nil {
		return "", err
	}
	// Only the line break is stripped: a trailing space is part of a
	// directory name as much as any other character.
	line := strings.TrimSuffix(strings.SplitN(out, "\n", 2)[0], "\r")
	if line == "" {
		return "", fmt.Errorf("no pane for session %s", id)
	}
	return line, nil
}

// noServer recognizes both messages tmux prints when no server is up:
// "no server running on <socket>" and, on Linux since 3.4, "error
// connecting to <socket> (No such file or directory)".
func noServer(out string) bool {
	return strings.Contains(out, "no server running") ||
		strings.Contains(out, "error connecting to")
}

// Pane is a managed session's agent pane: the process running in it, the
// size the preview draws it at, how many panes share its window, its tty,
// and the directory the agent sits in now. A count above one means the agent split
// the window itself, leaving its own pane a fraction of the geometry the
// manager pinned.
type Pane struct {
	PID       int
	Width     int
	Height    int
	Panes     int
	AltScreen bool
	// TTY is the pane's tty device ("/dev/pts/3"), which omp keys the
	// session it runs by.
	TTY  string
	Path string
}

// Panes returns every managed session's agent pane in a single tmux call,
// which doubles as a liveness check: a session absent from the map is gone.
// Only the agent's own pane counts, the one PaneTarget addresses, so a
// session whose agent split the window reports the agent's own process and
// the size the preview draws, never a teammate's. The filter runs here
// rather than as list-panes -f, which psmux ignores: the first pane 0 of a
// session is the one in its first window, since panes list in window order.
func (d *Driver) Panes() (map[string]Pane, error) {
	out, err := d.command("list-panes", "-a", "-F", "#{session_name} #{pane_index} #{pane_pid} #{pane_width} #{pane_height} #{window_panes} #{alternate_on} #{pane_tty} #{pane_current_path}").CombinedOutput()
	if err != nil {
		if noServer(string(out)) {
			return map[string]Pane{}, nil
		}
		return nil, fmt.Errorf("tmux list-panes: %w: %s", err, strings.TrimSpace(string(out)))
	}
	panes := map[string]Pane{}
	// Only the final line break is trimmed: the last line may end in a
	// space that is its path, or the separator before an empty one.
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		name, geometry, ok := strings.Cut(line, " ")
		if !ok || !strings.HasPrefix(name, prefix) {
			continue
		}
		id := strings.TrimPrefix(name, prefix)
		if _, taken := panes[id]; taken {
			continue
		}
		fields := strings.SplitN(geometry, " ", 8)
		if len(fields) < 8 {
			continue
		}
		pane := Pane{TTY: fields[6], Path: fields[7]}
		var index, altScreen int
		if _, err := fmt.Sscanf(geometry, "%d %d %d %d %d %d", &index, &pane.PID, &pane.Width, &pane.Height, &pane.Panes, &altScreen); err == nil && index == 0 {
			pane.AltScreen = altScreen == 1
			panes[id] = pane
		}
	}
	return panes, nil
}

var paneIDPattern = regexp.MustCompile(`^%[0-9]+$`)

// SessionOfProcess names the managed session whose pane process is pid or
// one of its ancestors. It serves a process that inherited neither $TMUX nor
// $TMUX_PANE, such as an MCP server a CLI starts with a scrubbed environment.
func (d *Driver) SessionOfProcess(pid int) (string, error) {
	out, err := d.run("list-panes", "-a", "-F", "#{pane_pid} #{session_name}")
	if err != nil {
		return "", err
	}
	sessions := map[int]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		panePID, name, _ := strings.Cut(line, " ")
		if id, err := strconv.Atoi(panePID); err == nil && strings.HasPrefix(name, prefix) {
			sessions[id] = strings.TrimPrefix(name, prefix)
		}
	}
	parents, err := processParents()
	if err != nil {
		return "", err
	}
	// The step bound stops a parent loop, which a racing ps snapshot can
	// produce when a pid is reused mid-listing.
	for steps := 0; pid > 1 && steps <= len(parents); steps++ {
		if name, ok := sessions[pid]; ok {
			return name, nil
		}
		pid = parents[pid]
	}
	return "", nil
}

// socketAndPid reads $TMUX from the right, since the socket path is the one
// field free to contain a comma.
func socketAndPid(tmuxEnv string) (socket, pid string, ok bool) {
	rest, _, found := cutLast(tmuxEnv, ",")
	if !found {
		return "", "", false
	}
	socket, pid, found = cutLast(rest, ",")
	return socket, pid, found && socket != "" && pid != ""
}

func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}
