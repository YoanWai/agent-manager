package dialogs

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The group form validates its name and builds the group under the picked
// parent; the parent and base steps go to the root.
func TestGroupFormWalksItsFields(t *testing.T) {
	d := NewGroupForm("/work/backend", 4)
	paths := &fakePaths{}
	if d.Focus() != GroupFormNameField || d.Dir() != "/work/backend" || !d.DirAuto() || d.Gen() != 4 {
		t.Fatalf("opened focus %d dir %q auto %v gen %d", d.Focus(), d.Dir(), d.DirAuto(), d.Gen())
	}
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, paths, "backend", 3); got.Action != GroupFormInvalid || got.Err != "group name cannot be empty" {
		t.Fatalf("enter on an empty name = %+v", got)
	}
	d.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("api/v2"), Paste: true}, paths, "backend", 3)
	d.Key(tea.KeyMsg{Type: tea.KeyTab}, paths, "backend", 3)
	if got := d.Key(tea.KeyMsg{Type: tea.KeyRight}, paths, "backend", 3); got.Action != GroupFormStepParent || got.Delta != 1 {
		t.Fatalf("right on the parent row = %+v, want the root asked to step the picker", got)
	}
	d.Key(tea.KeyMsg{Type: tea.KeyDown}, paths, "backend", 3)
	read := d.Key(runes("/"), paths, "backend", 3)
	if d.Focus() != GroupFormPathField || d.Dir() != "/work/backend/" || d.DirAuto() || !read.Suggest {
		t.Fatalf("focus %d dir %q auto %v %+v, want typing in the path field to own it and ask for completions", d.Focus(), d.Dir(), d.DirAuto(), read)
	}
	d.Key(tea.KeyMsg{Type: tea.KeyDown}, paths, "backend", 3)
	d.Key(tea.KeyMsg{Type: tea.KeyLeft}, paths, "backend", 3)
	if d.Worktree() != 2 {
		t.Fatalf("left from inherit = %d, want off", d.Worktree())
	}
	d.Key(tea.KeyMsg{Type: tea.KeyDown}, paths, "backend", 3)
	if got := d.Key(tea.KeyMsg{Type: tea.KeyLeft}, paths, "backend", 3); got.Action != GroupFormStepBase || got.Delta != -1 {
		t.Fatalf("left on the base row = %+v, want the root asked to step it", got)
	}
	got := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, paths, "backend", 3)
	want := GroupFormRequest{Parent: "backend", Path: "backend/api-v2", Worktree: 2, Name: "api/v2", Dir: "/work/backend/", Gen: 4}
	if got.Action != GroupFormSubmit || got.Request != want {
		t.Fatalf("enter = %+v, want %+v", got.Request, want)
	}
	if paths.resets != 4 {
		t.Fatalf("resets = %d, want one per focus change", paths.resets)
	}
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEsc}, paths, "backend", 3); got.Action != GroupFormCancel {
		t.Fatalf("esc = %+v", got)
	}
}

// Aimed at a connection, the form stops at the path: the parent is fixed
// and the worktree rows belong to the host.
func TestGroupFormAimedAtAHostStopsAtThePath(t *testing.T) {
	d := NewGroupForm("", 2)
	d.AimRemote("box")
	paths := &fakePaths{}
	d.Key(runes("api"), paths, "web", 3)
	d.Key(tea.KeyMsg{Type: tea.KeyTab}, paths, "web", 3)
	if got := d.Key(tea.KeyMsg{Type: tea.KeyRight}, paths, "web", 3); got.Action != GroupFormStay {
		t.Fatalf("right on a fixed parent = %+v", got)
	}
	d.Key(tea.KeyMsg{Type: tea.KeyTab}, paths, "web", 3)
	d.Key(tea.KeyMsg{Type: tea.KeyTab}, paths, "web", 3)
	if d.Focus() != GroupFormNameField {
		t.Fatalf("focus after the path = %d, want the name again, skipping the worktree rows", d.Focus())
	}
	got := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, paths, "web", 3)
	want := GroupFormRequest{Parent: "web", Path: "web/api", Name: "api", Gen: 2}
	if got.Action != GroupFormSubmit || got.Request != want {
		t.Fatalf("enter = %+v, want %+v", got.Request, want)
	}
	card := d.View(GroupFormView{
		Field: func(label, value string, _ bool) string { return label + "=" + value + "\n" },
		Input: func(textinput.Model) string { return "" }, Badge: func(s string) string { return s },
		Picker: func() string { return "PICKER" }, Worktrees: []string{"inherit"},
	})
	if strings.Contains(card.Body, "worktree") || strings.Contains(card.Body, "PICKER") || !strings.Contains(card.Body, "on box") {
		t.Fatalf("body = %q", card.Body)
	}
}

func TestGroupFormPathSuggestionsTakeTheKeys(t *testing.T) {
	d := NewGroupForm("", 1)
	paths := &fakePaths{}
	d.Key(tea.KeyMsg{Type: tea.KeyShiftTab}, paths, "", 3)
	d.Key(tea.KeyMsg{Type: tea.KeyShiftTab}, paths, "", 3)
	d.Key(tea.KeyMsg{Type: tea.KeyShiftTab}, paths, "", 3)
	if d.Focus() != GroupFormPathField {
		t.Fatalf("shift+tab wraps to %d, want the path field", d.Focus())
	}
	paths.active = true
	if got := d.Key(tea.KeyMsg{Type: tea.KeyTab}, paths, "", 3); got.Action != GroupFormComplete {
		t.Fatalf("tab over suggestions = %+v", got)
	}
	d.SetName("x")
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, paths, "", 3); got.Action != GroupFormSubmit || got.Request.Path != "x" {
		t.Fatalf("enter without a chosen suggestion = %+v, want a submit at the root", got)
	}
	paths.chosen = true
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, paths, "", 3); got.Action != GroupFormComplete {
		t.Fatalf("enter on a chosen suggestion = %+v", got)
	}
	d.Key(tea.KeyMsg{Type: tea.KeyDown}, paths, "", 3)
	if paths.moves != 1 || d.Focus() != GroupFormWorktreeField {
		t.Fatalf("moves %d focus %d, want the edge of the list to move focus on", paths.moves, d.Focus())
	}
	d.Key(tea.KeyMsg{Type: tea.KeyUp}, paths, "", 3)
	resets := paths.resets
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEsc}, paths, "", 3); got.Action != GroupFormStay || paths.resets != resets+1 {
		t.Fatalf("esc over suggestions = %+v, want them closed and the form kept", got)
	}
}

func TestGroupFormTakesACompletedPath(t *testing.T) {
	d := NewGroupForm("/work", 1)
	d.SetDir("/work/parent")
	if !d.DirAuto() || d.Dir() != "/work/parent" {
		t.Fatalf("SetDir left dir %q auto %v, want the parent's default kept inherited", d.Dir(), d.DirAuto())
	}
	d.TakePath("/work/parent/sub/")
	if d.DirAuto() || d.Dir() != "/work/parent/sub/" {
		t.Fatalf("TakePath left dir %q auto %v, want the path owned", d.Dir(), d.DirAuto())
	}
	d.SyncWidths(42)
	if d.Width() != 42 || d.path.Width != 42 {
		t.Fatalf("widths = %d %d, want both 42", d.name.Width, d.path.Width)
	}
}

func TestGroupFormViewFollowsTheFocus(t *testing.T) {
	d := NewGroupForm("", 1)
	paths := &fakePaths{}
	plain := lipgloss.NewStyle()
	view := GroupFormView{
		Field:        func(label, value string, focused bool) string { return label + "=" + value + "\n" },
		Input:        func(textinput.Model) string { return "input" },
		Badge:        func(group string) string { return "[" + group + "]" },
		Picker:       func() string { return "picker" },
		Subtle:       plain,
		Value:        plain,
		Worktrees:    []string{"inherit", "on", "off"},
		Base:         "auto",
		SuggestHints: [][2]string{{"tab", "complete"}},
	}
	card := d.View(view)
	if card.Title != "✦ New Group" || card.Body != "name=input\nparent=[root]\npath=input\nworktree=◂ inherit ▸\nbase=auto" {
		t.Fatalf("card = %+v", card)
	}
	if card.Hints[0] != [2]string{"tab/↑↓", "move"} {
		t.Fatalf("hints = %v", card.Hints)
	}
	d.Key(tea.KeyMsg{Type: tea.KeyTab}, paths, "", 3)
	view.Parent = "work"
	card = d.View(view)
	if card.Body != "name=input\nparent=[work]\npath=input\nworktree=◂ inherit ▸\nbase=auto\n\npicker" || card.Hints[0] != [2]string{"←→", "pick parent"} {
		t.Fatalf("parent card = %+v", card)
	}
	d.Key(tea.KeyMsg{Type: tea.KeyTab}, paths, "", 3)
	view.Suggestions = func() string { return "suggestions" }
	card = d.View(view)
	if card.Body != "name=input\nparent=[work]\npath=input\nsuggestions\nworktree=◂ inherit ▸\nbase=auto" || card.Hints[0] != [2]string{"tab", "complete"} {
		t.Fatalf("path card = %+v", card)
	}
	d.Key(tea.KeyMsg{Type: tea.KeyTab}, paths, "", 3)
	if card = d.View(view); card.Hints[1] != [2]string{"←→", "change"} {
		t.Fatalf("worktree hints = %v", card.Hints)
	}
}

// Each painted line answers for its field: a click focuses a field, a
// second click steps the worktree or the base, and a parent or a path
// suggestion is picked by its own line.
func TestGroupFormClicksFollowThePaintedLines(t *testing.T) {
	d := NewGroupForm("", 1)
	paths := &fakePaths{}
	plain := lipgloss.NewStyle()
	view := GroupFormView{
		Field:     func(label, value string, focused bool) string { return label + "=" + value + "\n" },
		Input:     func(textinput.Model) string { return "input" },
		Badge:     func(group string) string { return "[" + group + "]" },
		Picker:    func() string { return "root\nwork" },
		Subtle:    plain,
		Value:     plain,
		Worktrees: []string{"inherit", "on", "off"},
		Base:      "auto",
	}
	d.View(view)
	if got := d.Click(3, paths, 3); got.Action != GroupFormStay || d.Focus() != GroupFormWorktreeField || d.Worktree() != 0 {
		t.Fatalf("first click on worktree = %+v, focus %d worktree %d", got, d.Focus(), d.Worktree())
	}
	if d.Click(3, paths, 3); d.Worktree() != 1 {
		t.Fatalf("second click on worktree should step it, at %d", d.Worktree())
	}
	d.Click(4, paths, 3)
	if got := d.Click(4, paths, 3); got.Action != GroupFormStepBase || got.Delta != 1 {
		t.Fatalf("second click on base = %+v", got)
	}
	d.Click(1, paths, 3)
	d.View(view)
	if got := d.Click(7, paths, 3); got.Action != GroupFormPickParent || got.Entry != 1 {
		t.Fatalf("a click on the second parent = %+v", got)
	}
	if got := d.Wheel(-1); got.Action != GroupFormStepParent || got.Delta != -1 {
		t.Fatalf("the wheel over the parent picker = %+v", got)
	}
	d.Click(2, paths, 3)
	view.Suggestions = func() string { return "/a\n/b" }
	d.View(view)
	if got := d.Click(4, paths, 3); got.Action != GroupFormPickPath || got.Entry != 1 {
		t.Fatalf("a click on the second suggestion = %+v", got)
	}
	if got := d.Wheel(1); got.Action != GroupFormStay {
		t.Fatalf("the wheel off the parent field = %+v", got)
	}
}
