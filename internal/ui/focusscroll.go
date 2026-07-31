package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

// focusScrollStep is how many lines one wheel notch moves the focused
// pane through its history.
const focusScrollStep = 3

// focusScrollMsg carries a capture taken from a scrolled-back region of
// the focused pane's history.
type focusScrollMsg struct {
	sessID  string
	offset  int
	preview string
}

// wheelSGR is one wheel event in SGR mouse encoding (button 64 up, 65
// down) at a one-based pane cell, the form every app that turns on mouse
// tracking expects.
func wheelSGR(up bool, col, row int) string {
	button := 65
	if up {
		button = 64
	}
	return fmt.Sprintf("\x1b[<%d;%d;%dM", button, col+1, row+1)
}

// motionSGR is a pointer move with no button held, at a one-based pane
// cell: button 3 with the motion bit set.
func motionSGR(col, row int) string {
	return fmt.Sprintf("\x1b[<35;%d;%dM", col+1, row+1)
}

// hexBytes renders a string as the space-separated byte codes send-keys -H
// takes, which sidesteps tmux quoting for control sequences entirely.
func hexBytes(s string) string {
	codes := make([]string, 0, len(s))
	for i := 0; i < len(s); i++ {
		codes = append(codes, fmt.Sprintf("%02x", s[i]))
	}
	return strings.Join(codes, " ")
}

// wheelFocus routes one wheel notch. An application that has turned on
// mouse tracking scrolls itself and gets the event; anything else is a
// plain pane, so the wheel walks tmux's own scrollback instead. Agent CLIs
// are the first case: they run on the alternate screen, where tmux keeps
// no history at all, and do their own scrolling.
func (m *Model) wheelFocus(up bool, x, y int) tea.Cmd {
	sess, ok := m.selected()
	if !ok || m.mode != modeFocus || m.focus == nil {
		return nil
	}
	if m.paneMouse {
		row, col, inside := m.paneCell(x, y)
		if !inside {
			return nil
		}
		report := wheelSGR(up, col, row)
		if m.paneMotion {
			// An app tracking all motion places the wheel by where the
			// pointer last moved, and focus mode keeps every move for its
			// own selection, so the notch arrives with the pointer still
			// wherever the app last saw it.
			report = motionSGR(col, row) + report
		}
		command := "send-keys -t " + tmux.SessionName(sess.ID) + " -H " + hexBytes(report)
		if !m.focus.attempt(command) {
			if err := m.tmux.SendRaw(command); err != nil {
				m.err = err.Error()
			}
		}
		return nil
	}
	delta := 1
	if up {
		delta = -1
	}
	return m.scrollFocus(delta)
}

// scrollFocus moves the focused pane through its scrollback and fetches
// the region that lands on screen. Negative delta scrolls up into history.
// Scrolling stops the live view the same way tmux's own copy mode does:
// pushed frames are ignored until the pane is back at the bottom.
func (m *Model) scrollFocus(delta int) tea.Cmd {
	sess, ok := m.selected()
	if !ok || m.mode != modeFocus {
		return nil
	}
	offset := m.focusScroll - delta*focusScrollStep
	if offset < 0 {
		offset = 0
	}
	if offset > m.paneHistory {
		offset = m.paneHistory
	}
	if offset == m.focusScroll {
		return nil
	}
	m.focusScroll = offset
	if offset == 0 {
		// Back at the bottom: the watcher's next push repaints the live
		// pane, and one immediate fetch avoids waiting for it.
		return m.focusRegionCmd(sess.ID, 0)
	}
	return m.focusRegionCmd(sess.ID, offset)
}

// focusRegionCmd captures the pane region that sits offset lines above the
// live bottom. tmux numbers the visible screen from 0 down, and history
// above it with negative lines, so a scrolled window is just a shifted
// start and end.
func (m *Model) focusRegionCmd(sessID string, offset int) tea.Cmd {
	rows := m.paneBox.height
	if rows < 1 {
		rows = 1
	}
	command := fmt.Sprintf(`capture-pane -p -e -t %s -S %d -E %d`,
		tmux.SessionName(sessID), -offset, rows-1-offset)
	watch := m.focus
	return func() tea.Msg {
		if watch == nil {
			return nil
		}
		out, ok := watch.query(command)
		if !ok {
			return nil
		}
		return focusScrollMsg{sessID: sessID, offset: offset, preview: matchExecShape(out)}
	}
}

// scrolledBack reports whether the focused pane is showing history rather
// than its live bottom.
func (m *Model) scrolledBack() bool {
	return m.mode == modeFocus && m.focusScroll > 0
}
