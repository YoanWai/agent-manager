package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestSessionLayoutDefaultsToSplit(t *testing.T) {
	m := buildModel(t)
	if m.fullLayout {
		t.Fatal("split should be the default sessions layout")
	}
	if err := m.store.SetSetting(sessionLayoutSetting, "full"); err != nil {
		t.Fatal(err)
	}
	if !storedFullLayout(m.store) {
		t.Fatal("stored full choice should turn the full layout on")
	}
}

func TestZTogglesSessionLayout(t *testing.T) {
	m := buildModel(t)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	if !m.fullLayout {
		t.Fatal("z should turn the full layout on")
	}
	if chosen, err := m.store.Setting(sessionLayoutSetting); err != nil || chosen != "full" {
		t.Fatalf("want stored full, got %q err %v", chosen, err)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	if m.fullLayout {
		t.Fatal("a second z should return to the split layout")
	}
	if chosen, err := m.store.Setting(sessionLayoutSetting); err != nil || chosen != "split" {
		t.Fatalf("want stored split, got %q err %v", chosen, err)
	}
}

func TestSettingsTogglesSessionLayout(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if m.settings.fullLayout {
		t.Fatal("settings should open on split by default")
	}
	for m.settings.field != settingsFieldSessionLayout {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	if chosen, err := m.store.Setting(sessionLayoutSetting); err != nil || chosen != "full" {
		t.Fatalf("want stored full, got %q err %v", chosen, err)
	}
	if !m.fullLayout {
		t.Fatal("the model should mirror the saved full choice")
	}
}

// The full screen frame is the rail alone: no preview column, so the
// captured pane and the detail head stay with the split layout.
func TestFullLayoutFrameHasNoPreviewColumn(t *testing.T) {
	m := shotModel()
	split := ansi.Strip(m.View())
	if !strings.Contains(split, "token bucket limiter") {
		t.Fatalf("split frame lost its preview:\n%s", split)
	}
	m.fullLayout = true
	full := ansi.Strip(m.View())
	if strings.Contains(full, "token bucket limiter") {
		t.Fatalf("full screen frame still paints the preview:\n%s", full)
	}
	if !strings.Contains(full, "add-rate-limiting") {
		t.Fatalf("full screen frame lost the session tree:\n%s", full)
	}
	rows := strings.Split(m.View(), "\n")
	if len(rows) != m.height {
		t.Fatalf("full screen frame is %d rows, terminal is %d", len(rows), m.height)
	}
	for _, row := range rows {
		if got := ansi.StringWidth(row); got > m.width {
			t.Fatalf("full screen frame row is %d wide, terminal is %d", got, m.width)
		}
	}
}

// Meters and messages leave the rail foot in full screen for one condensed
// line: the machine readings inline, and the messages count when any exist.
func TestFullLayoutFootLine(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	foot := ansi.Strip(strings.Join(m.railFootLines(m.width-1), "\n"))
	for _, want := range []string{"cpu 22%", "mem 75%", "net"} {
		if !strings.Contains(foot, want) {
			t.Errorf("condensed foot line misses %q:\n%s", want, foot)
		}
	}
	if strings.Contains(foot, "messages") {
		t.Fatalf("no notices should mean no messages count:\n%s", foot)
	}
	if lines := m.railFootLines(m.width - 1); len(lines) != 1 {
		t.Fatalf("full screen foot should be one line, got %d", len(lines))
	}
}

func TestFullLayoutFootLineCountsMessages(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 120, 34
	m.fullLayout = true
	foot := ansi.Strip(strings.Join(m.railFootLines(m.width-1), "\n"))
	if !strings.Contains(foot, "messages") {
		t.Fatalf("active notices should show a messages count:\n%s", foot)
	}
	full := ansi.Strip(m.View())
	if !strings.Contains(full, "messages") {
		t.Fatalf("full screen frame lost the messages count:\n%s", full)
	}
}

// The full screen footer keeps the split's tiers and adds the key that
// returns to the split.
func TestFullLayoutFooterNamesZ(t *testing.T) {
	m := shotModel()
	split := ansi.Strip(m.viewFooter())
	if strings.Contains(split, "split view") {
		t.Fatalf("split footer should not offer the split it is in:\n%s", split)
	}
	m.fullLayout = true
	full := ansi.Strip(m.viewFooter())
	if !strings.Contains(full, "z split view") {
		t.Fatalf("full screen footer misses z:\n%s", full)
	}
}

// A compact session row is one line wearing the reply inline; the
// comfortable density unfolds it to three — name, the last prompt, the
// last reply — and a group is one line at either density, in either
// layout.
func TestRowHeightsFollowDensity(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	m.paneLines = map[string]string{"add-rate-limiting": "Running tests… (14s · esc to interrupt)"}
	row := m.rows[4]
	row.sess.LastPrompt = "add a token bucket limiter to the public api"
	if m.comfortableRows {
		t.Fatal("this test starts at the compact density")
	}
	if got := m.entryHeight(row); got != 1 {
		t.Fatalf("compact session entry height = %d, want 1", got)
	}
	if got := m.entryHeight(m.rows[2]); got != 1 {
		t.Fatalf("group entry height = %d, want 1", got)
	}
	lines := splitLines(m.renderTreeRow(row, false, m.width-1, 4, panelHex()))
	if len(lines) != 1 {
		t.Fatalf("compact row painted %d lines, want 1", len(lines))
	}
	top := ansi.Strip(lines[0])
	for _, want := range []string{"add-rate-limiting", "Running tests", "working", "claude"} {
		if !strings.Contains(top, want) {
			t.Errorf("compact row misses %q:\n%s", want, top)
		}
	}

	m.comfortableRows = true
	if got := m.entryHeight(m.rows[2]); got != 1 {
		t.Fatalf("comfortable group entry height = %d, want 1", got)
	}
	if got := m.entryHeight(row); got != 3 {
		t.Fatalf("comfortable session entry height = %d, want 3", got)
	}
	lines = splitLines(m.renderTreeRow(row, false, m.width-1, 4, panelHex()))
	if len(lines) != 3 {
		t.Fatalf("comfortable row painted %d lines, want 3", len(lines))
	}
	top = ansi.Strip(lines[0])
	for _, want := range []string{"add-rate-limiting", "working", "claude"} {
		if !strings.Contains(top, want) {
			t.Errorf("comfortable row line 1 misses %q:\n%s", want, top)
		}
	}
	if prompt := ansi.Strip(lines[1]); !strings.Contains(prompt, "❯ add a token bucket limiter") {
		t.Fatalf("line 2 should carry the last prompt behind ❯:\n%s", prompt)
	}
	if reply := ansi.Strip(lines[2]); !strings.Contains(reply, "↳ Running tests") {
		t.Fatalf("line 3 should carry the reply behind ↳:\n%s", reply)
	}

	// The same rhythm holds in the split layout.
	m.fullLayout = false
	if got := m.entryHeight(row); got != 3 {
		t.Fatalf("split comfortable session entry height = %d, want 3", got)
	}
	m.comfortableRows = false
	if got := m.entryHeight(row); got != 1 {
		t.Fatalf("split compact session entry height = %d, want 1", got)
	}
	narrow := splitLines(m.renderTreeRow(row, false, 60, 4, panelHex()))
	if len(narrow) != 1 {
		t.Fatalf("split compact row painted %d lines, want 1", len(narrow))
	}
}

func TestRowWaitingReplyWearsTheStateColor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := shotModel()
	m.fullLayout = true
	m.comfortableRows = true
	question := "Allow edits to router.go?"
	m.paneLines = map[string]string{"db-migrations": question}
	lines := splitLines(m.renderTreeRow(m.rows[0], false, m.width-1, 0, panelHex()))
	if len(lines) != 3 {
		t.Fatalf("waiting row painted %d lines, want 3", len(lines))
	}
	tinted := strings.TrimSuffix(
		lipgloss.NewStyle().Foreground(statusColor(status.Waiting)).Render(question), "\x1b[0m")
	if !strings.Contains(lines[2], tinted) {
		t.Fatalf("waiting question should wear the waiting color:\n%q", lines[2])
	}
}

func TestRowQuotesEveryStateAndDashesWhenSilent(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	m.comfortableRows = true
	m.paneLines = map[string]string{"notes": "All quiet, nothing queued."}
	lines := splitLines(m.renderTreeRow(m.rows[1], false, m.width-1, 1, panelHex()))
	if reply := strings.TrimSpace(ansi.Strip(lines[2])); reply != "↳ All quiet, nothing queued." {
		t.Fatalf("idle reply line = %q, want the last message", reply)
	}
	m.paneLines = nil
	lines = splitLines(m.renderTreeRow(m.rows[1], false, m.width-1, 1, panelHex()))
	if reply := strings.TrimSpace(ansi.Strip(lines[2])); reply != "-" {
		t.Fatalf("silent idle reply line = %q, want a dash", reply)
	}
}

func TestRowLongPromptTruncates(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	m.comfortableRows = true
	width := 80
	row := m.rows[1]
	row.sess.LastPrompt = strings.Repeat("triage the flaky integration suite and report ", 10)
	rendered := m.renderTreeRow(row, false, width, 1, panelHex())
	for _, line := range splitLines(rendered) {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("row line is %d wide, row is %d:\n%s", got, width, ansi.Strip(line))
		}
	}
	lines := splitLines(rendered)
	if prompt := ansi.Strip(lines[1]); !strings.Contains(prompt, "…") {
		t.Fatalf("long prompt should truncate with an ellipsis:\n%s", prompt)
	}
	for _, want := range []string{"idle", "grok"} {
		if !strings.Contains(ansi.Strip(lines[0]), want) {
			t.Errorf("meta should survive, misses %q:\n%s", want, ansi.Strip(lines[0]))
		}
	}
}

func TestQuickSendRecordsLastPrompt(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()
	m.quick.input.SetValue("carry on with the plan")
	if _, _ = m.submitQuick(); m.errBar.text != "" {
		t.Fatalf("send: %q", m.errBar.text)
	}
	got, err := m.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastPrompt != "carry on with the plan" {
		t.Fatalf("last prompt = %q, want the quick send", got.LastPrompt)
	}
}

// The launch notes are the manager's words, not a task: a decorated first
// prompt sheds them, and a note delivered on its own records nothing.
func TestTypedPromptStripsLaunchNotes(t *testing.T) {
	decorated := launch.CoordinationNote + "\n\n" + launch.RenameDirective + "\n\nfix the login flow"
	if got := typedPrompt(decorated); got != "fix the login flow" {
		t.Fatalf("typedPrompt = %q, want the bare task", got)
	}
	if got := typedPrompt(launch.DeferredRenameDirective); got != "" {
		t.Fatalf("a bare directive should record nothing, got %q", got)
	}
	if got := typedPrompt(launch.CoordinationNote); got != "" {
		t.Fatalf("a bare note should record nothing, got %q", got)
	}
	if got := typedPrompt("plain prompt"); got != "plain prompt" {
		t.Fatalf("an undecorated prompt should pass through, got %q", got)
	}
}

func TestLastMeaningfulPaneLineSkipsChrome(t *testing.T) {
	pane := "❯ Add a limiter\n\n\x1b[38;5;240m● Running tests\x1b[0m\n╰────────╯\n   ✶ \n\n"
	if got := lastMeaningfulPaneLine(pane); got != "● Running tests" {
		t.Fatalf("last meaningful line = %q", got)
	}
	if got := lastMeaningfulPaneLine("\n╭──╮\n│  │\n╰──╯\n"); got != "" {
		t.Fatalf("a pane of borders should yield nothing, got %q", got)
	}
}

// In the full screen layout, right opens the selected session as a full
// width focus: the pane grows to the whole terminal body and the frame
// paints it edge to edge, with the list hidden behind it.
func TestFullLayoutRightOpensFullWidthFocus(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "wide-open", t.TempDir(), "")
	m.selectSessionRow(t, "wide-open")
	m.fullLayout = true

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("right did not focus, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	sess := m.rows[m.cursor].sess
	want := [2]int{m.width, m.listBodyHeight()}
	if got := m.pane.geom[sess.ID]; got != want {
		t.Fatalf("focused pane pinned to %v, want %v", got, want)
	}
	panes, err := m.tmux.Panes()
	if err != nil {
		t.Fatalf("panes: %v", err)
	}
	if got := [2]int{panes[sess.ID].Width, panes[sess.ID].Height}; got != want {
		t.Fatalf("tmux pane is %v, want %v", got, want)
	}

	m.preview = "❯ hello from the pane\n"
	frame := ansi.Strip(m.View())
	if !strings.Contains(frame, "focused · ctrl+q back") {
		t.Fatalf("full width focus frame misses the focus rule:\n%s", frame)
	}
	if !m.pane.box.ok || m.pane.box.x != 0 || m.pane.box.width != m.width {
		t.Fatalf("pane box = %+v, want the whole width at column 0", m.pane.box)
	}
	if m.pane.box.y != m.listChromeRows() {
		t.Fatalf("pane box starts at row %d, want right under the focus rule at %d", m.pane.box.y, m.listChromeRows())
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	*m = *updated.(*Model)
	if m.mode != modeList || !m.fullRows() {
		t.Fatalf("ctrl+q should return to the full screen list, mode = %v", m.mode)
	}
}

// Left returns to the full screen list under the same guard the split's
// focus uses: only with the caret at the head of an empty prompt. The
// pane keeps its full size on the way out, since shrinking it would cost
// the agent its scrollback.
func TestFullFocusLeftReturnsAtPromptHead(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "wide-left", t.TempDir(), "")
	m.selectSessionRow(t, "wide-left")
	m.fullLayout = true

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("right did not focus, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	sess := m.rows[m.cursor].sess
	pinned := m.pane.geom[sess.ID]
	m.rows[m.cursor].sess.Tool = "claude-hooked"
	m.pane.forID = sess.ID
	m.pane.cursor = paneCursor{x: 4, y: 0, ok: true}
	m.preview = "❯ hi\n"

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("left inside a typed prompt left focus, mode = %v", m.mode)
	}

	m.pane.cursor = paneCursor{x: 2, y: 0, ok: true}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("left at the prompt head did not return, mode = %v", m.mode)
	}
	if !m.fullRows() {
		t.Fatal("returning should land on the full screen list")
	}
	if got := m.pane.geom[sess.ID]; got != pinned {
		t.Fatalf("returning resized the pane to %v, want %v kept", got, pinned)
	}
}

// While the quick bar is open on a session row, the full screen frame
// lifts a peek above it: where the session lives, then the tail of its
// captured pane. A group target spawns rather than answers, so it keeps
// the bar alone, and closing the bar drops the slice with it.
func TestFullLayoutQuickBarLiftsThePeek(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "peeked", dir, "")
	m.selectSessionRow(t, "peeked")
	m.fullLayout = true
	m.rows[m.cursor].sess.WorktreeBranch = "am/peek"
	m.preview = "❯ run the flaky suite\n● Running the flaky suite…\n"

	// Off by default: the bar docks alone until the setting opts in.
	m.openQuickMode()
	frame := ansi.Strip(m.View())
	if strings.Contains(frame, "⑂ am/peek") {
		t.Fatalf("the peek should stay down until opted into:\n%s", frame)
	}
	if !strings.Contains(frame, "type and press enter") {
		t.Fatalf("the bar should dock without the peek:\n%s", frame)
	}
	m.quick.active = false

	m.quickPeek = true
	m.openQuickMode()
	frame = ansi.Strip(m.View())
	if !strings.Contains(frame, filepath.Base(dir)) {
		t.Fatalf("peek misses the session's directory:\n%s", frame)
	}
	if !strings.Contains(frame, "⑂ am/peek") {
		t.Fatalf("peek misses the worktree branch:\n%s", frame)
	}
	if !strings.Contains(frame, "Running the flaky suite") {
		t.Fatalf("peek misses the captured pane tail:\n%s", frame)
	}
	if !strings.Contains(frame, "type and press enter") {
		t.Fatalf("the bar itself should dock under the peek:\n%s", frame)
	}

	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEsc})
	frame = ansi.Strip(m.View())
	if strings.Contains(frame, "Running the flaky suite") {
		t.Fatalf("closing the bar should drop the peek:\n%s", frame)
	}

	m.selectGroupRow(t, "")
	m.openQuickMode()
	frame = ansi.Strip(m.View())
	if strings.Contains(frame, "Running the flaky suite") {
		t.Fatalf("a group target should keep the bar alone:\n%s", frame)
	}
	if !strings.Contains(frame, "type and press enter") {
		t.Fatalf("a group target still docks the bar:\n%s", frame)
	}
}

// A does a real tmux attach from the full screen list, unchanged.
func TestFullLayoutAStillAttaches(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "handover", t.TempDir(), "")
	m.selectSessionRow(t, "handover")
	m.fullLayout = true

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	*m = *updated.(*Model)
	if m.mode == modeFocus {
		t.Fatal("A should attach, not focus")
	}
	if cmd == nil {
		t.Fatalf("attach did not start, err = %q", m.errBar.text)
	}
}

// A working session with nothing quotable yet animates a loader on its
// state line rather than holding a dash, and that loader keeps the
// startup tick alive so the frames actually advance.
func TestFullRowWorkingWithoutPaneLineAnimatesLoader(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	m.comfortableRows = true
	m.paneLines = nil
	row := m.rows[4]
	lines := splitLines(m.renderTreeRow(row, false, m.width-1, 4, panelHex()))
	if len(lines) != 3 {
		t.Fatalf("comfortable row painted %d lines, want 3", len(lines))
	}
	frame := startupFrames[m.startupPhase%len(startupFrames)]
	state := ansi.Strip(lines[2])
	if !strings.Contains(state, frame+" working") {
		t.Fatalf("working row without a pane line should animate a loader, got %q", state)
	}
	if !m.needsLoaderTick() {
		t.Fatal("a loader row should keep the startup tick alive")
	}
	m.paneLines = map[string]string{"add-rate-limiting": "Running tests…", "ui-polish": "Compiling…"}
	if m.hasWorkingLoaderRow() {
		t.Fatal("a quotable pane line should retire the loader")
	}
}

// The full screen layout pins sessions to the whole terminal body; the
// split pins them to the preview panel's box.
func TestPaneTargetSizeFollowsLayout(t *testing.T) {
	m := shotModel()
	splitW, splitH := m.paneTargetSize()
	if wantW, wantH := m.previewPaneWidth(), m.previewPaneHeight(); splitW != wantW || splitH != wantH {
		t.Fatalf("split target = %dx%d, want the preview box %dx%d", splitW, splitH, wantW, wantH)
	}
	m.fullLayout = true
	fullW, fullH := m.paneTargetSize()
	if fullW != m.width {
		t.Fatalf("full layout target width = %d, want the terminal's %d", fullW, m.width)
	}
	if fullH != m.listBodyHeight() {
		t.Fatalf("full layout target height = %d, want the body's %d", fullH, m.listBodyHeight())
	}
	if fullW <= splitW {
		t.Fatalf("full layout width %d should exceed the split's %d", fullW, splitW)
	}
}

// Toggling back to the split re-pins the width but never shrinks a pane
// that grew taller in the full layout: the painted view crops instead,
// because a height shrink clears a Codex scrollback (#369).
func TestSplitRepinKeepsTallerPaneHeight(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "tall", t.TempDir(), "")
	id := m.sessionRows()[0].ID

	m.fullLayout = true
	m.sessionsSized = false
	m.pane.geom = nil
	m.applyCmd(t, m.refreshCmd())
	fullW, fullH := m.paneTargetSize()
	if w, h := windowSize(t, id); w != fullW || h < fullH {
		t.Fatalf("full layout pinned session to %dx%d, want %dx%d", w, h, fullW, fullH)
	}

	m.fullLayout = false
	m.resizeSessions()
	splitW, _ := m.paneTargetSize()
	if w, h := windowSize(t, id); w != splitW || h < fullH {
		t.Fatalf("split re-pin sized session to %dx%d, want %dx%d with the height kept", w, h, splitW, fullH)
	}
}

// The compact cell quotes the agent's last message whenever there is
// one, whatever the state; only a session that has said nothing yet
// names the task it was given.
func TestCompactCellIsStatePicked(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	m.paneLines = map[string]string{
		"notes":         "All quiet, nothing queued.",
		"db-migrations": "Allow edits to router.go?",
	}
	idle := m.rows[1]
	idle.sess.LastPrompt = "verify the staging deploy is healthy"
	line := ansi.Strip(m.renderTreeRow(idle, false, m.width-1, 1, panelHex()))
	if !strings.Contains(line, "↳ All quiet, nothing queued.") {
		t.Fatalf("an idle session that has spoken should quote its reply:\n%s", line)
	}
	if strings.Contains(line, "verify the staging deploy") {
		t.Fatalf("the reply should win over the task:\n%s", line)
	}

	m.paneLines = map[string]string{"db-migrations": "Allow edits to router.go?"}
	line = ansi.Strip(m.renderTreeRow(idle, false, m.width-1, 1, panelHex()))
	if !strings.Contains(line, "❯ verify the staging deploy is healthy") {
		t.Fatalf("a silent idle session should name its task:\n%s", line)
	}

	line = ansi.Strip(m.renderTreeRow(m.rows[0], false, m.width-1, 0, panelHex()))
	if !strings.Contains(line, "↳ Allow edits to router.go?") {
		t.Fatalf("waiting compact row should quote its question:\n%s", line)
	}
}

// A status frozen before the archive (an older build's "working") must
// not read as alive from inside the archive.
func TestArchivedRowReadsDead(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	row := m.rows[4]
	row.sess.Archived = true
	line := ansi.Strip(m.renderTreeRow(row, false, m.width-1, 4, panelHex()))
	if !strings.Contains(line, statusLabel(status.Dead)) {
		t.Fatalf("archived row should read dead:\n%s", line)
	}
	if strings.Contains(line, statusLabel(status.Working)) {
		t.Fatalf("archived row still claims its frozen state:\n%s", line)
	}
}

// Full screen focus drops the footer padding the other transients keep:
// the pane is pinned on the way in anyway, so the freed rows join the
// body instead of holding blank space under the legend.
func TestFullFocusFooterIsOneRow(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	m.mode = modeFocus
	if got := lipgloss.Height(m.viewFooter()); got != 1 {
		t.Fatalf("full focus footer = %d rows, want 1", got)
	}
	m.fullLayout = false
	listed := lipgloss.Height(m.listFooter())
	if got := lipgloss.Height(m.viewFooter()); got != listed {
		t.Fatalf("split focus footer = %d rows, want the padded %d", got, listed)
	}
}
