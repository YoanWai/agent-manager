package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpScrollClampsToContent(t *testing.T) {
	m := helpModel()
	m.help.scrollBy(-5, m.helpContext())
	if m.help.scroll != 0 {
		t.Fatalf("scrolled above the top: %d", m.help.scroll)
	}
	limit := m.help.scrollLimit(m.helpContext())
	if limit == 0 {
		t.Fatal("the catalog should overflow a 30-row terminal")
	}
	m.help.scrollBy(1000, m.helpContext())
	if m.help.scroll != limit {
		t.Fatalf("scroll %d past the limit %d", m.help.scroll, limit)
	}
}

func TestHelpSearchShrinksTheScrollLimit(t *testing.T) {
	m := helpModel()
	full := m.help.scrollLimit(m.helpContext())
	m.help.query = "worktree"
	if narrowed := m.help.scrollLimit(m.helpContext()); narrowed >= full {
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
	m.mode = modeList
	m.openHelp()
	if m.help != (helpState{}) {
		t.Fatalf("reopened with stale state: %+v", m.help)
	}
}

func TestHelpStateOwnsKeyboardSearchAndScroll(t *testing.T) {
	ctx := featureHelpContext(120, 18)
	h := helpState{scope: helpReview}

	if transition := h.update(runeKey("/"), ctx); transition != helpStay || !h.searching {
		t.Fatalf("open search: transition = %+v, state = %+v", transition, h)
	}
	for _, r := range "fork" {
		h.update(runeKey(string(r)), ctx)
	}
	h.update(namedKey(tea.KeyBackspace), ctx)
	if h.query != "for" || !h.searching {
		t.Fatalf("search edit left query %q, searching = %v", h.query, h.searching)
	}
	h.update(namedKey(tea.KeyEnter), ctx)
	if h.query != "for" || h.searching {
		t.Fatalf("finish search left query %q, searching = %v", h.query, h.searching)
	}

	h.scroll = 3
	if transition := h.update(namedKey(tea.KeyEsc), ctx); transition != helpStay {
		t.Fatalf("clear search transition = %+v", transition)
	}
	if h.query != "" || h.scroll != 0 || h.scope != helpReview {
		t.Fatalf("clear search state = %+v", h)
	}
	transition := h.update(namedKey(tea.KeyEsc), ctx)
	if transition != helpClose {
		t.Fatalf("close transition = %+v", transition)
	}
	if h != (helpState{}) {
		t.Fatalf("closed help retained state: %+v", h)
	}

	h = helpState{scope: helpGlobal}
	h.update(namedKey(tea.KeyDown), ctx)
	oneRow := h.scroll
	h.update(namedKey(tea.KeyPgDown), ctx)
	if oneRow != 1 || h.scroll <= oneRow {
		t.Fatalf("down/page down moved from 0 to %d to %d", oneRow, h.scroll)
	}
	h.update(runeKey("G"), ctx)
	if h.scroll != h.scrollLimit(ctx) {
		t.Fatalf("bottom left scroll %d, limit %d", h.scroll, h.scrollLimit(ctx))
	}
	h.update(namedKey(tea.KeyHome), ctx)
	if h.scroll != 0 {
		t.Fatalf("home left scroll %d", h.scroll)
	}
}

func TestHelpStateOwnsQuitWithoutClosing(t *testing.T) {
	ctx := featureHelpContext(120, 30)
	h := helpState{scope: helpReview, query: "fork", searching: true, scroll: 2}
	want := h

	transition := h.update(tea.KeyMsg{Type: tea.KeyCtrlC}, ctx)
	if transition != helpQuit {
		t.Fatalf("ctrl+c transition = %+v", transition)
	}
	if h != want {
		t.Fatalf("ctrl+c changed state from %+v to %+v", want, h)
	}
}

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
	m := &Model{mode: modeDiff, diff: diffState{active: true, loading: true}}
	m.openHelp()
	if m.helpReturnMode != modeDiff || m.help.scope != helpReview {
		t.Fatalf("opened review help with return mode %v and scope %v", m.helpReturnMode, m.help.scope)
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
