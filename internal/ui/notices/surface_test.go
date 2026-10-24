package notices

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
)

// The modal asks the root for the list, the frame and the work its keys
// start; a key it takes is the user moving on.
type stubRoot struct {
	notices     []Notice
	dismissed   string
	foregrounds int
}

func (r *stubRoot) ActiveNotices() []Notice { return r.notices }

func (r *stubRoot) NoticesFrame() Frame { return Frame{Width: 100, Height: 40} }

func (r *stubRoot) RefreshNotices() tea.Cmd { return nil }

func (r *stubRoot) StartUpdate() tea.Cmd { return nil }

func (r *stubRoot) DismissNotice(id string) { r.dismissed = id }

func (r *stubRoot) PaintNoticesCard([]string, []presentation.PaintedKey) string { return "card" }

func (r *stubRoot) TakeForeground() { r.foregrounds++ }

func (r *stubRoot) OpenLink(string) tea.Cmd { return nil }

type stubHost struct{ pops int }

func (h *stubHost) CardSized(int, string, string, [][2]string) string { return "" }

func (h *stubHost) Size() (int, int) { return 100, 40 }

func (h *stubHost) Pop() { h.pops++ }

func TestSurfaceTakesAKeyAsTheUserMovingOn(t *testing.T) {
	root := &stubRoot{}
	host := &stubHost{}
	surface := NewSurface(root, NewPanel(nil), "")
	if handled, _ := surface.Key(host, key("esc")); !handled {
		t.Fatal("the messages modal must consume every key")
	}
	if root.foregrounds != 1 {
		t.Fatalf("a key reached the root %d times, want it to fence a completion once", root.foregrounds)
	}
	if host.pops != 1 {
		t.Fatalf("esc popped %d times, want the card closed", host.pops)
	}
}
