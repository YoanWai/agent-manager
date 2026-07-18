package tmux

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

const prefix = "am_"

// reviewOption is the global tmux user option the in-session Ctrl+R binding
// sets to signal the manager to open review for the session it just detached.
const reviewOption = "@am_review"

type Driver struct {
	bin string
}

func New() (*Driver, error) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return nil, fmt.Errorf("tmux not found on PATH: %w", err)
	}
	return &Driver{bin: bin}, nil
}

func sessionName(id string) string {
	return prefix + id
}

func (d *Driver) run(args ...string) (string, error) {
	out, err := exec.Command(d.bin, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (d *Driver) Create(id, cwd, command string, env map[string]string) error {
	name := sessionName(id)
	if _, err := d.run("new-session", "-d", "-s", name, "-c", cwd); err != nil {
		return err
	}
	if err := d.installSessionUX(name); err != nil {
		_ = d.Kill(id)
		return err
	}
	if command == "" {
		return nil
	}
	// env rides the command line as VAR=value prefixes because
	// new-session -e needs tmux >= 3.2
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
	if _, err := d.run("send-keys", "-t", name, line.String(), "Enter"); err != nil {
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

// installSessionUX adds a status-bar hint and a Ctrl+Q detach binding.
// The bind is server-global but only detaches inside am_* sessions;
// everywhere else Ctrl+Q passes through to the pane untouched.
func (d *Driver) installSessionUX(name string) error {
	options := [][]string{
		{"set-option", "-t", name, "status", "on"},
		{"set-option", "-t", name, "status-left", ""},
		{"set-option", "-t", name, "status-right", " agent-manager · Ctrl+Q = back · Ctrl+R = review "},
		{"set-option", "-t", name, "status-style", "bg=colour236,fg=colour249"},
		// hide the "0:windowname*" window list; it reads as noise here
		{"set-option", "-t", name, "window-status-format", ""},
		{"set-option", "-t", name, "window-status-current-format", ""},
		{"bind-key", "-n", "C-q", "if-shell", "-F", "#{m:" + prefix + "*,#{session_name}}", "detach-client", "send-keys C-q"},
		// Ctrl+R inside an am_* session leaves a marker and detaches; the
		// manager reads the marker on return and jumps straight to review.
		{"bind-key", "-n", "C-r", "if-shell", "-F", "#{m:" + prefix + "*,#{session_name}}", "set-option -g " + reviewOption + " 1 ; detach-client", "send-keys C-r"},
	}
	for _, args := range options {
		if _, err := d.run(args...); err != nil {
			return err
		}
	}
	return nil
}

// SendText types text into the session's pane as literal keystrokes and
// presses Enter, so the agent inside receives it as a user message.
func (d *Driver) SendText(id, text string) error {
	name := sessionName(id)
	if _, err := d.run("send-keys", "-t", name, "-l", "--", text); err != nil {
		return err
	}
	_, err := d.run("send-keys", "-t", name, "Enter")
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
	out, err := exec.Command(d.bin, "show-option", "-gqv", reviewOption).CombinedOutput()
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
	return exec.Command(d.bin, "attach-session", "-t", sessionName(id))
}

func (d *Driver) Kill(id string) error {
	if !d.Exists(id) {
		return nil
	}
	_, err := d.run("kill-session", "-t", sessionName(id))
	return err
}

func (d *Driver) Exists(id string) bool {
	err := exec.Command(d.bin, "has-session", "-t", sessionName(id)).Run()
	return err == nil
}

// CapturePane returns the visible pane content with ANSI escapes intact
// (-e), so previews keep the session's real colors. Strip before regex use.
func (d *Driver) CapturePane(id string) (string, error) {
	return d.run("capture-pane", "-p", "-e", "-t", sessionName(id))
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
	out, err := exec.Command(d.bin, "list-panes", "-a", "-F", "#{session_name} #{pane_pid}").CombinedOutput()
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
