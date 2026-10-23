package dialogs

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// fakeRoot is the root as the card reaches it, holding the width it paints
// a card on and the last text it reported.
type fakeRoot struct {
	width int
	err   string
}

func (r *fakeRoot) CardWidth(int) int { return r.width }

func (r *fakeRoot) ReportErr(text string) { r.err = text }

// fakeHost is the stack's host: a card's own size and a pop that says it
// happened.
type fakeHost struct {
	cols, rows int
	popped     int
}

func (h *fakeHost) CardSized(int, string, string, [][2]string) string { return "" }

func (h *fakeHost) Size() (int, int) { return h.cols, h.rows }

func (h *fakeHost) Pop() { h.popped++ }

func testOptions(root *fakeRoot) FormOptions {
	return FormOptions{
		Gen:       3,
		Dir:       "/work/parent",
		Cols:      120,
		Parents:   []spawn.GroupOption{{Path: ""}, {Path: "parent"}},
		Parent:    1,
		Lends:     map[string]Lend{"": {}, "parent": {Dir: "/work/parent", Base: "develop"}},
		Worktrees: []string{"inherit", "on", "off"},
		Styles:    spawn.Styles{Subtle: lipgloss.NewStyle(), Value: lipgloss.NewStyle(), Accent2: lipgloss.Color("#fff")},
	}
}

func TestGroupFormSurfaceInheritsItsParentsPath(t *testing.T) {
	root := &fakeRoot{width: 64}
	s := NewGroupFormSurface(root, testOptions(root))
	if s.Dir() != "/work/parent" || !s.DirAuto() || s.Selected() != "parent" || s.ParentPath() != "parent" {
		t.Fatalf("opened dir %q auto %v parent %q path %q", s.Dir(), s.DirAuto(), s.Selected(), s.ParentPath())
	}
	s.StepParent(1)
	if s.Selected() != "" {
		t.Fatalf("stepping past the last parent should wrap to the root, got %q", s.Selected())
	}
	if s.Dir() != "" {
		t.Fatalf("the root lends no directory, got %q", s.Dir())
	}
}

func TestGroupFormSurfaceStepsAndSubmitsToTheRoot(t *testing.T) {
	root := &fakeRoot{width: 64}
	s := NewGroupFormSurface(root, testOptions(root))
	s.SetName("api")
	s.SelectParent(0)
	handled, cmd := s.Key(&fakeHost{}, tea.KeyMsg{Type: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatal("enter on a named group should ask the root to create it")
	}
	submit, ok := cmd().(GroupFormSubmitMsg)
	if !ok || submit.Request.Path != "api" || submit.Request.Gen != 3 || submit.Remote.Host != "" {
		t.Fatalf("submit = %+v", submit)
	}
}

// A connection's group has no local directory to complete and no local
// create: the submit carries the host.
func TestGroupFormSurfaceOnAConnectionStopsAtThePath(t *testing.T) {
	root := &fakeRoot{width: 64}
	opts := testOptions(root)
	opts.Remote = Remote{Host: "box", Group: "web"}
	s := NewGroupFormSurface(root, opts)
	if s.ParentPath() != "web" || s.Dir() != "" {
		t.Fatalf("aimed at a host: parent %q dir %q", s.ParentPath(), s.Dir())
	}
	// The parent is fixed, so the focus cycles name, parent, path and back
	// without the worktree and base rows a local group has.
	s.Key(&fakeHost{}, tea.KeyMsg{Type: tea.KeyTab})
	s.Key(&fakeHost{}, tea.KeyMsg{Type: tea.KeyTab})
	if s.Focus() != GroupFormPathField {
		t.Fatalf("focus = %d, want the path", s.Focus())
	}
	s.Key(&fakeHost{}, tea.KeyMsg{Type: tea.KeyTab})
	if s.Focus() != GroupFormNameField {
		t.Fatalf("focus after the path = %d, want the name again", s.Focus())
	}
	s.SetName("api")
	ok, msg := s.Key(&fakeHost{}, tea.KeyMsg{Type: tea.KeyEnter})
	if !ok || msg == nil {
		t.Fatal("enter should submit")
	}
	submit := msg().(GroupFormSubmitMsg)
	if submit.Remote.Host != "box" || submit.Request.Path != "web/api" {
		t.Fatalf("submit = %+v", submit)
	}
}

func TestGroupFormSurfaceBaseAskAndInvalidName(t *testing.T) {
	root := &fakeRoot{width: 64}
	s := NewGroupFormSurface(root, testOptions(root))
	if _, cmd := s.Key(&fakeHost{}, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("an empty name reports the refusal on the root and queues nothing")
	}
	if root.err != "group name cannot be empty" {
		t.Fatalf("reported %q", root.err)
	}
	s.SetName("api")
	focusField(t, s, GroupFormBaseField)
	_, cmd := s.Key(&fakeHost{}, tea.KeyMsg{Type: tea.KeyRight})
	ask, ok := cmd().(GroupBaseAskMsg)
	if !ok || ask.Gen != 3 || ask.Delta != 1 {
		t.Fatalf("base step = %+v", ask)
	}
}

// The base the root steps lands on the card the ask came from, and the card
// shows the base its parent lends while the choice is auto.
func TestGroupFormSurfaceHoldsTheBaseTheRootSteps(t *testing.T) {
	root := &fakeRoot{width: 64}
	s := NewGroupFormSurface(root, testOptions(root))
	if got := BaseChoice(lipgloss.NewStyle(), lipgloss.NewStyle(), "", "develop"); got != "◂ auto ▸  develop from parent" {
		t.Fatalf("auto choice = %q", got)
	}
	*s.BaseRef() = "main"
	if s.Base() != "main" {
		t.Fatalf("base = %q", s.Base())
	}
}

func TestGroupFormSurfaceCancelPops(t *testing.T) {
	root := &fakeRoot{width: 64}
	host := &fakeHost{cols: 120, rows: 34}
	s := NewGroupFormSurface(root, testOptions(root))
	if handled, cmd := s.Key(host, tea.KeyMsg{Type: tea.KeyEsc}); !handled || cmd != nil || host.popped != 1 {
		t.Fatalf("esc handled %v cmd %v pops %d", handled, cmd, host.popped)
	}
}

func TestGroupFormSurfaceIsAPathField(t *testing.T) {
	root := &fakeRoot{width: 64}
	s := NewGroupFormSurface(root, testOptions(root))
	if s.PathTarget() != pathcomplete.Group {
		t.Fatalf("target = %v", s.PathTarget())
	}
	if value, ok := s.PathValue(); !ok || value != "/work/parent" {
		t.Fatalf("path value %q %v", value, ok)
	}
	s.TakePath("/work/parent/sub")
	if s.DirAuto() || s.Dir() != "/work/parent/sub" {
		t.Fatalf("take path left dir %q auto %v", s.Dir(), s.DirAuto())
	}
	if s.Paths() == nil {
		t.Fatal("no completer state")
	}
}

// focusField tabs the card to field.
func focusField(t *testing.T, s *GroupFormSurface, field int) {
	t.Helper()
	for range groupFormFieldCount {
		if s.Focus() == field {
			return
		}
		s.Key(&fakeHost{}, tea.KeyMsg{Type: tea.KeyTab})
	}
	t.Fatalf("focus = %d, want %d", s.Focus(), field)
}
