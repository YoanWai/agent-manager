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
)

const prefix = "am_"

// defaultSocket is the private tmux server name the manager runs every agent
// on. A dedicated -L socket keeps agent sessions off the user's default
// socket, where a shell tmux, a `go test`, or a stray kill-server would
// otherwise share a server with the live agents and take them all down at once.
const defaultSocket = "agentmgr"

// reviewOption is the global tmux user option the in-session Ctrl+R binding
// sets to signal the manager to open review for the session it just detached.
const reviewOption = "@am_review"

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
		return nil, fmt.Errorf("tmux not found on PATH: %w", err)
	}
	return &Driver{bin: bin, socket: socket}, nil
}

func (d *Driver) SocketName() string {
	return d.socket
}

func sessionName(id string) string {
	return prefix + id
}

func SessionName(id string) string {
	return sessionName(id)
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
		scriptPath, err = writeLaunchScript(id, envCommand(env, command), colorFgBg)
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

func envCommand(env map[string]string, command string) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var line strings.Builder
	for _, key := range keys {
		line.WriteString(key + "=" + ShellQuote(env[key]) + " ")
	}
	line.WriteString(command)
	return line.String()
}

func launchScriptPath(id string) string {
	return filepath.Join(os.TempDir(), "am-launch-"+id+".sh")
}

func writeLaunchScript(id, line, colorFgBg string) (string, error) {
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
	body := "#!/bin/sh\n" + header + line + "\nexec " + ShellQuote(shell) + "\n"
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
	options := [][]string{
		{"set-option", "-t", name, "status", "on"},
		// The default status-right-length of 40 truncates the hint before the
		// Ctrl+R part, so widen it to fit the whole footer.
		{"set-option", "-t", name, "status-right-length", "80"},
		{"set-option", "-t", name, "status-right", " agent-manager · Ctrl+q = back · Ctrl+r = review "},
		{"set-option", "-t", name, "status-style", "bg=colour236,fg=colour249"},
		// hide the "0:windowname*" window list; it reads as noise here
		{"set-option", "-t", name, "window-status-format", ""},
		{"set-option", "-t", name, "window-status-current-format", ""},
		// mouse on so tmux handles scrollback per-session instead of the
		// terminal emulator, whose buffer carries content from prior attaches.
		{"set-option", "-t", name, "mouse", "on"},
	}
	for _, args := range options {
		if _, err := d.run(args...); err != nil {
			return err
		}
	}
	return nil
}

// EnsureBindings installs the server-global Ctrl+Q / Ctrl+\ detach and
// Ctrl+R review bindings. All only act inside am_* sessions; elsewhere the
// key passes through to the pane. Idempotent, so it is safe to re-run for
// sessions that predate a binding.
func (d *Driver) EnsureBindings() error {
	binds := [][]string{
		{"bind-key", "-n", "C-q", "if-shell", "-F", "#{m:" + prefix + "*,#{session_name}}", "detach-client", "send-keys C-q"},
		{"bind-key", "-n", `C-\`, "if-shell", "-F", "#{m:" + prefix + "*,#{session_name}}", "detach-client", `send-keys C-\\`},
		{"bind-key", "-n", "C-r", "if-shell", "-F", "#{m:" + prefix + "*,#{session_name}}", "set-option -g " + reviewOption + " 1 ; detach-client", "send-keys C-r"},
	}
	for _, args := range binds {
		if _, err := d.run(args...); err != nil {
			return err
		}
	}
	return nil
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
	return d.pasteAndEnter(sessionName(id), text)
}

// Paste delivers text into the session's pane without submitting it. The
// focus path uses this for clipboard pastes: sending the bytes as raw
// keystrokes would turn every newline into an Enter press and submit the
// agent's prompt mid-paste.
func (d *Driver) Paste(id, text string) error {
	return d.paste(sessionName(id), text)
}

var pasteSeq atomic.Uint64

func (d *Driver) pasteAndEnter(target, text string) error {
	if err := d.paste(target, text); err != nil {
		return err
	}
	_, err := d.run("send-keys", "-t", target, "Enter")
	return err
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

// ReviewRequested reports whether the in-session Ctrl+R binding set the
// review marker before detaching. A missing tmux server means no request.
func (d *Driver) ReviewRequested() (bool, error) {
	out, err := exec.Command(d.bin, d.args("show-option", "-gqv", reviewOption)...).CombinedOutput()
	if err != nil {
		if noServer(string(out)) {
			return false, nil
		}
		return false, fmt.Errorf("tmux show-option %s: %w: %s", reviewOption, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

// ClearReviewRequest unsets the review marker so it fires once per request.
func (d *Driver) ClearReviewRequest() error {
	_, err := d.run("set-option", "-gu", reviewOption)
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
	return d.run("capture-pane", "-p", "-e", "-t", sessionName(id))
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
	_, err := d.run("resize-window", "-t", sessionName(id), "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
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

func (d *Driver) PanePID(id string) (int, error) {
	out, err := d.run("list-panes", "-t", sessionName(id), "-F", "#{pane_pid}")
	if err != nil {
		return 0, err
	}
	line := strings.TrimSpace(out)
	if line == "" {
		return 0, fmt.Errorf("no pane for session %s", id)
	}
	line = strings.SplitN(line, "\n", 2)[0]
	return strconv.Atoi(line)
}

// PaneCurrentPath is where the session's pane sits now, which follows any
// cd the shell or the agent made since launch, unlike the directory the
// session was created in.
func (d *Driver) PaneCurrentPath(id string) (string, error) {
	out, err := d.run("list-panes", "-t", sessionName(id), "-F", "#{pane_current_path}")
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

// Panes returns every managed session's pane pid in a single tmux call,
// which doubles as a liveness check: a session absent from the map is gone.
func (d *Driver) Panes() (map[string]int, error) {
	out, err := exec.Command(d.bin, d.args("list-panes", "-a", "-F", "#{session_name} #{pane_pid}")...).CombinedOutput()
	if err != nil {
		if noServer(string(out)) {
			return map[string]int{}, nil
		}
		return nil, fmt.Errorf("tmux list-panes: %w: %s", err, strings.TrimSpace(string(out)))
	}
	panes := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name, pidText, ok := strings.Cut(line, " ")
		if !ok || !strings.HasPrefix(name, prefix) {
			continue
		}
		id := strings.TrimPrefix(name, prefix)
		if _, taken := panes[id]; taken {
			continue
		}
		if pid, err := strconv.Atoi(pidText); err == nil {
			panes[id] = pid
		}
	}
	return panes, nil
}
