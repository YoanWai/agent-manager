package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func namedKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

// helpKeyTokens is every key the catalog spells out, split on the " / "
// that separates alternatives inside one row.
func helpKeyTokens() map[string]bool {
	tokens := map[string]bool{}
	for _, section := range helpSections(keybind.DefaultSession(), keybind.DefaultList(), true) {
		for _, row := range section.rows {
			for _, token := range strings.Split(row[0], " / ") {
				if token = strings.TrimSpace(token); token != "" {
					tokens[token] = true
				}
			}
		}
	}
	return tokens
}

// Every action of the list table is in the key map under the keys it
// binds today, so a key moved in settings shows up moved in ? too.
func TestHelpCatalogDocumentsEveryListAction(t *testing.T) {
	documented := helpKeyTokens()
	for _, action := range keybind.DefaultList().Actions() {
		for _, key := range keybind.DefaultList().Binding(action.Name).Keys() {
			if !documented[key.Glyph()] {
				t.Errorf("list action %s: key %q is not in the ? key map", action.Name, key.Glyph())
			}
		}
	}
	if !documented["ctrl+c"] {
		t.Error("ctrl+c quits from every mode and belongs in the key map")
	}
}

// The catalog follows the list table: a key moved in settings is named
// where it moved to, and an action turned off loses its row.
func TestHelpListRowsFollowTheKeyTable(t *testing.T) {
	list := keybind.DefaultList().
		With(keybind.NewSession, bindingOf(t, "N")).
		With(keybind.Kill, bindingOf(t)).
		With(keybind.Quit, bindingOf(t))
	rows := map[string]string{}
	for _, section := range helpSections(keybind.DefaultSession(), list, true) {
		for _, row := range section.rows {
			rows[section.title+"/"+row[0]] = row[1]
		}
	}
	if rows["list/N"] != "new session" {
		t.Errorf("new_session on N should be listed under N, got %q", rows["list/N"])
	}
	if _, stale := rows["list/n"]; stale {
		t.Error("n no longer opens a session and should have no row")
	}
	if _, quit := rows["list/q"]; quit {
		t.Error("quit off should have no row")
	}
	if got := rows["session under the cursor/X"]; got != "kill it / kill every live session (frees their RAM)" {
		t.Errorf("kill off should leave kill_all alone on its row, got %q", got)
	}
}

func TestHelpSectionListsAKeyOnce(t *testing.T) {
	for _, section := range helpSections(keybind.DefaultSession(), keybind.DefaultList(), true) {
		seen := map[string]bool{}
		for _, row := range section.rows {
			if row[0] == "" {
				continue
			}
			if seen[row[0]] {
				t.Errorf("section %q lists %q twice", section.title, row[0])
			}
			seen[row[0]] = true
		}
	}
}

func TestHelpEveryRowHasADescription(t *testing.T) {
	for _, section := range helpSections(keybind.DefaultSession(), keybind.DefaultList(), true) {
		if len(section.rows) == 0 {
			t.Errorf("section %q has no rows", section.title)
		}
		for _, row := range section.rows {
			if strings.TrimSpace(row[1]) == "" {
				t.Errorf("section %q: key %q has no description", section.title, row[0])
			}
		}
	}
}

func TestHelpSearchNarrowsToMatchingRows(t *testing.T) {
	all := helpSections(keybind.DefaultSession(), keybind.DefaultList(), true)
	if got := matchHelp(all, ""); len(got) != len(all) {
		t.Fatalf("empty query dropped sections: %d of %d", len(got), len(all))
	}
	got := matchHelp(all, "worktree")
	if len(got) == 0 {
		t.Fatal("worktree matches nothing")
	}
	for _, section := range got {
		for _, row := range section.rows {
			combined := strings.ToLower(row[0] + " " + row[1])
			if !strings.Contains(combined, "worktree") &&
				!strings.Contains(strings.ToLower(section.title), "worktree") {
				t.Errorf("section %q kept %q, which does not match", section.title, row[1])
			}
		}
	}
	if hits := matchHelp(all, "zzzz"); len(hits) != 0 {
		t.Fatalf("a query nothing answers kept %d sections", len(hits))
	}
}

func TestHelpSearchIsCaseInsensitive(t *testing.T) {
	lower := helpRowCount(matchHelp(helpSections(keybind.DefaultSession(), keybind.DefaultList(), true), "revive"))
	upper := helpRowCount(matchHelp(helpSections(keybind.DefaultSession(), keybind.DefaultList(), true), "REVIVE"))
	if lower == 0 || lower != upper {
		t.Fatalf("case changed the hits: %d lower, %d upper", lower, upper)
	}
}

// A section title standing in for its screen answers on its own: "review"
// should hand back the review screen, not the two rows spelling the word.
func TestHelpSearchOnASectionTitleKeepsItsRows(t *testing.T) {
	var review helpSection
	for _, section := range helpSections(keybind.DefaultSession(), keybind.DefaultList(), true) {
		if strings.HasPrefix(section.title, "review") {
			review = section
		}
	}
	if len(review.rows) == 0 {
		t.Fatal("no review section in the catalog")
	}
	for _, section := range matchHelp(helpSections(keybind.DefaultSession(), keybind.DefaultList(), true), "review") {
		if section.title != review.title {
			continue
		}
		if len(section.rows) != len(review.rows) {
			t.Fatalf("title match kept %d of %d rows", len(section.rows), len(review.rows))
		}
		return
	}
	t.Fatal("the review section did not survive its own title")
}

func TestReviewHelpOnlyShowsReviewBindingsAndSetupGuidance(t *testing.T) {
	m := &Model{width: 120, height: 30, mode: modeDiff, diff: diffState{active: true}}
	m.openHelp()
	sections := m.visibleHelpSections()
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

func TestHelpArrowStepRowsFollowSetting(t *testing.T) {
	hasRow := func(sections []helpSection, title, key string) bool {
		for _, section := range sections {
			if section.title != title {
				continue
			}
			for _, row := range section.rows {
				if row[0] == key {
					return true
				}
			}
		}
		return false
	}

	for _, enabled := range []bool{true, false} {
		sections := (&Model{arrowStep: enabled, keys: keybind.DefaultSession(), listKeys: keybind.DefaultList()}).visibleHelpSections()
		for _, row := range []struct{ title, key string }{
			{"list", "→"},
			{"list", "←"},
			{"inside a session (attached or focused)", "←"},
		} {
			if got := hasRow(sections, row.title, row.key); got != enabled {
				t.Errorf("arrow step enabled = %v: %q in %q = %v", enabled, row.key, row.title, got)
			}
		}
	}
}

func helpModel() *Model {
	return &Model{width: 120, height: 30, mode: modeHelp, arrowStep: true, keys: keybind.DefaultSession(), listKeys: keybind.DefaultList()}
}

func TestHelpScrollClampsToContent(t *testing.T) {
	m := helpModel()
	m.scrollHelp(-5)
	if m.help.scroll != 0 {
		t.Fatalf("scrolled above the top: %d", m.help.scroll)
	}
	limit := m.helpScrollLimit()
	if limit == 0 {
		t.Fatal("the catalog should overflow a 30-row terminal")
	}
	m.scrollHelp(1000)
	if m.help.scroll != limit {
		t.Fatalf("scroll %d past the limit %d", m.help.scroll, limit)
	}
}

func TestHelpSearchShrinksTheScrollLimit(t *testing.T) {
	m := helpModel()
	full := m.helpScrollLimit()
	m.help.query = "worktree"
	if narrowed := m.helpScrollLimit(); narrowed >= full {
		t.Fatalf("searched limit %d did not shrink below %d", narrowed, full)
	}
}

func TestHelpSearchTypesAndClears(t *testing.T) {
	m := helpModel()
	m.handleHelpKey(runeKey("/"))
	if !m.help.searching {
		t.Fatal("/ did not open the search")
	}
	for _, r := range "fork" {
		m.handleHelpKey(runeKey(string(r)))
	}
	if m.help.query != "fork" {
		t.Fatalf("typed query is %q", m.help.query)
	}
	m.handleHelpKey(namedKey(tea.KeyBackspace))
	if m.help.query != "for" {
		t.Fatalf("backspace left %q", m.help.query)
	}
	m.handleHelpKey(namedKey(tea.KeyEnter))
	if m.help.searching || m.help.query != "for" {
		t.Fatalf("enter should leave the field with the search on, got %v %q", m.help.searching, m.help.query)
	}
	// q types into the search rather than closing while the field is up.
	m.handleHelpKey(runeKey("/"))
	m.handleHelpKey(runeKey("q"))
	if m.mode != modeHelp || m.help.query != "forq" {
		t.Fatalf("q while searching: mode %v query %q", m.mode, m.help.query)
	}
}

func TestHelpEscClearsTheSearchBeforeClosing(t *testing.T) {
	m := helpModel()
	m.help.query = "fork"
	m.help.scroll = 3
	m.handleHelpKey(namedKey(tea.KeyEsc))
	if m.mode != modeHelp {
		t.Fatal("esc closed the map while a search was on")
	}
	if m.help.query != "" || m.help.scroll != 0 {
		t.Fatalf("esc left query %q scroll %d", m.help.query, m.help.scroll)
	}
	m.handleHelpKey(namedKey(tea.KeyEsc))
	if m.mode != modeList {
		t.Fatalf("esc on a clean map left mode %v", m.mode)
	}
}

func TestHelpOpensClean(t *testing.T) {
	m := helpModel()
	m.help = helpState{scroll: 4, query: "fork", searching: true}
	m.closeHelp()
	m.openHelp()
	if m.help != (helpState{}) {
		t.Fatalf("reopened with stale state: %+v", m.help)
	}
}

func TestHelpFramePaintsInsideTheTerminal(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		for _, height := range []int{14, 24, 40} {
			m := &Model{width: width, height: height, mode: modeHelp}
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
	m.help.scroll = m.helpScrollLimit()
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
	column := helpKeyColumn(keybind.DefaultSession(), keybind.DefaultList())
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
	room := cardInnerWidth(helpCardWidth(120)) - helpKeyColumn(keybind.DefaultSession(), keybind.DefaultList())
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

func TestHelpSessionRowsFollowTheKeyTable(t *testing.T) {
	rowFor := func(keys keybind.Table, key string) (string, bool) {
		for _, section := range helpSections(keys, keybind.DefaultList(), true) {
			if section.title != "inside a session (attached or focused)" {
				continue
			}
			for _, row := range section.rows {
				if row[0] == key {
					return row[1], true
				}
			}
		}
		return "", false
	}
	defaults := keybind.DefaultSession()
	if desc, ok := rowFor(defaults, "ctrl+q"); !ok || desc != `back to the manager (ctrl+\ too)` {
		t.Errorf("default detach row = %q, %v", desc, ok)
	}
	if _, ok := rowFor(defaults, "ctrl+r"); !ok {
		t.Error("default review row missing")
	}
	if _, ok := rowFor(defaults, "f3"); !ok {
		t.Error("default editor row missing")
	}

	custom := sessionOf(t, []string{"f9"}, nil, []string{"alt+e"})
	if desc, ok := rowFor(custom, "f9"); !ok || desc != "back to the manager" {
		t.Errorf("single detach row = %q, %v", desc, ok)
	}
	for _, gone := range []string{"ctrl+q", "ctrl+r", "f3"} {
		if desc, ok := rowFor(custom, gone); ok {
			t.Errorf("%s should have no row under the custom table, got %q", gone, desc)
		}
	}
	if desc, ok := rowFor(custom, "alt+e"); !ok || desc != "open its directory in an editor" {
		t.Errorf("editor row = %q, %v", desc, ok)
	}
}
