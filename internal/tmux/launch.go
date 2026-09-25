package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

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
