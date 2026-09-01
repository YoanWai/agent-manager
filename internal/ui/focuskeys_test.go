package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/tmux"
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
		// The editor sits on F3 now, so ctrl+o reaches the agent: Claude
		// Code and Gemini CLI both bind it.
		{"ctrl-o", tea.KeyMsg{Type: tea.KeyCtrlO}, "send-keys -t am_x C-o", true},
		{"tab-not-ctrl-i", tea.KeyMsg{Type: tea.KeyTab}, "send-keys -t am_x Tab", true},
		{"enter-not-ctrl-m", tea.KeyMsg{Type: tea.KeyEnter}, "send-keys -t am_x Enter", true},
		{"shift-tab", tea.KeyMsg{Type: tea.KeyShiftTab}, "send-keys -t am_x BTab", true},
		{"up", tea.KeyMsg{Type: tea.KeyUp}, "send-keys -t am_x Up", true},
		{"left", tea.KeyMsg{Type: tea.KeyLeft}, "send-keys -t am_x Left", true},
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
	if len(gotCells) < len(rawCells) {
		t.Fatalf("rendered shorter than raw: raw %d, rendered %d", len(rawCells), len(gotCells))
	}
	for i := range rawCells {
		if rawCells[i] != gotCells[i] {
			t.Fatalf("column %d background differs: raw %v, rendered %v", i, rawCells[i], gotCells[i])
		}
	}
	for i := len(rawCells); i < len(gotCells); i++ {
		if gotCells[i] {
			t.Fatalf("padding column %d invented a background", i)
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

func TestFocusModeForwardsArrowKeys(t *testing.T) {
	m := buildModel(t)
	createSessionOn(t, m, "focus-arrows", "quietchat", t.TempDir())
	m.selectSessionRow(t, "focus-arrows")
	sess := m.rows[m.cursor].sess
	quitAgent(t, m, sess.ID)
	m.focus = newFocusWatch(m.tmux, func(tea.Msg) {})
	t.Cleanup(m.focus.Close)

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after enter, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !m.focus.serving(sess.ID) {
		if time.Now().After(deadline) {
			t.Fatal("focus control client never became ready")
		}
		time.Sleep(30 * time.Millisecond)
	}

	ready := filepath.Join(t.TempDir(), "ready")
	reader := "touch " + tmux.ShellQuote(ready) + "; od -An -tx1 -N7"
	command := "sh -c " + tmux.ShellQuote(reader)
	if err := m.tmux.SendKeys(sess.ID, command, "Enter"); err != nil {
		t.Fatalf("start key reader: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatalf("check key reader: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("key reader never became ready")
		}
		time.Sleep(30 * time.Millisecond)
	}

	for _, key := range []tea.KeyType{tea.KeyUp, tea.KeyDown, tea.KeyEnter} {
		updated, _ = m.handleKey(tea.KeyMsg{Type: key})
		*m = *updated.(*Model)
	}

	deadline = time.Now().Add(5 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if strings.Contains(strings.Join(strings.Fields(pane), " "), "1b 5b 41 1b 5b 42 0a") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("arrow bytes never reached pane: %q", pane)
		}
		time.Sleep(30 * time.Millisecond)
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

// Ctrl+R while focused opens the review instead of reaching the pane, and
// closing the review lands back in focus rather than the list.
func TestFocusCtrlROpensReviewAndReturns(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "focusrev", dir, "")
	m.selectSessionRow(t, "focusrev")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after enter, mode = %v, err = %q", m.mode, m.errBar.text)
	}

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlR})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.mode != modeDiff || !m.diff.active {
		t.Fatalf("ctrl+r in focus should open review, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	if len(m.diff.set.Files) == 0 {
		t.Fatalf("review opened empty, err = %q", m.diff.errText)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("closing review should return to focus, mode = %v, err = %q", m.mode, m.errBar.text)
	}
}

// F3 opens the focused session's directory the way the list's o does,
// and a windowed editor leaves the focus where it was.
func TestFocusF3OpensEditor(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	dir := t.TempDir()
	createSession(t, m, "focusedit", dir, "")
	m.selectSessionRow(t, "focusedit")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after enter, mode = %v, err = %q", m.mode, m.errBar.text)
	}

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyF3})
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatalf("f3 in focus returned no launch, err = %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)

	if want := []string{"code", resolved(t, dir)}; !slices.Equal(*launched, want) {
		t.Fatalf("launched %v, want %v", *launched, want)
	}
	if m.mode != modeFocus {
		t.Fatalf("a windowed editor should leave the focus alone, mode = %v", m.mode)
	}
	if !strings.Contains(m.errBar.text, "code") {
		t.Fatalf("status line should name the editor, got %q", m.errBar.text)
	}
}

// An editor that took the terminal hands it back without the mouse
// reporting focus mode armed, so the pane's wheel and drag would be dead
// on return.
func TestFocusEditorThatTookTheScreenRearmsMouse(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "screenedit", t.TempDir(), "")
	m.selectSessionRow(t, "screenedit")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)

	updated, cmd := m.Update(editorDoneMsg{tookScreen: true})
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatal("returning from a terminal editor issued no mouse command")
	}
	if !batchContains(cmd(), tea.EnableMouseCellMotion()) {
		t.Fatalf("mouse reporting was not re-armed: %T", cmd())
	}

	// A windowed editor never took the terminal, so it has nothing to undo.
	updated, cmd = m.Update(editorDoneMsg{name: "code", path: "/tmp"})
	*m = *updated.(*Model)
	if cmd != nil {
		t.Fatalf("a windowed editor should leave the terminal alone, got %T", cmd())
	}
}

// Leaving focus must keep mouse reporting: handing it back would let a
// wheel notch scroll the manager out of view from the list.
func TestFocusExitKeepsMouse(t *testing.T) {
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

	// Leaving keeps mouse reporting on: handing the wheel back to the
	// terminal here would let a notch scroll the manager out of view.
	updated, exitCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	*m = *updated.(*Model)
	if exitCmd != nil && batchContains(exitCmd(), tea.DisableMouse()) {
		t.Fatal("leaving focus released mouse reporting to the terminal")
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
	clearRequestOnCleanup(t, m)

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

// caretModel is a focused model whose pane mirror is posed by hand: the
// captured rows, and the caret cell tmux reported over them.
func caretModel(t *testing.T, cursor paneCursor, rows ...string) *Model {
	t.Helper()
	engine, err := status.NewEngine(config.Config{Tools: map[string]config.Tool{
		"claude":    {ActivityCutoff: `(?m)^❯`},
		"gemini":    {ActivityCutoff: `(?m)^\s*[>!*] `},
		"unmarked":  {},
		"wide-mark": {ActivityCutoff: `(?m)^→`},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	m := &Model{engine: engine, mode: modeFocus}
	m.preview = strings.Join(rows, "\n") + "\n"
	m.pane.forID = "s1"
	m.pane.cursor = cursor
	return m
}

// Left is only free to mean "back to the list" where the agent would do
// nothing with it: at the head of its prompt, with the marker alone to
// the caret's left.
func TestCaretAtInputStart(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		cursor paneCursor
		rows   []string
		want   bool
	}{
		// tmux trims a row's trailing blanks, so an empty prompt is the
		// marker alone with the caret out on padding the row lacks.
		{"empty prompt", "claude", paneCursor{x: 2, y: 1, ok: true}, []string{"output", "❯"}, true},
		// Claude pads its marker with a non-breaking space, so the cell
		// between marker and caret is blank without being an ASCII space.
		{"nbsp padded prompt", "claude", paneCursor{x: 2, y: 0, ok: true}, []string{"❯\u00a0"}, true},
		{"nbsp padded with input", "claude", paneCursor{x: 2, y: 0, ok: true}, []string{"❯\u00a0write a test"}, true},
		{"nbsp padded mid-input", "claude", paneCursor{x: 5, y: 0, ok: true}, []string{"❯\u00a0write a test"}, false},
		{"caret before typed text", "claude", paneCursor{x: 2, y: 0, ok: true}, []string{"❯ hi"}, true},
		{"caret after typed text", "claude", paneCursor{x: 4, y: 0, ok: true}, []string{"❯ hi"}, false},
		{"caret one in", "claude", paneCursor{x: 3, y: 0, ok: true}, []string{"❯ hi"}, false},
		{"caret on the marker", "claude", paneCursor{x: 0, y: 0, ok: true}, []string{"❯ hi"}, false},
		// A wrapped prompt's continuation rows carry no marker: Left there
		// reaches the end of the row above and belongs to the agent.
		{"wrapped continuation", "claude", paneCursor{x: 2, y: 1, ok: true}, []string{"❯ a long", "  wrapped"}, false},
		{"plain output row", "claude", paneCursor{x: 2, y: 0, ok: true}, []string{"some output"}, false},
		// The marker has to open the row: one quoted mid-line is not a prompt.
		{"quoted marker", "claude", paneCursor{x: 8, y: 0, ok: true}, []string{"we use ❯ here"}, false},
		{"indented marker", "gemini", paneCursor{x: 4, y: 0, ok: true}, []string{"  > "}, true},
		{"indented marker mid-input", "gemini", paneCursor{x: 6, y: 0, ok: true}, []string{"  > hi"}, false},
		{"tool without a marker", "unmarked", paneCursor{x: 2, y: 0, ok: true}, []string{"❯"}, false},
		{"unknown tool", "nosuch", paneCursor{x: 2, y: 0, ok: true}, []string{"❯"}, false},
		{"no cursor report", "claude", paneCursor{x: 2, y: 1}, []string{"output", "❯"}, false},
		{"cursor row past the capture", "claude", paneCursor{x: 2, y: 9, ok: true}, []string{"❯"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := caretModel(t, c.cursor, c.rows...)
			if got := m.caretAtInputStart("s1", c.tool); got != c.want {
				t.Fatalf("caretAtInputStart = %v, want %v", got, c.want)
			}
		})
	}
}

// A double-width marker is measured in cells, not runes, so the caret's
// column lines up with the one tmux reported.
func TestCaretAtInputStartMeasuresMarkerInCells(t *testing.T) {
	m := caretModel(t, paneCursor{x: 2, y: 0, ok: true}, "→ hi")
	if !m.caretAtInputStart("s1", "wide-mark") {
		t.Fatal("caret at the head of a wide-marker prompt was not recognised")
	}
}

// pi composes on a bare row between rules and marks the caret cell with a
// reverse-video space, so the captured row strips to one blank at column
// zero (shapes taken from a live pane). Its zero-width prefix reads that as
// the prompt head; text before the caret, or a wrapped line continuing from
// above, keeps Left with the agent.
func TestCaretOnPisBareComposerRow(t *testing.T) {
	engine, err := status.NewEngine(config.Config{Tools: map[string]config.Tool{
		"pi": {
			ActivityCutoff: `(?ms)\A.*^─{8,}[ \t]*$`,
			InputPrefix:    "^",
		},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	build := func(x int, y int, rows ...string) *Model {
		m := &Model{engine: engine, mode: modeFocus}
		m.preview = strings.Join(rows, "\n") + "\n"
		m.pane.forID = "s1"
		m.pane.cursor = paneCursor{x: x, y: y, ok: true}
		return m
	}

	rest := []string{"output", "────────", " ", "────────", "~/dir", "$0.000"}
	m := build(0, 2, rest...)
	if !m.caretAtInputStart("s1", "pi") {
		t.Fatal("the caret cell pi draws on an empty composer row was not recognised")
	}
	m = build(2, 2, "output", "────────", "zz ", "────────", "~/dir")
	if m.caretAtInputStart("s1", "pi") {
		t.Fatal("a composer holding a draft was read as input start")
	}
	m = build(0, 3, "output", "────────", "a very long draft line that wraps", "", "────────")
	if m.caretAtInputStart("s1", "pi") {
		t.Fatal("the head of a wrapped draft line was read as input start")
	}
}

// The mirror belongs to whichever session pushed it, and a scrolled-back
// pane's rows no longer line up with the live caret: neither can decide.
func TestCaretAtInputStartNeedsCurrentPane(t *testing.T) {
	m := caretModel(t, paneCursor{x: 2, y: 0, ok: true}, "❯")
	if m.caretAtInputStart("other", "claude") {
		t.Fatal("another session's pane mirror decided the caret")
	}
	m.focusScroll = 3
	if m.caretAtInputStart("s1", "claude") {
		t.Fatal("a scrolled-back pane decided the caret")
	}
}

// Left leaves focus at the head of the prompt and reaches the agent
// anywhere else, so a typed prompt keeps its caret movement.
func TestFocusLeftUnfocusesAtPromptHead(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "leftie", t.TempDir(), "")
	m.selectSessionRow(t, "leftie")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after enter, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	sess := m.rows[m.cursor].sess
	m.rows[m.cursor].sess.Tool = "claude-hooked"
	m.pane.forID = sess.ID
	m.pane.cursor = paneCursor{x: 4, y: 0, ok: true}
	m.preview = "❯ hi\n"

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("left inside a typed prompt left focus, mode = %v", m.mode)
	}
	if m.errBar.text != "" {
		t.Fatalf("forwarding left set err: %q", m.errBar.text)
	}

	m.pane.cursor = paneCursor{x: 2, y: 0, ok: true}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("left at the prompt head did not unfocus, mode = %v", m.mode)
	}
}

// A focused terminal reads the same way: Left leaves at the head of a
// bare shell prompt and reaches the shell while a command is being typed.
func TestFocusLeftUnfocusesTerminalAtPromptHead(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "shellie", t.TempDir(), "")
	m.selectSessionRow(t, "shellie")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after enter, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	sess := m.rows[m.cursor].sess
	m.rows[m.cursor].sess.Tool = "terminal"
	m.pane.forID = sess.ID
	m.preview = "$ make test\n"
	m.pane.cursor = paneCursor{x: 11, y: 0, ok: true}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("left inside a typed command left focus, mode = %v", m.mode)
	}
	if m.errBar.text != "" {
		t.Fatalf("forwarding left set err: %q", m.errBar.text)
	}

	// Stock zsh ("yoan@mac ~ %"), bash, and bare markers all read as a
	// prompt head with the caret right behind the marker.
	for _, prompt := range []string{"$ ", "% ", "❯ ", "yoan@mac ~ % "} {
		// Focus is re-entered directly: going through the key path would
		// restart the live watcher, whose pushed capture races the fixture
		// pane set below.
		m.mode = modeFocus
		m.pane.forID = sess.ID
		m.preview = prompt + "\n"
		m.pane.cursor = paneCursor{x: len([]rune(prompt)), y: 0, ok: true}
		updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
		*m = *updated.(*Model)
		if m.mode != modeList {
			t.Fatalf("left at the head of %q did not unfocus, mode = %v", prompt, m.mode)
		}
	}
}

// The beta setting turns the whole pair off: right no longer focuses,
// and left at the prompt head forwards to the agent instead of leaving.
func TestArrowStepSettingDisablesThePair(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "optout", t.TempDir(), "")
	m.selectSessionRow(t, "optout")

	m.openSettings()
	m.settings.field = settingsFieldArrowStep
	m.cycleSetting(1)
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if chosen, err := m.store.Setting(arrowStepSetting); err != nil || chosen != "off" {
		t.Fatalf("toggle did not persist, chosen = %q, err = %v", chosen, err)
	}
	if storedArrowStep(m.store) {
		t.Fatal("storedArrowStep still reads on")
	}

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("right focused with the pair off, mode = %v", m.mode)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("enter should still focus, mode = %v", m.mode)
	}
	sess := m.rows[m.cursor].sess
	m.rows[m.cursor].sess.Tool = "claude-hooked"
	m.pane.forID = sess.ID
	m.pane.cursor = paneCursor{x: 2, y: 0, ok: true}
	m.preview = "❯ hi\n"
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("left left focus with the pair off, mode = %v", m.mode)
	}
}

// Alt+Left is a word jump inside the prompt, so it stays the agent's.
func TestFocusAltLeftStaysWithTheAgent(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "altleft", t.TempDir(), "")
	m.selectSessionRow(t, "altleft")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	sess := m.rows[m.cursor].sess
	m.rows[m.cursor].sess.Tool = "claude-hooked"
	m.pane.forID = sess.ID
	m.pane.cursor = paneCursor{x: 2, y: 0, ok: true}
	m.preview = "❯ hi\n"

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("alt+left left focus, mode = %v", m.mode)
	}
}

// pi composes on a bare blank row between rules. Its declared input_prefix
// is zero-width, so the caret sitting anywhere on that blank row counts as
// the prompt head, while typed text still belongs to the agent.
func TestFocusLeftUnfocusesOnPiBlankComposerRow(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "pileft", t.TempDir(), "")
	m.selectSessionRow(t, "pileft")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after enter, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	sess := m.rows[m.cursor].sess
	m.rows[m.cursor].sess.Tool = "pi-tool"
	m.pane.forID = sess.ID
	m.pane.cursor = paneCursor{x: 2, y: 1, ok: true}
	m.preview = "────────────\nxy\n────────────\n"

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("left inside typed pi input left focus, mode = %v", m.mode)
	}

	m.preview = "────────────\n\n────────────\n"
	m.pane.cursor = paneCursor{x: 0, y: 1, ok: true}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("left on pi's blank composer row did not unfocus, mode = %v", m.mode)
	}
}

// opencode's composer is a block of gutter rows; the caret sits on the
// draft's own text row and tracks every keystroke, parking at the
// text-start column of a blank gutter row only when the composer is empty
// (shapes and caret positions taken from a live opencode 1.18.21 pane).
// The gutter-bar prefix reads that as the prompt head; a multi-line
// draft's blank continuation row is rejected by the drafted row above it.
func TestCaretOnOpencodesGutterComposer(t *testing.T) {
	engine, err := status.NewEngine(config.Config{Tools: map[string]config.Tool{
		"opencode": {
			ActivityCutoff: `(?m)^\s*╹`,
			InputPrefix:    `(?m)^\s*┃`,
		},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	build := func(x int, y int, rows ...string) *Model {
		m := &Model{engine: engine, mode: modeFocus}
		m.preview = strings.Join(rows, "\n") + "\n"
		m.pane.forID = "s1"
		m.pane.cursor = paneCursor{x: x, y: y, ok: true}
		return m
	}

	footer := "  ┃  Build · Ox Alpha Free (Unlimited) OpenCode Zen · max"
	bar := "  ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀"

	// Empty composer at rest: caret at the text-start column of the middle
	// blank gutter row, blank gutter rows around it.
	m := build(5, 2, "", "  ┃", "  ┃", "  ┃", footer, bar)
	if !m.caretAtInputStart("s1", "opencode") {
		t.Fatal("the empty composer's caret row was not recognised")
	}

	// A draft: the caret tracks the text row, so mid-draft Left stays with
	// the agent, and the head of the draft is a no-op for opencode.
	m = build(7, 2, "", "  ┃", "  ┃  xy", "  ┃", footer, bar)
	if m.caretAtInputStart("s1", "opencode") {
		t.Fatal("a caret inside a draft was read as input start")
	}
	m = build(5, 2, "", "  ┃", "  ┃  xy", "  ┃", footer, bar)
	if !m.caretAtInputStart("s1", "opencode") {
		t.Fatal("the head of a draft was not recognised (Left is a no-op there)")
	}

	// A multi-line draft's blank second row: the drafted row above carries
	// the bar with text past it, and Left belongs to the agent (it moves
	// the caret to the end of the first line on a live pane).
	m = build(5, 2, "", "  ┃  first line", "  ┃", "  ┃", footer, bar)
	if m.caretAtInputStart("s1", "opencode") {
		t.Fatal("a multi-line draft's continuation row was read as input start")
	}

	// The fresh home screen: a wide-margin composer whose placeholder text
	// sits past the caret, which rests at the text-start column.
	margin := strings.Repeat(" ", 63)
	m = build(66, 1, margin+"┃", margin+"┃  Ask anything... \"Fix broken tests\"", margin+"┃", footer)
	if !m.caretAtInputStart("s1", "opencode") {
		t.Fatal("the home screen's empty composer was not recognised")
	}
}

// command-code never moves the terminal cursor: it parks at the bottom-left
// corner below its footer for the whole session and paints the composer's
// caret as reverse video inside the pane instead (live capture, cursor at
// 0,54 with the composer six rows up). The composer_placeholder declares
// that shape, so the parked cell reads the composer row above: the
// placeholder on screen proves the composer empty and Left free to leave,
// while a draft replaces the placeholder and keeps Left with the agent.
func TestCaretParkedBelowCommandCodesComposer(t *testing.T) {
	engine, err := status.NewEngine(config.Config{Tools: map[string]config.Tool{
		"command-code": {
			ActivityCutoff:      `(?m)^❯`,
			ComposerPlaceholder: "Ask your question...",
		},
		// A tool without a placeholder declaration never takes the parked
		// path, whatever its pane looks like.
		"claude": {ActivityCutoff: `(?m)^❯`},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	build := func(x int, y int, rows ...string) *Model {
		m := &Model{engine: engine, mode: modeFocus}
		m.preview = strings.Join(rows, "\n") + "\n"
		m.pane.forID = "s1"
		m.pane.cursor = paneCursor{x: x, y: y, ok: true}
		return m
	}
	footer := "  » accept edits on [shift+tab]\n  ? for shortcuts · taste on"
	composer := "────────────\n❯ Ask your question...\n────────────\n" + footer

	// The resting pane: the parked cell sits on a blank row below the
	// footer while the empty composer shows its placeholder.
	m := build(0, 7, "⠶ Working on it.", "", composer, "", "", "")
	if !m.caretAtInputStart("s1", "command-code") {
		t.Fatal("the parked caret over an empty composer was not recognised")
	}

	// A draft replaces the placeholder, so Left belongs to the agent.
	m = build(0, 7, "⠶ Working on it.", "", "────────────\n❯ z\n────────────\n"+footer, "", "", "")
	if m.caretAtInputStart("s1", "command-code") {
		t.Fatal("the parked caret over a draft was read as input start")
	}

	// The placeholder is painted on a pristine prompt only: command-code
	// drops it for good once a prompt has been typed, so a composer
	// cleared afterwards is a bare marker and just as empty. Measured on
	// v1.33.0, where twenty captures of a cleared composer all read "❯".
	for _, cleared := range []string{"❯", "❯ "} {
		m = build(0, 7, "⠶ Working on it.", "", "────────────\n"+cleared+"\n────────────\n"+footer, "", "", "")
		if !m.caretAtInputStart("s1", "command-code") {
			t.Fatalf("the parked caret over a cleared composer %q was not recognised", cleared)
		}
	}

	// A parked cell that is not at the left edge is not the parking spot.
	m = build(4, 7, "⠶ Working on it.", "", composer, "", "", "")
	if m.caretAtInputStart("s1", "command-code") {
		t.Fatal("a caret parked off the left edge was read as input start")
	}

	// Column zero over a painted row is a cursor on content, not the
	// blank corner the park promises, so the scan never starts.
	m = build(0, 6, "⠶ Working on it.", "", composer, "✻ Thought for 2 seconds [ctrl+o to expand]", "", "")
	if m.caretAtInputStart("s1", "command-code") {
		t.Fatal("a column-zero cursor on a footer row was read as the parked caret")
	}

	// A tool that declares no placeholder never takes the parked path, so
	// its own marker rules keep deciding.
	m = build(0, 7, "⠶ Working on it.", "", composer, "", "", "")
	if m.caretAtInputStart("s1", "claude") {
		t.Fatal("a tool without a declared placeholder took the parked path")
	}
}

// The full key path: Left over a parked caret above command-code's empty
// composer leaves focus, and the same press over a draft forwards into the
// pane instead.
func TestFocusLeftUnfocusesOnCommandCodesParkedCaret(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "ccleft", t.TempDir(), "")
	m.selectSessionRow(t, "ccleft")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after enter, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	sess := m.rows[m.cursor].sess
	m.rows[m.cursor].sess.Tool = "command-code"
	m.pane.forID = sess.ID
	m.pane.cursor = paneCursor{x: 0, y: 6, ok: true}
	m.preview = "✻ Thought for 2 seconds [ctrl+o to expand]\n\n────────────\n❯ Ask your question...\n────────────\n  ? for shortcuts\n\n\n\n"

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("left over the parked caret did not unfocus, mode = %v", m.mode)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after re-enter, mode = %v", m.mode)
	}
	m.pane.cursor = paneCursor{x: 0, y: 6, ok: true}
	m.preview = "✻ Thought for 2 seconds [ctrl+o to expand]\n\n────────────\n❯ z\n────────────\n  ? for shortcuts\n\n\n\n"
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("left over a draft left focus, mode = %v", m.mode)
	}

	// A session that has been typed in: the placeholder is gone for good
	// and the composer clears to a bare marker, with the prompts already
	// sent echoed above it on rows carrying that same marker. Pane and
	// caret copied from a live command-code v1.33.0 session, where the
	// caret parks on the last row and the footers sit between.
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("after re-enter, mode = %v", m.mode)
	}
	m.pane.cursor = paneCursor{x: 0, y: 9, ok: true}
	m.preview = "❯ did you forget about peerlist?\n" +
		"⠶ No, it is queued.\n" +
		" ✻ Worked for 19m 16s\n" +
		"────────────\n" +
		"❯\n" +
		"────────────\n" +
		"  » permission bypass on [shift+tab]\n" +
		"  ? for shortcuts · taste on\n" +
		"\n\n"
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("left over a cleared composer did not unfocus, mode = %v", m.mode)
	}
}
