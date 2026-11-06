package focus

import (
	"reflect"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ExtendedKeyMsg is a modified key Bubble Tea reads only as an unknown CSI
// sequence, such as Shift+Enter, spelled as a tmux key name.
type ExtendedKeyMsg struct {
	Name string
}

var extendedKeyNames = map[int]string{
	9:   "Tab",
	13:  "Enter",
	27:  "Escape",
	32:  "Space",
	127: "BSpace",
}

const (
	shiftBit = 1 << iota
	altBit
	ctrlBit
)

// bindableExtendedKeys are the decoded keys a manager binding can name, as
// Bubble Tea reads their legacy form.
var bindableExtendedKeys = map[string]tea.KeyType{
	"S-Tab":   tea.KeyShiftTab,
	"C-Space": tea.KeyCtrlAt,
}

// reachesLegacyPane reports whether tmux delivers name intact to a pane that
// never asked for extended keys. Alt alone arrives there as an ESC prefix on
// the bare key. tmux 3.1 to 3.5 turn any other modified key into the bare key,
// such as Enter for S-Enter, its name typed out, or nothing.
func reachesLegacyPane(name string) bool {
	key, alt := strings.CutPrefix(name, "M-")
	return alt && !strings.Contains(key, "-")
}

const teaPackage = "github.com/charmbracelet/bubbletea"

// ExtendedKey turns the unknown CSI sequence Bubble Tea hands the program into
// an ExtendedKeyMsg when it spells a key tmux can name, and returns any other
// message unchanged.
func ExtendedKey(msg tea.Msg) tea.Msg {
	sequence, ok := unknownCSISequence(msg)
	if !ok {
		return msg
	}
	name, ok := ExtendedKeyName(sequence)
	if !ok {
		return msg
	}
	return ExtendedKeyMsg{Name: name}
}

// Bubble Tea keeps the message type unexported, so it is matched by name.
func unknownCSISequence(msg tea.Msg) ([]byte, bool) {
	msgType := reflect.TypeOf(msg)
	if msgType == nil || msgType.Name() != "unknownCSISequenceMsg" || msgType.PkgPath() != teaPackage {
		return nil, false
	}
	if msgType.Kind() != reflect.Slice || msgType.Elem().Kind() != reflect.Uint8 {
		return nil, false
	}
	return reflect.ValueOf(msg).Bytes(), true
}

// ExtendedKeyName decodes CSI <code>;<mods> u and the xterm form
// CSI 27;<mods>;<code> ~ for Enter, Tab, Backspace, Escape and Space held
// with Shift, Alt or Ctrl. Any other key, modifier or field is refused.
func ExtendedKeyName(sequence []byte) (string, bool) {
	body, ok := strings.CutPrefix(string(sequence), "\x1b[")
	if !ok {
		return "", false
	}
	var codeField, modsField string
	if fields, ok := strings.CutSuffix(body, "u"); ok {
		codeField, modsField, ok = strings.Cut(fields, ";")
		if !ok {
			return "", false
		}
	} else if fields, ok := strings.CutSuffix(body, "~"); ok {
		parts := strings.Split(fields, ";")
		if len(parts) != 3 || parts[0] != "27" {
			return "", false
		}
		modsField, codeField = parts[1], parts[2]
	} else {
		return "", false
	}
	code, ok := decimal(codeField)
	if !ok {
		return "", false
	}
	key, ok := extendedKeyNames[code]
	if !ok {
		return "", false
	}
	mods, ok := decimal(modsField)
	if !ok {
		return "", false
	}
	bits := mods - 1
	if bits <= 0 || bits&^(shiftBit|altBit|ctrlBit) != 0 {
		return "", false
	}
	prefix := ""
	if bits&ctrlBit != 0 {
		prefix += "C-"
	}
	if bits&altBit != 0 {
		prefix += "M-"
	}
	if bits&shiftBit != 0 {
		prefix += "S-"
	}
	return prefix + key, true
}

func decimal(field string) (int, bool) {
	if field == "" || strings.Trim(field, "0123456789") != "" {
		return 0, false
	}
	value, err := strconv.Atoi(field)
	return value, err == nil
}
