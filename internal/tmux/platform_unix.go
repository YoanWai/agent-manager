//go:build !windows

package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

// Binary is the multiplexer the manager drives.
const Binary = "tmux"

// controlFlag attaches a control-mode client.
const controlFlag = "-C"

// hiddenWindowStatus is a window-status format that draws nothing.
const hiddenWindowStatus = ""

// pinsSplitPane: Resize can pin the agent's pane inside a split window.
const pinsSplitPane = true

// firstWindow is the window spec for a session's first window.
const firstWindow = "^"

// controlUnavailable reports why control mode cannot be used; tmux's can.
func controlUnavailable() error {
	return nil
}

// rootBinding binds a key inside managed sessions only; anywhere else on
// the server the key goes through to the pane as itself. That branch is a
// command string tmux parses, so a backslash in the key name is doubled.
func rootBinding(key keybind.Key, action ...string) []string {
	passThrough := "send-keys " + strings.ReplaceAll(key.Tmux(), `\`, `\\`)
	return []string{"bind-key", "-n", key.Tmux(), "if-shell", "-F", ownedBindingTest, strings.Join(action, " "), passThrough}
}

// requestAction leaves the request marker and detaches, as the one command
// string an if-shell branch takes.
func (d *Driver) requestAction(_ string, name string) []string {
	return []string{"set-option -g " + requestOption + " " + name + " ; detach-client"}
}

// ownedRootBindings lists the root-table keys carrying the manager's own
// session test. list-keys prints a key the way its parser reads it back,
// so a backslash comes doubled and is undone here for unbind-key, which
// takes the name as is.
func (d *Driver) ownedRootBindings(id string) ([]string, error) {
	out, err := d.runIn(id, "list-keys", "-T", "root")
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "bind-key" || fields[1] != "-T" || fields[2] != "root" {
			continue
		}
		if !strings.Contains(line, ownedBindingTest) {
			continue
		}
		keys = append(keys, strings.ReplaceAll(fields[3], `\\`, `\`))
	}
	return keys, nil
}

// recordRootBindings has nothing to record: the session test in each
// binding is what marks it as the manager's.
func recordRootBindings([]string) [][]string {
	return nil
}

// commandEnv is the environment tmux runs with: nil inherits this process's.
func commandEnv() []string {
	return nil
}

// sessionRoute is empty: tmux has one server for every session, and rejects
// a -t ahead of the command word.
func sessionRoute(string) []string {
	return nil
}

// eachServer runs fn once for the single server that hosts every session.
func (d *Driver) eachServer(fn func(id string) error) error {
	return fn("")
}

// splitCommandList leaves a command list whole: tmux runs it in one go.
func splitCommandList(args []string) [][]string {
	return [][]string{args}
}

// bindingArg is a bind-key argument as tmux takes it, verbatim.
func bindingArg(s string) string {
	return s
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

// startSession creates the detached session, running the launch script when
// there is one, in a single command list.
func (d *Driver) startSession(id, cwd string, width, height int, scriptPath string, theme *PaneTheme) error {
	var args []string
	// Ahead of new-session in the same command list, so the options are in
	// place before the pane process exists and can query its background.
	if theme != nil {
		args = append(paneThemeArgs(*theme), ";")
	}
	args = append(args, "new-session", "-d", "-s", sessionName(id), "-c", cwd)
	// A detached session sizes to tmux's 80x24 default and holds it until a
	// client attaches, so its pane preview renders narrow. Booting at the
	// preview panel's size makes the preview fit from the first frame.
	if width > 0 && height > 0 {
		args = append(args, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
	}
	// A short `sh <script>` window command, which execs the user's shell
	// once the agent exits.
	if scriptPath != "" {
		args = append(args, "sh "+ShellQuote(scriptPath))
	}
	_, err := d.run(args...)
	return err
}

// ShellQuote wraps a string in single quotes for POSIX sh; the config
// dir on macOS contains a space, so paths sent into panes must be quoted.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ShellInvoke is a pane shell command line running the program at path.
func ShellInvoke(path string) string {
	return ShellQuote(path)
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

func launchScriptPath(id string) string {
	return filepath.Join(os.TempDir(), "am-launch-"+id+".sh")
}

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

// SessionOfPane names the managed session a tmux pane belongs to, for a
// caller that knows which pane it sits in but not which session it is.
// A terminal the manager opens carries no launch command, so it gets no
// launch script and the session id never reaches its environment; the
// pane it runs in still says which session tmux filed it under.
//
// tmuxEnv is the caller's $TMUX, socket,server_pid,session_id. A pane on any
// other server belongs to some other tmux, not to this manager, and answers
// empty. The socket alone cannot tell servers apart: it keeps its path across
// a restart while pane ids start again at %0, so an environment inherited from
// a previous server would name whichever session owns that id now. The pid
// is what differs.
func (d *Driver) SessionOfPane(tmuxEnv, paneID string) (string, error) {
	// tmux resolves a target it cannot parse to the current session and
	// exits 0, which would answer for a session this pane is not in.
	if !paneIDPattern.MatchString(paneID) {
		return "", nil
	}
	socket, pid, ok := socketAndPid(tmuxEnv)
	if !ok {
		return "", nil
	}
	out, err := d.run("display-message", "-p", "-t", paneID, "#{socket_path} #{pid} #{session_name}")
	if err != nil {
		return "", err
	}
	serverSocket, serverPid, name := splitPaneInfo(strings.TrimRight(out, "\n"))
	if resolvedSocket(socket) != resolvedSocket(serverSocket) || pid != serverPid {
		return "", nil
	}
	if !strings.HasPrefix(name, prefix) {
		return "", nil
	}
	return strings.TrimPrefix(name, prefix), nil
}

// splitPaneInfo reads the name and the pid off the end, so a socket path
// with spaces in it stays whole. An unknown pane leaves the name empty.
func splitPaneInfo(out string) (socket, pid, name string) {
	rest, name, _ := cutLast(out, " ")
	socket, pid, _ = cutLast(rest, " ")
	return socket, pid, name
}

// resolvedSocket puts two socket paths in the same terms before they are
// compared: macOS reports /private/tmp where the other side says /tmp, and
// a temporary directory is routinely a symlink on either platform.
func resolvedSocket(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

func processParents() (map[int]int, error) {
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	parents := map[int]int{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		ppid, ppidErr := strconv.Atoi(fields[1])
		if pidErr == nil && ppidErr == nil {
			parents[pid] = ppid
		}
	}
	return parents, nil
}
