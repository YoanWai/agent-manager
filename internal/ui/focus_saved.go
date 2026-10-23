package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// focusSaved is the saved prompts list the saved prompt key opens over a
// focused session. A pick is pasted into the agent's prompt, unsent. It is
// a surface: while it is on top the manager reads the keys, so esc and
// ctrl+c close it and nothing reaches the agent behind it.
type focusSaved struct {
	list   spawn.SavedList
	gen    uint64
	styles spawn.Styles
}

// focusSavedHost is what the list reaches on the root beyond the stack:
// the focused session, and the keys it shares with focus.
type focusSavedHost interface {
	keys() keybind.Table
	selected() (store.Session, bool)
	pasteFocused(sess store.Session, text string) tea.Cmd
	leaveFocus() tea.Cmd
}

// newFocusSaved is the one way to build the surface, so a surface on the
// stack always has the styles its card paints with.
func newFocusSaved(gen uint64, styles spawn.Styles) *focusSaved {
	return &focusSaved{list: spawn.OpenSavedList(nil, true, false), gen: gen, styles: styles}
}

func (m *Model) openFocusSaved() tea.Cmd {
	s := newFocusSaved(uint64(m.nextComposerGen()), m.spawnStyles())
	m.push(s)
	return savedPromptsLoadCmd(m.services.store, savedForFocus, s.gen, 0)
}

func (m *Model) loadFocusSaved(msg savedPromptsLoadedMsg) {
	if saved, ok := surfaceOf[*focusSaved](m); ok && saved.gen == msg.gen {
		saved.list.Load(spawnSaved(msg.rows), msg.err)
	}
}

// Key keeps the leave key working while the list is up, and the saved
// prompt key closes it again. Every other key is the list's.
func (s *focusSaved) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	keys := h.(focusSavedHost).keys()
	switch {
	case keys.Binding(keybind.Detach).Has(msg.String()):
		return true, h.(focusSavedHost).leaveFocus()
	case keys.Binding(keybind.SavedPrompt).Has(msg.String()):
		h.Pop()
		return true, nil
	}
	cmd, pick := s.list.Key(msg)
	return true, tea.Batch(cmd, s.apply(h, pick))
}

func (s *focusSaved) Click(h surfaceHost, line int) tea.Cmd {
	return s.apply(h, s.list.Click(line))
}

func (s *focusSaved) Wheel(_ surfaceHost, arrow tea.KeyMsg) tea.Cmd {
	cmd, _ := s.list.Key(arrow)
	return cmd
}

// View is the list's card, which takes the frame while it is up.
func (s *focusSaved) View(h surfaceHost, _ string) string {
	cols, _ := h.Size()
	width := min(48, max(28, cols-4))
	lines := s.list.Lines(cardInnerWidth(width), 8, s.styles)
	return h.CardSized(width, "saved prompts", strings.Join(lines, "\n"), [][2]string{{"↵", "paste"}, {"esc", "close"}})
}

func (s *focusSaved) Footer() [][2]string { return s.list.Hint("paste", "close") }

func (s *focusSaved) Owner() inputOwner { return ownerManager }

// Dismissible: the filter is a search, not a draft, so a stray click
// closes the list the way esc does.
func (s *focusSaved) Dismissible() bool { return true }

func (s *focusSaved) Leave(surfaceHost) {}

// apply takes the list off the stack once it has closed itself, on esc or a
// pick, and pastes a pick into the focused pane.
func (s *focusSaved) apply(h surfaceHost, pick spawn.SavedPick) tea.Cmd {
	if pick.Closed {
		h.Pop()
	}
	host := h.(focusSavedHost)
	sess, ok := host.selected()
	if !pick.Picked || !ok {
		return nil
	}
	return host.pasteFocused(sess, pick.Saved.Text)
}
