package dialogs

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Rename edits the selected session's name and tool, or a group's name,
// default path and worktree choice. It owns the fields, focus, keys and
// validation. The root seeds it from the selected row, owns the path
// completion under the path field and the group base choice it probes, runs
// the rename on the effect lane and closes it from the fenced completion.
type Rename struct {
	group     bool
	path      string
	sessionID string
	input     textinput.Model
	dir       textinput.Model
	worktree  int
	focus     int
	tools     []string
	toolIndex int
}

// Rename fields, in focus order; a session has the name alone.
const (
	RenameNameField = iota
	RenamePathField
	RenameWorktreeField
	RenameBaseField
)

func nameField(name string) textinput.Model {
	input := textinput.New()
	input.CharLimit = 60
	input.Prompt = ""
	input.Focus()
	input.SetValue(name)
	return input
}

// RenameGroup opens an edit of the group at path, its default directory and
// its worktree choice index.
func RenameGroup(path, dir string, worktree int) Rename {
	field := newField("default working directory", 400)
	field.Prompt = ""
	field.SetValue(dir)
	return Rename{
		group:    true,
		path:     path,
		input:    nameField(baseName(path)),
		dir:      field,
		worktree: worktree,
	}
}

// RenameSession opens a rename of one session, its tool picked from tools.
// A tool missing from tools (a removed config block) stays selectable, so a
// save does not silently reassign the session to the first configured tool.
func RenameSession(id, name, tool string, tools []string) Rename {
	toolIndex := 0
	for i, candidate := range tools {
		if candidate == tool {
			toolIndex = i
			break
		}
	}
	if len(tools) == 0 || tools[toolIndex] != tool {
		tools = append([]string{tool}, tools...)
		toolIndex = 0
	}
	return Rename{
		sessionID: id,
		input:     nameField(name),
		tools:     tools,
		toolIndex: toolIndex,
	}
}

func (d Rename) IsGroup() bool     { return d.group }
func (d Rename) Path() string      { return d.path }
func (d Rename) SessionID() string { return d.sessionID }
func (d Rename) Focus() int        { return d.focus }
func (d Rename) Worktree() int     { return d.worktree }

// Name is the typed name as a submit would send it.
func (d Rename) Name() string { return cleanName(d.input.Value()) }

// SetName replaces the typed name.
func (d *Rename) SetName(name string) { d.input.SetValue(name) }

// Tools are the tools a session rename cycles through.
func (d Rename) Tools() []string { return d.tools }

// Dir is the typed default directory of a group edit.
func (d Rename) Dir() string { return d.dir.Value() }

// SetDir puts a completed path in the path field, the cursor at its end.
func (d *Rename) SetDir(path string) {
	d.dir.SetValue(path)
	d.dir.CursorEnd()
}

// NameField is the name input, sized for the row it is drawn in.
func (d *Rename) NameField(width int) textinput.Model {
	if width >= 5 {
		d.input.Width = width
	}
	return d.input
}

// Tool is the tool a session rename would save, empty for a group.
func (d Rename) Tool() string {
	if len(d.tools) == 0 {
		return ""
	}
	return d.tools[d.toolIndex]
}

func (d *Rename) cycleTool(delta int) {
	if len(d.tools) == 0 {
		return
	}
	n := len(d.tools)
	d.toolIndex = (d.toolIndex + delta + n) % n
}

// PathSuggestions is the root's directory completion under the path field.
type PathSuggestions interface {
	Active() bool
	Chosen() bool
	Move(delta int) bool
	Reset()
}

func (d *Rename) focusField(paths PathSuggestions, delta int) {
	paths.Reset()
	fields := 2
	if d.group {
		fields = 4
	}
	d.focus = (d.focus + delta + fields) % fields
	d.input.Blur()
	d.dir.Blur()
	switch d.focus {
	case RenameNameField:
		d.input.Focus()
	case RenamePathField:
		d.dir.Focus()
	}
}

// RenameAction is what a key asks the root to do.
type RenameAction uint8

const (
	RenameStay RenameAction = iota
	RenameCancel
	// RenameComplete applies the highlighted path suggestion.
	RenameComplete
	// RenameStepBase steps the root's group base choice by Delta.
	RenameStepBase
	// RenameInvalid carries the error the root reports.
	RenameInvalid
	RenameSubmit
)

// RenameRequest is a validated rename. A group edit fills OldGroup,
// NewGroup, Dir and Worktree; a session rename fills SessionID and Tool.
type RenameRequest struct {
	Group     bool
	SessionID string
	Tool      string
	OldGroup  string
	NewGroup  string
	Dir       string
	Worktree  int
	Name      string
}

// RenameKey is a key's outcome. Cmd is the focused field's own command;
// Suggest asks for completions of the path field as it now reads.
type RenameKey struct {
	Action  RenameAction
	Delta   int
	Err     string
	Request RenameRequest
	Cmd     tea.Cmd
	Suggest bool
}

// Key handles one key. worktrees is how many worktree choices the group
// field cycles through.
func (d *Rename) Key(msg tea.KeyMsg, paths PathSuggestions, worktrees int) RenameKey {
	suggesting := d.group && d.focus == RenamePathField && paths.Active()
	switch msg.String() {
	case "esc":
		if suggesting {
			paths.Reset()
			return RenameKey{}
		}
		return RenameKey{Action: RenameCancel}
	case "tab":
		if !d.group {
			d.cycleTool(1)
			return RenameKey{}
		}
		if suggesting {
			return RenameKey{Action: RenameComplete}
		}
		d.focusField(paths, 1)
		return RenameKey{}
	case "shift+tab":
		if !d.group {
			d.cycleTool(-1)
			return RenameKey{}
		}
		if suggesting {
			return RenameKey{}
		}
		d.focusField(paths, -1)
		return RenameKey{}
	case "up", "down":
		if !d.group {
			break
		}
		delta := 1
		if msg.String() == "up" {
			delta = -1
		}
		if suggesting && paths.Move(delta) {
			return RenameKey{}
		}
		d.focusField(paths, delta)
		return RenameKey{}
	case "left", "right":
		delta := 1
		if msg.String() == "left" {
			delta = -1
		}
		if d.group && d.focus == RenameWorktreeField {
			d.worktree = (d.worktree + delta + worktrees) % worktrees
			return RenameKey{}
		}
		if d.group && d.focus == RenameBaseField {
			return RenameKey{Action: RenameStepBase, Delta: delta}
		}
	case "enter":
		if suggesting && paths.Chosen() {
			return RenameKey{Action: RenameComplete}
		}
		return d.submit()
	}
	var cmd tea.Cmd
	switch d.focus {
	case RenameNameField:
		d.input, cmd = d.input.Update(msg)
	case RenamePathField:
		d.dir, cmd = d.dir.Update(msg)
		return RenameKey{Cmd: cmd, Suggest: true}
	}
	return RenameKey{Cmd: cmd}
}

// submit validates the name and hands the root the rename it captured.
func (d *Rename) submit() RenameKey {
	name := d.Name()
	if name == "" {
		return RenameKey{Action: RenameInvalid, Err: "name cannot be empty"}
	}
	if d.group {
		newPath := name
		if parent := parentGroup(d.path); parent != "" {
			newPath = parent + "/" + name
		}
		return RenameKey{Action: RenameSubmit, Request: RenameRequest{
			Group:    true,
			OldGroup: d.path,
			NewGroup: newPath,
			Dir:      d.dir.Value(),
			Worktree: d.worktree,
			Name:     name,
		}}
	}
	return RenameKey{Action: RenameSubmit, Request: RenameRequest{
		SessionID: d.sessionID,
		Name:      name,
		Tool:      d.Tool(),
	}}
}

// Hints are the footer keys while the rename is open.
func (d Rename) Hints() [][2]string {
	if d.group {
		return [][2]string{{"tab", "name / path"}, {"↵", "save"}, {"esc", "cancel"}}
	}
	if tool := d.Tool(); tool != "" {
		return [][2]string{{"tab", "tool: " + tool}, {"↵", "save"}, {"esc", "cancel"}}
	}
	return [][2]string{{"↵", "save"}, {"esc", "cancel"}}
}

// GroupFieldsView is what the group edit's fields draw with: the root's
// styles, its field renderer, the open path suggestions, the worktree
// choice labels and the rendered base choice.
type GroupFieldsView struct {
	Width       int
	Label       lipgloss.Style
	Focused     lipgloss.Style
	Subtle      lipgloss.Style
	Value       lipgloss.Style
	Input       func(textinput.Model) string
	Suggestions func() string
	Worktrees   []string
	Base        string
}

// GroupFields draws the path, worktree and base rows under a group's head,
// the focused row's label in the accent.
func (d *Rename) GroupFields(view GroupFieldsView) string {
	label := func(field int) lipgloss.Style {
		if d.focus == field {
			return view.Focused
		}
		return view.Label
	}
	if fieldWidth := view.Width - 12; fieldWidth >= 10 {
		d.dir.Width = fieldWidth
	}
	out := label(RenamePathField).Width(10).Render("path") + view.Input(d.dir)
	if d.focus == RenamePathField && view.Suggestions != nil {
		out += "\n" + view.Suggestions()
	}
	out += "\n" + label(RenameWorktreeField).Width(10).Render("worktree") +
		view.Subtle.Render("◂ ") + view.Value.Render(view.Worktrees[d.worktree]) + view.Subtle.Render(" ▸")
	out += "\n" + label(RenameBaseField).Width(10).Render("base") + view.Base
	return out
}
