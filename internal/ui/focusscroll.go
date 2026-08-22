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
	rows    int
	preview string
	ok      bool
}

// Mouse button codes as the reports carry them: the two wheel
// directions, and a pointer move with no button held (button 3 with the
// motion bit set).
const (
	wheelUpButton   = 64
	wheelDownButton = 65
	motionButton    = 35
	leftButton      = 0
	middleButton    = 1
	rightButton     = 2
	motionBit       = 32
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
	return m.mouseReport(button, false, col, row)
}

// mouseReport encodes one press, drag, release or wheel event for the
// focused pane. An all-motion app needs a pointer move ahead of discrete
// events because focus mode does not pass its ordinary pointer moves on.
func (m *Model) mouseReport(button int, release bool, col, row int) (string, bool) {
	if m.pane.sgr {
		terminator := "M"
		if release {
			terminator = "m"
		}
		report := fmt.Sprintf("\x1b[<%d;%d;%d%s", button, col+1, row+1, terminator)
		if m.pane.motion && !release {
			report = sgrMouse(motionButton, col, row) + report
		}
		return report, true
	}
	if release {
		// X10 cannot name the released button: button 3 denotes all releases.
		button = 3
	}
	report, ok := x10Mouse(button, col, row)
	if !ok {
		return "", false
	}
	if m.pane.motion && !release {
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
// guardedMouseCommand has tmux drop the report unless the pane still tracks
// the mouse when it arrives. The cached flag trails a pane that left mouse
// mode by a debounce, and a report the application no longer expects is
// printed on its input line instead. mouse_any_flag covers every mode, so
// the per-mode flags would only repeat it.
func guardedMouseCommand(sessID, report string) (string, []string) {
	target := tmux.PaneTarget(sessID)
	const condition = "#{mouse_any_flag}"
	send := "send-keys -t " + target + " -H " + hexBytes(report)
	command := "if-shell -F -t " + target + " '" + condition + "' '" + send + "'"
	return command, []string{"if-shell", "-F", "-t", target, condition, send}
}

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
		command, args := guardedMouseCommand(sess.ID, report)
		if !m.focus.attempt(command) {
			if err := m.tmux.SendCommand(args...); err != nil {
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

// forwardFocusMouse sends an explicit Alt-modified click lifecycle to a
// focused application that owns the mouse. Normal clicks remain available
// for agent-manager's selection and clipboard behavior.
func (m *Model) forwardFocusMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	row, col, ok := m.forwardedMouseCell(msg)
	if !ok {
		return m, nil
	}
	button := m.forwardedMouseButton(msg)
	release := msg.Action == tea.MouseActionRelease
	if msg.Action == tea.MouseActionMotion {
		button |= motionBit
	}
	report, ok := m.mouseReport(button, release, col, row+m.paneRowOffset(m.pane.box.height))
	if !ok {
		return m, nil
	}
	m.sendFocusReport(report)
	return m, nil
}

// sendFocusReport delivers encoded mouse bytes to the focused session's
// pane, preferring the live control pipe over a forked tmux call.
func (m *Model) sendFocusReport(report string) {
	sess, ok := m.selected()
	if !ok || m.focus == nil {
		return
	}
	command, args := guardedMouseCommand(sess.ID, report)
	if !m.focus.attempt(command) {
		if err := m.tmux.SendCommand(args...); err != nil {
			m.errBar.text = err.Error()
		}
	}
}

// forwardedMouseCell uses the current cell while the pointer is in the pane.
// A release immediately after leaving the pane belongs to the active gesture,
// so it lands at that gesture's final in-pane cell instead.
func (m *Model) forwardedMouseCell(msg tea.MouseMsg) (row, col int, ok bool) {
	if row, col, inside := m.paneCell(msg.X, msg.Y); inside {
		m.forwardingRow, m.forwardingCol = row, col
		return row, col, true
	}
	if msg.Action == tea.MouseActionRelease && m.forwardingMouse {
		return m.forwardingRow, m.forwardingCol, true
	}
	return 0, 0, false
}

// forwardedMouseButton keeps an X10 release paired with the press that
// started its Alt-forwarded lifecycle. Bubble Tea reports X10 releases as
// MouseButtonNone, while SGR needs the button that was released.
func (m *Model) forwardedMouseButton(msg tea.MouseMsg) int {
	if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonNone {
		return m.forwardingButton
	}
	return mouseButton(msg.Button)
}

func mouseButton(button tea.MouseButton) int {
	switch button {
	case tea.MouseButtonMiddle:
		return middleButton
	case tea.MouseButtonRight:
		return rightButton
	default:
		return leftButton
	}
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
	return m.requestFocusRegion(sess.ID)
}

// requestFocusRegion fetches the current scroll target. Every notch gets
// its own command: callers (including tests) rely on a non-nil command
// per notch, and stale replies are discarded by the offset guard in
// Update, so a burst costs extra captures but never a wrong frame.
func (m *Model) requestFocusRegion(sessID string) tea.Cmd {
	return m.focusRegionCmd(sessID, m.focusScroll)
}

// focusRegionCmd captures the pane region that sits offset lines above the
// live bottom. The pane's height rarely matches the panel — resizeSessions
// leaves a pane taller rather than costing Codex its scrollback, and the
// session's status line makes the visible pane one row shorter than the
// pinned window — so the pane's bottom, not tmux's visible row zero,
// anchors the window: the capture reaches rows+offset lines into history
// and runs to the visible end ("-E -"), and the window is cut from its
// end, which needs no pane height at all. tmux clamps the start to the
// history top, so a shallow history just yields a shorter frame.
func (m *Model) focusRegionCmd(sessID string, offset int) tea.Cmd {
	rows := m.previewPaneHeight()
	command := fmt.Sprintf(`capture-pane -p -e -t %s -S %d -E -`,
		tmux.PaneTarget(sessID), -(offset + rows))
	watch := m.focus
	return func() tea.Msg {
		if watch == nil {
			return focusScrollMsg{sessID: sessID, offset: offset, rows: rows}
		}
		out, ok := watch.query(command)
		return focusScrollMsg{sessID: sessID, offset: offset, rows: rows,
			preview: bottomWindow(matchExecShape(out), rows, offset), ok: ok}
	}
}

// bottomWindow cuts the rows lines that end offset lines above the
// capture's last row.
func bottomWindow(capture string, rows, offset int) string {
	lines := strings.Split(strings.TrimSuffix(capture, "\n"), "\n")
	end := len(lines) - offset
	if end < 0 {
		end = 0
	}
	start := end - rows
	if start < 0 {
		start = 0
	}
	return strings.Join(lines[start:end], "\n") + "\n"
}

// scrolledBack reports whether the focused pane is showing history rather
// than its live bottom.
func (m *Model) scrolledBack() bool {
	return m.mode == modeFocus && m.focusScroll > 0
}
