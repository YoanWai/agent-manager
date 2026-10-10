package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func (d *Driver) AttachCommand(id string) *exec.Cmd {
	return exec.Command(d.bin, d.args("attach-session", "-t", sessionName(id))...)
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
