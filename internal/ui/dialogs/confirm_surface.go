package dialogs

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// ConfirmSurface is the yes/no card in front of a lifecycle call, a remote
// call or a connection removal. The request the answer runs is the root's,
// held by its Root, so popping the card is dropping the request.
type ConfirmSurface struct {
	prompt    Confirm
	glyphMode presentation.GlyphMode
	root      ConfirmRoot
}

// ConfirmRoot is what the card reaches on the root beyond surface.Host.
type ConfirmRoot interface {
	// Paint draws the shared yes/no card, which Settings' reset draws too.
	Paint(title, question, consequence string, destructive bool, answer string) string
	// Run runs the request the card was opened for.
	Run() tea.Cmd
}

func NewConfirmSurface(root ConfirmRoot, prompt Confirm, glyphMode presentation.GlyphMode) *ConfirmSurface {
	return &ConfirmSurface{prompt: prompt, glyphMode: glyphMode, root: root}
}

// Prompt is the question the card asks.
func (s *ConfirmSurface) Prompt() Confirm { return s.prompt }

// Root is the request the card holds.
func (s *ConfirmSurface) Root() ConfirmRoot { return s.root }

func (s *ConfirmSurface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	switch s.prompt.Key(msg) {
	case ConfirmCancel:
		h.Pop()
	case ConfirmAccept:
		h.Pop()
		return true, s.root.Run()
	}
	return true, nil
}

// Click does nothing: the painted keys take the clicks.
func (s *ConfirmSurface) Click(surface.Host, int) tea.Cmd { return nil }

func (s *ConfirmSurface) Wheel(surface.Host, tea.KeyMsg) tea.Cmd { return nil }

func (s *ConfirmSurface) View(surface.Host, string) string {
	question, consequence := SplitLabel(s.prompt.Label)
	return s.root.Paint(s.prompt.Title(s.glyphMode), question, consequence, s.prompt.Destructive(), s.prompt.Answer())
}

func (s *ConfirmSurface) Footer() [][2]string { return ConfirmHints(s.prompt.Answer()) }

func (s *ConfirmSurface) Owner() surface.Owner { return surface.Manager }

// Dismissible: a stray click or ctrl+c is the cancel, there is no draft.
func (s *ConfirmSurface) Dismissible() bool { return true }

func (s *ConfirmSurface) Leave(surface.Host) {}
