package ui

import (
	"os"
	"path/filepath"
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

func typeHelpSearch(m *Model, query string, finish bool) {
	m.handleHelpKey(runeKey("/"))
	for _, r := range query {
		m.handleHelpKey(runeKey(string(r)))
	}
	if finish {
		m.handleHelpKey(namedKey(tea.KeyEnter))
	}
}

func TestReviewHelpOnlyShowsReviewBindingsAndSetupGuidance(t *testing.T) {
	m := &Model{
		layout: layoutState{
			width:  120,
			height: 30,
		},
		mode: modeDiff,
		services: services{
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
	}
	seedReviewForTest(m, uireview.Target{ID: "review"}, git.ScopeUncommitted, "/repo", diff.Set{}, true)
	m.openHelp()
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{"Review keys", "Tell your agent what to review", "comment on the line"} {
		if !strings.Contains(frame, want) {
			t.Errorf("review help missing %q:\n%s", want, frame)
		}
	}
	for _, unwanted := range []string{"new session", "quick prompt", "messages (M)"} {
		if strings.Contains(frame, unwanted) {
			t.Errorf("review help includes %q:\n%s", unwanted, frame)
		}
	}
}

func TestGlobalHelpShowsAgentManagementGuidance(t *testing.T) {
	frame := ansi.Strip(preparedView(helpModel()))
	if !strings.Contains(frame, "Tell your agent to manage sessions and terminals in Agent Manager") {
		t.Fatalf("global help is missing agent-management guidance:\n%s", frame)
	}
}

func TestHelpFramePaintsInsideTheTerminal(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		for _, height := range []int{14, 24, 40} {
			for _, query := range []string{"", "revive"} {
				m := &Model{
					layout: layoutState{
						width:  width,
						height: height,
					},
					mode: modeHelp,
					help: helpFeature{state: uihelp.New(uihelp.Global)},
					services: services{
						keys:     keybind.DefaultSession(),
						listKeys: keybind.DefaultList(),
					},
				}
				if query != "" {
					typeHelpSearch(m, query, true)
				}
				lines := strings.Split(preparedView(m), "\n")
				if len(lines) != height {
					t.Errorf("%dx%d query %q: %d rows painted", width, height, query, len(lines))
				}
				for i, line := range lines {
					if got := ansi.StringWidth(line); got > width {
						t.Errorf("%dx%d query %q: row %d is %d wide", width, height, query, i, got)
					}
				}
			}
		}
	}
}

func TestHelpBodyShowsMoreMarkersWhenItOverflows(t *testing.T) {
	m := helpModel()
	frame := ansi.Strip(preparedView(m))
	if !strings.Contains(frame, "more below") {
		t.Fatal("an overflowing map should say there is more below")
	}
	m.handleHelpKey(runeKey("G"))
	frame = ansi.Strip(preparedView(m))
	if !strings.Contains(frame, "more above") {
		t.Fatal("a map scrolled to the end should say there is more above")
	}
}

func TestHelpReportsWhenNothingMatches(t *testing.T) {
	m := helpModel()
	typeHelpSearch(m, "zzzz", true)
	if frame := ansi.Strip(preparedView(m)); !strings.Contains(frame, "no key matches that") {
		t.Fatal("a query nothing answers should say so")
	}
}

func TestHelpMatchesBaselineContentAndLayout(t *testing.T) {
	previousProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previousProfile) })
	previousTheme := current
	applyTheme(themes[0])
	t.Cleanup(func() { applyTheme(previousTheme) })

	tests := []struct {
		name         string
		model        *Model
		assertCursor bool
	}{
		{name: "global", model: helpModel()},
		{name: "search_ime", model: helpModel(), assertCursor: true},
		{name: "review", model: helpModel()},
		{name: "narrow_error", model: helpModel()},
	}
	tests[1].model.layout.width, tests[1].model.layout.height = 100, 28
	tests[1].model.focus.runtime.imeCursor = &cursorAnchor{}
	typeHelpSearch(tests[1].model, "中文", false)
	tests[2].model.help.state = uihelp.New(uihelp.Review)
	tests[2].model.help.returnMode = modeDiff
	tests[3].model.layout.width, tests[3].model.layout.height = 60, 14
	tests[3].model.errBar = errBar{text: "refresh failed"}
	typeHelpSearch(tests[3].model, "zzzz", true)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			golden, err := os.ReadFile(filepath.Join("testdata", "help", test.name+".txt"))
			if err != nil {
				t.Fatal(err)
			}
			got := readableHelpFrame(preparedView(test.model))
			want := strings.TrimSuffix(string(golden), "\n")
			if got != want {
				t.Fatalf("help content and layout changed from 560a463:\nwant:\n%s\n\ngot:\n%s", want, got)
			}
			if test.assertCursor {
				if _, _, ok := test.model.focus.runtime.imeCursor.get(); !ok {
					t.Fatal("search frame did not publish its IME cursor")
				}
			}
		})
	}
}

func readableHelpFrame(frame string) string {
	lines := strings.Split(ansi.Strip(frame), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func TestHelpOpensDocs(t *testing.T) {
	m := helpModel()
	var opened string
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })

	frame := preparedView(m)
	if !strings.Contains(frame, hintCap("o", "docs")) {
		t.Fatal("the docs shortcut should wear the footer badge")
	}
	_, cmd := m.handleHelpKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m.applyCmd(t, cmd)
	if opened != docsURL {
		t.Fatalf("o should open the docs, got %q", opened)
	}
	if m.mode != modeHelp {
		t.Fatal("opening the docs should leave the key map up")
	}

	opened = ""
	plain := ansi.Strip(frame)
	lineIndex := -1
	var line string
	for i, row := range strings.Split(plain, "\n") {
		if strings.Contains(row, "o docs") {
			lineIndex = i
			line = row
			break
		}
	}
	if lineIndex < 0 {
		t.Fatal("painted frame has no docs hit")
	}
	x := ansi.StringWidth(line[:strings.Index(line, "o docs")]) + 1
	_, cmd = m.handleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: lineIndex})
	m.applyCmd(t, cmd)
	if opened != docsURL {
		t.Fatalf("clicking the footer should open the docs, got %q", opened)
	}
}

func TestHelpDocsHitIgnoresTheSearchRow(t *testing.T) {
	m := helpModel()
	for _, msg := range []tea.KeyMsg{runeKey("/"), runeKey("o"), namedKey(tea.KeySpace), runeKey("docs"), namedKey(tea.KeyEnter)} {
		m.handleHelpKey(msg)
	}
	frame := preparedView(m)
	plain := strings.Split(ansi.Strip(frame), "\n")
	searchLine := -1
	for i, line := range plain {
		if strings.Contains(line, "search o docs") {
			searchLine = i
		}
	}
	if searchLine < 0 {
		t.Fatal("the search row should show the query")
	}
	if !m.help.docsHit.ok || m.help.docsHit.y0 <= searchLine {
		t.Fatalf("the docs hit should sit on the footer below the search row, hit %d search %d", m.help.docsHit.y0, searchLine)
	}
	if !strings.Contains(plain[m.help.docsHit.y0], "o docs") {
		t.Fatalf("the docs hit missed the footer: %q", plain[m.help.docsHit.y0])
	}

	m.handleHelpKey(runeKey("/"))
	preparedView(m)
	if m.help.docsHit.ok {
		t.Fatal("typing a search should not leave a docs hit")
	}
}
