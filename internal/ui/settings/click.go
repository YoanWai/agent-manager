package settings

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
)

var enterKey = tea.KeyMsg{Type: tea.KeyEnter}

// Click is a press at col of the body's painted line, in whichever panel
// is up. A command being typed keeps the dialog still.
func (s *State) Click(col, line int, ctx Context) Result {
	switch {
	case s.Editor.typing:
		return Result{}
	case s.Prompts.open:
		return s.clickPrompts(line)
	case s.CLIPicker:
		return s.clickCLI(line, ctx)
	case s.KeyPicker:
		return s.clickKey(line)
	}
	return s.clickRow(col, line, ctx)
}

// Wheel moves through whichever list is up a row at a time, as ↑↓ do. A
// command being typed, a key capture, a question and the prompt editor
// keep it still.
func (s *State) Wheel(step int, ctx Context) Result {
	if s.Editor.typing || s.KeyCapture || s.KeyReset || (s.Prompts.open && !s.Prompts.Listing()) {
		return Result{}
	}
	if step < 0 {
		return s.Key(tea.KeyMsg{Type: tea.KeyUp}, ctx)
	}
	return s.Key(tea.KeyMsg{Type: tea.KeyDown}, ctx)
}

// clickRow is a press on the main dialog. The save button saves, and the
// docs and saved prompts rows open at once. The symbols row steps at once
// where it paints its preview. Any other row takes the first click as the
// cursor, and the next one runs it the way ↵ runs an action row and →
// steps a value.
func (s *State) clickRow(col, line int, ctx Context) Result {
	if line == 0 {
		if col < s.saveWidth {
			return Result{Exit: Save}
		}
		return Result{}
	}
	field, ok := s.lines.At(line)
	switch {
	case !ok:
		return Result{}
	case field == FieldGlyphs && col < s.glyphWidth:
		s.Field = field
		return s.Cycle(1)
	case field == FieldDocs || field == FieldPrompts:
		s.Field = field
		return s.Key(enterKey, ctx)
	case !presentation.ClickRow(&s.Field, field):
		return Result{}
	}
	switch field {
	case FieldKeybindings, FieldCLIs, FieldBugReport, FieldFeatureRequest:
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

// The editor's two fields, as the panel's focus names them.
const (
	promptNameField = 0
	promptTextField = 1
)

// clickPrompts is a press on the saved prompts panel. In the list the
// first click moves the cursor to a prompt and a second opens it in the
// editor, as ↵ does; in the editor a click on the other field moves the
// focus there, as tab does.
func (s *State) clickPrompts(line int) Result {
	p := &s.Prompts
	held, ok := p.lines.At(line)
	switch {
	case p.busy || p.confirm || !ok:
		return Result{}
	case p.editing:
		if held != p.focus {
			return s.promptsKey(tea.KeyMsg{Type: tea.KeyTab})
		}
		return Result{}
	case presentation.ClickRow(&p.cursor, held):
		return s.promptsKey(enterKey)
	}
	return Result{}
}

// clickCLI is a press on the CLI picker: a checkbox toggles at once, and
// the request line opens its form.
func (s *State) clickCLI(line int, ctx Context) Result {
	row, ok := s.lines.At(line)
	if !ok {
		return Result{}
	}
	s.CLICursor = row
	return s.cliPickerKey(enterKey, ctx)
}

// clickKey is a press on the key picker. The first moves the cursor to the
// action painted there, and a "more" line to the action it hides; one on
// the action already under the cursor starts capture as ↵ does. A capture
// or a reset question keeps the rows still.
func (s *State) clickKey(line int) Result {
	row, ok := s.lines.At(line)
	if s.KeyCapture || s.KeyReset || !ok || !presentation.ClickRow(&s.KeyCursor, row) {
		return Result{}
	}
	return s.keyPickerKey(enterKey)
}
