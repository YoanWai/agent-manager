package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

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
	if panes > 1 {
		windowWidth = width + (windowWidth - paneWidth)
		windowHeight = height + (windowHeight - paneHeight)
	} else {
		windowWidth, windowHeight = width, height
	}
	if _, err := d.run("resize-window", "-t", windowTarget(id),
		"-x", strconv.Itoa(windowWidth), "-y", strconv.Itoa(windowHeight)); err != nil {
		return err
	}
	if panes < 2 {
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

var paneIDPattern = regexp.MustCompile(`^%[0-9]+$`)

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

// splitPaneInfo reads the name and the pid off the end, so a socket path
// with spaces in it stays whole. An unknown pane leaves the name empty.
func splitPaneInfo(out string) (socket, pid, name string) {
	rest, name, _ := cutLast(out, " ")
	socket, pid, _ = cutLast(rest, " ")
	return socket, pid, name
}

func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}
