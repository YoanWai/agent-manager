package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/YoanWai/agent-manager/internal/deps"
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
}

// PaneTheme is the background agent panes are rendered on. The manager
// knows that color — it paints every capture on it and repaints the
// terminal to it for a full-screen attach — but an agent inside a pane
// cannot discover it: these sessions run on a server whose only client is
// in control mode, so there is no terminal to answer an OSC 11 background
// query, and the environment carries no COLORFGBG either. Declaring both
// on the server hands an auto-detecting agent the answer the manager
// already renders, instead of leaving it to guess.
type PaneTheme struct {
	Background string // "#rrggbb"; tmux answers pane OSC 11 queries with it
	ColorFgBg  string // "fg;bg" color indexes for agents reading COLORFGBG
}

// paneThemeArgs is the option pair as a tmux command list. window-style and
// the environment are both server-global: every managed session lives on
// this socket and wants the same answer, and a global option also reaches
// windows a user opens inside a session later.
func paneThemeArgs(t PaneTheme) []string {
	return []string{
		"set-option", "-g", "window-style", "bg=" + t.Background, ";",
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
	out, err := exec.Command(d.bin, d.args(paneThemeArgs(*theme)...)...).CombinedOutput()
	if err != nil {
		if noServer(string(out)) {
			return nil
		}
		return fmt.Errorf("tmux set pane theme: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
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

func sessionName(id string) string {
	return prefix + id
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
	return sessionName(id) + ":^.0"
}

// tmux requires -L <socket> before the command word.
func (d *Driver) args(a ...string) []string {
	return append([]string{"-L", d.socket}, a...)
}

func (d *Driver) run(args ...string) (string, error) {
	out, err := exec.Command(d.bin, d.args(args...)...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
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
	var args []string
	// Hold the push lock across loading the theme and the command list that
	// writes it, so a concurrent PushPaneTheme cannot land a newer theme
	// between the load and the write and be clobbered by this stale one.
	d.paneThemePush.Lock()
	// Ahead of new-session in the same command list, so the options are in
	// place before the pane process exists and can query its background.
	var colorFgBg string
	if theme := d.paneTheme.Load(); theme != nil {
		args = append(paneThemeArgs(*theme), ";")
		colorFgBg = theme.ColorFgBg
	}
	if afterCreateThemeLoad != nil {
		afterCreateThemeLoad()
	}
	args = append(args, "new-session", "-d", "-s", name, "-c", cwd)
	// A detached session sizes to tmux's 80x24 default and holds it until a
	// client attaches, so its pane preview renders narrow. Booting at the
	// preview panel's size makes the preview fit from the first frame.
	if width > 0 && height > 0 {
		args = append(args, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
	}
	// Launch via a short `sh <script>` window command. Typing the full line
	// with send-keys truncates around 1024 bytes, which breaks long first
	// prompts mid-path. A script has no practical length limit, and exec'ing
	// the user shell afterwards matches "type into a shell" (pane stays up).
	var scriptPath string
	if command != "" {
		var err error
		scriptPath, err = writeLaunchScript(id, env, command, colorFgBg)
		if err != nil {
			d.paneThemePush.Unlock()
			return err
		}
		args = append(args, "sh "+ShellQuote(scriptPath))
	}
	_, runErr := d.run(args...)
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

// ShellQuote wraps a string in single quotes for POSIX sh; the config
// dir on macOS contains a space, so paths sent into panes must be quoted.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ExportEnv prefixes a command with exports of the session environment, for
// a command typed into a pane whose shell does not carry it: a session
// launched before the manager started exporting these values still holds a
// shell that never received them, and it keeps them once this agent exits
// too.
func ExportEnv(env map[string]string, command string) string {
	var line strings.Builder
	for _, key := range sortedKeys(env) {
		line.WriteString("export " + key + "=" + ShellQuote(env[key]) + "; ")
	}
	line.WriteString(command)
	return line.String()
}

// exportLines exports the session environment into the pane's shell, so it
// outlives the launch command. Quitting the agent leaves a shell that still
// knows which managed session it belongs to, and an agent started again
// from that shell is the same session to every manager subcommand.
func exportLines(env map[string]string) string {
	var lines strings.Builder
	for _, key := range sortedKeys(env) {
		lines.WriteString("export " + key + "=" + ShellQuote(env[key]) + "\n")
	}
	return lines.String()
}

func sortedKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func launchScriptPath(id string) string {
	return filepath.Join(os.TempDir(), "am-launch-"+id+".sh")
}

// relaunchHint lands in the pane the moment the agent exits, which is where
// the user is looking when they wonder how to get it back.
const relaunchHint = "agent-manager: agent exited - press v in Agent Manager to relaunch it here."

func writeLaunchScript(id string, env map[string]string, command, colorFgBg string) (string, error) {
	path := launchScriptPath(id)
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	// Export COLORFGBG in the pane itself. The global option tmux carries in
	// its environment does not reach this first process — it inherits the
	// server's own environment, fixed when the server started, so a host
	// shell that exports COLORFGBG hands the agent that stale pair instead.
	// Exporting here lands the theme's value on the agent regardless.
	var header string
	if colorFgBg != "" {
		header = "export COLORFGBG=" + ShellQuote(colorFgBg) + "\n"
	}
	body := "#!/bin/sh\n" + header + exportLines(env) + command + "\n" +
		"printf '%s\\n' " + ShellQuote(relaunchHint) + "\n" +
		"exec " + ShellQuote(shell) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		return "", fmt.Errorf("launch script: %w", err)
	}
	return path, nil
}

func (d *Driver) installSessionUX(name string) error {
	if err := d.EnsureBindings(); err != nil {
		return err
	}
	if err := d.styleStatusBar(name); err != nil {
		return err
	}
	_, err := d.run("set-option", "-t", name, "status-left", "")
	return err
}

// styleStatusBar sets a session's status bar chrome, leaving status-left (the
// name label) untouched so re-styling a live session keeps its label.
func (d *Driver) styleStatusBar(name string) error {
	primary, err := d.run("show-options", "-t", name, "-v", "prefix")
	if err != nil {
		return err
	}
	secondary, err := d.run("show-options", "-t", name, "-v", "prefix2")
	if err != nil {
		return err
	}
	options := [][]string{
		{"set-option", "-t", name, "status", "on"},
		// The default status-right-length of 40 truncates the hints, so widen it
		// to fit the whole footer, including the configured-prefix fallback.
		{"set-option", "-t", name, "status-right-length", "100"},
		{"set-option", "-t", name, "status-right", attachStatusRight(strings.TrimSpace(primary), strings.TrimSpace(secondary))},
		{"set-option", "-t", name, "status-style", "bg=colour236,fg=colour249"},
		// hide the "0:windowname*" window list; it reads as noise here
		{"set-option", "-t", name, "window-status-format", ""},
		{"set-option", "-t", name, "window-status-current-format", ""},
		// mouse on so tmux handles scrollback per-session instead of the
		// terminal emulator, whose buffer carries content from prior attaches.
		{"set-option", "-t", name, "mouse", "on"},
	}
	_, err = d.run(commandList(options...)...)
	return err
}

func attachStatusRight(primary, secondary string) string {
	exits := make([]string, 0, 2)
	if primary != "C-q" && secondary != "C-q" {
		exits = append(exits, "Ctrl+q")
	}
	for _, candidate := range []string{primary, secondary} {
		if candidate != "" && candidate != "None" {
			exits = append(exits, candidate+" d")
			break
		}
	}
	return " agent-manager · Ctrl+r = review · F3 = editor · " + strings.Join(exits, " / ") + " = back "
}

// EnsureBindings installs the server-wide setup every managed session
// relies on: the in-session key bindings, and the pane numbering
// PaneTarget addresses. A tmux config that starts panes at 1 is the
// dangerous case for the numbering, because tmux answers a target whose
// index does not exist with the active pane rather than an error, so
// every capture and keystroke would silently follow the focused pane.
func (d *Driver) EnsureBindings() error {
	inSession := "#{m:" + prefix + "*,#{session_name}}"
	request := func(name string) string {
		return "set-option -g " + requestOption + " " + name + " ; detach-client"
	}
	commands := [][]string{
		{"set-window-option", "-g", "pane-base-index", "0"},
		{"bind-key", "-n", "C-q", "if-shell", "-F", inSession, "detach-client", "send-keys C-q"},
		{"bind-key", "-n", `C-\`, "if-shell", "-F", inSession, "detach-client", `send-keys C-\\`},
		{"bind-key", "-n", "C-r", "if-shell", "-F", inSession, request(RequestReview), "send-keys C-r"},
		{"bind-key", "-n", "F3", "if-shell", "-F", inSession, request(RequestEditor), "send-keys F3"},
		// The editor used to sit on C-o, which Claude Code, Gemini CLI and
		// readline all bind; a server that outlives the update still carries
		// that binding until it is dropped.
		{"unbind-key", "-n", "C-o"},
		// Restore the standard fallback when the prefix shadows a direct binding.
		{"bind-key", "-T", "prefix", "d", "detach-client"},
	}
	_, err := d.run(commandList(commands...)...)
	return err
}

// RefreshChrome re-applies the status bar chrome to a live session so a
// session created before a manager update picks up the current footer,
// without disturbing its name label.
func (d *Driver) RefreshChrome(id string) error {
	return d.styleStatusBar(sessionName(id))
}

// SendText delivers text into the session's pane and presses Enter, so the
// agent inside receives it as a user message.
func (d *Driver) SendText(id, text string) error {
	return d.pasteAndEnter(PaneTarget(id), text)
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
	return d.paste(PaneTarget(id), text)
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
func (d *Driver) pasteAndEnter(target, text string) error {
	before, baseline := d.capturePlain(target)
	if err := d.paste(target, text); err != nil {
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

// paste loads text into a tmux buffer and pastes it into the pane.
// tmux send-keys silently stops around 1024 bytes; load-buffer does not.
func (d *Driver) paste(target, text string) error {
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
	buf := fmt.Sprintf("am_paste_%d", pasteSeq.Add(1))
	if _, err := d.run("load-buffer", "-b", buf, path); err != nil {
		return err
	}
	// Preserve bracketed-paste boundaries when the pane application requests
	// them. Codex uses paste-burst detection without these markers and can
	// consume the immediately following Enter as part of the paste, leaving
	// the prompt in its composer instead of submitting it.
	if _, err := d.run("paste-buffer", "-p", "-d", "-b", buf, "-t", target); err != nil {
		_, _ = d.run("delete-buffer", "-b", buf)
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

func (d *Driver) AttachCommand(id string) *exec.Cmd {
	return exec.Command(d.bin, d.args("attach-session", "-t", sessionName(id))...)
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
	err := exec.Command(d.bin, d.args("has-session", "-t", sessionName(id))...).Run()
	return err == nil
}

// CapturePane returns the visible pane content with ANSI escapes intact
// (-e), so previews keep the session's real colors. Strip before regex use.
func (d *Driver) CapturePane(id string) (string, error) {
	return d.run("capture-pane", "-p", "-e", "-t", PaneTarget(id))
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
	if _, err := d.run("resize-window", "-t", sessionName(id), "-x", strconv.Itoa(width), "-y", strconv.Itoa(height)); err != nil {
		return err
	}
	return d.fitAgentPane(id, width, height)
}

// fitAgentPane grows a split window until the agent's own pane is the size
// the preview asked for. Teammate panes an agent opened beside itself take
// their columns out of the window, so pinning the window alone leaves the
// pane the preview draws at a fraction of the panel, with the rest of the
// panel blank. The shortfall is added to the window rather than taken from
// the teammates, so fitting the agent pane never shrinks one, which is what
// keeps a Codex teammate's scrollback (#369).
func (d *Driver) fitAgentPane(id string, width, height int) error {
	out, err := d.run("display-message", "-p", "-t", PaneTarget(id),
		"#{window_panes} #{window_width} #{window_height} #{pane_width} #{pane_height}")
	if err != nil {
		return err
	}
	var panes, windowWidth, windowHeight, paneWidth, paneHeight int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d %d %d %d %d",
		&panes, &windowWidth, &windowHeight, &paneWidth, &paneHeight); err != nil {
		return fmt.Errorf("tmux geometry %q for session %s: %w", strings.TrimSpace(out), id, err)
	}
	if panes < 2 {
		return nil
	}
	growWidth, growHeight := max(0, width-paneWidth), max(0, height-paneHeight)
	if growWidth == 0 && growHeight == 0 {
		return nil
	}
	if _, err := d.run("resize-window", "-t", sessionName(id),
		"-x", strconv.Itoa(windowWidth+growWidth), "-y", strconv.Itoa(windowHeight+growHeight)); err != nil {
		return err
	}
	_, err = d.run("resize-pane", "-t", PaneTarget(id), "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
	return err
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
// size the preview draws it at, and how many panes share its window. A
// count above one means the agent split the window itself, leaving its own
// pane a fraction of the geometry the manager pinned.
type Pane struct {
	PID    int
	Width  int
	Height int
	Panes  int
}

// Panes returns every managed session's agent pane in a single tmux call,
// which doubles as a liveness check: a session absent from the map is gone.
// The filter keeps the agent's own pane, the one PaneTarget addresses, so a
// session whose agent split the window reports the agent's own process and
// the size the preview draws, never a teammate's.
func (d *Driver) Panes() (map[string]Pane, error) {
	out, err := exec.Command(d.bin, d.args("list-panes", "-a", "-f", "#{==:#{pane_index},0}", "-F", "#{session_name} #{pane_pid} #{pane_width} #{pane_height} #{window_panes}")...).CombinedOutput()
	if err != nil {
		if noServer(string(out)) {
			return map[string]Pane{}, nil
		}
		return nil, fmt.Errorf("tmux list-panes: %w: %s", err, strings.TrimSpace(string(out)))
	}
	panes := map[string]Pane{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, geometry, ok := strings.Cut(line, " ")
		if !ok || !strings.HasPrefix(name, prefix) {
			continue
		}
		id := strings.TrimPrefix(name, prefix)
		if _, taken := panes[id]; taken {
			continue
		}
		var pane Pane
		if _, err := fmt.Sscanf(geometry, "%d %d %d %d", &pane.PID, &pane.Width, &pane.Height, &pane.Panes); err == nil {
			panes[id] = pane
		}
	}
	return panes, nil
}
