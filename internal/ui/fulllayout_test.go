package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
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

// The layout is a settings choice, so neither footer offers a key for it.
func TestFullLayoutFooterOffersNoLayoutKey(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	full := ansi.Strip(m.viewFooter())
	if strings.Contains(full, "split view") {
		t.Fatalf("full screen footer should leave the layout to settings:\n%s", full)
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
	if rule := ansi.Strip(m.focusFactsLine(m.width)); !strings.Contains(frame, rule) {
		t.Fatalf("full width focus frame misses the focus rule %q:\n%s", rule, frame)
	}
	if !strings.Contains(frame, sess.Name) {
		t.Fatalf("the focus rule should name the session %q:\n%s", sess.Name, frame)
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

func TestShellRowSkipsThePromptLine(t *testing.T) {
	m := shotModel()
	m.cfg = config.Config{Tools: map[string]config.Tool{"terminal": {Shell: true}, "claude": {}}}
	m.comfortableRows = true
	shell := m.rows[4]
	shell.sess.Tool = "terminal"
	shell.sess.Status = status.Idle
	shell.sess.LastPrompt = "this never rode a shell row"
	m.paneLines = map[string]string{shell.sess.ID: "~/dev/api $ go test ./..."}

	if got := m.entryHeight(shell); got != 2 {
		t.Fatalf("comfortable shell entry height = %d, want 2", got)
	}
	lines := splitLines(m.renderTreeRow(shell, false, m.width-1, 4, panelHex()))
	if len(lines) != 2 {
		t.Fatalf("comfortable shell row painted %d lines, want 2:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if reply := ansi.Strip(lines[1]); !strings.Contains(reply, "↳ ~/dev/api $ go test") {
		t.Fatalf("line 2 should carry the shell's own last line behind ↳:\n%s", reply)
	}
	if body := ansi.Strip(strings.Join(lines, "\n")); strings.Contains(body, "this never rode a shell row") {
		t.Fatalf("a shell row has no prompt line to paint:\n%s", body)
	}

	// An agent beside it keeps all three.
	agent := m.rows[4]
	if got := m.entryHeight(agent); got != 3 {
		t.Fatalf("comfortable agent entry height = %d, want 3", got)
	}
	if got := len(splitLines(m.renderTreeRow(agent, false, m.width-1, 4, panelHex()))); got != 3 {
		t.Fatalf("comfortable agent row painted %d lines, want 3", got)
	}

	// Compact keeps every session on one row, shell included.
	m.comfortableRows = false
	if got := m.entryHeight(shell); got != 1 {
		t.Fatalf("compact shell entry height = %d, want 1", got)
	}
}

func TestFullLayoutTransientFootersAreOneRow(t *testing.T) {
	m := shotModel()
	m.cfg = config.Config{Tools: map[string]config.Tool{"claude": {}}}
	m.fullLayout = true
	m.quick.active = true
	if got := lipgloss.Height(m.viewFooter()); got != 1 {
		t.Fatalf("full screen quick prompt footer = %d rows, want 1", got)
	}
	body := m.listBodyHeight()

	m.fullLayout = false
	listed := lipgloss.Height(m.listFooter())
	if listed < 2 {
		t.Fatalf("this test needs a list footer taller than a tier, got %d rows", listed)
	}
	if got := lipgloss.Height(m.viewFooter()); got != listed {
		t.Fatalf("split quick prompt footer = %d rows, want the padded %d", got, listed)
	}

	// The rows the tier gives up go to the list body, which is what the
	// full screen pane is pinned to.
	m.fullLayout = true
	m.quick.active = false
	if resting := m.listBodyHeight(); body <= resting {
		t.Fatalf("quick prompt body = %d rows, want more than the resting %d", body, resting)
	}
}

func TestFullFocusRuleNamesTheSession(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	m.mode = modeFocus
	m.queuedMessages = map[string]int{"add-rate-limiting": 2}
	// A wide terminal holds every reading.
	if wide := ansi.Strip(m.focusFactsLine(200)); !strings.Contains(wide, "started ") {
		t.Errorf("a wide focus line should carry every reading:\n%s", wide)
	}
	facts := ansi.Strip(m.focusFactsLine(m.width))
	if strings.Contains(facts, "started ") {
		t.Errorf("the sparest reading should go first as the line narrows:\n%s", facts)
	}
	for _, want := range []string{"add-rate-limiting", "claude", "working", "dev/api", "cpu 4.2%", "ram ", "2 queued"} {
		if !strings.Contains(facts, want) {
			t.Errorf("full screen focus line misses %q:\n%s", want, facts)
		}
	}
	if strings.Contains(facts, "ctrl+q") {
		t.Errorf("the keys belong to the footer, not this line:\n%s", facts)
	}
	// The line is the facts alone; the hairline under it is its own row.
	if strings.Contains(facts, "─") {
		t.Errorf("no rule should run through the facts:\n%s", facts)
	}
	if got := ansi.StringWidth(facts); got != m.width {
		t.Fatalf("facts line is %d wide, terminal is %d", got, m.width)
	}
	if got := len(splitLines(m.focusFactsLine(m.width))); got != 1 {
		t.Fatalf("facts painted %d lines, want 1", got)
	}

	// The frame puts the facts under the band and a rule between them and
	// the pane.
	rows := splitLines(ansi.Strip(m.View()))
	head := m.headerRows()
	isRule := func(row string) bool {
		trimmed := strings.TrimSpace(row)
		return trimmed != "" && strings.Trim(trimmed, "\u2500") == ""
	}
	if got := rows[head]; !isRule(got) {
		t.Fatalf("row %d should be the rule under the band, got:\n%s", head, got)
	}
	if got := rows[head+1]; !strings.Contains(got, "add-rate-limiting") {
		t.Fatalf("row %d should carry the facts, got:\n%s", head+1, got)
	}
	if got := rows[head+2]; !isRule(got) {
		t.Fatalf("row %d should be the rule under the facts, got:\n%s", head+2, got)
	}

	// Too narrow for both sides, the name survives and the readings go.
	narrow := ansi.Strip(m.focusFactsLine(52))
	if !strings.Contains(narrow, "add-rate-limiting") || strings.Contains(narrow, "cpu ") {
		t.Fatalf("a narrow line keeps the name and drops the readings:\n%s", narrow)
	}
	if got := ansi.StringWidth(narrow); got > 52 {
		t.Fatalf("narrow line is %d wide, want at most 52", got)
	}

	// The split's own rule still names the keys: its detail head above the
	// pane already says which session this is.
	if split := ansi.Strip(focusTopRule(m.width)); !strings.Contains(split, "ctrl+q back") {
		t.Fatalf("split focus rule lost its keys:\n%s", split)
	}
}

func TestFocusFactsWriteHomeAsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory to shorten")
	}
	m := shotModel()
	m.fullLayout = true
	m.mode = modeFocus
	m.rows[m.cursor].sess.Cwd = filepath.Join(home, "dev", "api")
	for i := range m.sessions {
		if m.sessions[i].ID == m.rows[m.cursor].sess.ID {
			m.sessions[i].Cwd = m.rows[m.cursor].sess.Cwd
		}
	}
	facts := ansi.Strip(m.focusFactsLine(200))
	if !strings.Contains(facts, "~/dev/api") {
		t.Fatalf("a path under home should read from ~:\n%s", facts)
	}
	if strings.Contains(facts, home) {
		t.Fatalf("the home prefix should not survive:\n%s", facts)
	}
}
