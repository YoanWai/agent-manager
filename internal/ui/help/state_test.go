package help

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStateClampsKeyboardScrollToTheViewport(t *testing.T) {
	state := New(Global)
	viewport := Viewport{Rows: 5, Lines: 20}

	state.Update(namedKey(tea.KeyUp), viewport)
	if state.scroll != 0 {
		t.Fatalf("scrolled above the top: %d", state.scroll)
	}
	state.Update(runeKey("G"), viewport)
	if state.scroll != 15 {
		t.Fatalf("bottom scroll = %d, want 15", state.scroll)
	}
	state.Update(namedKey(tea.KeyPgDown), viewport)
	if state.scroll != 15 {
		t.Fatalf("page down passed the bottom: %d", state.scroll)
	}
	state.Update(namedKey(tea.KeyHome), viewport)
	if state.scroll != 0 {
		t.Fatalf("home left scroll %d", state.scroll)
	}
	state.Update(namedKey(tea.KeyDown), viewport)
	state.Update(namedKey(tea.KeyPgDown), viewport)
	if state.scroll != 5 {
		t.Fatalf("down then page down moved to %d, want 5", state.scroll)
	}
}

func TestStateOwnsSearchEditingAndClearBeforeClose(t *testing.T) {
	state := New(Review)
	viewport := Viewport{Rows: 4, Lines: 12}

	if action := state.Update(runeKey("/"), viewport); action != Stay || !state.searching {
		t.Fatalf("open search: action = %v, state = %+v", action, state)
	}
	for _, r := range "fork" {
		state.Update(runeKey(string(r)), viewport)
	}
	state.Update(namedKey(tea.KeyBackspace), viewport)
	if state.query != "for" || !state.searching {
		t.Fatalf("edited search = %+v", state)
	}
	state.Update(namedKey(tea.KeyEnter), viewport)
	if state.query != "for" || state.searching {
		t.Fatalf("finished search = %+v", state)
	}

	state.scroll = 3
	if action := state.Update(namedKey(tea.KeyEsc), viewport); action != Stay {
		t.Fatalf("clear action = %v, want Stay", action)
	}
	if state.query != "" || state.scroll != 0 || state.scope != Review {
		t.Fatalf("clear left state %+v", state)
	}
	if action := state.Update(namedKey(tea.KeyEsc), viewport); action != Close {
		t.Fatalf("close action = %v, want Close", action)
	}
	if state != (State{}) {
		t.Fatalf("closed help retained state: %+v", state)
	}
}

func TestStateTreatsCloseKeysAsTextWhileSearching(t *testing.T) {
	state := New(Global)
	viewport := Viewport{Rows: 4, Lines: 12}
	state.Update(runeKey("/"), viewport)
	state.Update(runeKey("q"), viewport)
	state.Update(runeKey("?"), viewport)
	if state.query != "q?" || !state.searching {
		t.Fatalf("search state = %+v", state)
	}
}

func TestStateQuitDoesNotMutateFeatureState(t *testing.T) {
	state := State{scope: Review, query: "fork", searching: true, scroll: 2}
	want := state
	if action := state.Update(tea.KeyMsg{Type: tea.KeyCtrlC}, Viewport{Rows: 4, Lines: 12}); action != Quit {
		t.Fatalf("ctrl+c action = %v, want Quit", action)
	}
	if state != want {
		t.Fatalf("ctrl+c changed state from %+v to %+v", want, state)
	}
}

func TestNewAlwaysStartsWithCleanStateAndRequestedScope(t *testing.T) {
	state := State{query: "old", searching: true, scroll: 9, scope: Global}
	state = New(Review)
	if state != (State{scope: Review}) {
		t.Fatalf("new review state = %+v", state)
	}
}
