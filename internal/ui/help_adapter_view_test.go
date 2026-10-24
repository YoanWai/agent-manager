package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/keybind"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uihelp "github.com/YoanWai/agent-manager/internal/ui/help"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func typeHelpSearch(m *Model, query string, finish bool) {
	m.handleKey(runeKey("/"))
	for _, r := range query {
		m.handleKey(runeKey(string(r)))
	}
	if finish {
		m.handleKey(namedKey(tea.KeyEnter))
	}
}

func TestReviewHelpOnlyShowsReviewBindingsAndSetupGuidance(t *testing.T) {
	m := &Model{
		layout: layoutState{
			width:  120,
			height: 30,
		},
		screen: screen{base: baseReview},
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

// The arrow-step rows reach the map from the Model's own preference, not
// only from a context a test builds by hand.
func TestHelpArrowStepRowsFollowSetting(t *testing.T) {
	hasRow := func(lines []string, title, key string) bool {
		inSection := false
		for _, line := range lines {
			plain := ansi.Strip(line)
			if heading, ok := strings.CutPrefix(plain, "▍"); ok {
				inSection = strings.HasPrefix(heading, title+" ")
				continue
			}
			if inSection && strings.HasPrefix(plain, key+" ") {
				return true
			}
		}
		return false
	}

	for _, enabled := range []bool{true, false} {
		m := helpModel()
		m.prefs.arrowStep = enabled
		lines := helpSurfaceOf(m).State().Content(uihelp.ContextFor(helpHost{m}, 84)).Lines
		for _, row := range []struct{ title, key string }{
			{"list", "→"},
			{"list", "←"},
			{"inside a session (attached or focused)", "←"},
		} {
			if got := hasRow(lines, row.title, row.key); got != enabled {
				t.Errorf("arrow step enabled = %v: %q in %q = %v", enabled, row.key, row.title, got)
			}
		}
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
					services: services{
						keys:     keybind.DefaultSession(),
						listKeys: keybind.DefaultList(),
					},
				}
				m.openHelp()
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
	m.handleKey(runeKey("G"))
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
	previousTheme := uitheme.Current()
	applyTheme(uitheme.Palettes[0])
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
	tests[1].model.focus.imeCursor = &uiframe.CursorAnchor{}
	typeHelpSearch(tests[1].model, "中文", false)
	*helpSurfaceOf(tests[2].model).State() = uihelp.New(uihelp.Review)
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
				if _, _, ok := test.model.focus.imeCursor.Get(); !ok {
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
	t.Cleanup(func() { openBrowser = uiterminal.DefaultOpenBrowser })

	frame := preparedView(m)
	if !strings.Contains(frame, uitheme.HintCap("o", "docs")) {
		t.Fatal("the docs shortcut should wear the footer badge")
	}
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m.applyCmd(t, cmd)
	if opened != docsURL {
		t.Fatalf("o should open the docs, got %q", opened)
	}
	if !helpOpen(m) {
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
	m, cmd = leftClickCmd(m, x, lineIndex)
	m.applyCmd(t, cmd)
	if opened != docsURL {
		t.Fatalf("clicking the footer should open the docs, got %q", opened)
	}
}

func TestHelpDocsHitIgnoresTheSearchRow(t *testing.T) {
	m := helpModel()
	for _, msg := range []tea.KeyMsg{runeKey("/"), runeKey("o"), namedKey(tea.KeySpace), runeKey("docs"), namedKey(tea.KeyEnter)} {
		m.handleKey(msg)
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
	docs, ok := docsKeyHit(m)
	if !ok || docs.y <= searchLine {
		t.Fatalf("the docs hit should sit on the footer below the search row, hit %+v search %d", docs, searchLine)
	}
	if !strings.Contains(plain[docs.y], "o docs") {
		t.Fatalf("the docs hit missed the footer: %q", plain[docs.y])
	}

	m.handleKey(runeKey("/"))
	preparedView(m)
	if _, ok := docsKeyHit(m); ok {
		t.Fatal("typing a search should not leave a docs hit")
	}
}

func docsKeyHit(m *Model) (keyHit, bool) {
	for _, hit := range m.layout.keyHits {
		if hit.key.String() == "o" {
			return hit, true
		}
	}
	return keyHit{}, false
}

func TestHelpAdapterKeepsReviewLoaderRunningUnderTheCard(t *testing.T) {
	m := helpModel()
	m.clearSurfaces()
	m.base = baseReview
	seedReviewForTest(m, uireview.Target{ID: "review"}, git.ScopeUncommitted, "/repo", diff.Set{}, false)
	if cmd := m.startStartupTick(); cmd == nil {
		t.Fatal("loading review should start the loader")
	}
	m.openHelp()
	if m.base != baseReview {
		t.Fatalf("help over review left mode %v", m.screenID())
	}
	if title := helpSurfaceOf(m).State().Content(uihelp.ContextFor(helpHost{m}, 84)).Title; title != "? Review keys" {
		t.Fatalf("opened review help title = %q", title)
	}
	m.Update(uistartup.TickMsg{})
	if !m.startup.Animating {
		t.Fatal("review is still loading under the card, so the loader keeps ticking")
	}
	m.handleKey(namedKey(tea.KeyEsc))
	if m.base != baseReview || helpOpen(m) || !m.startup.Animating {
		t.Fatalf("close left mode %v, help open %t, loader %t", m.screenID(), helpOpen(m), m.startup.Animating)
	}
}

func TestHelpAdapterReopensWithCleanFeatureState(t *testing.T) {
	m := helpModel()
	m.handleKey(runeKey("/"))
	for _, r := range "fork" {
		m.handleKey(runeKey(string(r)))
	}
	if frame := ansi.Strip(viewHelpOf(m)); !strings.Contains(frame, "search fork") {
		t.Fatalf("test setup did not type a search:\n%s", frame)
	}
	m.base = baseList
	m.clearSurfaces()
	m.openHelp()
	if frame := ansi.Strip(viewHelpOf(m)); strings.Contains(frame, "search fork") {
		t.Fatalf("reopened help retained stale search:\n%s", frame)
	}
}

func TestHelpAdapterRecomputesRowsWithoutMutatingScroll(t *testing.T) {
	m := helpModel()
	m.layout.height = 18
	m.handleKey(runeKey("G"))
	clean := helpLayoutOf(m)
	if clean.Content.Offset == 0 {
		t.Fatal("test setup did not scroll the catalog")
	}

	m.handleKey(runeKey("/"))
	searching := helpLayoutOf(m)
	if len(searching.Content.Head) != 2 || searching.Rows >= clean.Rows {
		t.Fatalf("searching head/rows = %d/%d, clean rows = %d", len(searching.Content.Head), searching.Rows, clean.Rows)
	}
	before := searching.Content.Offset
	first := viewHelpOf(m)
	second := viewHelpOf(m)
	if got := helpLayoutOf(m).Content.Offset; got != before {
		t.Fatalf("render mutated scroll from %d to %d", before, got)
	}
	if first != second {
		t.Fatal("same help state rendered different frames")
	}

	m.handleKey(namedKey(tea.KeyEnter))
	finished := helpLayoutOf(m)
	if len(finished.Content.Head) != 0 || finished.Rows != clean.Rows || finished.Content.Offset != before {
		t.Fatalf("finished empty search = head %d rows %d offset %d; want 0/%d/%d",
			len(finished.Content.Head), finished.Rows, finished.Content.Offset, clean.Rows, before)
	}

	m.handleKey(runeKey("/"))
	m.handleKey(namedKey(tea.KeyEsc))
	cleared := helpLayoutOf(m)
	if len(cleared.Content.Head) != 0 || cleared.Content.Offset != 0 {
		t.Fatalf("escape from search left head %d offset %d", len(cleared.Content.Head), cleared.Content.Offset)
	}
}

func TestHelpAdapterCountsRenderedNoMatchLine(t *testing.T) {
	m := helpModel()
	m.handleKey(runeKey("/"))
	for _, r := range "zzzz" {
		m.handleKey(runeKey(string(r)))
	}
	m.handleKey(namedKey(tea.KeyEnter))
	layout := helpLayoutOf(m)
	if got := len(layout.Content.Lines); got != 1 {
		t.Fatalf("no-match content has %d rendered lines, want 1", got)
	}
	m.handleKey(runeKey("G"))
	if offset := helpLayoutOf(m).Content.Offset; offset != 0 {
		t.Fatalf("bottom on one rendered line moved to %d", offset)
	}
}

func TestHelpAdapterUsesFreshThemeStyles(t *testing.T) {
	previousProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previousProfile) })
	previous := uitheme.Current()
	t.Cleanup(func() { applyTheme(previous) })
	m := helpModel()

	applyTheme(uitheme.Palettes[uitheme.Index("classic")])
	classic := helpSurfaceOf(m).State().Content(uihelp.ContextFor(helpHost{m}, 84))
	applyTheme(uitheme.Palettes[uitheme.Index("solarized light")])
	light := helpSurfaceOf(m).State().Content(uihelp.ContextFor(helpHost{m}, 84))
	if strings.Join(classic.Lines, "\n") == strings.Join(light.Lines, "\n") {
		t.Fatal("theme switch left help styles unchanged")
	}
	if ansi.Strip(strings.Join(classic.Lines, "\n")) != ansi.Strip(strings.Join(light.Lines, "\n")) {
		t.Fatal("theme switch changed help content")
	}
}

func TestHelpAdapterScopeUsesOnlyCopiedValues(t *testing.T) {
	m := helpModel()
	*helpSurfaceOf(m).State() = uihelp.New(uihelp.Review)
	content := helpSurfaceOf(m).State().Content(uihelp.ContextFor(helpHost{m}, 84))
	if content.Title != "? Review keys" || len(content.Lines) == 0 {
		t.Fatalf("review content = %+v", content)
	}
}

func TestHelpModeOwnsKeysBeforeListOverlays(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.applyCmd(t, m.openQuickMode())
	if m.quick() == nil {
		t.Fatal("quick bar did not open")
	}
	m.openHelp()

	m.handleKey(runeKey("/"))
	m.handleKey(runeKey("q"))
	frame := ansi.Strip(preparedView(m))
	if !helpOpen(m) || !strings.Contains(frame, "search q") {
		t.Fatalf("list overlay intercepted Help input, screen = %v:\n%s", m.screenID(), frame)
	}
}

func TestHelpContextForwardsCustomSessionAndListTablesThroughView(t *testing.T) {
	m := helpModel()
	m.layout.height = 160
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
