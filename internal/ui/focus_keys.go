package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
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
// while the manager, its rail and its live preview stay on screen. The
// dead-pane probe and the finished acknowledgement run on the effect
// lane; the completion enters focus only while this selection holds.
func (m *Model) focusSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if sess.Archived {
		return m.attachSelected()
	}
	// Cleared on the press, as attach does, so a notice the lane posts
	// before entry, such as a dropped detach request, stays up.
	m.clearErr()
	m.enqueueEffect(focusRequest{sessionID: sess.ID, generation: m.gens.foreground}, 0, false)
	return m, m.nextEffectCmd()
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
//
// A zero-width input_prefix declares a bare composer row (pi's). Its
// painted cursor is the only thing locating that row, so tmux's position
// stays meaningful when the application hides the terminal cursor.
func (m *Model) caretAtInputStart(sessID, tool string) bool {
	pane := m.focus.pane.Pane()
	if m.services.engine == nil || pane.SessionID != sessID || m.focus.pane.ScrolledBack() {
		return false
	}
	_, bareInput := m.services.engine.InputPrefix(tool, "")
	caretCellKnown := pane.Cursor.Visible ||
		(pane.Cursor.PositionKnown && (m.services.engine.ParksItsCaret(tool) || bareInput))
	if !caretCellKnown {
		return false
	}
	rows := strings.Split(strings.TrimSuffix(m.workspace.preview, "\n"), "\n")
	if pane.Cursor.Y < 0 || pane.Cursor.Y >= len(rows) {
		return false
	}
	row := ansi.Strip(rows[pane.Cursor.Y])
	prefix, ok := m.services.engine.InputPrefix(tool, row)
	if !ok {
		return m.caretParksAndComposerIsEmpty(tool, rows)
	}
	if !status.HasTextBeforeCaret(m.services.engine, tool, row, pane.Cursor.X) &&
		pane.Cursor.X >= ansi.StringWidth(prefix) {
		return m.caretRowEndsAPromptHead(tool, rows, pane.Cursor.Y)
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
	cursor := m.focus.pane.Pane().Cursor
	// The parking spot is a blank corner cell: column zero on a row with
	// nothing painted on it. A cursor at column zero over any other
	// content is not the park, whatever sits above it.
	if cursor.X != 0 || strings.TrimSpace(ansi.Strip(rows[cursor.Y])) != "" {
		return false
	}
	for y := cursor.Y - 1; y >= 0; y-- {
		row := ansi.Strip(rows[y])
		if _, ok := m.services.engine.InputPrefix(tool, row); !ok {
			continue
		}
		return m.services.engine.ComposerIsEmpty(tool, row)
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
	abovePrefix, ok := m.services.engine.InputPrefix(tool, above)
	if !ok || m.services.engine.MatchesActivityCutoff(tool, above) {
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

// leaveFocus returns to the list. Mouse reporting stays on: handing it back
// to the terminal here would let a wheel notch scroll the manager out of
// view, so the list swallows the wheel instead.
func (m *Model) leaveFocus() tea.Cmd {
	if report := m.focus.pane.Leave(); report != "" {
		m.sendFocusReport(report)
	}
	return m.leaveFocusMode()
}

func (m *Model) leaveFocusMode() tea.Cmd {
	m.mode = modeList
	// A run opened before the session was entered would pair with the very
	// click that comes back here, focusing it again instead of leaving.
	m.rail.ResetClickHistory()
	m.flushPendingNotice()
	return nil
}

// focusPagesScrollback reads the Focus policy for the footer and status
// line, which name the page keys only while they page.
func (m *Model) focusPagesScrollback(sess store.Session) bool {
	return m.focus.pane.PagesScrollback(sess.ID)
}

// handleFocusKey forwards focused input to the pane except for session
// actions and paging a normal-screen pane's history. Every plain
// character, q included, still reaches the agent.
func (m *Model) handleFocusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, m.leaveFocus()
	}
	result := m.focus.pane.Key(msg, uifocus.KeyContext{
		SessionID:   sess.ID,
		Rows:        m.focusPaneRows(),
		Detach:      m.services.keys.Binding(keybind.Detach).Has(msg.String()),
		Editor:      m.services.keys.Binding(keybind.Editor).Has(msg.String()),
		Review:      m.services.keys.Binding(keybind.Review).Has(msg.String()),
		ArrowStep:   m.prefs.arrowStep,
		AtInputHead: msg.Type == tea.KeyLeft && !msg.Alt && m.caretAtInputStart(sess.ID, sess.Tool),
	})
	switch result.Action {
	case uifocus.LeaveFocus:
		if result.SendReport != "" {
			m.sendFocusReport(result.SendReport)
		}
		return m, m.leaveFocusMode()
	case uifocus.ScrollPane:
		if result.Region == nil {
			return m, nil
		}
		return m, m.focusRegionRequestCmd(*result.Region)
	case uifocus.OpenEditor:
		return m.openEditor()
	case uifocus.OpenReview:
		cmd := m.openDiff()
		if m.mode == modeDiff {
			m.reviewNav.ret = reviewReturn{kind: reviewReturnFocus, sessionID: sess.ID}
		}
		return m, cmd
	}
	var resume tea.Cmd
	if result.Region != nil {
		resume = m.focusRegionRequestCmd(*result.Region)
	}
	if msg.Paste {
		m.dispatchInput(inputRequest{kind: inputPaste, session: sess, text: string(msg.Runes)})
		return m, tea.Batch(resume, m.nextEffectCmd())
	}
	command, ok := focusKeyCommand(tmux.PaneTarget(sess.ID), msg)
	if !ok {
		return m, resume
	}
	m.dispatchInput(inputRequest{kind: inputKeys, session: sess, command: command, submit: result.Submit})

	return m, tea.Batch(resume, m.nextEffectCmd())
}

// pasteFocused is the seam tests swap to observe pastes into the pane.
var pasteFocused = func(driver *tmux.Driver, id, text string) error {
	return driver.Paste(id, text)
}
