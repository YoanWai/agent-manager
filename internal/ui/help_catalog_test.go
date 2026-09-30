package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

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

func TestHelpNamesTheInboxBadgeOnASessionRow(t *testing.T) {
	for _, section := range helpSections(keybind.DefaultSession(), keybind.DefaultList(), true) {
		if section.title != "the mark on a session row" {
			continue
		}
		for _, row := range section.rows {
			if strings.Contains(row[0], "✉") && strings.Contains(row[1], "another agent") {
				return
			}
		}
		t.Fatal("the mark section does not name the inbox badge")
	}
	t.Fatal("no mark section")
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
		model := &Model{
			services: services{
				keys:     keybind.DefaultSession(),
				listKeys: keybind.DefaultList(),
			},
			prefs: preferences{
				arrowStep: enabled,
			},
		}
		sections := model.help.visibleSections(model.helpContext())
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
