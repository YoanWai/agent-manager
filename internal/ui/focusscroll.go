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

// Mouse button codes as the reports carry them: the two wheel
// directions, and a pointer move with no button held (button 3 with the
// motion bit set).
const (
	wheelUpButton   = 64
	wheelDownButton = 65
	motionButton    = 35
)

// x10Limit is the highest cell the original encoding can name: each
// coordinate is one byte biased by 32, so it runs out at 223.
const x10Limit = 223

// sgrMouse is one mouse report in SGR encoding at a one-based pane cell.
func sgrMouse(button, col, row int) string {
	return fmt.Sprintf("\x1b[<%d;%d;%dM", button, col+1, row+1)
}

// x10Mouse is one mouse report in the original encoding, for an app that
// tracks the mouse without asking for SGR. Not ok past the cell the
// encoding can name, where the report would land on the wrong column.
func x10Mouse(button, col, row int) (string, bool) {
	if col >= x10Limit || row >= x10Limit {
		return "", false
	}
	return string([]byte{0x1b, '[', 'M', byte(32 + button), byte(33 + col), byte(33 + row)}), true
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

// wheelReport is what one wheel notch looks like on the wire for this
// pane: the notch itself, in the encoding the pane asked for, behind a
// pointer move when the app tracks all motion. An app tracking all
// motion places the wheel by where the pointer last moved, and focus
// mode keeps every move for its own selection, so without the move the
// notch arrives with the pointer wherever the app last saw it.
func (m *Model) wheelReport(up bool, col, row int) (string, bool) {
	button := wheelDownButton
	if up {
		button = wheelUpButton
	}
	if m.pane.sgr {
		report := sgrMouse(button, col, row)
		if m.pane.motion {
			report = sgrMouse(motionButton, col, row) + report
		}
		return report, true
	}
	report, ok := x10Mouse(button, col, row)
	if !ok {
		return "", false
	}
	if m.pane.motion {
		move, moveOK := x10Mouse(motionButton, col, row)
		if !moveOK {
			return "", false
		}
		report = move + report
	}
	return report, true
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
	if m.pane.mouse {
		row, col, inside := m.paneCell(x, y)
		if !inside {
			return nil
		}
		report, ok := m.wheelReport(up, col, row+m.paneRowOffset(m.pane.box.height))
		if !ok {
			return nil
		}
		command := "send-keys -t " + tmux.SessionName(sess.ID) + " -H " + hexBytes(report)
		if !m.focus.attempt(command) {
			if err := m.tmux.SendRaw(command); err != nil {
				m.errBar.text = err.Error()
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
	if offset > m.pane.history {
		offset = m.pane.history
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
	rows := m.pane.box.height
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
