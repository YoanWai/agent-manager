package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const prefix = "am_"

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

func (d *Driver) Create(id, cwd, command string) error {
	name := sessionName(id)
	if _, err := d.run("new-session", "-d", "-s", name, "-c", cwd); err != nil {
		return err
	}
	if err := d.installSessionUX(name); err != nil {
		return err
	}
	if command != "" {
		if _, err := d.run("send-keys", "-t", name, command, "Enter"); err != nil {
			return err
		}
	}
	return nil
}

// installSessionUX adds a status-bar hint and a Ctrl+Q detach binding.
// The bind is server-global but only detaches inside am_* sessions;
// everywhere else Ctrl+Q passes through to the pane untouched.
func (d *Driver) installSessionUX(name string) error {
	options := [][]string{
		{"set-option", "-t", name, "status", "on"},
		{"set-option", "-t", name, "status-left", ""},
		{"set-option", "-t", name, "status-right", " agent-manager · Ctrl+Q = back "},
		{"set-option", "-t", name, "status-style", "bg=colour236,fg=colour249"},
		{"bind-key", "-n", "C-q", "if-shell", "-F", "#{m:" + prefix + "*,#{session_name}}", "detach-client", "send-keys C-q"},
	}
	for _, args := range options {
		if _, err := d.run(args...); err != nil {
			return err
		}
	}
	return nil
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

func (d *Driver) List() ([]string, error) {
	out, err := exec.Command(d.bin, "list-sessions", "-F", "#{session_name}").CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "no server running") {
			return nil, nil
		}
		return nil, fmt.Errorf("tmux list-sessions: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(line, prefix) {
			ids = append(ids, strings.TrimPrefix(line, prefix))
		}
	}
	return ids, nil
}
