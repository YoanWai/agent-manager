package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	tea "github.com/charmbracelet/bubbletea"
)

// forkSurface names a fork of one session. It holds the captured source row
// the fork request carries, and the generation that fences its completion:
// a result only closes the card it was submitted from.
type forkSurface struct {
	uidialogs.Fork
	source store.Session
	gen    int
}

// forkHost is what the card reaches on the root beyond the stack.
type forkHost interface {
	pop()
	reportErr(text string)
	clearErr()
	submitFork(request forkRequest) tea.Cmd
	viewFork(s *forkSurface) string
}

func newForkSurface(source store.Session, gen int) *forkSurface {
	s := &forkSurface{source: source, gen: gen}
	s.Open(source.Name, source.Group)
	return s
}

func (m *Model) submitFork(request forkRequest) tea.Cmd {
	m.queueFork(request)
	return m.nextEffectCmd()
}

func (s *forkSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	host := h.(forkHost)
	key := s.Fork.Key(msg)
	switch key.Action {
	case uidialogs.ForkCancel:
		h.pop()
		host.clearErr()
	case uidialogs.ForkInvalid:
		host.reportErr(key.Err)
	case uidialogs.ForkSubmit:
		return true, host.submitFork(forkRequest{source: s.source, name: key.Name, gen: s.gen})
	}
	return true, key.Cmd
}

// Click and Wheel do nothing: one field, always focused, and its painted
// keys take the clicks.
func (s *forkSurface) Click(surfaceHost, int) tea.Cmd { return nil }

func (s *forkSurface) Wheel(surfaceHost, tea.KeyMsg) tea.Cmd { return nil }

func (s *forkSurface) View(h surfaceHost, _ string) string { return h.(forkHost).viewFork(s) }

func (s *forkSurface) Footer() [][2]string { return [][2]string{{"↵", "create"}, {"esc", "cancel"}} }

func (s *forkSurface) Owner() inputOwner { return ownerManager }

// Dismissible: the name is a draft, so a stray click or ctrl+c keeps it.
func (s *forkSurface) Dismissible() bool { return false }

func (s *forkSurface) Leave(surfaceHost) {}
