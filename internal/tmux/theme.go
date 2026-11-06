package tmux

import (
	"fmt"
	"strconv"
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
	// Palette is the host terminal's colors 0 to 15 as "#rrggbb", empty
	// where the host gave no answer, nil when the host was never asked, as
	// on Windows, which leaves pane-colours alone.
	// tmux answers pane OSC 4 queries with it, since captured rows keep
	// their color indexes and the host draws them in its own palette.
	Palette []string
}

// paneThemeArgs is the pane options as a tmux command list. window-style,
// pane-colours and the environment are all server-global: every managed
// session lives on this socket and wants the same answer, and a global
// option also reaches windows a user opens inside a session later.
// pane-colours is cleared before the answered colors go in, so a color the
// current host left unanswered drops what an earlier host declared. -q skips
// it on tmux before 3.3, which lacks the option.
func paneThemeArgs(t PaneTheme) []string {
	args := []string{
		"set-option", "-g", "window-style", "fg=" + t.Foreground + ",bg=" + t.Background, ";",
		"set-environment", "-g", "COLORFGBG", t.ColorFgBg,
	}
	if t.Palette == nil {
		return args
	}
	args = append(args, ";", "set-option", "-q", "-gu", "pane-colours")
	for index, color := range t.Palette {
		if color != "" {
			args = append(args, ";", "set-option", "-q", "-g", "pane-colours["+strconv.Itoa(index)+"]", color)
		}
	}
	return args
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
	return d.eachServer(func(id string) error {
		if _, err := d.runIn(id, paneThemeArgs(*theme)...); err != nil && !noServer(err.Error()) {
			return fmt.Errorf("tmux set pane theme: %w", err)
		}
		return nil
	})
}
