package focus

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestExtendedKeyName(t *testing.T) {
	for _, tc := range []struct {
		sequence string
		want     string
	}{
		{"\x1b[13;2u", "S-Enter"},
		{"\x1b[13;3u", "M-Enter"},
		{"\x1b[13;5u", "C-Enter"},
		{"\x1b[13;6u", "C-S-Enter"},
		{"\x1b[13;8u", "C-M-S-Enter"},
		{"\x1b[9;2u", "S-Tab"},
		{"\x1b[9;5u", "C-Tab"},
		{"\x1b[127;5u", "C-BSpace"},
		{"\x1b[27;2u", "S-Escape"},
		{"\x1b[32;2u", "S-Space"},
		{"\x1b[27;2;13~", "S-Enter"},
		{"\x1b[27;5;13~", "C-Enter"},
		{"\x1b[27;4;9~", "M-S-Tab"},
	} {
		got, ok := ExtendedKeyName([]byte(tc.sequence))
		if !ok || got != tc.want {
			t.Errorf("ExtendedKeyName(%q) = %q, %t, want %q", tc.sequence, got, ok, tc.want)
		}
	}
}

func TestExtendedKeyNameRefusesWhatItCannotName(t *testing.T) {
	for _, sequence := range []string{
		"\x1b[13u",        // no modifier: Bubble Tea reads plain Enter itself
		"\x1b[13;1u",      // a modifier field holding no modifier
		"\x1b[13;10u",     // super
		"\x1b[13;2:1u",    // an event type
		"\x1b[13:13;2u",   // an alternate key
		"\x1b[97;5u",      // a letter, which a manager binding may name
		"\x1b[57399;2u",   // a keypad key
		"\x1b[27;2;97~",   // a letter in the xterm form
		"\x1b[28;2;13~",   // not the xterm modified key form
		"\x1b[27;2;13;1~", // an extra field
		"\x1b[13;2~",      // a function key form
		"\x1b[;2u",        // no code
		"\x1b[13;u",       // no modifier value
		"\x1b[13;2",       // no final byte
		"[13;2u",          // no escape
		"\x1b[99999999999999999999;2u",
	} {
		if got, ok := ExtendedKeyName([]byte(sequence)); ok {
			t.Errorf("ExtendedKeyName(%q) = %q, want refused", sequence, got)
		}
	}
}

type extendedKeyRecorder struct {
	got []ExtendedKeyMsg
}

func (r *extendedKeyRecorder) Init() tea.Cmd { return nil }

func (r *extendedKeyRecorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(ExtendedKeyMsg); ok {
		r.got = append(r.got, key)
		if len(r.got) == 2 {
			return r, tea.Quit
		}
	}
	return r, nil
}

func (r *extendedKeyRecorder) View() string { return "" }

// Bubble Tea hands these sequences over as an unexported type, so an upgrade
// that renames it has to fail here instead of dropping Shift+Enter again.
func TestExtendedKeyReadsBubbleTeasUnknownSequence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	recorder := &extendedKeyRecorder{}
	program := tea.NewProgram(recorder,
		tea.WithContext(ctx),
		tea.WithInput(bytes.NewReader([]byte("\x1b[13;2u\x1b[27;5;13~"))),
		tea.WithOutput(io.Discard),
		tea.WithoutRenderer(),
		tea.WithoutSignalHandler(),
		tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg { return ExtendedKey(msg) }),
	)
	if _, err := program.Run(); err != nil {
		t.Fatalf("Run: %v, got %v", err, recorder.got)
	}
	want := []ExtendedKeyMsg{{Name: "S-Enter"}, {Name: "C-Enter"}}
	if len(recorder.got) != len(want) || recorder.got[0] != want[0] || recorder.got[1] != want[1] {
		t.Fatalf("program read %v, want %v", recorder.got, want)
	}
}

func TestExtendedKeyPassesOtherMessagesThrough(t *testing.T) {
	size := tea.WindowSizeMsg{Width: 80, Height: 24}
	if got := ExtendedKey(size); got != tea.Msg(size) {
		t.Fatalf("ExtendedKey(%#v) = %#v", size, got)
	}
}

// Measured on tmux 3.1c, 3.2a, 3.3a and 3.5a: a pane that never asked for
// extended keys gets Alt alone as an ESC prefix on the bare key, and any other
// modified key as the bare key, its name typed out, or nothing.
func TestReachesLegacyPane(t *testing.T) {
	for name, want := range map[string]bool{
		"M-Enter":     true,
		"M-Tab":       true,
		"M-BSpace":    true,
		"M-Escape":    true,
		"M-Space":     true,
		"S-Enter":     false,
		"C-Enter":     false,
		"C-S-Enter":   false,
		"M-S-Enter":   false,
		"C-M-Enter":   false,
		"C-M-S-Enter": false,
		"S-Tab":       false,
		"C-BSpace":    false,
		"C-Escape":    false,
		"S-Space":     false,
		"C-Space":     false,
	} {
		if got := reachesLegacyPane(name); got != want {
			t.Errorf("reachesLegacyPane(%q) = %t, want %t", name, got, want)
		}
	}
}
