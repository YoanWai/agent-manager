package dialogs

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
	tea "github.com/charmbracelet/bubbletea"
)

// The row submits the values the dialog validated; the request itself stays
// the root's to build.
func TestRenameSurfaceSubmitsTheValidatedRequest(t *testing.T) {
	root := &renameStub{}
	s := NewRenameSurface(root, RenameGroup("work/old", "", 0), "main")
	host := &stubHost{}
	s.SetName("new")
	s.Key(host, tea.KeyMsg{Type: tea.KeyEnter})
	if len(root.requests) != 1 {
		t.Fatalf("enter queued %d requests, want one", len(root.requests))
	}
	if got := root.requests[0]; !got.Group || got.OldGroup != "work/old" || got.NewGroup != "work/new" {
		t.Fatalf("request = %+v, want the group edit the row held", got)
	}
	if root.bases[0] != "main" {
		t.Fatalf("base = %q, want the choice the root probed", root.bases[0])
	}
}

// A step keeps the choice the root probed, and esc closes the row.
func TestRenameSurfaceStepsTheBaseAndCloses(t *testing.T) {
	root := &renameStub{base: "next"}
	s := NewRenameSurface(root, RenameGroup("work", "", 0), "")
	host := &stubHost{}
	for range 3 {
		s.Key(host, tea.KeyMsg{Type: tea.KeyTab})
	}
	s.Key(host, tea.KeyMsg{Type: tea.KeyRight})
	if root.steps != 1 || s.Base != "next" {
		t.Fatalf("steps = %d base = %q, want the choice the root stepped to", root.steps, s.Base)
	}
	s.Key(host, tea.KeyMsg{Type: tea.KeyEsc})
	if host.popped != 1 {
		t.Fatalf("esc popped %d times, want the row closed", host.popped)
	}
}

// An invalid name is the root's to report, and the row stays open.
func TestRenameSurfaceReportsAnInvalidName(t *testing.T) {
	root := &renameStub{}
	s := NewRenameSurface(root, RenameSession("s", "alpha", "claude", []string{"claude"}), "")
	host := &stubHost{}
	s.SetName("")
	s.Key(host, tea.KeyMsg{Type: tea.KeyEnter})
	if len(root.errs) != 1 || host.popped != 0 {
		t.Fatalf("an empty name reported %v and popped %d times", root.errs, host.popped)
	}
}

// The row is not a card: it leaves the frame as it is and holds the list
// still under a stray click.
func TestRenameSurfacePaintsNoCard(t *testing.T) {
	s := NewRenameSurface(&renameStub{}, RenameGroup("work", "", 0), "")
	if s.Dismissible() {
		t.Fatal("a rename holds a typed draft and must not close on a stray click")
	}
	if got := s.View(&stubHost{}, "below"); got != "below" {
		t.Fatalf("view = %q, want the frame unchanged", got)
	}
	if got := s.PathTarget(); got != pathcomplete.Rename {
		t.Fatalf("target = %v, want the rename's field", got)
	}
	if value, ok := s.PathValue(); !ok || value != "" {
		t.Fatalf("path value = %q ok=%v, want a group's path field", value, ok)
	}
}
