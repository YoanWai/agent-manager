package help

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Surface is the key map on the root's stack. It opens over whatever screen
// is underneath, the list or Review, and closing pops back to it, so nothing
// remembers where it came from.
type Surface struct {
	state State
	root  Root
}

// Root is what the key map reads live from the root beyond surface.Host:
// the copied tables, glyphs and styles it paints from, the card's geometry,
// and the link and the fence only the root runs.
type Root interface {
	// Reads is the presentation state, which Context copies for a body
	// width columns wide.
	Reads
	// InnerWidth is the body columns of a card width wide.
	InnerWidth(width int) int
	// HintRows is the rows hints take at the foot of a body width wide.
	HintRows(hints [][2]string, width int) int
	// StatusShown is whether the status bar has a message, which the card
	// makes room for.
	StatusShown() bool
	// TakeForeground marks a key as the user moving on, as every key the
	// root routes does, which fences a completion started before it.
	TakeForeground()
	OpenDocs() tea.Cmd
}

// Card is the key map's card as it paints: its content, its width, and the
// body rows the window shows.
type Card struct {
	Content Content
	Width   int
	Rows    int
	subtle  lipgloss.Style
}

func NewSurface(root Root, scope Scope) *Surface {
	return &Surface{state: New(scope), root: root}
}

// State is the key map's search, scroll and scope.
func (s *Surface) State() *State { return &s.state }

func (s *Surface) Card(h surface.Host) Card {
	cols, rows := h.Size()
	width := CardWidth(cols)
	inner := s.root.InnerWidth(width)
	ctx := ContextFor(s.root, inner)
	content := s.state.Content(ctx)
	room := rows - 5 - s.root.HintRows(content.Hints, inner) - len(content.Head)
	if s.root.StatusShown() {
		room -= 2
	}
	return Card{Content: content, Width: width, Rows: max(room, 1), subtle: ctx.Styles.Subtle}
}

func (s *Surface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	s.root.TakeForeground()
	return true, s.step(h, msg)
}

func (s *Surface) step(h surface.Host, msg tea.KeyMsg) tea.Cmd {
	card := s.Card(h)
	switch s.state.Update(msg, Viewport{Rows: card.Rows, Lines: len(card.Content.Lines)}) {
	case OpenDocs:
		return s.root.OpenDocs()
	case Close:
		h.Pop()
	}
	return nil
}

func (s *Surface) Click(_ surface.Host, line int) tea.Cmd {
	s.state.Click(line)
	return nil
}

func (s *Surface) Wheel(h surface.Host, arrow tea.KeyMsg) tea.Cmd {
	return s.step(h, arrow)
}

func (s *Surface) View(h surface.Host, _ string) string {
	card := s.Card(h)
	lines := append([]string(nil), card.Content.Head...)
	lines = append(lines, presentation.Window(card.Content.Lines, card.Rows, card.Content.Offset, card.subtle)...)
	return h.CardSized(card.Width, card.Content.Title, strings.Join(lines, "\n"), card.Content.Hints)
}

// Footer is empty: the card names its own keys on its bottom edge.
func (s *Surface) Footer() [][2]string { return nil }

func (s *Surface) Owner() surface.Owner { return surface.Manager }

func (s *Surface) Dismissible() bool { return true }

// Leave has nothing to undo. The screen underneath stays, so Review's
// loader never stopped while the card covered it.
func (s *Surface) Leave(surface.Host) {}
