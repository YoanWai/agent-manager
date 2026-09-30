package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/keybind"
	uihelp "github.com/YoanWai/agent-manager/internal/ui/help"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestHelpAdapterPreservesQuitCommand(t *testing.T) {
	m := helpModel()
	model, cmd := m.handleHelpKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if model != m || cmd == nil {
		t.Fatalf("ctrl+c returned model %T and nil command = %v", model, cmd == nil)
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c command produced %T, want tea.QuitMsg", msg)
	}
}

func TestHelpAdapterRestoresReviewBeforeRestartingLoader(t *testing.T) {
	m := helpModel()
	m.mode = modeDiff
	seedReviewForTest(m, uireview.Target{ID: "review"}, git.ScopeUncommitted, "/repo", diff.Set{}, false)
	m.openHelp()
	if m.helpReturnMode != modeDiff {
		t.Fatalf("opened review help with return mode %v", m.helpReturnMode)
	}
	if title := m.help.Content(m.helpContext(84)).Title; title != "? Review keys" {
		t.Fatalf("opened review help title = %q", title)
	}

	_, cmd := m.handleHelpKey(namedKey(tea.KeyEsc))
	if m.mode != modeDiff || m.helpReturnMode != modeList {
		t.Fatalf("close left mode %v and return mode %v", m.mode, m.helpReturnMode)
	}
	if cmd == nil || !m.startup.startupAnimating {
		t.Fatal("review mode was not restored before its loader restart")
	}
	msg := cmd()
	if _, ok := msg.(startupTickMsg); !ok {
		t.Fatalf("restart command produced %T, want startupTickMsg", msg)
	}
}

func TestHelpAdapterReopensWithCleanFeatureState(t *testing.T) {
	m := helpModel()
	m.handleHelpKey(runeKey("/"))
	for _, r := range "fork" {
		m.handleHelpKey(runeKey(string(r)))
	}
	if frame := ansi.Strip(m.viewHelp()); !strings.Contains(frame, "search fork") {
		t.Fatalf("test setup did not type a search:\n%s", frame)
	}
	m.mode = modeList
	m.openHelp()
	if frame := ansi.Strip(m.viewHelp()); strings.Contains(frame, "search fork") {
		t.Fatalf("reopened help retained stale search:\n%s", frame)
	}
}

func TestHelpAdapterRecomputesRowsWithoutMutatingScroll(t *testing.T) {
	m := helpModel()
	m.height = 18
	m.handleHelpKey(runeKey("G"))
	clean := m.helpLayout()
	if clean.content.Offset == 0 {
		t.Fatal("test setup did not scroll the catalog")
	}

	m.handleHelpKey(runeKey("/"))
	searching := m.helpLayout()
	if len(searching.content.Head) != 2 || searching.rows >= clean.rows {
		t.Fatalf("searching head/rows = %d/%d, clean rows = %d", len(searching.content.Head), searching.rows, clean.rows)
	}
	before := searching.content.Offset
	first := m.viewHelp()
	second := m.viewHelp()
	if got := m.helpLayout().content.Offset; got != before {
		t.Fatalf("render mutated scroll from %d to %d", before, got)
	}
	if first != second {
		t.Fatal("same help state rendered different frames")
	}

	m.handleHelpKey(namedKey(tea.KeyEnter))
	finished := m.helpLayout()
	if len(finished.content.Head) != 0 || finished.rows != clean.rows || finished.content.Offset != before {
		t.Fatalf("finished empty search = head %d rows %d offset %d; want 0/%d/%d",
			len(finished.content.Head), finished.rows, finished.content.Offset, clean.rows, before)
	}

	m.handleHelpKey(runeKey("/"))
	m.handleHelpKey(namedKey(tea.KeyEsc))
	cleared := m.helpLayout()
	if len(cleared.content.Head) != 0 || cleared.content.Offset != 0 {
		t.Fatalf("escape from search left head %d offset %d", len(cleared.content.Head), cleared.content.Offset)
	}
}

func TestHelpAdapterCountsRenderedNoMatchLine(t *testing.T) {
	m := helpModel()
	m.handleHelpKey(runeKey("/"))
	for _, r := range "zzzz" {
		m.handleHelpKey(runeKey(string(r)))
	}
	m.handleHelpKey(namedKey(tea.KeyEnter))
	layout := m.helpLayout()
	if got := len(layout.content.Lines); got != 1 {
		t.Fatalf("no-match content has %d rendered lines, want 1", got)
	}
	m.handleHelpKey(runeKey("G"))
	if offset := m.helpLayout().content.Offset; offset != 0 {
		t.Fatalf("bottom on one rendered line moved to %d", offset)
	}
}

func TestHelpAdapterUsesFreshThemeStyles(t *testing.T) {
	previousProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previousProfile) })
	previous := current
	t.Cleanup(func() { applyTheme(previous) })
	m := helpModel()

	applyTheme(themes[themeIndex("classic")])
	classic := m.help.Content(m.helpContext(84))
	applyTheme(themes[themeIndex("solarized light")])
	light := m.help.Content(m.helpContext(84))
	if strings.Join(classic.Lines, "\n") == strings.Join(light.Lines, "\n") {
		t.Fatal("theme switch left help styles unchanged")
	}
	if ansi.Strip(strings.Join(classic.Lines, "\n")) != ansi.Strip(strings.Join(light.Lines, "\n")) {
		t.Fatal("theme switch changed help content")
	}
}

func TestHelpAdapterScopeUsesOnlyCopiedValues(t *testing.T) {
	m := helpModel()
	m.help = uihelp.New(uihelp.Review)
	content := m.help.Content(m.helpContext(84))
	if content.Title != "? Review keys" || len(content.Lines) == 0 {
		t.Fatalf("review content = %+v", content)
	}
}

func TestHelpModeOwnsKeysBeforeListOverlays(t *testing.T) {
	m := helpModel()
	m.rail.SetSearch(m.rail.Search(), true)
	m.quick.active = true

	m.handleKey(runeKey("/"))
	m.handleKey(runeKey("q"))
	frame := ansi.Strip(preparedView(m))
	if m.mode != modeHelp || !strings.Contains(frame, "search q") {
		t.Fatalf("list overlay intercepted Help input, mode = %v:\n%s", m.mode, frame)
	}
}

func TestHelpContextForwardsCustomSessionAndListTablesThroughView(t *testing.T) {
	m := helpModel()
	m.height = 160
	m.services.listKeys = m.services.listKeys.With(keybind.NewSession, bindingOf(t, "N"))
	m.services.keys = sessionOf(t, []string{"f9"}, []string{"ctrl+g"}, []string{"alt+e"})
	frame := ansi.Strip(preparedView(m))
	hasRow := func(key, description string) bool {
		for _, line := range strings.Split(frame, "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimSpace(strings.TrimPrefix(line, "│"))
			if strings.HasPrefix(line, key+" ") && strings.Contains(line, description) {
				return true
			}
		}
		return false
	}

	for _, want := range [][2]string{
		{"N", "new session"},
		{"f9", "back to the manager"},
		{"ctrl+g", "review the session's diff"},
		{"alt+e", "open its directory in an editor"},
	} {
		if !hasRow(want[0], want[1]) {
			t.Errorf("custom Help frame missing row %q / %q:\n%s", want[0], want[1], frame)
		}
	}
	for _, stale := range [][2]string{
		{"n", "new session"},
		{"ctrl+q", "back to the manager"},
		{"ctrl+r", "review the session's diff"},
		{"f3", "open its directory in an editor"},
	} {
		if hasRow(stale[0], stale[1]) {
			t.Errorf("custom Help frame retained default row %q / %q:\n%s", stale[0], stale[1], frame)
		}
	}
}
