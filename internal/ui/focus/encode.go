package focus

import tea "github.com/charmbracelet/bubbletea"

// namedKeys maps bubbletea key types to tmux send-keys key names.
// Populated in init: the Ctrl+letter block is generated, then the keys
// whose byte values double as named keys (Tab is Ctrl+I, Enter is Ctrl+M)
// get their proper names.
var namedKeys = map[tea.KeyType]string{}

func init() {
	for i := 1; i <= 26; i++ {
		namedKeys[tea.KeyType(i)] = "C-" + string(rune('a'+i-1))
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
		namedKeys[keyType] = name
	}
}

// EncodeKey spells one key press for the focused pane: text as the raw
// bytes to write, special keys as a tmux key name. Exactly one of raw and
// name is set when ok. ok is false for keys tmux cannot represent, which
// are dropped.
func EncodeKey(msg tea.KeyMsg) (raw []byte, name string, ok bool) {
	// Pastes go through the tmux buffer path: as raw bytes their newlines
	// would land as Enter presses and submit the agent's prompt.
	if msg.Paste {
		return nil, "", false
	}
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		runes := msg.Runes
		if msg.Type == tea.KeySpace {
			runes = []rune{' '}
		}
		text := []byte(string(runes))
		raw = make([]byte, 0, len(text)+1)
		if msg.Alt {
			// Alt arrives as an ESC prefix on the wire; replay it as one.
			raw = append(raw, 0x1b)
		}
		return append(raw, text...), "", true
	}
	name, ok = namedKeys[msg.Type]
	if !ok {
		return nil, "", false
	}
	if msg.Alt {
		name = "M-" + name
	}
	return nil, name, true
}
