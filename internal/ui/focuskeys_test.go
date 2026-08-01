package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestFocusKeyCommand(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want string
		ok   bool
	}{
		{"runes", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")}, "send-keys -t am_x -H 68 69", true},
		{"utf8", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ש")}, "send-keys -t am_x -H d7 a9", true},
		{"space", tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}, "send-keys -t am_x -H 20", true},
		{"alt-rune", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true}, "send-keys -t am_x -H 1b 78", true},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "send-keys -t am_x Enter", true},
		{"escape", tea.KeyMsg{Type: tea.KeyEsc}, "send-keys -t am_x Escape", true},
		{"ctrl-c", tea.KeyMsg{Type: tea.KeyCtrlC}, "send-keys -t am_x C-c", true},
		{"tab-not-ctrl-i", tea.KeyMsg{Type: tea.KeyTab}, "send-keys -t am_x Tab", true},
		{"enter-not-ctrl-m", tea.KeyMsg{Type: tea.KeyEnter}, "send-keys -t am_x Enter", true},
		{"shift-tab", tea.KeyMsg{Type: tea.KeyShiftTab}, "send-keys -t am_x BTab", true},
		{"up", tea.KeyMsg{Type: tea.KeyUp}, "send-keys -t am_x Up", true},
		{"alt-up", tea.KeyMsg{Type: tea.KeyUp, Alt: true}, "send-keys -t am_x M-Up", true},
		{"pgup", tea.KeyMsg{Type: tea.KeyPgUp}, "send-keys -t am_x PPage", true},
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}, "send-keys -t am_x BSpace", true},
		// A bracketed paste never rides the raw-bytes path: its newlines
		// would reach the agent as bare Enter presses and submit the prompt.
		{"paste", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("do it\n"), Paste: true}, "", false},
	}
	for _, c := range cases {
		got, ok := focusKeyCommand("am_x", c.msg)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

// A captured row's background run must survive rendering unchanged: an
// app that paints a bar and resets only the foreground leaves those cells
// with a background in tmux's own grid, and the preview has to reproduce
// exactly those columns, no wider and no narrower.

// bgRun returns, for each column, whether a background color is active,
// by walking the row's SGR sequences the way a terminal would.
func bgRun(row string, width int) []bool {
	out := make([]bool, 0, width)
	bg := false
	i := 0
	for i < len(row) && len(out) < width {
		if row[i] == 0x1b {
			end := i
			for end < len(row) && !strings.ContainsRune("mK", rune(row[end])) {
				end++
			}
			if end < len(row) && row[end] == 'm' {
				params := row[i+2 : end]
				for _, p := range strings.Split(params, ";") {
					switch {
					case p == "0" || p == "" || p == "49":
						bg = false
					case strings.HasPrefix(p, "4") && len(p) == 2:
						bg = true
					case p == "48":
						bg = true
					}
				}
			}
			i = end + 1
			continue
		}
		out = append(out, bg)
		i++
	}
	return out
}

func TestPreviewLinePreservesBackgroundColumns(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	raw := "\x1b[38;5;239m\x1b[48;5;237m❯\x1b[39m \x1b[38;5;231mhello there\x1b[39m"
	width := 40
	got := previewLine(raw, width)

	rawCells := bgRun(raw, width)
	gotCells := bgRun(got, width)
	t.Logf("raw plain=%q width=%d", ansi.Strip(raw), ansi.StringWidth(ansi.Strip(raw)))
	t.Logf("raw bg cells=%v", rawCells)
	t.Logf("out bg cells=%v", gotCells)
	if len(rawCells) != len(gotCells) {
		t.Fatalf("cell counts differ: raw %d, rendered %d", len(rawCells), len(gotCells))
	}
	for i := range rawCells {
		if rawCells[i] != gotCells[i] {
			t.Fatalf("column %d background differs: raw %v, rendered %v", i, rawCells[i], gotCells[i])
		}
	}
}

// The caret overpaints one cell and nothing else: a row the agent drew
// with its own background must keep exactly that background everywhere
// except the caret, or the row appears to flash a band on every blink.
func TestCaretKeepsRowColours(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	raw := "\x1b[48;5;237m\x1b[38;5;231mprompt text here\x1b[0m"
	width := 30
	m := paneAt(t, raw)
	m.pane.cursor = paneCursor{x: 3, y: 0, ok: true}
	m.cursorOn = true

	plainRow := previewLine(raw, width)
	withCaret := m.renderPaneRow(0, raw, width)

	if ansi.Strip(withCaret) != ansi.Strip(plainRow) {
		t.Fatalf("caret changed the row text: %q vs %q", ansi.Strip(withCaret), ansi.Strip(plainRow))
	}

	plainCells := bgRun(plainRow, width)
	caretCells := bgRun(withCaret, width)
	if len(plainCells) != len(caretCells) {
		t.Fatalf("cell counts differ: %d vs %d", len(plainCells), len(caretCells))
	}
	for i := range plainCells {
		if plainCells[i] != caretCells[i] {
			t.Fatalf("column %d background changed by the caret: %v vs %v (row %q)",
				i, plainCells[i], caretCells[i], withCaret)
		}
	}
}

// The caret blinks while focused and stops when focus ends.
func TestCursorBlinks(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "blinker", t.TempDir(), "")
	m.selectSessionRow(t, "blinker")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if !m.cursorOn {
		t.Fatal("caret starts hidden")
	}

	updated, cmd := m.Update(cursorBlinkMsg{})
	*m = *updated.(*Model)
	if m.cursorOn {
		t.Fatal("caret did not blink off")
	}
	if cmd == nil {
		t.Fatal("blink timer was not re-armed while focused")
	}

	// Typing must show the caret again immediately.
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	*m = *updated.(*Model)
	if !m.cursorOn {
		t.Fatal("typing left the caret hidden")
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	*m = *updated.(*Model)
	if _, cmd := m.Update(cursorBlinkMsg{}); cmd != nil {
		t.Fatal("blink timer kept running after focus ended")
	}
}

// The setting swaps which key focuses and which attaches, and persists.
func TestSettingsSwapsFocusKey(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if !m.settings.enterFocuses {
		t.Fatal("settings should open with enter focusing")
	}
	card := ansi.Strip(m.viewSettings())
	if !strings.Contains(card, "session keys") {
		t.Fatalf("settings card has no session keys row:\n%s", card)
	}
	for i := 0; i < settingsFieldFocusKey; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settings.field != settingsFieldFocusKey {
		t.Fatalf("stepping down should reach the session keys field, got %d", m.settings.field)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	if !strings.Contains(ansi.Strip(m.viewSettings()), "attach") {
		t.Fatal("swapped card does not read attach on enter")
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.enterFocuses() {
		t.Fatal("swapped choice did not persist")
	}
}

// With the keys swapped, Enter attaches and A focuses.
func TestSwappedKeysRouteActions(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "swapped", t.TempDir(), "")
	m.selectSessionRow(t, "swapped")

	// Default: enter focuses.
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("enter did not focus by default, mode = %v", m.mode)
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	*m = *updated.(*Model)

	// Swap through the settings screen, the same path a user takes.
	m.openSettings()
	m.settings.field = settingsFieldFocusKey
	m.cycleSetting(1)
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if chosen, err := m.store.Setting(focusKeySetting); err != nil || chosen != "attach" {
		t.Fatalf("swap did not persist, chosen = %q, err = %v", chosen, err)
	}
	// Swapped: A focuses instead.
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("A did not focus after the swap, mode = %v, err = %q", m.mode, m.errBar.text)
	}
}

// Enter on a live session row focuses it; typed keys land in its pane and
// ctrl+q returns to the list without touching the pane.
func TestFocusModeForwardsKeys(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "focusme", t.TempDir(), "")
	m.selectSessionRow(t, "focusme")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after enter, mode = %v, err = %q", m.mode, m.errBar.text)
	}

	sess := m.rows[m.cursor].sess
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("ping-focus")},
		{Type: tea.KeyEnter},
	} {
		updated, _ := m.handleKey(msg)
		*m = *updated.(*Model)
	}
	if m.errBar.text != "" {
		t.Fatalf("forwarding set err: %q", m.errBar.text)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if strings.Contains(pane, "ping-focus") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("typed text never reached pane: %q", pane)
		}
		time.Sleep(30 * time.Millisecond)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("ctrl+q left mode = %v", m.mode)
	}
}

// A focused session that disappears drops the UI back to the list.
func TestFocusModeExitsWhenSessionDies(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "doomed", t.TempDir(), "")
	m.selectSessionRow(t, "doomed")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after enter, mode = %v", m.mode)
	}

	sess := m.rows[m.cursor].sess
	if err := m.store.Delete(sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	m.tmux.Kill(sess.ID)
	m.applyCmd(t, m.refreshCmd())
	if m.mode != modeList {
		t.Fatalf("after session death, mode = %v", m.mode)
	}
}

// Leaving focus must hand the mouse back to the terminal, or native
// selection stays broken everywhere in the app after one focus session.
func TestFocusExitReleasesMouse(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "mouseback", t.TempDir(), "")
	m.selectSessionRow(t, "mouseback")

	updated, enterCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if enterCmd == nil {
		t.Fatal("entering focus issued no mouse command")
	}
	// Entering focus batches the mouse switch with the caret timer; the
	// message types are unexported, so compare against what the public
	// command produces.
	if !batchContains(enterCmd(), tea.EnableMouseCellMotion()) {
		t.Fatalf("entering focus never enabled mouse reporting: %T", enterCmd())
	}

	updated, exitCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	*m = *updated.(*Model)
	if exitCmd == nil {
		t.Fatal("leaving focus issued no mouse command")
	}
	if m.sel.active {
		t.Fatal("selection survived leaving focus")
	}
}

// The rail marks which session is focused, so the mode is readable from
// the list as well as from the pane.
func TestRailShowsFocusBadge(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "badged", t.TempDir(), "")
	m.selectSessionRow(t, "badged")

	if strings.Contains(ansi.Strip(m.View()), "FOCUS") {
		t.Fatal("FOCUS badge shown before focusing")
	}
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if !strings.Contains(ansi.Strip(m.View()), "FOCUS") {
		t.Fatal("focused rail row carries no FOCUS badge")
	}
}

// batchContains reports whether a command's message is want, or a batch
// carrying a command that produces want.
func batchContains(msg tea.Msg, want tea.Msg) bool {
	if msg == want {
		return true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return false
	}
	for _, cmd := range batch {
		if cmd == nil {
			continue
		}
		if batchContains(cmd(), want) {
			return true
		}
	}
	return false
}

// A paste while focused goes through the tmux paste path as one block, so
// the agent's composer receives the newlines instead of Enter presses that
// submit the prompt mid-paste.
func TestFocusPasteKeepsPromptInComposer(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "paster", t.TempDir(), "")
	m.selectSessionRow(t, "paster")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("enter did not focus, mode = %v", m.mode)
	}

	var pastedID, pastedText string
	calls := 0
	restore := pasteFocused
	pasteFocused = func(d *tmux.Driver, id, text string) error {
		calls++
		pastedID, pastedText = id, text
		return nil
	}
	t.Cleanup(func() { pasteFocused = restore })

	text := "line one\nline two\n"
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
	*m = *updated.(*Model)

	if calls != 1 {
		t.Fatalf("paste path called %d times, want 1 (err=%q)", calls, m.errBar.text)
	}
	if pastedText != text {
		t.Fatalf("pasted text = %q, want %q", pastedText, text)
	}
	if wantID := m.sessionRows()[0].ID; pastedID != wantID {
		t.Fatalf("pasted into %q, want %q", pastedID, wantID)
	}
}

// Mouse reporting is off by default and only enabled during resize mode.
// After a tmux attach/detach, no mouse re-arming is needed because mouse
// is off: the terminal handles native text selection directly.
// This test verifies the handler returns no mouse-enable command.
func TestDetachNoMouseReArm(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { m.tmux.ClearReviewRequest() })

	_, cmd := m.Update(attachDoneMsg{})
	if cmd != nil {
		t.Fatalf("detach should not re-arm mouse, got %T", cmd)
	}
}

// Ctrl+\ mirrors ctrl+q: it leaves focus without touching the pane.
func TestFocusModeCtrlBackslashUnfocuses(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "focusme", t.TempDir(), "")
	m.selectSessionRow(t, "focusme")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after enter, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlBackslash})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("ctrl+\\ left mode = %v", m.mode)
	}
}
