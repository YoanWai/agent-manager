package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// focusSaved is the saved prompts list the saved prompt key opens over a
// focused session. A pick is pasted into the agent's prompt, unsent. It is
// a surface: while it is on top the manager reads the keys, so ctrl+c and
// esc close it and nothing reaches the agent behind it.
type focusSaved struct {
	list spawn.SavedList
	gen  uint64
}

func (m *Model) openFocusSaved() tea.Cmd {
	s := &focusSaved{list: spawn.OpenSavedList(nil, true, false), gen: uint64(m.nextComposerGen())}
	m.pushSurface(s)
	return savedPromptsLoadCmd(m.services.store, savedForFocus, s.gen, 0)
}

// focusSavedOpen is the saved prompts list on the stack, if it is open.
func (m *Model) focusSavedOpen() (*focusSaved, bool) {
	for _, s := range m.surfaces {
		if saved, ok := s.(*focusSaved); ok {
			return saved, true
		}
	}
	return nil, false
}

func (m *Model) loadFocusSaved(msg savedPromptsLoadedMsg) {
	if saved, ok := m.focusSavedOpen(); ok && saved.gen == msg.gen {
		saved.list.Load(spawnSaved(msg.rows), msg.err)
	}
}

// Key keeps the leave key working while the list is up, and the saved
// prompt key closes it again. Every other key is the list's.
func (s *focusSaved) Key(m *Model, msg tea.KeyMsg) (bool, tea.Cmd) {
	switch {
	case m.services.keys.Binding(keybind.Detach).Has(msg.String()):
		return true, m.leaveFocus()
	case m.services.keys.Binding(keybind.SavedPrompt).Has(msg.String()):
		m.popSurface()
		return true, nil
	}
	cmd, pick := s.list.Key(msg)
	return true, tea.Batch(cmd, m.applyFocusSavedPick(pick))
}

func (s *focusSaved) Click(m *Model, line int) tea.Cmd {
	return m.applyFocusSavedPick(s.list.Click(line))
}

func (s *focusSaved) Wheel(_ *Model, arrow tea.KeyMsg) tea.Cmd {
	cmd, _ := s.list.Key(arrow)
	return cmd
}

// View is the list's card, which takes the frame while it is up.
func (s *focusSaved) View(m *Model, _ string) string {
	width := min(48, max(28, m.layout.width-4))
	lines := s.list.Lines(cardInnerWidth(width), 8, m.spawnStyles())
	return m.cardSized(width, "saved prompts", strings.Join(lines, "\n"), [][2]string{{"↵", "paste"}, {"esc", "close"}})
}

func (s *focusSaved) Footer() [][2]string { return s.list.Hint("paste", "close") }

func (s *focusSaved) Owner() inputOwner { return ownerManager }

// applyFocusSavedPick takes the list off the stack once it has closed
// itself, on esc or a pick, and pastes a pick into the focused pane.
func (m *Model) applyFocusSavedPick(pick spawn.SavedPick) tea.Cmd {
	if pick.Closed {
		m.popSurface()
	}
	sess, ok := m.selected()
	if !pick.Picked || !ok || m.mode != modeFocus {
		return nil
	}
	m.dispatchInput(inputRequest{kind: inputPaste, session: sess, text: pick.Saved.Text, echo: true})
	return m.nextEffectCmd()
}
