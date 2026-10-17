package settings

import tea "github.com/charmbracelet/bubbletea"

var enterKey = tea.KeyMsg{Type: tea.KeyEnter}

// Click is a press on a main row. The first moves the cursor there; one on
// the row already under it runs that row the way ↵ runs an action row and
// → steps a value.
func (s *State) Click(field int, ctx Context) Result {
	if field != s.Field {
		s.Field = field
		return Result{}
	}
	switch field {
	case FieldKeybindings, FieldCLIs, FieldDocs, FieldBugReport, FieldFeatureRequest:
		return s.Key(enterKey, ctx)
	case FieldUpdate:
		if ctx.UpdateReady {
			return s.Key(enterKey, ctx)
		}
		return Result{}
	case FieldEditor:
		if s.Editor.custom {
			return s.Key(enterKey, ctx)
		}
	}
	return s.Cycle(1)
}

// The editor's two fields, as its painted lines name them.
const (
	promptNameField = -2
	promptTextField = -3
)

// ClickPrompts is a press on line of the saved prompts panel's body. In
// the list the first click moves the cursor to a prompt and a second opens
// it in the editor, as ↵ does; in the editor a click on the other field
// moves the focus there, as tab does.
func (s *State) ClickPrompts(line int) Result {
	p := &s.Prompts
	if p.busy || p.confirm || line < 0 || line >= len(p.lines) {
		return Result{}
	}
	holds := p.lines[line]
	if p.editing {
		if (holds == promptNameField && p.focus != 0) || (holds == promptTextField && p.focus != 1) {
			return s.promptsKey(tea.KeyMsg{Type: tea.KeyTab})
		}
		return Result{}
	}
	switch {
	case holds < 0:
		return Result{}
	case holds != p.cursor:
		p.cursor = holds
		return Result{}
	}
	return s.promptsKey(enterKey)
}

// ClickCLI is a press on line of the CLI picker's body: a checkbox toggles
// at once, and the request line opens its form.
func (s *State) ClickCLI(line int, ctx Context) Result {
	switch {
	case line >= 0 && line < len(s.CLINames):
		s.CLICursor = line
		return s.cliPickerKey(enterKey, ctx)
	case line == len(s.CLINames)+1:
		s.CLICursor = len(s.CLINames)
		return s.cliPickerKey(enterKey, ctx)
	}
	return Result{}
}

// ClickKey is a press on line of the key picker's body. The first moves the
// cursor to the action painted there, and a "more" line to the action it
// hides; one on the action already under the cursor starts capture as ↵
// does. A capture or a reset question keeps the rows still.
func (s *State) ClickKey(line int) Result {
	if s.KeyCapture || s.KeyReset || line < 0 || line >= len(s.keyLines) {
		return Result{}
	}
	row := s.keyLines[line]
	switch {
	case row < 0:
		return Result{}
	case row != s.KeyCursor:
		s.KeyCursor = row
		return Result{}
	}
	return s.keyPickerKey(enterKey)
}
