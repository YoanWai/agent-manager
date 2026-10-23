package tmux

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CapturePane returns the visible pane content with ANSI escapes intact
// (-e), so previews keep the session's real colors. Strip before regex use.
func (d *Driver) CapturePane(id string) (string, error) {
	return d.run("capture-pane", "-p", "-e", "-t", PaneTarget(id))
}

// CapturePaneContext is CapturePane bounded by ctx, for a watcher whose
// close must not wait on a capture still in flight.
func (d *Driver) CapturePaneContext(ctx context.Context, id string) (string, error) {
	return d.runContext(ctx, "capture-pane", "-p", "-e", "-t", PaneTarget(id))
}

// CapturePaneHistory captures the pane with up to `lines` history rows
// above the visible ones, for extractions whose anchor — a message
// bullet, a prompt echo — can scroll off the visible screen.
func (d *Driver) CapturePaneHistory(id string, lines int) (string, error) {
	return d.run("capture-pane", "-p", "-e", "-S", "-"+strconv.Itoa(lines), "-t", PaneTarget(id))
}

// CapturePaneHistoryContext is CapturePaneHistory bounded by ctx.
func (d *Driver) CapturePaneHistoryContext(ctx context.Context, id string, lines int) (string, error) {
	return d.runContext(ctx, "capture-pane", "-p", "-e", "-S", "-"+strconv.Itoa(lines), "-t", PaneTarget(id))
}

// capturePlain drops the escapes CapturePane keeps, which an application is
// free to write partway through a line, breaking a match on the text.
func (d *Driver) capturePlain(target string) (string, error) {
	return d.run("capture-pane", "-p", "-t", target)
}

func (d *Driver) capturePlainContext(ctx context.Context, target string) (string, error) {
	return d.runContext(ctx, "capture-pane", "-p", "-t", target)
}

// Display expands a format against the session's agent pane, for callers
// that read pane state without a control client.
func (d *Driver) Display(id, format string) (string, error) {
	return d.run("display-message", "-p", "-t", PaneTarget(id), format)
}

// DisplayContext is Display bounded by ctx.
func (d *Driver) DisplayContext(ctx context.Context, id, format string) (string, error) {
	return d.runContext(ctx, "display-message", "-p", "-t", PaneTarget(id), format)
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
// rather than as list-panes -f, which tmux 3.1 lacks and psmux ignores: the
// first pane 0 of a session is the one in its first window, since panes
// list in window order.
func (d *Driver) Panes() (map[string]Pane, error) {
	out, err := d.command("list-panes", "-a", "-F", "#{session_name} #{pane_index} #{pane_pid} #{pane_width} #{pane_height} #{window_panes} #{alternate_on} #{pane_tty} #{pane_current_path}").CombinedOutput()
	if err != nil {
		// A server with no session in it, as the startup list-keys leaves
		// for a moment, answers "no current target".
		if noServer(string(out)) || strings.Contains(string(out), "no current target") {
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
