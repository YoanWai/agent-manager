package ui

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/x/ansi"
)

var focusNamedKeys = map[rune]string{
	tea.KeyEnter:  "Enter",
	tea.KeyTab:    "Tab",
	tea.KeyUp:     "Up",
	tea.KeyDown:   "Down",
	tea.KeyLeft:   "Left",
	tea.KeyRight:  "Right",
	tea.KeyHome:   "Home",
	tea.KeyEnd:    "End",
	tea.KeyPgUp:   "PPage",
	tea.KeyPgDown: "NPage",
	tea.KeyDelete: "DC",
	tea.KeyInsert: "IC",
	tea.KeyF1:     "F1",
	tea.KeyF2:     "F2",
	tea.KeyF3:     "F3",
	tea.KeyF4:     "F4",
	tea.KeyF5:     "F5",
	tea.KeyF6:     "F6",
	tea.KeyF7:     "F7",
	tea.KeyF8:     "F8",
	tea.KeyF9:     "F9",
	tea.KeyF10:    "F10",
	tea.KeyF11:    "F11",
	tea.KeyF12:    "F12",
}

// focusKeyCommand encodes one key press as a tmux send-keys command for
// the focused session. Text goes as hex byte codes (-H), which sidesteps
// tmux command-line quoting entirely; special keys go by tmux key name.
// ok is false for keys tmux cannot represent, which are dropped.
func focusKeyCommand(target string, msg tea.KeyPressMsg) (string, bool) {
	alt := msg.Mod.Contains(tea.ModAlt)
	if text := focusKeyText(msg); text != "" {
		raw := []byte(text)
		codes := make([]string, 0, len(raw)+1)
		if alt {
			// Alt arrives as an ESC prefix on the wire; replay it as one.
			codes = append(codes, "1b")
		}
		for _, b := range raw {
			codes = append(codes, fmt.Sprintf("%02x", b))
		}
		return "send-keys -t " + target + " -H " + strings.Join(codes, " "), true
	}
	name, ok := focusKeyName(msg)
	if !ok {
		return "", false
	}
	if alt {
		name = "M-" + name
	}
	return "send-keys -t " + target + " " + name, true
}

func focusKeyText(msg tea.KeyPressMsg) string {
	if msg.Text != "" {
		return msg.Text
	}
	if msg.Mod&^(tea.ModAlt|tea.ModShift) != 0 || !unicode.IsPrint(msg.Code) {
		return ""
	}
	if msg.Mod.Contains(tea.ModShift) {
		return string(unicode.ToUpper(msg.Code))
	}
	return string(msg.Code)
}

func focusKeyName(msg tea.KeyPressMsg) (string, bool) {
	ctrl := msg.Mod.Contains(tea.ModCtrl)
	shift := msg.Mod.Contains(tea.ModShift)
	switch {
	case msg.Code == tea.KeyBackspace && ctrl:
		// tmux has no modified BSpace or Escape names and types them as
		// text; 0x08 is the byte a legacy terminal reports for ctrl+backspace.
		return "C-h", true
	case msg.Code == tea.KeyBackspace:
		return "BSpace", true
	case msg.Code == tea.KeyEscape, ctrl && msg.Code == '[':
		return "Escape", true
	case ctrl && (msg.Code == tea.KeySpace || msg.Code == '@'):
		return "C-Space", true
	case ctrl && strings.ContainsRune(`\]^_`, msg.Code):
		return "C-" + string(msg.Code), true
	case ctrl && msg.Code >= 'a' && msg.Code <= 'z':
		return "C-" + string(msg.Code), true
	}
	name, named := focusNamedKeys[msg.Code]
	if !named {
		return "", false
	}
	if shift && msg.Code == tea.KeyTab {
		return "BTab", true
	}
	if shift {
		name = "S-" + name
	}
	if ctrl {
		name = "C-" + name
	}
	return name, true
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
	// The full screen layout opens the session across the whole body, so
	// its pane grows to fill the frame before the first capture paints.
	if m.fullLayout {
		m.pinFullFocusPane(sess.ID)
	}
	// Focusing is deliberate, so the client opens now rather than waiting
	// for the cursor to settle, and any failure backoff is lifted.
	if m.focus != nil {
		m.focus.retryNow()
	}
	m.watchSelection()
	m.clearSelection()
	m.cursorOn = true
	m.focusScroll = 0
	m.focusFetchInFlight = false
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
	return m, m.cursorBlink()
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
//
// A tool may park the terminal cursor below its footer and paint its own
// block cursor inside the composer instead (command-code does). For one
// declaring composer_placeholder, a caret on a blank row reads the
// composer row instead: the placeholder being on screen is the evidence
// the composer is empty and its caret at the head of the prompt; a draft
// replaces the placeholder and Left belongs to the agent.
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
		return m.caretParksAndComposerIsEmpty(tool, rows)
	}
	if !textBeforeCaret(m.engine, tool, row, m.pane.cursor.x) &&
		m.pane.cursor.x >= ansi.StringWidth(prefix) {
		return m.caretRowEndsAPromptHead(tool, rows, m.pane.cursor.y)
	}
	return false
}

// caretParksAndComposerIsEmpty serves tools that park the terminal cursor
// below their footer and paint the composer's caret themselves. The parked
// cell sits on a blank row, so the composer is found by searching up for
// the nearest marker row, and an empty composer there is what proves Left
// costs the agent nothing: a draft holds the key instead.
// A caret cell that is not parked on a blank row is none of this path's
// business: the marker rules decide it as usual.
func (m *Model) caretParksAndComposerIsEmpty(tool string, rows []string) bool {
	// The parking spot is a blank corner cell: column zero on a row with
	// nothing painted on it. A cursor at column zero over any other
	// content is not the park, whatever sits above it.
	if m.pane.cursor.x != 0 || strings.TrimSpace(ansi.Strip(rows[m.pane.cursor.y])) != "" {
		return false
	}
	for y := m.pane.cursor.y - 1; y >= 0; y-- {
		row := ansi.Strip(rows[y])
		if _, ok := m.engine.InputPrefix(tool, row); !ok {
			continue
		}
		return m.engine.ComposerIsEmpty(tool, row)
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
func (m *Model) handleFocusKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
	if msg.Code == tea.KeyLeft && msg.Mod == 0 && m.arrowStep && m.caretAtInputStart(sess.ID, sess.Tool) {
		return m, m.leaveFocus()
	}
	// Enter is how a drafted prompt leaves the composer, so the draft is
	// snapshotted on its way in; alt+enter only breaks the line.
	if msg.Code == tea.KeyEnter && !msg.Mod.Contains(tea.ModAlt) {
		m.stashTypedPrompt(sess)
	}
	resume := m.wakeFocusInput(sess.ID)
	command, ok := focusKeyCommand(tmux.PaneTarget(sess.ID), msg)
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

// handleFocusPaste hands a bracketed paste to the focused pane through the
// tmux buffer path: as raw keystrokes its newlines would land as Enter
// presses and submit the agent's prompt.
func (m *Model) handleFocusPaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, m.leaveFocus()
	}
	resume := m.wakeFocusInput(sess.ID)
	if err := pasteFocused(m.tmux, sess.ID, msg.Content); err != nil {
		m.errBar.text = err.Error()
	}
	return m, resume
}

// wakeFocusInput is what every keystroke and paste does before reaching
// the pane. The cursor comes back on, since a caret that blinks out
// mid-keystroke reads as a dropped character, and a scrolled-back view
// follows the input to the live bottom.
func (m *Model) wakeFocusInput(sessID string) tea.Cmd {
	m.cursorOn = true
	if !m.scrolledBack() {
		return nil
	}
	m.focusScroll = 0
	return m.requestFocusRegion(sessID)
}
