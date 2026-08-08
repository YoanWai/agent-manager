package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
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
	m.sel = focusSelection{}
	m.copied = 0
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

// leaveFocus returns to the list. Mouse reporting stays on: handing it back
// to the terminal here would let a wheel notch scroll the manager out of
// view, so the list swallows the wheel instead.
func (m *Model) leaveFocus() tea.Cmd {
	m.mode = modeList
	m.sel = focusSelection{}
	m.copied = 0
	return nil
}

// handleFocusKey forwards every key into the focused pane. Ctrl+Q and
// ctrl+\ are the reserved keys: they return to the list, mirroring detach
// from a real attach, and every plain character - q included - reaches
// the agent.
func (m *Model) handleFocusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+q" || msg.String() == `ctrl+\` {
		return m, m.leaveFocus()
	}
	sess, ok := m.selected()
	if !ok {
		return m, m.leaveFocus()
	}
	// Typing puts the cursor back on: a caret that blinks out mid-keystroke
	// reads as a dropped character.
	m.cursorOn = true
	// Keystrokes land at the live bottom, so the view follows them there.
	var resume tea.Cmd
	if m.scrolledBack() {
		m.focusScroll = 0
		resume = m.focusRegionCmd(sess.ID, 0)
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
