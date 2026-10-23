package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// focusRoot is the root as a focused session reads it: the key it forwards
// to the agent, the mouse over the pane, and the fence a key sets.
type focusRoot struct{ *Model }

func (r focusRoot) Key(msg tea.KeyMsg) tea.Cmd {
	_, cmd := r.handleFocusKey(msg)
	return cmd
}

func (r focusRoot) Mouse(msg tea.MouseMsg) tea.Cmd {
	_, cmd := r.focusMouse(msg)
	return cmd
}

func (r focusRoot) TakeForeground() { r.takeForeground() }

// focusSavedRoot is the root as the saved prompts list reads it: the live
// key tables, the card's body width, and the paste a pick makes.
type focusSavedRoot struct{ *Model }

func (r focusSavedRoot) Keys() keybind.Table { return r.keys() }

func (r focusSavedRoot) InnerWidth(width int) int { return cardInnerWidth(width) }

// PasteFocused pastes a pick into the selected session, which is the
// focused one while the list is up.
func (r focusSavedRoot) PasteFocused(pick spawn.SavedPick) tea.Cmd {
	sess, ok := r.selected()
	if !ok || !pick.Picked {
		return nil
	}
	return r.pasteFocused(sess, pick.Saved.Text)
}

func (r focusSavedRoot) LeaveFocus() tea.Cmd { return r.leaveFocus() }

func newFocusSurface(m *Model) *uifocus.Surface { return uifocus.NewSurface(focusRoot{m}) }

// focused is whether a session is focused, with or without a card over it.
func (m *Model) focused() bool {
	_, ok := surfaceOf[*uifocus.Surface](m)
	return ok
}

func (m *Model) openFocusSaved() tea.Cmd {
	s := uifocus.OpenSaved(focusSavedRoot{m}, uint64(m.nextComposerGen()), m.spawnStyles())
	m.push(s)
	return savedPromptsLoadCmd(m.services.store, savedForFocus, s.Gen(), 0)
}

func (m *Model) loadFocusSaved(msg savedPromptsLoadedMsg) {
	if saved, ok := surfaceOf[*uifocus.Saved](m); ok && saved.Gen() == msg.gen {
		saved.Load(spawnSaved(msg.rows), msg.err)
	}
}

var (
	_ uifocus.Root      = focusRoot{}
	_ uifocus.SavedRoot = focusSavedRoot{}
)
