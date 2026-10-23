package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// focusSaved is the saved prompts list the saved prompt key opens over a
// focused session. A pick is pasted into the agent's prompt, unsent.
type focusSaved struct {
	list spawn.SavedList
	gen  uint64
}

func (m *Model) openFocusSaved() tea.Cmd {
	m.focus.saved = focusSaved{list: spawn.OpenSavedList(nil, true, false), gen: uint64(m.nextComposerGen())}
	return savedPromptsLoadCmd(m.services.store, savedForFocus, m.focus.saved.gen, 0)
}

func (m *Model) loadFocusSaved(msg savedPromptsLoadedMsg) {
	saved := &m.focus.saved
	if m.mode == modeFocus && saved.list.Open() && saved.gen == msg.gen {
		saved.list.Load(spawnSaved(msg.rows), msg.err)
	}
}

func (m *Model) closeFocusSaved() { m.focus.saved = focusSaved{} }

// handleFocusSavedKey keeps the leave key working while the list is up,
// and the saved prompt key closes it again.
func (m *Model) handleFocusSavedKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.services.keys.Binding(keybind.Detach).Has(msg.String()):
		m.closeFocusSaved()
		return m, m.leaveFocus()
	case m.services.keys.Binding(keybind.SavedPrompt).Has(msg.String()):
		m.closeFocusSaved()
		return m, nil
	}
	cmd, pick := m.focus.saved.list.Key(msg)
	return m, tea.Batch(cmd, m.pasteFocusSaved(pick))
}

func (m *Model) handleFocusSavedClick(line int) tea.Cmd {
	return m.pasteFocusSaved(m.focus.saved.list.Click(line))
}

func (m *Model) pasteFocusSaved(pick spawn.SavedPick) tea.Cmd {
	sess, ok := m.selected()
	if !pick.Picked || !ok || m.mode != modeFocus {
		return nil
	}
	m.dispatchInput(inputRequest{kind: inputPaste, session: sess, text: pick.Saved.Text, echo: true})
	return m.nextEffectCmd()
}

// viewFocusSaved is the list's card, which takes the frame while it is up.
func (m *Model) viewFocusSaved() string {
	width := min(48, max(28, m.layout.width-4))
	lines := m.focus.saved.list.Lines(cardInnerWidth(width), 8, m.spawnStyles())
	return m.cardSized(width, "saved prompts", strings.Join(lines, "\n"), [][2]string{{"↵", "paste"}, {"esc", "close"}})
}
