package focus

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// Saved is the saved prompts list the saved prompt key opens over a
// focused session. A pick is pasted into the agent's prompt, unsent. It is
// a surface: while it is on top the manager reads the keys, so esc and
// ctrl+c close it and nothing reaches the agent behind it.
type Saved struct {
	list   spawn.SavedList
	gen    uint64
	styles spawn.Styles
	root   SavedRoot
}

// SavedRoot is what the list reaches on the root beyond surface.Host:
// the focused session, and the keys it shares with focus.
type SavedRoot interface {
	// Keys is the live key tables, for the leave key and the saved
	// prompt key that closes the list again.
	Keys() keybind.Table
	// InnerWidth is the body columns of a card width wide.
	InnerWidth(width int) int
	// PasteFocused pastes a picked prompt into the focused session's
	// prompt, unsent.
	PasteFocused(pick spawn.SavedPick) tea.Cmd
	LeaveFocus() tea.Cmd
}

// OpenSaved is the one way to build the surface, so a surface on the stack
// always has the styles its card paints with. The root queues the catalog
// read for Gen and hands it to Load.
func OpenSaved(root SavedRoot, gen uint64, styles spawn.Styles) *Saved {
	return &Saved{list: spawn.OpenSavedList(nil, true, false), gen: gen, styles: styles, root: root}
}

// Gen is the generation the catalog read was asked with, so a result lands
// on the list that asked.
func (s *Saved) Gen() uint64 { return s.gen }

// Load is the catalog as one read found it.
func (s *Saved) Load(rows []spawn.Saved, err error) { s.list.Load(rows, err) }

// Key keeps the leave key working while the list is up, and the saved
// prompt key closes it again. Every other key is the list's.
func (s *Saved) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	keys := s.root.Keys()
	switch {
	case keys.Binding(keybind.Detach).Has(msg.String()):
		return true, s.root.LeaveFocus()
	case keys.Binding(keybind.SavedPrompt).Has(msg.String()):
		h.Pop()
		return true, nil
	}
	cmd, pick := s.list.Key(msg)
	return true, tea.Batch(cmd, s.apply(h, pick))
}

func (s *Saved) Click(h surface.Host, line int) tea.Cmd {
	return s.apply(h, s.list.Click(line))
}

func (s *Saved) Wheel(_ surface.Host, arrow tea.KeyMsg) tea.Cmd {
	cmd, _ := s.list.Key(arrow)
	return cmd
}

// View is the list's card, which takes the frame while it is up.
func (s *Saved) View(h surface.Host, _ string) string {
	cols, _ := h.Size()
	width := min(48, max(28, cols-4))
	lines := s.list.Lines(s.root.InnerWidth(width), 8, s.styles)
	return h.CardSized(width, "saved prompts", strings.Join(lines, "\n"), [][2]string{{"↵", "paste"}, {"esc", "close"}})
}

func (s *Saved) Footer() [][2]string { return s.list.Hint("paste", "close") }

func (s *Saved) Owner() surface.Owner { return surface.Manager }

// Dismissible: the filter is a search, not a draft, so a stray click
// closes the list the way esc does.
func (s *Saved) Dismissible() bool { return true }

func (s *Saved) Leave(surface.Host) {}

// apply takes the list off the stack once it has closed itself, on esc or a
// pick, and pastes a pick into the focused pane.
func (s *Saved) apply(h surface.Host, pick spawn.SavedPick) tea.Cmd {
	if pick.Closed {
		h.Pop()
	}
	return s.root.PasteFocused(pick)
}
