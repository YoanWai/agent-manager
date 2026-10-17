package dialogs

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakePaths stands in for the root's path completion.
type fakePaths struct {
	active, chosen bool
	moves, resets  int
}

func (p *fakePaths) Active() bool { return p.active }
func (p *fakePaths) Chosen() bool { return p.chosen }
func (p *fakePaths) Move(delta int) bool {
	p.moves++
	return false
}
func (p *fakePaths) Reset() { p.resets++ }

func TestRenameSessionCyclesToolsAndSubmits(t *testing.T) {
	d := RenameSession("s1", "box", "claude", []string{"claude", "zsh"})
	paths := &fakePaths{}
	d.Key(tea.KeyMsg{Type: tea.KeyTab}, paths, 3)
	if d.Tool() != "zsh" {
		t.Fatalf("tab picked %q, want the next tool", d.Tool())
	}
	d.input.SetValue(" a/b ")
	got := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, paths, 3)
	want := RenameRequest{SessionID: "s1", Name: "a-b", Tool: "zsh"}
	if got.Action != RenameSubmit || got.Request != want {
		t.Fatalf("enter = %+v, want %+v", got, want)
	}
}

func TestRenameSessionKeepsAMissingTool(t *testing.T) {
	d := RenameSession("s1", "box", "gone", []string{"claude"})
	if d.Tool() != "gone" || len(d.tools) != 2 {
		t.Fatalf("tool %q tools %v, want the missing tool kept selectable", d.Tool(), d.tools)
	}
}

func TestRenameGroupWalksItsFields(t *testing.T) {
	d := RenameGroup("work/old", "", 0)
	paths := &fakePaths{}
	d.input.SetValue("")
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, paths, 3); got.Action != RenameInvalid || got.Err != "name cannot be empty" {
		t.Fatalf("enter on an empty name = %+v", got)
	}
	d.input.SetValue("new")
	d.Key(tea.KeyMsg{Type: tea.KeyDown}, paths, 3)
	read := d.Key(runes("/"), paths, 3)
	if d.Focus() != RenamePathField || d.Dir() != "/" || !read.Suggest {
		t.Fatalf("focus %d dir %q %+v, want typing in the path field to ask for completions", d.Focus(), d.Dir(), read)
	}
	d.Key(tea.KeyMsg{Type: tea.KeyDown}, paths, 3)
	if got := d.Key(tea.KeyMsg{Type: tea.KeyRight}, paths, 3); got.Action != RenameStay || d.Worktree() != 1 {
		t.Fatalf("right on worktree = %+v index %d", got, d.Worktree())
	}
	d.Key(tea.KeyMsg{Type: tea.KeyDown}, paths, 3)
	if got := d.Key(tea.KeyMsg{Type: tea.KeyLeft}, paths, 3); got.Action != RenameStepBase || got.Delta != -1 {
		t.Fatalf("left on base = %+v, want the root asked to step it", got)
	}
	got := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, paths, 3)
	want := RenameRequest{Group: true, OldGroup: "work/old", NewGroup: "work/new", Dir: "/", Worktree: 1, Name: "new"}
	if got.Action != RenameSubmit || got.Request != want {
		t.Fatalf("enter = %+v, want %+v", got.Request, want)
	}
	if paths.resets != 3 {
		t.Fatalf("resets = %d, want one per focus change", paths.resets)
	}
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEsc}, paths, 3); got.Action != RenameCancel {
		t.Fatalf("esc = %+v", got)
	}
}

func TestRenamePathSuggestionsTakeTheKeys(t *testing.T) {
	d := RenameGroup("work", "", 0)
	paths := &fakePaths{}
	d.Key(tea.KeyMsg{Type: tea.KeyTab}, paths, 3)
	paths.active = true
	if got := d.Key(tea.KeyMsg{Type: tea.KeyTab}, paths, 3); got.Action != RenameComplete {
		t.Fatalf("tab over suggestions = %+v", got)
	}
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, paths, 3); got.Action != RenameSubmit {
		t.Fatalf("enter without a chosen suggestion = %+v, want a submit", got)
	}
	paths.chosen = true
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, paths, 3); got.Action != RenameComplete {
		t.Fatalf("enter on a chosen suggestion = %+v", got)
	}
	d.Key(tea.KeyMsg{Type: tea.KeyDown}, paths, 3)
	if paths.moves != 1 || d.Focus() != RenameWorktreeField {
		t.Fatalf("moves %d focus %d, want the edge of the list to move focus on", paths.moves, d.Focus())
	}
	d.Key(tea.KeyMsg{Type: tea.KeyUp}, paths, 3)
	resets := paths.resets
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEsc}, paths, 3); got.Action != RenameStay || paths.resets != resets+1 {
		t.Fatalf("esc over suggestions = %+v, want them closed and the dialog kept", got)
	}
}

func TestRenameHints(t *testing.T) {
	if got := RenameGroup("work", "", 0).Hints(); got[0] != [2]string{"tab", "name / path"} {
		t.Fatalf("group hints = %v", got)
	}
	if got := RenameSession("s", "n", "zsh", []string{"zsh"}).Hints(); got[0] != [2]string{"tab", "tool: zsh"} {
		t.Fatalf("session hints = %v", got)
	}
}
