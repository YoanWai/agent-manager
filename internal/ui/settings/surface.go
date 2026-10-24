package settings

import (
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// Surface is the Settings card on the root's stack. It owns the dialog; the
// cache, the pending count and the generation stay on the root because the
// spawn form and the quick bar read the cache too. Closing it by esc, the
// save button or ctrl+c is the same pop, and Leave queues the save; the root
// starts it.
type Surface struct {
	state State
	root  Root
}

// Root is what the Settings card reads and does on the root beyond
// surface.Host: the live key tables and update state a key reads, the work a
// key asks for, what closing saves and the card chrome.
type Root interface {
	// Context is what a key reads live: the tools, the cached hidden CLIs
	// and default tool, and the key tables.
	Context() Context
	// Apply runs what a key asked beyond the dialog's own state and says
	// whether the card closes.
	Apply(d *State, result Result) (tea.Cmd, bool)
	Leave(d *State)
	Paint(d *State) string
}

func NewSurface(root Root, state State) *Surface {
	return &Surface{state: state, root: root}
}

func (s *Surface) State() *State { return &s.state }

func (s *Surface) SetState(state State) { s.state = state }

func (s *Surface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	return true, s.apply(h, s.state.Key(msg, s.root.Context()))
}

// ClickAt is a press at col of the body's painted line: the save button and
// the symbols row act over their own text.
func (s *Surface) ClickAt(h surface.Host, col, line int) tea.Cmd {
	return s.apply(h, s.state.Click(col, line, s.root.Context()))
}

func (s *Surface) Click(h surface.Host, line int) tea.Cmd { return s.ClickAt(h, 0, line) }

func (s *Surface) Wheel(h surface.Host, arrow tea.KeyMsg) tea.Cmd {
	step := 1
	if arrow.Type == tea.KeyUp {
		step = -1
	}
	return s.apply(h, s.state.Wheel(step, s.root.Context()))
}

func (s *Surface) apply(h surface.Host, result Result) tea.Cmd {
	cmd, closes := s.root.Apply(&s.state, result)
	if !closes {
		return cmd
	}
	h.Pop()
	return nil
}

func (s *Surface) View(surface.Host, string) string { return s.root.Paint(&s.state) }

// Footer is empty: the card paints its own hints.
func (s *Surface) Footer() [][2]string { return nil }

func (s *Surface) Owner() surface.Owner { return surface.Manager }

// Dismissible: a command or a prompt being typed is a draft, so a stray
// click or ctrl+c keeps it.
func (s *Surface) Dismissible() bool {
	return !s.state.Editor.Typing() && !s.state.Prompts.Editing()
}

func (s *Surface) Leave(surface.Host) { s.root.Leave(&s.state) }
