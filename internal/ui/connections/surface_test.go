package connections

import (
	"testing"

	remote "github.com/YoanWai/agent-manager/internal/remote/value"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// stubRoot is the root as the card reaches it, so the card is driven here
// without one.
type stubRoot struct {
	width  int
	gen    uint64
	next   remote.Connection
	saved  bool
	report error
}

func (r *stubRoot) Look() DialogView {
	return DialogView{
		Field: func(label, value string, _ bool) string { return label + " " + value },
		Input: func(in textinput.Model) string { return in.Value() },
		Muted: lipgloss.NewStyle(),
	}
}

func (r *stubRoot) CardWidth(int) int { return r.width }

func (r *stubRoot) Save(gen uint64, _ string, next remote.Connection) tea.Cmd {
	r.gen, r.next, r.saved = gen, next, true
	return nil
}

func (r *stubRoot) Report(err error) tea.Cmd {
	r.report = err
	return nil
}

// stubHost is the stack as the card reads it: a size and a pop.
type stubHost struct{ popped bool }

func (h *stubHost) CardSized(int, string, string, [][2]string) string { return "" }
func (h *stubHost) Size() (int, int)                                  { return 100, 40 }
func (h *stubHost) Pop()                                              { h.popped = true }

func TestSurfaceSavesAValidatedConnection(t *testing.T) {
	root := &stubRoot{width: 64}
	s := NewSurface(root, remote.Connection{}, nil, 3)
	host := &stubHost{}
	for _, r := range []struct{ key tea.KeyMsg }{
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("box")}},
		{tea.KeyMsg{Type: tea.KeyTab}},
		{tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("me@box")}},
	} {
		s.Key(host, r.key)
	}
	handled, _ := s.Key(host, tea.KeyMsg{Type: tea.KeyEnter})
	if !handled || host.popped {
		t.Fatalf("handled = %t, popped = %t: enter must ask the root and leave the card up", handled, host.popped)
	}
	if !root.saved || root.gen != 3 || root.next != (remote.Connection{Name: "box", Destination: "me@box"}) {
		t.Fatalf("saved = %t gen = %d next = %+v, want the validated connection on the card's gen", root.saved, root.gen, root.next)
	}
	if s.Owner() != surface.Manager || s.Dismissible() {
		t.Fatal("a card holding fields is a manager-owned card a stray click keeps")
	}
}

func TestSurfacePopsOnCancelAndClearsTheStatusLine(t *testing.T) {
	root := &stubRoot{}
	s := NewSurface(root, remote.Connection{}, nil, 1)
	host := &stubHost{}
	s.Key(host, tea.KeyMsg{Type: tea.KeyEsc})
	if !host.popped {
		t.Fatal("esc did not pop the card")
	}
	if root.report != nil {
		t.Fatalf("cancel reported %v, want the status line cleared", root.report)
	}
}

func TestSurfaceReportsARefusedSave(t *testing.T) {
	root := &stubRoot{}
	s := NewSurface(root, remote.Connection{}, []remote.Connection{{Name: "box", Destination: "me@box"}}, 1)
	host := &stubHost{}
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("box")},
		{Type: tea.KeyTab},
		{Type: tea.KeyRunes, Runes: []rune("me@elsewhere")},
		{Type: tea.KeyEnter},
	} {
		s.Key(host, key)
	}
	if root.report == nil {
		t.Fatal("a name already taken must reach the status line")
	}
}
