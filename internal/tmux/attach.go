package tmux

import (
	"fmt"
	"os/exec"
	"strings"
)

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
