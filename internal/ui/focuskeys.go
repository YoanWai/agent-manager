package ui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// focusNamedKeys maps bubbletea key types to tmux send-keys key names.
// Populated in init: the Ctrl+letter block is generated, then the keys
// whose byte values double as named keys (Tab is Ctrl+I, Enter is Ctrl+M)
// get their proper names.
var focusNamedKeys = map[tea.KeyType]string{}

func init() {
	for i := 1; i <= 26; i++ {
		focusNamedKeys[tea.KeyType(i)] = "C-" + string(rune('a'+i-1))
	}
	for keyType, name := range map[tea.KeyType]string{
		tea.KeyEnter:      "Enter",
		tea.KeyTab:        "Tab",
		tea.KeyShiftTab:   "BTab",
		tea.KeyBackspace:  "BSpace",
		tea.KeyEsc:        "Escape",
		tea.KeyUp:         "Up",
		tea.KeyDown:       "Down",
		tea.KeyLeft:       "Left",
		tea.KeyRight:      "Right",
		tea.KeyShiftUp:    "S-Up",
		tea.KeyShiftDown:  "S-Down",
		tea.KeyShiftLeft:  "S-Left",
		tea.KeyShiftRight: "S-Right",
		tea.KeyCtrlUp:     "C-Up",
		tea.KeyCtrlDown:   "C-Down",
		tea.KeyCtrlLeft:   "C-Left",
		tea.KeyCtrlRight:  "C-Right",
		tea.KeyHome:       "Home",
		tea.KeyEnd:        "End",
		tea.KeyPgUp:       "PPage",
		tea.KeyPgDown:     "NPage",
		tea.KeyDelete:     "DC",
		tea.KeyInsert:     "IC",
		tea.KeyF1:         "F1",
		tea.KeyF2:         "F2",
		tea.KeyF3:         "F3",
		tea.KeyF4:         "F4",
		tea.KeyF5:         "F5",
		tea.KeyF6:         "F6",
		tea.KeyF7:         "F7",
		tea.KeyF8:         "F8",
		tea.KeyF9:         "F9",
		tea.KeyF10:        "F10",
		tea.KeyF11:        "F11",
		tea.KeyF12:        "F12",
	} {
		focusNamedKeys[keyType] = name
	}
}

// focusKeyCommand encodes one key press as a tmux send-keys command for
// the focused session. Text goes as hex byte codes (-H), which sidesteps
// tmux command-line quoting entirely; special keys go by tmux key name.
// ok is false for keys tmux cannot represent, which are dropped.
func focusKeyCommand(target string, msg tea.KeyMsg) (string, bool) {
	// Pastes go through the tmux buffer path: as raw bytes their newlines
	// would land as Enter presses and submit the agent's prompt.
	if msg.Paste {
		return "", false
	}
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		runes := msg.Runes
		if msg.Type == tea.KeySpace {
			runes = []rune{' '}
		}
		raw := []byte(string(runes))
		codes := make([]string, 0, len(raw)+1)
		if msg.Alt {
			// Alt arrives as an ESC prefix on the wire; replay it as one.
			codes = append(codes, "1b")
		}
		for _, b := range raw {
			codes = append(codes, fmt.Sprintf("%02x", b))
		}
		return "send-keys -t " + target + " -H " + strings.Join(codes, " "), true
	}
	name, ok := focusNamedKeys[msg.Type]
	if !ok {
		return "", false
	}
	if msg.Alt {
		name = "M-" + name
	}
	return "send-keys -t " + target + " " + name, true
}

// focusSelected enters focus mode: keys go to the selected session's pane
// while the manager, its rail and its live preview stay on screen.
func (m *Model) focusSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if sess.Archived {
		return m.attachSelected()
	}
	if !m.tmux.Exists(sess.ID) {
		m.errBar.text = deadSessionHint
		return m, nil
	}
	m.errBar.text = ""
	if err := m.store.AcknowledgeFinished(sess.ID); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	m.mode = modeFocus
	// Focusing is deliberate, so the client opens now rather than waiting
	// for the cursor to settle, and any failure backoff is lifted.
	if m.focus != nil {
		m.focus.retryNow()
	}
	m.watchSelection()
	m.clearSelection()
	m.cursorOn = true
	m.focusScroll = 0
	// Pane state from a previously watched session must not route this
	// one's wheel; a fresh watcher's first pushed capture reports the real
	// values. When the watcher is already streaming this session and the
	// cache came from its own capture, it stays: a quiet pane pushes
	// nothing, so a reset here would leave the wheel routed as a plain
	// pane with no history until the agent next paints.
	if m.focus == nil || !m.focus.serving(sess.ID) || m.pane.forID != sess.ID {
		m.pane.mouse = false
		m.pane.motion = false
		m.pane.sgr = false
		m.pane.history = 0
	}
	// Mouse reporting makes the pane a closed window: clicks land here
	// instead of the host terminal, so a drag selects pane text alone and
	// never the rail beside it.
	return m, tea.Batch(tea.EnableMouseCellMotion, m.cursorBlink())
}

// caretAtInputStart reports whether the agent's caret sits at the head of
// its prompt, with nothing but the prompt marker to its left. Left is a
// no-op for the agent there, which is what frees the key to mean "back to
// the list" without ever costing a keystroke inside the prompt: anywhere
// else it still moves the caret.
//
// A wrapped prompt's continuation rows carry no marker, so a caret at the
// head of one of them forwards Left as usual and reaches the end of the
// row above.
func (m *Model) caretAtInputStart(sessID, tool string) bool {
	if m.engine == nil || !m.pane.cursor.ok || m.pane.forID != sessID || m.scrolledBack() {
		return false
	}
	rows := strings.Split(strings.TrimSuffix(m.preview, "\n"), "\n")
	if m.pane.cursor.y < 0 || m.pane.cursor.y >= len(rows) {
		return false
	}
	row := ansi.Strip(rows[m.pane.cursor.y])
	prefix, ok := m.engine.InputPrefix(tool, row)
	if !ok {
		return false
	}
	if !textBeforeCaret(m.engine, tool, row, m.pane.cursor.x) &&
		m.pane.cursor.x >= ansi.StringWidth(prefix) {
		return m.caretRowEndsAPromptHead(tool, rows, m.pane.cursor.y)
	}
	return false
}

// caretRowEndsAPromptHead rejects the row when a draft continues onto it:
// a multi-line prompt's blank continuation line looks exactly like an empty
// composer, and Left there belongs to the agent. The row above tells them
// apart when it carries the same marker with text past it. A wrapped line
// is rejected the same way; the rule that bounds the input box (pi's rule)
// is not draft text and does not reject.
func (m *Model) caretRowEndsAPromptHead(tool string, rows []string, y int) bool {
	if y == 0 {
		return true
	}
	above := ansi.Strip(rows[y-1])
	abovePrefix, ok := m.engine.InputPrefix(tool, above)
	if !ok || m.engine.MatchesActivityCutoff(tool, above) {
		return true
	}
	return strings.TrimSpace(above[len(abovePrefix):]) == ""
}

// textBeforeCaret reports whether anything but blanks sits between a tool's
// prompt marker and the caret on this row, which is how a line someone has
// half written is told from an empty prompt. A row that is not an input line
// carries no such text. Claude pads its marker with a non-breaking space, so
// blank means any space rune, not the ASCII one alone; tmux trims a row's
// trailing blanks, so a row that ends before the caret is blank the rest of
// the way.
func textBeforeCaret(engine *status.Engine, tool, row string, caretX int) bool {
	prefix, ok := engine.InputPrefix(tool, row)
	if !ok {
		return false
	}
	line := []rune(row)
	for cell := ansi.StringWidth(prefix); cell < caretX; {
		index := runeAtColumn(line, cell)
		if index >= len(line) {
			return false
		}
		if !unicode.IsSpace(line[index]) {
			return true
		}
		cell += ansi.StringWidth(string(line[index]))
	}
	return false
}

// leaveFocus returns to the list. Mouse reporting stays on: handing it back
// to the terminal here would let a wheel notch scroll the manager out of
// view, so the list swallows the wheel instead.
func (m *Model) leaveFocus() tea.Cmd {
	m.mode = modeList
	m.clearSelection()
	m.pending = pendingClick{}
	m.clearForwardingMouse()
	return nil
}

// handleFocusKey forwards every key into the focused pane. Ctrl+Q and
// ctrl+\ return to the list, Ctrl+R opens the review and F3 the editor,
// mirroring the bindings a real attach gets, and every plain character - q
// included - reaches the agent.
func (m *Model) handleFocusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+q" || msg.String() == `ctrl+\` {
		return m, m.leaveFocus()
	}
	sess, ok := m.selected()
	if !ok {
		return m, m.leaveFocus()
	}
	// F3 opens the session's directory in an editor, matching the binding a
	// real attach gets. A windowed editor leaves the focus where it is; one
	// that draws in the terminal takes it back on exit.
	if msg.String() == "f3" {
		return m.openEditor()
	}
	// Ctrl+R opens the review, matching the binding a real attach gets;
	// closing it focuses the session again rather than landing in the list.
	if msg.String() == "ctrl+r" {
		m.clearSelection()
		cmd := m.openDiff()
		if m.mode == modeDiff {
			m.diff.refocus = true
		}
		return m, cmd
	}
	if msg.Type == tea.KeyLeft && !msg.Alt && m.arrowStep && m.caretAtInputStart(sess.ID, sess.Tool) {
		return m, m.leaveFocus()
	}
	// Typing puts the cursor back on: a caret that blinks out mid-keystroke
	// reads as a dropped character.
	m.cursorOn = true
	// Keystrokes land at the live bottom, so the view follows them there.
	var resume tea.Cmd
	if m.scrolledBack() {
		m.focusScroll = 0
		resume = m.requestFocusRegion(sess.ID)
	}
	if msg.Paste {
		if err := pasteFocused(m.tmux, sess.ID, string(msg.Runes)); err != nil {
			m.errBar.text = err.Error()
		}
		return m, resume
	}
	command, ok := focusKeyCommand(tmux.SessionName(sess.ID), msg)
	if !ok {
		return m, resume
	}
	if m.focus == nil || !m.focus.attempt(command) {
		// Nothing went over the pipe; one forked send-keys keeps the key
		// from being swallowed.
		if err := m.tmux.SendRaw(command); err != nil {
			m.errBar.text = err.Error()
		}
	}
	return m, resume
}
