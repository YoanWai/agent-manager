package ui

import (
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
)

// noticesSurface is the messages modal over the list. It owns the cursor,
// the body scroll and the rows it painted for clicks; the notices
// themselves, and the dismissed set they are filtered by, stay on the root,
// since the footer's unread mark reads them with the modal closed.
type noticesSurface struct {
	panel uinotices.Panel
	root  noticesHost
}

// noticesHost is what the modal reads and asks of the root beyond
// surfaceHost: the list it shows, the terminal it is painted for, the work
// its keys start, and the card it floats on the backdrop.
type noticesHost interface {
	activeNotices() []uinotices.Notice
	noticesFrame() uinotices.Frame
	refreshNotices() tea.Cmd
	startUpdate() tea.Cmd
	dismissNotice(id string)
	// paintNoticesCard floats the card and records its body and the keys on
	// its bottom edge for clicks.
	paintNoticesCard(card []string, foot []presentation.PaintedKey) string
	// takeForeground marks a key as the user moving on, which fences a
	// failed dismissal from reopening the card behind their back.
	takeForeground()
}

// newNoticesSurface copies the root's panel for its dismissed set, which the
// copy shares, and opens it on the notice with id, or the first.
func newNoticesSurface(root noticesHost, base uinotices.Panel, id string) *noticesSurface {
	s := &noticesSurface{panel: base, root: root}
	s.panel.Open(root.activeNotices(), id)
	return s
}

func (s *noticesSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	s.root.takeForeground()
	return true, s.apply(h, s.panel.Key(msg, s.root.activeNotices(), s.root.noticesFrame()))
}

func (s *noticesSurface) Click(h surfaceHost, line int) tea.Cmd {
	return s.apply(h, s.panel.Click(line, s.root.activeNotices(), s.root.noticesFrame()))
}

func (s *noticesSurface) Wheel(_ surfaceHost, arrow tea.KeyMsg) tea.Cmd {
	step := 1
	if arrow.Type == tea.KeyUp {
		step = -1
	}
	s.panel.Wheel(step, s.root.activeNotices(), s.root.noticesFrame())
	return nil
}

// View floats the card on the backdrop; the frame below is not shown.
func (s *noticesSurface) View(surfaceHost, string) string {
	notices, frame := s.root.activeNotices(), s.root.noticesFrame()
	return s.root.paintNoticesCard(s.panel.View(notices, frame), s.panel.Foot(notices, frame))
}

// Footer is empty: the card names its keys on its bottom edge.
func (s *noticesSurface) Footer() [][2]string { return nil }

func (s *noticesSurface) Owner() inputOwner { return ownerManager }

func (s *noticesSurface) Dismissible() bool { return true }

func (s *noticesSurface) Leave(surfaceHost) {}

func (s *noticesSurface) apply(h surfaceHost, out uinotices.Outcome) tea.Cmd {
	switch out.Action {
	case uinotices.Refresh:
		return s.root.refreshNotices()
	case uinotices.StartUpdate:
		return s.root.startUpdate()
	case uinotices.OpenLink:
		return openLink(out.URL)
	case uinotices.Dismiss:
		s.root.dismissNotice(out.ID)
		if out.Close {
			h.pop()
		}
	case uinotices.Close:
		h.pop()
	}
	return nil
}
