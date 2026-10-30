package notices

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// Surface is the messages modal over the list. It owns the cursor, the body
// scroll and the rows it painted for clicks; the notices themselves, and the
// dismissed set they are filtered by, stay on the root, since the footer's
// unread mark reads them with the modal closed.
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
	DismissNotice(id string)
	// PaintNoticesCard floats the card and records its body and the keys on
	// its bottom edge for clicks.
	PaintNoticesCard(card []string, foot []presentation.PaintedKey) string
	// TakeForeground marks a key as the user moving on, which fences a
	// failed dismissal from reopening the card behind their back.
	TakeForeground()
	OpenLink(url string) tea.Cmd
}

// NewSurface copies the root's panel for its dismissed set, which the copy
// shares, and opens it on the notice with id, or the first.
func NewSurface(root Root, base Panel, id string) *Surface {
	s := &Surface{panel: base, root: root}
	s.panel.Open(root.ActiveNotices(), id)
	return s
}

// Panel is the modal's own state, which the root keeps a selection across a
// list change with.
func (s *Surface) Panel() *Panel { return &s.panel }

func (s *Surface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	s.root.TakeForeground()
	return true, s.apply(h, s.panel.Key(msg, s.root.ActiveNotices(), s.root.NoticesFrame()))
}

func (s *Surface) Click(h surface.Host, line int) tea.Cmd {
	return s.apply(h, s.panel.Click(line, s.root.ActiveNotices(), s.root.NoticesFrame()))
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
		s.root.DismissNotice(out.ID)
		if out.Close {
			h.Pop()
		}
	case Close:
		h.Pop()
	}
	return nil
}
