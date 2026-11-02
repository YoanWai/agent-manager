package notices

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// Surface is the messages modal over the list. It owns the cursor, the body
// scroll and the rows it painted for clicks; the notices themselves, and the
// dismissed and seen sets they are built with, stay on the root, since the
// footer's unread mark reads them with the modal closed.
type Surface struct {
	panel Panel
	root  Root
}

// Root is what the modal reads and asks of the root beyond surface.Host: the
// list it shows, the terminal it is painted for, the work its keys start, the
// card it floats on the backdrop and the link it opens.
type Root interface {
	ActiveNotices() []Notice
	NoticesFrame() Frame
	RefreshNotices() tea.Cmd
	StartUpdate() tea.Cmd
	// MarkNotice dismisses a notice, or marks the one the panel shows read.
	MarkNotice(mark Mark)
	// PaintNoticesCard floats the card and records its body and the keys on
	// its bottom edge for clicks.
	PaintNoticesCard(card []string, foot []presentation.PaintedKey) string
	// TakeForeground marks a key as the user moving on, which fences a
	// failed dismissal from reopening the card behind their back.
	TakeForeground()
	OpenLink(url string) tea.Cmd
}

// Mark is what the user did to a notice: read it by selecting it, or
// dismiss it for good.
type Mark struct {
	ID      string
	Dismiss bool
}

// NewSurface copies the root's panel for its dismissed and seen sets, which
// the copy shares, and opens it on the notice with id, or the first.
func NewSurface(root Root, base Panel, id string) *Surface {
	s := &Surface{panel: base, root: root}
	s.panel.Open(root.ActiveNotices(), id)
	s.seeSelected()
	return s
}

// Panel is the modal's own state, which the root keeps a selection across a
// list change with.
func (s *Surface) Panel() *Panel { return &s.panel }

func (s *Surface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	s.root.TakeForeground()
	cmd := s.apply(h, s.panel.Key(msg, s.root.ActiveNotices(), s.root.NoticesFrame()))
	s.seeSelected()
	return true, cmd
}

func (s *Surface) Click(h surface.Host, line int) tea.Cmd { return s.ClickAt(h, 0, line) }

// ClickAt takes the column too: a split inbox shows the message beside the
// list, on the same rows.
func (s *Surface) ClickAt(h surface.Host, col, line int) tea.Cmd {
	cmd := s.apply(h, s.panel.Click(col, line, s.root.ActiveNotices(), s.root.NoticesFrame()))
	s.seeSelected()
	return cmd
}

// seeSelected reads the list again, since a dismissal changes it.
func (s *Surface) seeSelected() {
	s.root.MarkNotice(Mark{ID: s.panel.Selected(s.root.ActiveNotices())})
}

func (s *Surface) Wheel(_ surface.Host, arrow tea.KeyMsg) tea.Cmd {
	step := 1
	if arrow.Type == tea.KeyUp {
		step = -1
	}
	s.panel.Wheel(step, s.root.ActiveNotices(), s.root.NoticesFrame())
	return nil
}

// View floats the card on the backdrop; the frame below is not shown.
func (s *Surface) View(surface.Host, string) string {
	notices, frame := s.root.ActiveNotices(), s.root.NoticesFrame()
	return s.root.PaintNoticesCard(s.panel.View(notices, frame), s.panel.Foot(notices, frame))
}

// Footer is empty: the card names its keys on its bottom edge.
func (s *Surface) Footer() [][2]string { return nil }

func (s *Surface) Owner() surface.Owner { return surface.Manager }

func (s *Surface) Dismissible() bool { return true }

// Leave has nothing to undo: the list and the dismissed set live on the root.
func (s *Surface) Leave(surface.Host) {}

func (s *Surface) apply(h surface.Host, out Outcome) tea.Cmd {
	switch out.Action {
	case Refresh:
		return s.root.RefreshNotices()
	case StartUpdate:
		return s.root.StartUpdate()
	case OpenLink:
		return s.root.OpenLink(out.URL)
	case Dismiss:
		s.root.MarkNotice(Mark{ID: out.ID, Dismiss: true})
		if out.Close {
			h.Pop()
		}
	case Close:
		h.Pop()
	}
	return nil
}
