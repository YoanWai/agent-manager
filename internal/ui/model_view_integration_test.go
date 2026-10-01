package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionLayoutDefaultsToSplit(t *testing.T) {
	m := buildModel(t)
	if m.prefs.fullLayout {
		t.Fatal("split should be the default sessions layout")
	}
	if err := m.services.store.SetSetting(sessionLayoutSetting, "full"); err != nil {
		t.Fatal(err)
	}
	if !storedFullLayout(m.services.store) {
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
	if chosen, err := m.services.store.Setting(sessionLayoutSetting); err != nil || chosen != "full" {
		t.Fatalf("want stored full, got %q err %v", chosen, err)
	}
	if !m.prefs.fullLayout {
		t.Fatal("the model should mirror the saved full choice")
	}
}

func TestSettingsToggleChromeIndependently(t *testing.T) {
	for _, tc := range []struct {
		name       string
		field      int
		hideHeader bool
		hideStats  bool
	}{
		{name: "header only", field: settingsFieldHeader, hideHeader: true},
		{name: "stats only", field: settingsFieldStats, hideStats: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			m.openSettings()
			if m.settings.hideHeader || m.settings.hideStats {
				t.Fatal("header and stats should show by default")
			}
			settings := ansi.Strip(m.viewSettings())
			for _, want := range []string{"header", "computer stats"} {
				if !strings.Contains(settings, want) {
					t.Fatalf("settings missing %q:\n%s", want, settings)
				}
			}

			for m.settings.field != tc.field {
				m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
			}
			m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
			settings = ansi.Strip(m.viewSettings())
			headerValue, statsValue := "show", "show"
			if tc.hideHeader {
				headerValue = "hide"
			}
			if tc.hideStats {
				statsValue = "hide"
			}
			for _, row := range []struct {
				label string
				value string
			}{
				{label: "header", value: headerValue},
				{label: "computer stats", value: statsValue},
			} {
				found := false
				for _, line := range strings.Split(settings, "\n") {
					if !strings.Contains(line, row.label) {
						continue
					}
					found = true
					if !strings.Contains(line, row.value) {
						t.Fatalf("%s row missing %q:\n%s", row.label, row.value, line)
					}
				}
				if !found {
					t.Fatalf("settings missing %s row:\n%s", row.label, settings)
				}
			}
			m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})

			if m.prefs.hideHeader != tc.hideHeader || m.prefs.hideStats != tc.hideStats {
				t.Fatalf("model visibility = header %t stats %t, want header %t stats %t",
					m.prefs.hideHeader, m.prefs.hideStats, tc.hideHeader, tc.hideStats)
			}
			if storedHideHeader(m.services.store) != tc.hideHeader || storedHideStats(m.services.store) != tc.hideStats {
				t.Fatalf("reloaded visibility = header %t stats %t, want header %t stats %t",
					storedHideHeader(m.services.store), storedHideStats(m.services.store), tc.hideHeader, tc.hideStats)
			}
		})
	}
}

func TestLayoutsCanHideHeader(t *testing.T) {
	for _, tc := range []struct {
		name string
		full bool
	}{
		{name: "split"},
		{name: "full", full: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := shotModel()
			m.prefs.fullLayout = tc.full
			shownBody := m.listBodyHeight()
			if got := m.headerRows(); got != 1 {
				t.Fatalf("shown header = %d rows, want 1", got)
			}

			m.prefs.hideHeader = true
			if got := m.headerRows(); got != 0 {
				t.Fatalf("hidden header = %d rows, want 0", got)
			}
			if rows := m.viewHeaderRows(); len(rows) != 0 {
				t.Fatalf("hidden header still paints %d rows", len(rows))
			}
			if got := m.listBodyHeight(); got != shownBody+1 {
				t.Fatalf("hidden header body = %d rows, want %d", got, shownBody+1)
			}
			if rows := strings.Split(preparedView(m), "\n"); len(rows) != m.height {
				t.Fatalf("headerless frame = %d rows, terminal is %d", len(rows), m.height)
			}
		})
	}
}

// The full screen frame is the rail alone: no preview column, so the
// captured pane and the detail head stay with the split layout.
func TestFullLayoutFrameHasNoPreviewColumn(t *testing.T) {
	m := shotModel()
	split := ansi.Strip(preparedView(m))
	if !strings.Contains(split, "token bucket limiter") {
		t.Fatalf("split frame lost its preview:\n%s", split)
	}
	m.prefs.fullLayout = true
	full := ansi.Strip(preparedView(m))
	if strings.Contains(full, "token bucket limiter") {
		t.Fatalf("full screen frame still paints the preview:\n%s", full)
	}
	if !strings.Contains(full, "add-rate-limiting") {
		t.Fatalf("full screen frame lost the session tree:\n%s", full)
	}
	rows := strings.Split(preparedView(m), "\n")
	if len(rows) != m.height {
		t.Fatalf("full screen frame is %d rows, terminal is %d", len(rows), m.height)
	}
	for _, row := range rows {
		if got := ansi.StringWidth(row); got > m.width {
			t.Fatalf("full screen frame row is %d wide, terminal is %d", got, m.width)
		}
	}
}

// The layout is a settings choice, so neither footer offers a key for it.
func TestFullLayoutFooterOffersNoLayoutKey(t *testing.T) {
	m := shotModel()
	m.prefs.fullLayout = true
	full := ansi.Strip(m.viewFooter())
	if strings.Contains(full, "split view") {
		t.Fatalf("full screen footer should leave the layout to settings:\n%s", full)
	}
}

// In the full screen layout, right opens the selected session as a full
// width focus: the pane grows to the whole terminal body and the frame
// paints it edge to edge, with the list hidden behind it.
func TestFullLayoutRightOpensFullWidthFocus(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "wide-open", t.TempDir(), "")
	m.selectSessionRow(t, "wide-open")
	m.prefs.fullLayout = true

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	m.drainEffects(t)
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("right did not focus, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	sess := railSelectedSession(m)
	want := [2]int{m.width, m.listBodyHeight()}
	if got := m.focusRuntime.lastPaneSizes[sess.ID]; got != want {
		t.Fatalf("focused pane pinned to %v, want %v", got, want)
	}
	panes, err := m.services.tmux.Panes()
	if err != nil {
		t.Fatalf("panes: %v", err)
	}
	if got := [2]int{panes[sess.ID].Width, panes[sess.ID].Height}; got != want {
		t.Fatalf("tmux pane is %v, want %v", got, want)
	}

	m.workspace.preview = "❯ hello from the pane\n"
	frame := ansi.Strip(preparedView(m))
	if rule := ansi.Strip(m.focusFactsLine(m.width)); !strings.Contains(frame, rule) {
		t.Fatalf("full width focus frame misses the focus rule %q:\n%s", rule, frame)
	}
	if !strings.Contains(frame, sess.Name) {
		t.Fatalf("the focus rule should name the session %q:\n%s", sess.Name, frame)
	}
	box := m.focusPane.FrameBox()
	if !box.Valid || box.X != 0 || box.Width != m.width {
		t.Fatalf("pane box = %+v, want the whole width at column 0", box)
	}
	wantPaneY := m.listChromeRows() + m.listBodyHeight() - 1
	if box.Y != wantPaneY {
		t.Fatalf("pane box starts at row %d, want compact content at the bottom row %d", box.Y, wantPaneY)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	m.drainEffects(t)
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
	m.prefs.fullLayout = true

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	m.drainEffects(t)
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("right did not focus, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	sess := railSelectedSession(m)
	pinned := m.focusRuntime.lastPaneSizes[sess.ID]
	setRailSessionTool(m, sess.ID, "claude-hooked")
	setFocusPaneID(m, sess.ID)
	setFocusCursor(m, paneCursor{x: 4, y: 0, ok: true})
	m.workspace.preview = "❯ hi\n"

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	m.drainEffects(t)
	*m = *updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("left inside a typed prompt left focus, mode = %v", m.mode)
	}

	setFocusCursor(m, paneCursor{x: 2, y: 0, ok: true})
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	m.drainEffects(t)
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("left at the prompt head did not return, mode = %v", m.mode)
	}
	if !m.fullRows() {
		t.Fatal("returning should land on the full screen list")
	}
	if got := m.focusRuntime.lastPaneSizes[sess.ID]; got != pinned {
		t.Fatalf("returning resized the pane to %v, want %v kept", got, pinned)
	}
}

// A does a real tmux attach from the full screen list, unchanged.
func TestFullLayoutAStillAttaches(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "handover", t.TempDir(), "")
	m.selectSessionRow(t, "handover")
	m.prefs.fullLayout = true

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	m.drainEffects(t)
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
	m.prefs.fullLayout = true
	m.prefs.comfortableRows = true
	m.workspace.paneLines = nil
	row := sessionRow(t, m, "add-rate-limiting")
	lines := splitLines(m.renderTreeRow(row, false, m.width-1, 4, panelHex()))
	if len(lines) != 3 {
		t.Fatalf("comfortable row painted %d lines, want 3", len(lines))
	}
	frame := startupFrames[m.startup.startupPhase%len(startupFrames)]
	state := ansi.Strip(lines[2])
	if !strings.Contains(state, frame+" working") {
		t.Fatalf("working row without a pane line should animate a loader, got %q", state)
	}
	if !m.needsLoaderTick() {
		t.Fatal("a loader row should keep the startup tick alive")
	}
	m.workspace.paneLines = map[string]string{"add-rate-limiting": "Running tests…", "ui-polish": "Compiling…"}
	m.rebuildRows()
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
	m.prefs.fullLayout = true
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

	m.prefs.fullLayout = true
	m.startup.sessionsSized = false
	m.focusRuntime.lastPaneSizes = nil
	m.applyCmd(t, m.refreshCmd())
	fullW, fullH := m.paneTargetSize()
	if w, h := windowSize(t, id); w != fullW || h < fullH {
		t.Fatalf("full layout pinned session to %dx%d, want %dx%d", w, h, fullW, fullH)
	}

	m.prefs.fullLayout = false
	m.resizeSessions()
	m.drainEffects(t)
	splitW, _ := m.paneTargetSize()
	if w, h := windowSize(t, id); w != splitW || h < fullH {
		t.Fatalf("split re-pin sized session to %dx%d, want %dx%d with the height kept", w, h, splitW, fullH)
	}
}

// Full screen focus drops the footer padding the other transients keep:
// the pane is pinned on the way in anyway, so the freed rows join the
// body instead of holding blank space under the legend.
func TestFullFocusFooterIsOneRow(t *testing.T) {
	m := shotModel()
	m.prefs.fullLayout = true
	m.mode = modeFocus
	if got := lipgloss.Height(m.viewFooter()); got != 1 {
		t.Fatalf("full focus footer = %d rows, want 1", got)
	}
	m.prefs.fullLayout = false
	listed := lipgloss.Height(m.listFooter())
	if got := lipgloss.Height(m.viewFooter()); got != listed {
		t.Fatalf("split focus footer = %d rows, want the padded %d", got, listed)
	}
}

func TestFullLayoutTransientFootersAreOneRow(t *testing.T) {
	m := shotModel()
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"claude": {}}}
	m.prefs.fullLayout = true
	m.quick.active = true
	if got := lipgloss.Height(m.viewFooter()); got != 1 {
		t.Fatalf("full screen quick prompt footer = %d rows, want 1", got)
	}
	body := m.listBodyHeight()

	m.prefs.fullLayout = false
	listed := lipgloss.Height(m.listFooter())
	if listed < 2 {
		t.Fatalf("this test needs a list footer taller than a tier, got %d rows", listed)
	}
	if got := lipgloss.Height(m.viewFooter()); got != listed {
		t.Fatalf("split quick prompt footer = %d rows, want the padded %d", got, listed)
	}

	// The rows the tier gives up go to the list body, which is what the
	// full screen pane is pinned to.
	m.prefs.fullLayout = true
	m.quick.active = false
	if resting := m.listBodyHeight(); body <= resting {
		t.Fatalf("quick prompt body = %d rows, want more than the resting %d", body, resting)
	}
}

func TestFullFocusRuleNamesTheSession(t *testing.T) {
	m := shotModel()
	m.prefs.fullLayout = true
	m.mode = modeFocus
	m.workspace.queuedMessages = map[string]int{"add-rate-limiting": 2}
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

	rows := splitLines(ansi.Strip(preparedView(m)))
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

	narrow := ansi.Strip(m.focusFactsLine(52))
	if !strings.Contains(narrow, "add-rate-limiting") || strings.Contains(narrow, "cpu ") {
		t.Fatalf("a narrow line keeps the name and drops the readings:\n%s", narrow)
	}
	if got := ansi.StringWidth(narrow); got > 52 {
		t.Fatalf("narrow line is %d wide, want at most 52", got)
	}

	// The split's own rule still names the keys: its detail head above the
	// pane already says which session this is.
	if split := ansi.Strip(focusTopRule(m.width, m.services.keys)); !strings.Contains(split, `ctrl+q / ctrl+\ back`) {
		t.Fatalf("split focus rule lost its keys:\n%s", split)
	}
}

func TestFocusFactsWriteHomeAsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory to shorten")
	}
	m := shotModel()
	m.prefs.fullLayout = true
	m.mode = modeFocus
	setRailSessionCwd(m, railSelectedSession(m).ID, filepath.Join(home, "dev", "api"))
	facts := ansi.Strip(m.focusFactsLine(200))
	if !strings.Contains(facts, "~/dev/api") {
		t.Fatalf("a path under home should read from ~:\n%s", facts)
	}
	if strings.Contains(facts, home) {
		t.Fatalf("the home prefix should not survive:\n%s", facts)
	}
}

func TestFullFootShowsBattery(t *testing.T) {
	m := shotModel()
	m.prefs.fullLayout = true
	m.workspace.snap.BatteryOK, m.workspace.snap.BatteryPercent = true, 33
	foot := ansi.Strip(strings.Join(m.fullFootLine(m.width), "\n"))
	if !strings.Contains(foot, "batt 33%") || strings.Contains(foot, "charging") {
		t.Fatalf("foot lacks the battery reading:\n%s", foot)
	}
	m.workspace.snap.BatteryCharging = true
	foot = ansi.Strip(strings.Join(m.fullFootLine(m.width), "\n"))
	if !strings.Contains(foot, "batt 33% charging") {
		t.Fatalf("foot lacks the charging suffix:\n%s", foot)
	}
	m.workspace.snap.BatteryOK = false
	if foot = ansi.Strip(strings.Join(m.fullFootLine(m.width), "\n")); strings.Contains(foot, "batt") {
		t.Fatalf("foot shows a battery the machine lacks:\n%s", foot)
	}
}
