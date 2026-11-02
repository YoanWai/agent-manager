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
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"
	uisurface "github.com/YoanWai/agent-manager/internal/ui/surface"
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

func TestHelpContentAndLayout(t *testing.T) {
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
		{name: "global_compatible", model: helpModel()},
		{name: "search_ime_compatible", model: helpModel(), assertCursor: true},
	}
	tests[1].model.layout.width, tests[1].model.layout.height = 100, 28
	tests[1].model.focus.imeCursor = &uiframe.CursorAnchor{}
	typeHelpSearch(tests[1].model, "中文", false)
	*helpSurfaceOf(tests[2].model).State() = uihelp.New(uihelp.Review)
	tests[3].model.layout.width, tests[3].model.layout.height = 60, 14
	tests[3].model.errBar = errBar{text: "refresh failed"}
	typeHelpSearch(tests[3].model, "zzzz", true)

	tests[4].model.prefs.glyphMode = presentation.GlyphModeBasic
	tests[5].model.prefs.glyphMode = presentation.GlyphModeBasic
	tests[5].model.layout.width, tests[5].model.layout.height = 100, 28
	tests[5].model.focus.imeCursor = &uiframe.CursorAnchor{}
	typeHelpSearch(tests[5].model, "中文", false)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			golden, err := os.ReadFile(filepath.Join("testdata", "help", test.name+".txt"))
			if err != nil {
				t.Fatal(err)
			}
			got := readableHelpFrame(preparedView(test.model))
			want := strings.TrimSuffix(string(golden), "\n")
			if got != want {
				t.Fatalf("help content and layout changed:\nwant:\n%s\n\ngot:\n%s", want, got)
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
	if !ok || docs.Y <= searchLine {
		t.Fatalf("the docs hit should sit on the footer below the search row, hit %+v search %d", docs, searchLine)
	}
	if !strings.Contains(plain[docs.Y], "o docs") {
		t.Fatalf("the docs hit missed the footer: %q", plain[docs.Y])
	}

	m.handleKey(runeKey("/"))
	preparedView(m)
	if _, ok := docsKeyHit(m); ok {
		t.Fatal("typing a search should not leave a docs hit")
	}
}

func docsKeyHit(m *Model) (keyHit, bool) {
	for _, hit := range m.layout.keyHits {
		if hit.Key.String() == "o" {
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

func helpOverList(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	// The real trigger: the help key on the list.
	m.Update(runeKey("?"))
	if !helpOpen(m) {
		t.Fatal("? did not open the key map")
	}
	return m
}

func TestHelpSurfaceOpensOnTheListKey(t *testing.T) {
	m := helpOverList(t)
	if !m.onList() {
		t.Fatalf("help changed the base screen to %v", m.screenID())
	}
	if got := helpSurfaceOf(m).State().Content(uihelp.ContextFor(helpHost{m}, 84)).Title; got == "? Review keys" {
		t.Fatalf("list help opened with review scope: %q", got)
	}
}

func TestHelpSurfaceClosesOnEscAndBackdrop(t *testing.T) {
	if !(&uihelp.Surface{}).Dismissible() {
		t.Fatal("help must be dismissible")
	}
	for name, close := range map[string]func(*Model) *Model{
		"esc": func(m *Model) *Model { m.Update(namedKey(tea.KeyEsc)); return m },
		"click off the card": func(m *Model) *Model {
			preparedView(m)
			return leftClick(m, 0, 0)
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := close(helpOverList(t))
			if helpOpen(m) || m.effects.Quitting {
				t.Fatalf("help open = %t, quitting = %t", helpOpen(m), m.effects.Quitting)
			}
		})
	}
}

// Closing help over Review is a pop: Review stays the mode underneath, and
// its loader is the poller's to restart rather than a close side effect.
func TestHelpSurfaceOverReviewScopesToReviewAndPopsBack(t *testing.T) {
	m := helpModel()
	m.clearSurfaces()
	m.base = baseReview
	seedReviewForTest(m, uireview.Target{ID: "review"}, git.ScopeUncommitted, "/repo", diff.Set{}, false)
	m.openHelp()
	if got := helpSurfaceOf(m).State().Content(uihelp.ContextFor(helpHost{m}, 84)).Title; got != "? Review keys" {
		t.Fatalf("help over review has title %q", got)
	}
	m.handleKey(namedKey(tea.KeyEsc))
	if helpOpen(m) || m.base != baseReview {
		t.Fatalf("esc left help open = %t, screen = %v", helpOpen(m), m.screenID())
	}
}

func TestHelpSurfaceHoldsTheScopeItWasOpenedWith(t *testing.T) {
	s := uihelp.NewSurface(helpHost{helpModel()}, uihelp.Review)
	if !s.Dismissible() || s.Owner() != uisurface.Manager {
		t.Fatal("help must be a dismissible manager-owned card")
	}
}

func TestWheelScrollsHelp(t *testing.T) {
	m := helpModel()
	preparedView(m)
	if m = wheel(m, tea.MouseButtonWheelDown, 60, 15); helpLayoutOf(m).Content.Offset != 1 {
		t.Fatalf("a wheel notch down should scroll one row, offset %d", helpLayoutOf(m).Content.Offset)
	}
	if m = wheel(m, tea.MouseButtonWheelUp, 60, 15); helpLayoutOf(m).Content.Offset != 0 {
		t.Fatalf("a wheel notch up should scroll back, offset %d", helpLayoutOf(m).Content.Offset)
	}
	if m.layout.card.holds(1, 15) {
		t.Fatalf("test setup: the card covers the left edge: %+v", m.layout.card)
	}
	if m = wheel(m, tea.MouseButtonWheelDown, 1, 15); helpLayoutOf(m).Content.Offset != 0 || !helpOpen(m) {
		t.Fatalf("a wheel off the card should do nothing, offset %d mode %v", helpLayoutOf(m).Content.Offset, m.screenID())
	}
}

func TestClickOnHelpsSearchLineResumesTheSearch(t *testing.T) {
	m := helpModel()
	for _, msg := range []tea.KeyMsg{runeKey("/"), runeKey("fork"), namedKey(tea.KeyEnter)} {
		m.handleKey(msg)
	}
	x, y := paintedCell(t, m, "search fork")
	if m = leftClick(m, x, y); !helpSurfaceOf(m).State().Searching() || !helpOpen(m) {
		t.Fatalf("a click on the search line should resume typing, searching %v mode %v", helpSurfaceOf(m).State().Searching(), m.screenID())
	}
}

func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func namedKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func helpModel() *Model {
	m := &Model{
		layout: layoutState{
			width:  120,
			height: 30,
		},
		services: services{
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
		prefs: preferences{
			arrowStep: true,
		},
	}
	m.openHelp()
	return m
}

// helpSurfaceOf is the key map on the stack, which a test reads state from.
func helpSurfaceOf(m *Model) *uihelp.Surface {
	s, ok := surfaceOf[*uihelp.Surface](m)
	if !ok {
		panic("the key map is not open")
	}
	return s
}

func helpLayoutOf(m *Model) uihelp.Card { return helpSurfaceOf(m).Card(m) }

func viewHelpOf(m *Model) string { return helpSurfaceOf(m).View(m, "") }

func helpOpen(m *Model) bool {
	_, ok := surfaceOf[*uihelp.Surface](m)
	return ok
}

func TestHelpCloseFollowsSavedBindingWithoutStealingSearch(t *testing.T) {
	m := helpModel()
	m.services.listKeys = m.services.listKeys.With(keybind.Help, bindingOf(t, "H", "f2"))
	m.handleKey(runeKey("?"))
	if !m.helpOpen() {
		t.Fatal("old help binding closed dialog")
	}
	m.handleKey(runeKey("/"))
	m.handleKey(runeKey("H"))
	if frame := ansi.Strip(preparedView(m)); !strings.Contains(frame, "search H") {
		t.Fatalf("bound key stole search: %s", frame)
	}
	m.handleKey(namedKey(tea.KeyEnter))
	m.handleKey(tea.KeyMsg{Type: tea.KeyF2})
	if m.helpOpen() {
		t.Fatal("saved help binding did not close dialog")
	}
	m.openHelp()
	m.services.listKeys = m.services.listKeys.With(keybind.Help, bindingOf(t))
	m.handleKey(runeKey("?"))
	if !m.helpOpen() {
		t.Fatal("disabled help binding retained old alias")
	}
	m.handleKey(namedKey(tea.KeyEnter))
	if !m.helpOpen() {
		t.Fatal("unadvertised Enter closed help")
	}
	m.handleKey(namedKey(tea.KeyEsc))
	if m.helpOpen() {
		t.Fatal("Escape did not close help")
	}
}

func TestHelpToggleDoesNotOverrideDocs(t *testing.T) {
	m := helpModel()
	m.services.listKeys = m.services.listKeys.With(keybind.Help, bindingOf(t, "o"))
	_, cmd := m.handleKey(runeKey("o"))
	if !m.helpOpen() || cmd == nil {
		t.Fatal("toggle stole local docs action")
	}
	for _, pair := range helpSurfaceOf(m).State().Content(uihelp.ContextFor(helpHost{m}, 84)).Hints {
		if pair[1] == "close" && strings.Contains(pair[0], "o") {
			t.Fatalf("docs key advertised as close: %v", pair)
		}
	}
}
