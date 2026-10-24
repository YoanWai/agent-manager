package connections

import (
	"errors"

	remote "github.com/YoanWai/agent-manager/internal/remote/value"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// Surface is the add and edit SSH connection card. It owns the dialog and
// the connections its name must not collide with, read when it opens; the
// root runs the save on its effect lane.
type Surface struct {
	dialog Dialog
	others []remote.Connection
	gen    uint64
	root   Root
}

// Root is what the card reaches on the root beyond surface.Host: the tones
// it paints from, its width, and the save and status line only the root runs.
type Root interface {
	// Look is the field painter, the cursor-marked input and the muted tone
	// the dialog paints with.
	Look() DialogView
	// CardWidth is the width of a card on a terminal cols wide.
	CardWidth(cols int) int
	// Save queues the write of a connection the dialog validated: it adds
	// one when editing names none, else updates the one it names.
	Save(gen uint64, editing string, next remote.Connection) tea.Cmd
	// Report shows a refusal on the status line. A nil error clears the one
	// a cancel is leaving behind.
	Report(err error) tea.Cmd
}

func NewSurface(root Root, editing remote.Connection, others []remote.Connection, gen uint64) *Surface {
	s := &Surface{others: others, gen: gen, root: root}
	s.dialog.Open(editing)
	return s
}

// Gen tells this card from the one opened before it, so a save that
// completes late cannot close a card the user reopened.
func (s *Surface) Gen() uint64 { return s.gen }

func (s *Surface) Dialog() *Dialog { return &s.dialog }

func (s *Surface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	// A pasted run is text: without this a pasted "tab" is the focus key.
	if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Alt {
		msg.Paste = true
	}
	key := s.dialog.Key(msg, s.others)
	switch key.Action {
	case DialogCancel:
		h.Pop()
		return true, s.root.Report(nil)
	case DialogRefused:
		return true, s.root.Report(errors.New(key.Refusal))
	case DialogSave:
		return true, tea.Batch(key.Cmd, s.root.Save(s.gen, key.Editing, key.Next))
	}
	return true, key.Cmd
}

func (s *Surface) Click(_ surface.Host, line int) tea.Cmd {
	s.dialog.Click(line)
	return nil
}

// Wheel: three fields and nothing to scroll.
func (s *Surface) Wheel(surface.Host, tea.KeyMsg) tea.Cmd { return nil }

func (s *Surface) View(h surface.Host, _ string) string {
	card := s.dialog.View(s.root.Look())
	cols, _ := h.Size()
	return h.CardSized(s.root.CardWidth(cols), card.Title, card.Body, card.Hints)
}

// Footer is empty: the card paints its own hints.
func (s *Surface) Footer() [][2]string { return nil }

func (s *Surface) Owner() surface.Owner { return surface.Manager }

// Dismissible: the fields hold a draft, so a stray click keeps it.
func (s *Surface) Dismissible() bool { return false }

func (s *Surface) Leave(surface.Host) {}
