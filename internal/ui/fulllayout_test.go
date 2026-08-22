package ui

import (
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

// A full screen session row is two lines at any density: the last prompt
// beside the name with the meta still right aligned, and the state-picked
// line under it. Groups have no state line, so they keep the density's
// height.
func TestFullRowsAreTwoLinesAtCompactDensity(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	m.paneLines = map[string]string{"add-rate-limiting": "Running tests… (14s · esc to interrupt)"}
	row := m.rows[4]
	row.sess.LastPrompt = "add a token bucket limiter to the public api"
	if m.comfortableRows {
		t.Fatal("this test exercises the compact density")
	}
	if got := m.entryHeight(row); got != 2 {
		t.Fatalf("full screen session entry height = %d, want 2", got)
	}
	if got := m.entryHeight(m.rows[2]); got != 1 {
		t.Fatalf("full screen group entry height = %d, want 1", got)
	}
	lines := splitLines(m.renderTreeRow(row, false, m.width-1, 4, panelHex()))
	if len(lines) != 2 {
		t.Fatalf("full screen row painted %d lines, want 2", len(lines))
	}
	top := ansi.Strip(lines[0])
	for _, want := range []string{"add-rate-limiting", "token bucket limiter", "working", "claude"} {
		if !strings.Contains(top, want) {
			t.Errorf("row line 1 misses %q:\n%s", want, top)
		}
	}
	if second := ansi.Strip(lines[1]); !strings.Contains(second, "Running tests") {
		t.Fatalf("working row should quote the last pane line:\n%s", second)
	}
}

func TestFullRowWaitingLineWearsTheStateColor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := shotModel()
	m.fullLayout = true
	question := "Allow edits to router.go?"
	m.paneLines = map[string]string{"db-migrations": question}
	lines := splitLines(m.renderTreeRow(m.rows[0], false, m.width-1, 0, panelHex()))
	if len(lines) != 2 {
		t.Fatalf("waiting row painted %d lines, want 2", len(lines))
	}
	tinted := strings.TrimSuffix(
		lipgloss.NewStyle().Foreground(statusColor(status.Waiting)).Render(question), "\x1b[0m")
	if !strings.Contains(lines[1], tinted) {
		t.Fatalf("waiting question should wear the waiting color:\n%q", lines[1])
	}
}

func TestFullRowRestingStatesHoldADash(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	m.paneLines = map[string]string{"notes": "❯ some resting prompt line"}
	lines := splitLines(m.renderTreeRow(m.rows[1], false, m.width-1, 1, panelHex()))
	if second := strings.TrimSpace(ansi.Strip(lines[1])); second != "-" {
		t.Fatalf("idle row second line = %q, want a dash", second)
	}
}

func TestFullRowLongPromptTruncates(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	width := 80
	row := m.rows[1]
	row.sess.LastPrompt = strings.Repeat("triage the flaky integration suite and report ", 10)
	rendered := m.renderTreeRow(row, false, width, 1, panelHex())
	for _, line := range splitLines(rendered) {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("row line is %d wide, row is %d:\n%s", got, width, ansi.Strip(line))
		}
	}
	top := ansi.Strip(splitLines(rendered)[0])
	if !strings.Contains(top, "…") {
		t.Fatalf("long prompt should truncate with an ellipsis:\n%s", top)
	}
	for _, want := range []string{"idle", "grok"} {
		if !strings.Contains(top, want) {
			t.Errorf("meta should survive a long prompt, misses %q:\n%s", want, top)
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
