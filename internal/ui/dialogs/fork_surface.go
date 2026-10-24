package dialogs

import (
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// ForkSurface names a fork of one session. The source row the request
// carries is the root's, held by its Root; the generation fences the
// completion, so a result only closes the card it was submitted from.
type ForkSurface struct {
	Fork
	gen  int
	root ForkRoot
}

// ForkRoot is what the card reaches on the root beyond surface.Host.
type ForkRoot interface {
	ReportErr(text string)
	ClearErr()
	// Submit queues the fork of the source the card was opened on.
	Submit(name string, gen int) tea.Cmd
	Paint(form *Fork) string
}

func NewForkSurface(root ForkRoot, sourceName, sourceGroup string, gen int) *ForkSurface {
	s := &ForkSurface{gen: gen, root: root}
	s.Open(sourceName, sourceGroup)
	return s
}

func (s *ForkSurface) Gen() int { return s.gen }

// Root is the source the card holds.
func (s *ForkSurface) Root() ForkRoot { return s.root }

func (s *ForkSurface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	key := s.Fork.Key(msg)
	switch key.Action {
	case ForkCancel:
		h.Pop()
		s.root.ClearErr()
	case ForkInvalid:
		s.root.ReportErr(key.Err)
	case ForkSubmit:
		return true, s.root.Submit(key.Name, s.gen)
	}
	return true, key.Cmd
}

// Click and Wheel do nothing: one field, always focused, and its painted
// keys take the clicks.
func (s *ForkSurface) Click(surface.Host, int) tea.Cmd { return nil }

func (s *ForkSurface) Wheel(surface.Host, tea.KeyMsg) tea.Cmd { return nil }

func (s *ForkSurface) View(surface.Host, string) string { return s.root.Paint(&s.Fork) }

func (s *ForkSurface) Footer() [][2]string { return [][2]string{{"↵", "create"}, {"esc", "cancel"}} }

func (s *ForkSurface) Owner() surface.Owner { return surface.Manager }

// Dismissible: the name is a draft, so a stray click or ctrl+c keeps it.
func (s *ForkSurface) Dismissible() bool { return false }

func (s *ForkSurface) Leave(surface.Host) {}
