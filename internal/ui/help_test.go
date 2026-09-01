package ui

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func runeKey(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func namedKey(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func typedKeys(text string, trailing ...tea.KeyPressMsg) []tea.KeyPressMsg {
	keys := make([]tea.KeyPressMsg, 0, len(text)+len(trailing))
	for _, r := range text {
		keys = append(keys, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return append(keys, trailing...)
}

// helpKeyTokens is every key the catalog spells out, split on the " / "
// that separates alternatives inside one row.
func helpKeyTokens() map[string]bool {
	tokens := map[string]bool{}
	for _, section := range helpSections() {
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

// helpDocumentedBy names the catalog token that documents each key the list
// handler answers to, for the keys whose written form is not the literal:
// the arrows and the shift pairs are one row each.
var helpDocumentedBy = map[string]string{
	"up": "↑↓", "down": "↑↓", "k": "↑↓", "j": "↑↓",
	"right": "→", "left": "←",
	"enter": "↵", " ": "space",
	"shift+up": "K", "shift+down": "J",
	"shift+k": "K", "shift+j": "J", "shift+a": "A", "shift+v": "V",
	"shift+r": "R", "shift+x": "X", "shift+f": "F", "shift+m": "M",
	"shift+t": "T",
	// Ctrl+C quits from every mode, so it belongs to no one context.
	"ctrl+c": "q",
}

var caseLiteral = regexp.MustCompile(`case ((?:"(?:[^"\\]|\\.)*"(?:, )?)+):`)

// listModeKeys reads the keys the list handler actually answers to out of
// the handler itself, so a binding added there without a line in the key map
// fails this test rather than shipping undocumented.
func listModeKeys(t *testing.T) []string {
	t.Helper()
	source, err := os.ReadFile("keys.go")
	if err != nil {
		t.Fatalf("read keys.go: %v", err)
	}
	text := string(source)
	// The list switch is the tail of handleKey, after the mode and the
	// transient-state dispatches.
	start := strings.Index(text, "\n\tswitch msg.String() {")
	if start < 0 {
		t.Fatal("list-mode switch not found in keys.go")
	}
	end := strings.Index(text[start:], "\nfunc ")
	if end < 0 {
		t.Fatal("end of handleKey not found in keys.go")
	}

	var keys []string
	for _, match := range caseLiteral.FindAllStringSubmatch(text[start:start+end], -1) {
		for _, literal := range strings.Split(match[1], ", ") {
			key, err := strconv.Unquote(literal)
			if err != nil {
				t.Fatalf("case literal %s: %v", literal, err)
			}
			keys = append(keys, key)
		}
	}
	if len(keys) < 20 {
		t.Fatalf("only %d list keys parsed out of keys.go, the scan is broken", len(keys))
	}
	return keys
}

func TestHelpCatalogDocumentsEveryListKey(t *testing.T) {
	documented := helpKeyTokens()
	for _, key := range listModeKeys(t) {
		token := key
		if alias, ok := helpDocumentedBy[key]; ok {
			token = alias
		}
		if !documented[token] {
			t.Errorf("list key %q is not in the ? key map (looked for %q)", key, token)
		}
	}
}

func TestHelpSectionListsAKeyOnce(t *testing.T) {
	for _, section := range helpSections() {
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
	for _, section := range helpSections() {
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
	all := helpSections()
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
	lower := helpRowCount(matchHelp(helpSections(), "revive"))
	upper := helpRowCount(matchHelp(helpSections(), "REVIVE"))
	if lower == 0 || lower != upper {
		t.Fatalf("case changed the hits: %d lower, %d upper", lower, upper)
	}
}

// A section title standing in for its screen answers on its own: "review"
// should hand back the review screen, not the two rows spelling the word.
func TestHelpSearchOnASectionTitleKeepsItsRows(t *testing.T) {
	var review helpSection
	for _, section := range helpSections() {
		if strings.HasPrefix(section.title, "review") {
			review = section
		}
	}
	if len(review.rows) == 0 {
		t.Fatal("no review section in the catalog")
	}
	for _, section := range matchHelp(helpSections(), "review") {
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
	frame := ansi.Strip(m.viewFrame())
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
	frame := ansi.Strip(helpModel().viewFrame())
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
		sections := (&Model{arrowStep: enabled}).visibleHelpSections()
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
	return &Model{width: 120, height: 30, mode: modeHelp, arrowStep: true}
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
				lines := strings.Split(m.viewFrame(), "\n")
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
	frame := ansi.Strip(m.viewFrame())
	if !strings.Contains(frame, "more below") {
		t.Fatal("an overflowing map should say there is more below")
	}
	m.help.scroll = m.helpScrollLimit()
	frame = ansi.Strip(m.viewFrame())
	if !strings.Contains(frame, "more above") {
		t.Fatal("a map scrolled to the end should say there is more above")
	}
}

func TestHelpReportsWhenNothingMatches(t *testing.T) {
	m := helpModel()
	m.help.query = "zzzz"
	if frame := ansi.Strip(m.viewFrame()); !strings.Contains(frame, "no key matches that") {
		t.Fatal("a query nothing answers should say so")
	}
}

// The column is measured from the catalog, so a key can never render clipped
// against its own description. This is what catches a long binding added later.
func TestHelpKeyColumnFitsEveryKey(t *testing.T) {
	column := helpKeyColumn()
	for _, section := range helpSections() {
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
	room := cardInnerWidth(helpCardWidth(120)) - helpKeyColumn()
	for _, section := range helpSections() {
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
		for _, section := range helpSections() {
			for _, row := range section.rows {
				highlightMatch(row[1], query, 60)
			}
		}
	}
}
