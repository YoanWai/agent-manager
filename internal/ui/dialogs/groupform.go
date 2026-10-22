package dialogs

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// GroupForm is the New Group form: its fields, focus, keys, validation and
// the group a submit builds. The root seeds its path from the parent, owns
// the parent picker it shares with the New Session form, the path completion
// under its path field and the group base choice it probes, runs the create
// on the effect lane and closes it from the fenced completion.
type GroupForm struct {
	name          textinput.Model
	path          textinput.Model
	pathAuto      bool
	worktreeIndex int
	focus         int
	// remote names the SSH connection the group is created on; its rows
	// stop at the path, since the host owns the worktree choice.
	remote string
	// gen tells this group form from the one that stood in the same place
	// before it, so a completion cannot close a form the user since reopened.
	gen int
	// lines is the field each painted body line belongs to, for clicks.
	lines presentation.Lines[fieldLine]
}

// fieldLine is a body line's field, and its entry or -1: a path
// suggestion under the path field, a parent in the picker under its field.
type fieldLine struct {
	field int
	entry int
}

// Group form fields, in focus order.
const (
	GroupFormNameField = iota
	GroupFormParentField
	GroupFormPathField
	GroupFormWorktreeField
	GroupFormBaseField
	groupFormFieldCount
)

// NewGroupForm opens the form on the name field. The path field inherits
// dir until the user types a path of their own.
func NewGroupForm(dir string, gen int) GroupForm {
	name := newField("group-name", 60)
	name.Focus()
	path := newField("default working directory", 400)
	path.SetValue(dir)
	return GroupForm{name: name, path: path, pathAuto: true, gen: gen}
}

// AimRemote points the form at a connection: the parent is fixed, the
// directory is the host's to resolve, and the worktree rows are left out.
func (d *GroupForm) AimRemote(host string) {
	d.remote = host
	d.path.Placeholder = "the group's directory on " + host
}

func (d GroupForm) Remote() string { return d.remote }

// fieldCount is how many rows the focus cycles through.
func (d GroupForm) fieldCount() int {
	if d.remote != "" {
		return GroupFormWorktreeField
	}
	return groupFormFieldCount
}

func (d GroupForm) Gen() int      { return d.gen }
func (d GroupForm) Focus() int    { return d.focus }
func (d GroupForm) Worktree() int { return d.worktreeIndex }

// Name is the typed name; a submit cleans it into the group path.
func (d GroupForm) Name() string { return d.name.Value() }

// SetName replaces the typed name.
func (d *GroupForm) SetName(name string) { d.name.SetValue(name) }

// Dir is the typed default directory.
func (d GroupForm) Dir() string { return d.path.Value() }

// DirAuto says the directory is still the parent's default.
func (d GroupForm) DirAuto() bool { return d.pathAuto }

// SetDir fills the directory without making it the user's own.
func (d *GroupForm) SetDir(dir string) { d.path.SetValue(dir) }

// TakePath fills in a completed path, which the user then owns.
func (d *GroupForm) TakePath(path string) {
	d.path.SetValue(path)
	d.path.CursorEnd()
	d.pathAuto = false
}

// Width is the columns the text fields are sized to.
func (d GroupForm) Width() int { return d.name.Width }

// SyncWidths sizes both text fields to the card, keeping their cursors.
func (d *GroupForm) SyncWidths(width int) {
	d.name.Width = width
	d.path.Width = width
	d.name.SetCursor(d.name.Position())
	d.path.SetCursor(d.path.Position())
}

// GroupFormAction is what a key asks the root to do.
type GroupFormAction uint8

const (
	GroupFormStay GroupFormAction = iota
	GroupFormCancel
	// GroupFormComplete applies the highlighted path suggestion.
	GroupFormComplete
	// GroupFormStepParent steps the root's parent picker by Delta.
	GroupFormStepParent
	// GroupFormPickParent moves the root's parent picker to Entry.
	GroupFormPickParent
	// GroupFormPickPath completes the path suggestion at Entry.
	GroupFormPickPath
	// GroupFormStepBase steps the root's group base choice by Delta.
	GroupFormStepBase
	// GroupFormInvalid carries the error the root reports.
	GroupFormInvalid
	GroupFormSubmit
)

// GroupFormRequest is a validated new group: Path under Parent, the
// worktree choice index, the name and directory as typed, and the dialog
// generation the submit came from.
type GroupFormRequest struct {
	Parent   string
	Path     string
	Worktree int
	Name     string
	Dir      string
	Gen      int
}

// GroupFormKey is a key's outcome. Cmd is the focused field's own command;
// Suggest asks for completions of the path field as it now reads.
type GroupFormKey struct {
	Action  GroupFormAction
	Delta   int
	Entry   int
	Err     string
	Request GroupFormRequest
	Cmd     tea.Cmd
	Suggest bool
}

// Key handles one key. parent is the picker's selected group; worktrees is
// how many worktree choices the field cycles through.
func (d *GroupForm) Key(msg tea.KeyMsg, paths PathSuggestions, parent string, worktrees int) GroupFormKey {
	suggesting := d.focus == GroupFormPathField && paths.Active()
	switch msg.String() {
	case "esc":
		if suggesting {
			paths.Reset()
			return GroupFormKey{}
		}
		return GroupFormKey{Action: GroupFormCancel}
	case "tab":
		if suggesting {
			return GroupFormKey{Action: GroupFormComplete}
		}
		d.focusStep(paths, 1)
		return GroupFormKey{}
	case "shift+tab":
		d.focusStep(paths, -1)
		return GroupFormKey{}
	case "up", "down":
		delta := 1
		if msg.String() == "up" {
			delta = -1
		}
		if suggesting && paths.Move(delta) {
			return GroupFormKey{}
		}
		d.focusStep(paths, delta)
		return GroupFormKey{}
	case "left", "right":
		delta := 1
		if msg.String() == "left" {
			delta = -1
		}
		switch d.focus {
		case GroupFormWorktreeField:
			d.stepWorktree(delta, worktrees)
			return GroupFormKey{}
		case GroupFormBaseField:
			return GroupFormKey{Action: GroupFormStepBase, Delta: delta}
		case GroupFormParentField:
			if d.remote != "" {
				return GroupFormKey{}
			}
			return GroupFormKey{Action: GroupFormStepParent, Delta: delta}
		}
	case "enter":
		if suggesting && paths.Chosen() {
			return GroupFormKey{Action: GroupFormComplete}
		}
		return d.Submit(parent)
	}

	var cmd tea.Cmd
	switch d.focus {
	case GroupFormNameField:
		d.name, cmd = d.name.Update(msg)
	case GroupFormPathField:
		d.path, cmd = d.path.Update(msg)
		d.pathAuto = false
		return GroupFormKey{Cmd: cmd, Suggest: true}
	}
	return GroupFormKey{Cmd: cmd}
}

// Click acts on the painted body line a click landed on: a field takes the
// focus, a second click on the worktree or base row steps it, and a parent
// or a path suggestion is picked.
func (d *GroupForm) Click(line int, paths PathSuggestions, worktrees int) GroupFormKey {
	at, ok := d.lines.At(line)
	if !ok {
		return GroupFormKey{}
	}
	switch {
	case at.field == GroupFormParentField && at.entry >= 0:
		return GroupFormKey{Action: GroupFormPickParent, Entry: at.entry}
	case at.field == GroupFormPathField && at.entry >= 0:
		return GroupFormKey{Action: GroupFormPickPath, Entry: at.entry}
	case at.field != d.focus:
		d.focusOn(paths, at.field)
	case at.field == GroupFormWorktreeField:
		d.stepWorktree(1, worktrees)
	case at.field == GroupFormBaseField:
		return GroupFormKey{Action: GroupFormStepBase, Delta: 1}
	}
	return GroupFormKey{}
}

// Wheel steps the parent picker while it is open under its field.
func (d GroupForm) Wheel(step int) GroupFormKey {
	if d.focus != GroupFormParentField {
		return GroupFormKey{}
	}
	return GroupFormKey{Action: GroupFormStepParent, Delta: step}
}

func (d *GroupForm) stepWorktree(delta, worktrees int) {
	d.worktreeIndex = (d.worktreeIndex + delta + worktrees) % worktrees
}

func (d *GroupForm) focusStep(paths PathSuggestions, delta int) {
	d.focusOn(paths, (d.focus+delta+d.fieldCount())%d.fieldCount())
}

func (d *GroupForm) focusOn(paths PathSuggestions, field int) {
	paths.Reset()
	d.focus = field
	d.name.Blur()
	d.path.Blur()
	switch d.focus {
	case GroupFormNameField:
		d.name.Focus()
	case GroupFormPathField:
		d.path.Focus()
	}
}

// Submit validates the name and builds the group under parent; the root
// checks the directory on the effect lane.
func (d GroupForm) Submit(parent string) GroupFormKey {
	name := cleanName(d.name.Value())
	if name == "" {
		return GroupFormKey{Action: GroupFormInvalid, Err: "group name cannot be empty"}
	}
	full := name
	if parent != "" {
		full = parent + "/" + name
	}
	return GroupFormKey{Action: GroupFormSubmit, Request: GroupFormRequest{
		Parent:   parent,
		Path:     full,
		Worktree: d.worktreeIndex,
		Name:     d.name.Value(),
		Dir:      d.path.Value(),
		Gen:      d.gen,
	}}
}

// GroupFormView is what the form draws with: the root's field renderer,
// input view and group badge, the selected parent, the picker under the
// parent row, the open path suggestions and their hints, the worktree
// choice labels and the rendered base choice.
type GroupFormView struct {
	GlyphMode    presentation.GlyphMode
	Field        func(label, value string, focused bool) string
	Input        func(textinput.Model) string
	Badge        func(string) string
	Parent       string
	Picker       func() string
	Suggestions  func() string
	SuggestHints [][2]string
	Subtle       lipgloss.Style
	Value        lipgloss.Style
	Worktrees    []string
	Base         string
}

// View paints the form's body and records the line each field takes, for
// clicks.
func (d *GroupForm) View(view GroupFormView) Card {
	var body strings.Builder
	d.lines.Reset()
	add := func(text string, field, entry int) {
		body.WriteString(text)
		d.lines.Show(fieldLine{field: field, entry: entry}, strings.Count(text, "\n"))
	}
	field := func(label, value string, id int) {
		add(view.Field(label, value, d.focus == id), id, -1)
	}
	field("name", view.Input(d.name), GroupFormNameField)
	parent := view.Badge(displayGroup(view.Parent))
	if d.remote != "" {
		parent += view.Subtle.Render("  on " + d.remote)
	}
	field("parent", parent, GroupFormParentField)
	field("path", view.Input(d.path), GroupFormPathField)
	suggesting := d.focus == GroupFormPathField && view.Suggestions != nil
	if suggesting {
		for i, line := range strings.Split(view.Suggestions(), "\n") {
			add(line+"\n", GroupFormPathField, i)
		}
	}
	if d.remote == "" {
		worktree := view.Subtle.Render("◂ ") + view.Value.Render(view.Worktrees[d.worktreeIndex]) + view.Subtle.Render(" ▸")
		field("worktree", worktree, GroupFormWorktreeField)
		field("base", view.Base, GroupFormBaseField)
		if d.focus == GroupFormParentField {
			add("\n", GroupFormParentField, -1)
			for i, line := range strings.Split(view.Picker(), "\n") {
				add(line+"\n", GroupFormParentField, i)
			}
		}
	}
	hints := [][2]string{{"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	switch {
	case suggesting:
		hints = view.SuggestHints
	case d.remote != "":
	case d.focus == GroupFormParentField:
		hints = [][2]string{{"←→", "pick parent"}, {"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	case d.focus == GroupFormWorktreeField || d.focus == GroupFormBaseField:
		hints = [][2]string{{"tab/↑↓", "move"}, {"←→", "change"}, {"↵", "create"}, {"esc", "cancel"}}
	}
	return Card{Title: presentation.GlyphsFor(view.GlyphMode).NewGlyph + " New Group", Body: strings.TrimRight(body.String(), "\n"), Hints: hints}
}
