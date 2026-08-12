package ui

import (
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// pinnedHost forces the direction marks on for a test that covers the host
// they exist for, whatever terminal the suite itself runs under.
func pinnedHost(t *testing.T) {
	t.Helper()
	previous := bidiPin
	bidiPin = func() bool { return true }
	t.Cleanup(func() { bidiPin = previous })
}

// Pure-RTL pane rows that sit beside empty rail cells must still open with a
// strong LTR mark before any Hebrew. Hosts that pick paragraph direction from
// the first strong character otherwise right-justify the whole terminal line
// and the pane text lands in the rail.
func TestHebrewPaneRowKeepsLTRParagraph(t *testing.T) {
	pinnedHost(t)
	he := strings.Join([]string{
		"english line",
		"  העצמון 25 חולון",
		"mixed ת.ז. end",
		"",
		"  העצמון 25 חולון",
	}, "\n")
	// Tall frames put pure-Hebrew pane rows next to empty rail padding, which
	// is the geometry that flips paragraph direction without a leading LRM.
	for _, width := range []int{100, 140, 180} {
		for _, height := range []int{40, 50} {
			foundHebrew := false
			m := shotModel()
			m.width, m.height = width, height
			m.preview = he + "\n" + strings.Repeat("  העצמון 25 חולון\n", 20)
			m.mode = modeFocus
			for i, line := range strings.Split(m.View(), "\n") {
				plain := ansi.Strip(line)
				heb := strings.IndexFunc(plain, func(r rune) bool {
					return unicode.In(r, unicode.Hebrew)
				})
				if heb < 0 {
					continue
				}
				foundHebrew = true
				lrm := strings.IndexRune(plain, '\u200e')
				if lrm < 0 || lrm > heb {
					t.Errorf("%dx%d line %d: LRM missing or after Hebrew (lrm=%d heb=%d)\n%q",
						width, height, i, lrm, heb, plain)
				}
				if got := ansi.StringWidth(line); got > width {
					t.Errorf("%dx%d line %d overflows at %d", width, height, i, got)
				}
			}
			if !foundHebrew {
				t.Fatalf("%dx%d frame did not render a Hebrew preview row", width, height)
			}
		}
	}
}

func TestHebrewRailNameStaysLTR(t *testing.T) {
	pinnedHost(t)
	got := pinFrameLTR("█ ◆ העצמון   waiting\nplain row\n\u200e\u2066שלום\u2069")
	lines := strings.Split(got, "\n")
	if !strings.HasPrefix(lines[0], "\u200e") {
		t.Fatalf("row with a leading Hebrew rail name should gain an LRM: %q", lines[0])
	}
	if lines[1] != "plain row" {
		t.Fatalf("LTR row should be untouched: %q", lines[1])
	}
	if strings.HasPrefix(lines[2], "\u200e\u200e") {
		t.Fatalf("already pinned row should not gain a second mark: %q", lines[2])
	}
}

// Frame rows arrive styled, and an escape sequence ends in a Latin letter.
// Classifying the raw row finds that letter first, calls the row LTR, and the
// pin never fires on any row the manager actually paints.
func TestHebrewRowPinnedThroughANSI(t *testing.T) {
	pinnedHost(t)
	for _, row := range []string{
		"\x1b[31mשלום",
		"\x1b[38;2;120;130;140m█\x1b[0m   שלום עולם",
		"שלום",
	} {
		got := pinFrameLTR(row)
		if !strings.HasPrefix(got, "\u200e") {
			t.Errorf("row %q should have gained an LRM, got %q", row, got)
		}
		if !strings.HasSuffix(got, row) {
			t.Errorf("row %q must survive the pin intact, got %q", row, got)
		}
	}
	for _, row := range []string{"\x1b[31mplain latin", "\x1b[31mlatin שלום mixed"} {
		if got := pinFrameLTR(row); got != row {
			t.Errorf("row leading with strong LTR should be untouched: %q -> %q", row, got)
		}
	}
}

// A host that runs its own bidi reorders a row on the direction marks and the
// frame stops matching what the renderer believes it painted, so an unpinned
// host must receive no format characters at all.
func TestUnpinnedHostEmitsNoDirectionMarks(t *testing.T) {
	previous := bidiPin
	bidiPin = func() bool { return false }
	t.Cleanup(func() { bidiPin = previous })

	body := strings.Repeat("  שלום עולם אני כותב עברית כאן\n", 25)
	for _, size := range [][2]int{{211, 53}, {180, 40}, {152, 44}, {120, 34}} {
		for _, md := range []mode{modeList, modeFocus} {
			m := shotModel()
			m.width, m.height = size[0], size[1]
			m.mode = md
			m.preview = body
			frame := m.View()
			for _, mark := range []string{"\u200e", "\u2066", "\u2069"} {
				if strings.Contains(frame, mark) {
					t.Errorf("%dx%d mode %v: frame carries %q", size[0], size[1], md, mark)
				}
			}
			rows := strings.Split(frame, "\n")
			if len(rows) != m.height {
				t.Errorf("%dx%d mode %v: frame is %d rows, want %d", size[0], size[1], md, len(rows), m.height)
			}
			for index, row := range rows {
				if width := ansi.StringWidth(row); width > m.width {
					t.Errorf("%dx%d mode %v: row %d is %d cells, want <= %d",
						size[0], size[1], md, index, width, m.width)
				}
			}
		}
	}
}

func TestDetectBidiPin(t *testing.T) {
	for _, testCase := range []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"bare terminal", map[string]string{}, false},
		{"wezterm", map[string]string{"TERM_PROGRAM": "WezTerm"}, false},
		{"iterm", map[string]string{"TERM_PROGRAM": "iTerm.app"}, true},
		{"iterm over ssh", map[string]string{"LC_TERMINAL": "iTerm2"}, true},
		{"forced on", map[string]string{"AGENT_MANAGER_RTL_PIN": "1"}, true},
		{"forced off", map[string]string{"AGENT_MANAGER_RTL_PIN": "off", "TERM_PROGRAM": "iTerm.app"}, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			for _, key := range []string{"TERM_PROGRAM", "LC_TERMINAL", "AGENT_MANAGER_RTL_PIN"} {
				t.Setenv(key, "")
			}
			for key, value := range testCase.env {
				t.Setenv(key, value)
			}
			if got := detectBidiPin(); got != testCase.want {
				t.Errorf("detectBidiPin() = %v, want %v", got, testCase.want)
			}
		})
	}
}
