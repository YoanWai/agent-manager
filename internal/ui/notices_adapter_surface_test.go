package ui

import (
	"testing"

	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
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
	job := &uieffects.Job{Request: uinotices.DismissRequest{ID: uinotices.Welcome, ForegroundGen: m.gens.foreground, Modal: true}}

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

type errTest string

func (e errTest) Error() string { return string(e) }
