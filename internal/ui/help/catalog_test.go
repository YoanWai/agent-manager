package help

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

func helpKeyTokens(ctx Context) map[string]bool {
	tokens := map[string]bool{}
	for _, section := range helpSections(ctx) {
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

func TestCatalogDocumentsEveryListAction(t *testing.T) {
	ctx := testContext(84)
	documented := helpKeyTokens(ctx)
	for _, action := range ctx.ListKeys.Actions() {
		for _, key := range ctx.ListKeys.Binding(action.Name).Keys() {
			if !documented[key.Glyph()] {
				t.Errorf("list action %s: key %q is not in the map", action.Name, key.Glyph())
			}
		}
	}
	if !documented["ctrl+c"] {
		t.Error("ctrl+c quits from every mode and belongs in the map")
	}
}

func TestCatalogFollowsCustomListBindings(t *testing.T) {
	ctx := testContext(84)
	ctx.ListKeys = ctx.ListKeys.
		With(keybind.NewSession, bindingOf(t, "N")).
		With(keybind.Kill, bindingOf(t)).
		With(keybind.Quit, bindingOf(t))
	rows := map[string]string{}
	for _, section := range helpSections(ctx) {
		for _, row := range section.rows {
			rows[section.title+"/"+row[0]] = row[1]
		}
	}
	if rows["list/N"] != "new session" {
		t.Errorf("new_session on N = %q", rows["list/N"])
	}
	if _, stale := rows["list/n"]; stale {
		t.Error("old new-session key remains")
	}
	if _, quit := rows["list/q"]; quit {
		t.Error("disabled quit remains")
	}
	if got := rows["session under the cursor/X"]; got != "kill it / kill every live session (frees their RAM)" {
		t.Errorf("kill-all fallback row = %q", got)
	}
}

func TestCatalogUsesCallerOwnedRailGlyphs(t *testing.T) {
	ctx := testContext(84)
	ctx.Glyphs = Glyphs{RowMenu: "MENU", Reorder: "GRIP"}
	rows := helpSections(ctx)[0].rows
	var joined strings.Builder
	for _, row := range rows {
		joined.WriteString(row[0])
		joined.WriteByte('\n')
	}
	if got := joined.String(); !strings.Contains(got, "MENU") || !strings.Contains(got, "drag GRIP") {
		t.Fatalf("list bindings did not use caller glyphs:\n%s", got)
	}
}

func TestCatalogRowsAreCompleteAndUniqueWithinASection(t *testing.T) {
	for _, section := range helpSections(testContext(84)) {
		if len(section.rows) == 0 {
			t.Errorf("section %q has no rows", section.title)
		}
		seen := map[string]bool{}
		for _, row := range section.rows {
			if strings.TrimSpace(row[1]) == "" {
				t.Errorf("section %q: key %q has no description", section.title, row[0])
			}
			if row[0] != "" && seen[row[0]] {
				t.Errorf("section %q lists %q twice", section.title, row[0])
			}
			seen[row[0]] = row[0] != ""
		}
	}
}

func TestCatalogNamesTheInboxBadge(t *testing.T) {
	for _, section := range helpSections(testContext(84)) {
		if section.title != "the mark on a session row" {
			continue
		}
		for _, row := range section.rows {
			if strings.Contains(row[0], "✉") && strings.Contains(row[1], "another agent") {
				return
			}
		}
		t.Fatal("mark section does not name the inbox badge")
	}
	t.Fatal("no mark section")
}

func TestCatalogSearchMatchesRowsAndWholeSections(t *testing.T) {
	all := helpSections(testContext(84))
	if got := matchHelp(all, ""); len(got) != len(all) {
		t.Fatalf("empty query kept %d of %d sections", len(got), len(all))
	}
	got := matchHelp(all, "worktree")
	if len(got) == 0 {
		t.Fatal("worktree matches nothing")
	}
	for _, section := range got {
		for _, row := range section.rows {
			combined := strings.ToLower(row[0] + " " + row[1])
			if !strings.Contains(combined, "worktree") && !strings.Contains(strings.ToLower(section.title), "worktree") {
				t.Errorf("section %q kept non-match %q", section.title, row[1])
			}
		}
	}
	if hits := matchHelp(all, "zzzz"); len(hits) != 0 {
		t.Fatalf("impossible query kept %d sections", len(hits))
	}
	if lower, upper := helpRowCount(matchHelp(all, "revive")), helpRowCount(matchHelp(all, "REVIVE")); lower == 0 || lower != upper {
		t.Fatalf("case changed hits: %d lower, %d upper", lower, upper)
	}

	var review helpSection
	for _, section := range all {
		if strings.HasPrefix(section.title, "review") {
			review = section
		}
	}
	for _, section := range matchHelp(all, "review") {
		if section.title == review.title && len(section.rows) == len(review.rows) {
			return
		}
	}
	t.Fatal("title match did not retain the whole review section")
}

func TestCatalogArrowRowsFollowTheSetting(t *testing.T) {
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
		ctx := testContext(84)
		ctx.ArrowStep = enabled
		for _, row := range []struct{ title, key string }{
			{"list", "→"},
			{"list", "←"},
			{"inside a session (attached or focused)", "←"},
		} {
			if got := hasRow(helpSections(ctx), row.title, row.key); got != enabled {
				t.Errorf("enabled = %v: %q in %q = %v", enabled, row.key, row.title, got)
			}
		}
	}
}

func TestCatalogSessionRowsFollowCustomBindings(t *testing.T) {
	rowFor := func(keys keybind.Table, key string) (string, bool) {
		ctx := testContext(84)
		ctx.SessionKeys = keys
		for _, section := range helpSections(ctx) {
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
		t.Errorf("custom detach row = %q, %v", desc, ok)
	}
	for _, gone := range []string{"ctrl+q", "ctrl+r", "f3"} {
		if desc, ok := rowFor(custom, gone); ok {
			t.Errorf("%s remains with %q", gone, desc)
		}
	}
	if desc, ok := rowFor(custom, "alt+e"); !ok || desc != "open its directory in an editor" {
		t.Errorf("custom editor row = %q, %v", desc, ok)
	}

	pinned := defaults.With(keybind.TmuxPrefix, bindingOf(t, "ctrl+b"))
	if desc, ok := rowFor(pinned, "ctrl+b"); !ok || desc != "attached: tmux's prefix, in place of yours" {
		t.Errorf("tmux_prefix row = %q, %v", desc, ok)
	}
}
