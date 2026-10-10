package tmux

import (
	"fmt"
	"os/exec"
	"strings"
)

// PaneTheme is the text and background colors agent panes are rendered on.
// The manager knows them — it paints every capture with them and sets the
// terminal to them for a full-screen attach — but an agent inside a pane
// cannot discover them: these sessions run on a server whose only client is
// in control mode, so there is no terminal to answer an OSC 10 or 11 color
// query, and the environment carries no COLORFGBG either. Declaring them on
// the server hands an auto-detecting agent the answer the manager already
// renders, instead of leaving it to guess.
type PaneTheme struct {
	Foreground string // "#rrggbb"; tmux answers pane OSC 10 queries with it
	Background string // "#rrggbb"; tmux answers pane OSC 11 queries with it
	ColorFgBg  string // "fg;bg" color indexes for agents reading COLORFGBG
}

// paneThemeArgs is the option pair as a tmux command list. window-style and
// the environment are both server-global: every managed session lives on
// this socket and wants the same answer, and a global option also reaches
// windows a user opens inside a session later.
func paneThemeArgs(t PaneTheme) []string {
	return []string{
		"set-option", "-g", "window-style", "fg=" + t.Foreground + ",bg=" + t.Background, ";",
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
