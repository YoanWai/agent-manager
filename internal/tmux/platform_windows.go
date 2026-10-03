//go:build windows

package tmux

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/proctree"
	"github.com/YoanWai/agent-manager/internal/pwsh"
)

// Binary is the multiplexer the manager drives: psmux speaks tmux's command
// language on Windows.
const Binary = "psmux"

// controlFlag attaches a control-mode client. psmux's -C echoes every
// command back; -CC answers in the plain block protocol tmux -C speaks.
const controlFlag = "-CC"

// firstWindow is the window spec for a session's first window. psmux reads
// "^" in a target as a window name, so it is named by id instead: each
// session has its own server, whose window ids start at @1 and are never
// reused, so @1 is the window the session opened with.
const firstWindow = "@1"

// hiddenWindowStatus is a window-status format that draws nothing. psmux's
// client keeps its default format for an empty one, so it gets a lone
// style reset instead.
const hiddenWindowStatus = "#[default]"

// pinsSplitPane is off: psmux sizes resize-pane on a detached window
// against the last client's area rather than the window, scaling the pane
// past the size asked for, and drops -y when -x comes with it. A split
// window is sized to the preview whole, the panes keeping their shares.
const pinsSplitPane = false

// errControlUnavailable keeps the manager off psmux's control client. Its
// output thread slices each line at byte 200 for a debug log and panics
// when that cuts a multibyte character, which an agent's box-drawn UI does
// on nearly every capture; the client then goes silent while still
// attached. Previews poll capture-pane instead.
var errControlUnavailable = errors.New("psmux control mode is not used: its client panics on wide pane lines")

func controlUnavailable() error {
	return errControlUnavailable
}

// commandEnv keeps psmux from parking a warm standby server per namespace:
// every managed session passes its own directory and size, which a warm
// server cannot take, so it would only cost a server and a shell. It also
// lets an attach run from inside a psmux pane. psmux refuses any attach
// from a pane as nesting, where tmux refuses only one into the server
// drawing that pane; a managed session is always another server, so a
// manager started inside the user's own psmux attaches the way it does
// inside tmux.
func commandEnv() []string {
	return append(os.Environ(), "PSMUX_NO_WARM=1", "PSMUX_ALLOW_NESTING=1")
}

// sessionRoute sends a command to the server hosting session id. psmux runs
// one server per session, and a command without a target reaches the one
// $TMUX names or the most recent, so anything server-scoped (global
// options, bindings, buffers) is routed with a -t ahead of the command.
func sessionRoute(id string) []string {
	if id == "" {
		return nil
	}
	return []string{"-t", sessionName(id)}
}

// splitCommandList breaks a command list into the separate invocations
// psmux needs: its command line takes one command and reads a ";" as part
// of it. A leading session route goes on every command, and a trailing \;
// a value carries for tmux's sake is a plain ";" here.
func splitCommandList(args []string) [][]string {
	var route []string
	if len(args) >= 2 && args[0] == "-t" {
		route, args = args[:2], args[2:]
	}
	var commands [][]string
	start := 0
	for i := 0; i <= len(args); i++ {
		if i < len(args) && args[i] != ";" {
			continue
		}
		command := make([]string, 0, len(route)+i-start)
		command = append(command, route...)
		for _, arg := range args[start:i] {
			if strings.HasSuffix(arg, `\;`) {
				arg = strings.TrimSuffix(arg, `\;`) + ";"
			}
			command = append(command, arg)
		}
		commands = append(commands, command)
		start = i + 1
	}
	return commands
}

// bindingArg wire-quotes one word of a binding's command. psmux forwards
// bind-key to its server as the arguments joined by spaces, which the
// server splits again, so a word with spaces in it would fall apart; the
// server decodes this double-quoted form back to the word. Quoting a bare
// word matters too: an argument that is literally detach-client turns off
// psmux's -t routing, sending the binding to whichever server the
// namespace used last.
func bindingArg(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`).Replace(s) + `"`
}

// rootBinding binds a key straight to its action, one argument per word.
// Every client of a psmux server is attached to that server's one managed
// session, so there is no other session to pass the key through to; and
// psmux's client quits only for a binding whose whole command is
// detach-client, not for one inside an if-shell branch.
func rootBinding(key keybind.Key, action ...string) []string {
	command := []string{"bind-key", "-n", key.Tmux()}
	for _, word := range action {
		command = append(command, bindingArg(word))
	}
	return command
}

// requestAction leaves the request marker and detaches. psmux runs a
// detach-client that follows another command server-side, where it does not
// end the attached client, so the binding detaches the way a script would:
// a run-shell calls psmux to set the marker on this server and then detach
// every client of the session.
func (d *Driver) requestAction(id, name string) []string {
	// Every argument is quoted: PowerShell reads a bare @am_request as a
	// splatted variable and drops it.
	psmux := "& " + pwsh.Quote(d.bin) + " -L " + pwsh.Quote(d.socket)
	session := pwsh.Quote(sessionName(id))
	script := psmux + " -t " + session + " set-option -g " + pwsh.Quote(requestOption) + " " + pwsh.Quote(name) +
		"; " + psmux + " detach-client -s " + session
	return []string{"run-shell", script}
}

// ownedRootBindings reads the keys an earlier run recorded on the server.
func (d *Driver) ownedRootBindings(id string) ([]string, error) {
	out, err := d.runIn(id, "show-option", "-gqv", rootKeysOption)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// recordRootBindings stores the keys bound this run for the next to remove.
func recordRootBindings(keys []string) [][]string {
	if len(keys) == 0 {
		return [][]string{{"set-option", "-gu", rootKeysOption}}
	}
	return [][]string{{"set-option", "-g", rootKeysOption, strings.Join(keys, " ")}}
}

// eachServer runs fn for every managed session's server, visiting all of
// them before reporting the first failure. No server at all is no work.
func (d *Driver) eachServer(fn func(id string) error) error {
	out, err := d.run("list-sessions", "-F", "#{session_name}")
	if err != nil {
		if noServer(err.Error()) {
			return nil
		}
		return err
	}
	var first error
	for _, name := range strings.Split(out, "\n") {
		name = strings.TrimSpace(name)
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if err := fn(strings.TrimPrefix(name, prefix)); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// SocketPath identifies the psmux namespace this driver talks to. psmux has
// no socket file: -L names registry files under its data directory, so the
// directory plus the namespace is what tells two managers' servers apart,
// the way TMUX_TMPDIR plus the socket name does for tmux.
func (d *Driver) SocketPath() string {
	if cached := d.socketPath.Load(); cached != nil {
		return *cached
	}
	path := filepath.Join(psmuxDir(), d.socket)
	d.socketPath.Store(&path)
	return path
}

// psmuxDir is where psmux keeps its registry: PSMUX_DATA_DIR when it is an
// absolute path, else ~\.psmux, and \.psmux with no home, as psmux does.
func psmuxDir() string {
	if custom := strings.TrimRight(os.Getenv("PSMUX_DATA_DIR"), `\/`); custom != "" && filepath.IsAbs(custom) {
		return custom
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return `\.psmux`
	}
	return filepath.Join(home, ".psmux")
}

// startSession creates the detached session. psmux runs the program after
// -- directly with its arguments intact, so the launch script goes to
// PowerShell as a file; -NoExit leaves that same shell at a prompt once the
// agent exits. The pane theme goes to the new server once it exists; the
// pane itself already has COLORFGBG through -e and the script.
func (d *Driver) startSession(id, cwd string, width, height int, scriptPath string, theme *PaneTheme) error {
	args := []string{"new-session", "-d", "-s", sessionName(id), "-c", cwd}
	if width > 0 && height > 0 {
		args = append(args, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
	}
	if theme != nil {
		args = append(args, "-e", "COLORFGBG="+theme.ColorFgBg)
	}
	if scriptPath != "" {
		shell, err := pwsh.Path()
		if err != nil {
			return err
		}
		args = append(args, "--", shell, "-NoLogo", "-NoExit", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	}
	if _, err := d.run(args...); err != nil {
		return err
	}
	if theme != nil {
		// A session Create reports as failed must not be left running.
		if _, err := d.runIn(id, paneThemeArgs(*theme)...); err != nil {
			_, _ = d.run("kill-session", "-t", sessionName(id))
			return err
		}
	}
	return nil
}

// ShellQuote renders a string as a PowerShell literal, the pane shell here.
func ShellQuote(s string) string {
	return pwsh.Quote(s)
}

// ShellInvoke is a pane shell command line running the program at path:
// PowerShell reads a quoted path as a string, and & runs it.
func ShellInvoke(path string) string {
	return "& " + ShellQuote(path)
}

// ExportEnv prefixes a command with assignments of the session environment,
// for a command typed into a pane whose shell does not carry it.
func ExportEnv(env map[string]string, command string) string {
	var line strings.Builder
	for _, key := range sortedKeys(env) {
		line.WriteString("$env:" + key + " = " + ShellQuote(env[key]) + "; ")
	}
	line.WriteString(command)
	return line.String()
}

// exportLines sets the session environment in the pane's shell, so it
// outlives the launch command.
func exportLines(env map[string]string) string {
	var lines strings.Builder
	for _, key := range sortedKeys(env) {
		lines.WriteString("$env:" + key + " = " + ShellQuote(env[key]) + "\n")
	}
	return lines.String()
}

func launchScriptPath(id string) string {
	return filepath.Join(os.TempDir(), "am-launch-"+id+".ps1")
}

// writeLaunchScript writes the PowerShell script the pane shell runs with
// -NoExit: once the agent exits, that shell stays as the user's prompt,
// the way the POSIX script execs $SHELL.
func writeLaunchScript(id string, env map[string]string, command, colorFgBg string) (string, error) {
	path := launchScriptPath(id)
	var header string
	if colorFgBg != "" {
		header = "$env:COLORFGBG = " + ShellQuote(colorFgBg) + "\n"
	}
	body := header + exportLines(env) + command + "\n" +
		"Write-Host " + ShellQuote(relaunchHint) + "\n"
	if err := os.WriteFile(path, pwsh.ScriptFile(body), 0o600); err != nil {
		return "", fmt.Errorf("launch script: %w", err)
	}
	return path, nil
}

// serverDirPattern is the directory psmux puts in $TMUX, psmux-<server pid>.
var serverDirPattern = regexp.MustCompile(`^psmux-([0-9]+)$`)

// SessionOfPane names the managed session a psmux pane belongs to, for a
// caller that knows which pane it sits in but not which session it is.
//
// tmuxEnv is the caller's $TMUX, which psmux writes as
// /tmp/psmux-<server pid>/<namespace>,<port>,0. A pane in another namespace
// answers empty. Pane ids repeat across psmux's per-session servers, so the
// server pid is what picks the session, and it also refuses an environment
// inherited from a server that has since exited.
func (d *Driver) SessionOfPane(tmuxEnv, paneID string) (string, error) {
	if !paneIDPattern.MatchString(paneID) {
		return "", nil
	}
	socket, _, ok := socketAndPid(tmuxEnv)
	if !ok {
		return "", nil
	}
	socket = strings.ReplaceAll(socket, `\`, "/")
	if path.Base(socket) != d.socket {
		return "", nil
	}
	match := serverDirPattern.FindStringSubmatch(path.Base(path.Dir(socket)))
	if match == nil {
		return "", nil
	}
	serverPid := match[1]
	out, err := d.run("list-panes", "-a", "-F", "#{pid} #{pane_id} #{session_name}")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != serverPid || fields[1] != paneID {
			continue
		}
		if !strings.HasPrefix(fields[2], prefix) {
			return "", nil
		}
		return strings.TrimPrefix(fields[2], prefix), nil
	}
	return "", nil
}

func processParents() (map[int]int, error) {
	entries, err := proctree.Snapshot()
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	parents := make(map[int]int, len(entries))
	for _, entry := range entries {
		parents[entry.PID] = entry.PPID
	}
	return parents, nil
}
