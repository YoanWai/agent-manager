package ui

import (
	"testing"

	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
)

func noticesOverList(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	// The real trigger: the messages key on the list.
	m.Update(runeKey("M"))
	if !m.noticesOpen() {
		t.Fatal("M did not open the messages modal")
	}
	return m
}

func TestNoticesSurfaceOpensOverTheList(t *testing.T) {
	m := noticesOverList(t)
	if !m.onList() || len(m.surfaces) != 1 {
		t.Fatalf("screen = %v, surfaces = %d", m.screenID(), len(m.surfaces))
	}
	m.openNotices("")
	if len(m.surfaces) != 1 {
		t.Fatalf("opening an open panel stacked another: %d surfaces", len(m.surfaces))
	}
}

func TestNoticesSurfaceClosesOnEscAndBackdrop(t *testing.T) {
	if !(&uinotices.Surface{}).Dismissible() {
		t.Fatal("the messages modal must be dismissible")
	}
	for name, closeIt := range map[string]func(*Model) *Model{
		"esc": func(m *Model) *Model { m.Update(namedKey(tea.KeyEsc)); return m },
		"click off the card": func(m *Model) *Model {
			return clickBackdrop(t, m)
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := closeIt(noticesOverList(t))
			if m.noticesOpen() || m.effects.Quitting {
				t.Fatalf("modal open = %t, quitting = %t", m.noticesOpen(), m.effects.Quitting)
			}
		})
	}
}

// A dismissal that fails to persist reopens the modal only while nothing
// else is on screen; a card the user opened since owns the screen.
func TestNoticesSurfaceFencesAStaleDismissalCompletion(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.layout.width, m.layout.height = 120, 34
	m.openNotices(uinotices.Welcome)
	job := &uieffects.Job{Request: noticeDismissRequest{id: uinotices.Welcome, foregroundGen: m.gens.foreground, modal: true}}

	m.Pop()
	m.openHelp()
	m.applyNoticeDismiss(job, errTest("persist failed"))
	if m.noticesOpen() {
		t.Fatal("a failed dismissal opened the modal over another card")
	}

	m.Pop()
	m.applyNoticeDismiss(job, errTest("persist failed"))
	if !m.noticesOpen() {
		t.Fatal("a failed dismissal did not reopen the modal on a clear list")
	}
}

// The modal asks the root for the list, the frame and the work its keys
// start; a key it takes is the user moving on.
type stubNoticesRoot struct {
	notices     []uinotices.Notice
	dismissed   string
	foregrounds int
}

func (r *stubNoticesRoot) ActiveNotices() []uinotices.Notice { return r.notices }

func (r *stubNoticesRoot) NoticesFrame() uinotices.Frame {
	return uinotices.Frame{Width: 100, Height: 40}
}

func (r *stubNoticesRoot) RefreshNotices() tea.Cmd { return nil }

func (r *stubNoticesRoot) StartUpdate() tea.Cmd { return nil }

func (r *stubNoticesRoot) DismissNotice(id string) { r.dismissed = id }

func (r *stubNoticesRoot) PaintNoticesCard([]string, []presentation.PaintedKey) string {
	return "card"
}

func (r *stubNoticesRoot) TakeForeground() { r.foregrounds++ }

func (r *stubNoticesRoot) OpenLink(string) tea.Cmd { return nil }

type stubSurfaceHost struct{ pops int }

func (h *stubSurfaceHost) CardSized(int, string, string, [][2]string) string { return "" }

func (h *stubSurfaceHost) Size() (int, int) { return 100, 40 }

func (h *stubSurfaceHost) Pop() { h.pops++ }

func TestNoticesSurfaceTakesAKeyAsTheUserMovingOn(t *testing.T) {
	root := &stubNoticesRoot{}
	h := &stubSurfaceHost{}
	s := uinotices.NewSurface(root, uinotices.NewPanel(nil), "")
	if handled, _ := s.Key(h, namedKey(tea.KeyEsc)); !handled {
		t.Fatal("the messages modal must consume every key")
	}
	if root.foregrounds != 1 {
		t.Fatalf("a key reached the root %d times, want it to fence a completion once", root.foregrounds)
	}
	if h.pops != 1 {
		t.Fatalf("esc popped %d times, want the card closed", h.pops)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
