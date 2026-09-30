package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestReviewHelpOnlyShowsReviewBindingsAndSetupGuidance(t *testing.T) {
	m := &Model{
		width:  120,
		height: 30,
		mode:   modeDiff,
		diff:   diffState{active: true},
		services: services{
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
	}
	m.openHelp()
	sections := m.help.visibleSections(m.helpContext())
	if len(sections) != 1 || !strings.HasPrefix(sections[0].title, "review") {
		t.Fatalf("review help sections = %+v", sections)
	}
	frame := ansi.Strip(m.View())
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
	frame := ansi.Strip(helpModel().View())
	if !strings.Contains(frame, "Tell your agent to manage sessions and terminals in Agent Manager") {
		t.Fatalf("global help is missing agent-management guidance:\n%s", frame)
	}
}

func TestHelpFramePaintsInsideTheTerminal(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		for _, height := range []int{14, 24, 40} {
			m := &Model{
				width:  width,
				height: height,
				mode:   modeHelp,
				services: services{
					keys:     keybind.DefaultSession(),
					listKeys: keybind.DefaultList(),
				},
			}
			for _, query := range []string{"", "revive"} {
				m.help.query = query
				lines := strings.Split(m.View(), "\n")
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
	frame := ansi.Strip(m.View())
	if !strings.Contains(frame, "more below") {
		t.Fatal("an overflowing map should say there is more below")
	}
	m.help.scroll = m.help.scrollLimit(m.helpContext())
	frame = ansi.Strip(m.View())
	if !strings.Contains(frame, "more above") {
		t.Fatal("a map scrolled to the end should say there is more above")
	}
}

func TestHelpReportsWhenNothingMatches(t *testing.T) {
	m := helpModel()
	m.help.query = "zzzz"
	if frame := ansi.Strip(m.View()); !strings.Contains(frame, "no key matches that") {
		t.Fatal("a query nothing answers should say so")
	}
}

// The column is measured from the catalog, so a key can never render clipped
// against its own description. This is what catches a long binding added later.
func TestHelpKeyColumnFitsEveryKey(t *testing.T) {
	column := helpKeyColumn(keybind.DefaultSession(), keybind.DefaultList(), true)
	for _, section := range helpSections(keybind.DefaultSession(), keybind.DefaultList(), true) {
		for _, row := range section.rows {
			if w := ansi.StringWidth(row[0]); w >= column {
				t.Errorf("key %q is %d wide, the column is %d", row[0], w, column)
			}
		}
	}
}

// Descriptions have to survive the default card too: a row wider than the
// column leaves it renders with an ellipsis instead of its own words.
func TestHelpDescriptionsFitTheDefaultCard(t *testing.T) {
	room := cardInnerWidth(helpCardWidth(120)) - helpKeyColumn(keybind.DefaultSession(), keybind.DefaultList(), true)
	for _, section := range helpSections(keybind.DefaultSession(), keybind.DefaultList(), true) {
		for _, row := range section.rows {
			if w := ansi.StringWidth(row[1]); w > room {
				t.Errorf("section %q: %q is %d wide, only %d is left beside the key column",
					section.title, row[1], w, room)
			}
		}
	}
}

func TestHelpHighlightSurvivesAnAwkwardQuery(t *testing.T) {
	// Folding "İ" lengthens it, which is the case that would slice out of
	// range if the run were measured by the raw query.
	for _, query := range []string{"İ", "ẞ", "", "  ", "the", "THE"} {
		for _, section := range helpSections(keybind.DefaultSession(), keybind.DefaultList(), true) {
			for _, row := range section.rows {
				highlightMatch(row[1], query, 60)
			}
		}
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
	tests[1].model.width, tests[1].model.height = 100, 28
	tests[1].model.help = helpState{scope: helpGlobal, query: "中文", searching: true}
	tests[1].model.focusPane.imeCursor = &cursorAnchor{}
	tests[2].model.help = helpState{scope: helpReview}
	tests[2].model.helpReturnMode = modeDiff
	tests[3].model.width, tests[3].model.height = 60, 14
	tests[3].model.errBar = errBar{text: "refresh failed"}
	tests[3].model.help = helpState{scope: helpGlobal, query: "zzzz"}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			golden, err := os.ReadFile(filepath.Join("testdata", "help", test.name+".txt"))
			if err != nil {
				t.Fatal(err)
			}
			got := readableHelpFrame(test.model.View())
			want := strings.TrimSuffix(string(golden), "\n")
			if got != want {
				t.Fatalf("help content and layout changed from 560a463:\nwant:\n%s\n\ngot:\n%s", want, got)
			}
			if test.assertCursor {
				if _, _, ok := test.model.focusPane.imeCursor.get(); !ok {
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
