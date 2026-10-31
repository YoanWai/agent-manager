package quick

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/composer"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// surfaceRoot is the root as the bar reaches it, recording instead of
// forwarding to the list.
type surfaceRoot struct {
	keys  []string
	mouse int
}

func (r *surfaceRoot) Key(msg tea.KeyMsg) (tea.Cmd, bool) {
	r.keys = append(r.keys, msg.String())
	return nil, true
}

func (r *surfaceRoot) Mouse(tea.MouseMsg) tea.Cmd { r.mouse++; return nil }

var _ SurfaceRoot = (*surfaceRoot)(nil)

// The bar takes every key the stack offers it and leaves the mouse to the
// list, which paints the bar in its own footer.
func TestSurfaceTakesKeysAndTheListsMouse(t *testing.T) {
	root := &surfaceRoot{}
	s := NewSurface(root)
	if handled, _ := s.Key(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}); !handled {
		t.Fatal("the bar let a key fall past it")
	}
	s.Mouse(nil, tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if len(root.keys) != 1 || root.mouse != 1 {
		t.Fatalf("root saw keys %v and %d mouse events, want one of each", root.keys, root.mouse)
	}
	if s.Dismissible() {
		t.Fatal("the bar holds a draft and must not close on a click")
	}
	if s.Owner() != surface.Manager {
		t.Fatalf("owner = %v, want the manager", s.Owner())
	}
	if below := "frame"; s.View(nil, below) != below {
		t.Fatal("the bar must leave the frame to the list footer")
	}
	if pairs := s.Footer(); pairs != nil {
		t.Fatalf("footer pairs = %v, want none: the bar is the footer", pairs)
	}
	if cmd := s.Click(nil, 0); cmd != nil {
		t.Fatal("a click on the bar is the list's, not the surface's")
	}
	if cmd := s.Wheel(nil, tea.KeyMsg{Type: tea.KeyUp}); cmd != nil {
		t.Fatal("the wheel is the list's, not the surface's")
	}
}

// Leave deletes the images the draft held: reopening the bar starts a fresh
// prompt, so nothing would reference them.
func TestLeaveDropsTheDraftsImages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "draft.png")
	if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewSurface(&surfaceRoot{})
	s.Attachments = []composer.Attachment{{ID: 1, Path: path}}
	s.Leave(nil)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("closing the bar kept its image: %v", err)
	}
	if len(s.Attachments) != 0 {
		t.Fatalf("attachments = %v, want none left", s.Attachments)
	}
}
