package settings

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// EditorRow is the Settings row for the command the editor key runs. It
// steps through automatic, each GUI editor found on PATH, what $VISUAL and
// $EDITOR name, and a custom line, which is the only entry that is typed.
type EditorRow struct {
	found  []string
	auto   string
	probed bool
	picked string
	custom bool
	typed  string
	typing bool
	input  textinput.Model
}

// EditorsProbe is a PATH probe's answer: the editors found and what an
// unset row resolves to.
type EditorsProbe struct {
	Found []string
	Auto  string
	// Generation is the Settings visit that asked, so a late answer does
	// not land on a later visit's row.
	Generation uint64
}

// NewEditorRow places a stored line. It reads as custom until the probe
// can say it names a found editor.
func NewEditorRow(stored string, probe *EditorsProbe) EditorRow {
	row := EditorRow{custom: stored != "", typed: stored}
	if probe != nil {
		row.ApplyProbe(*probe)
	}
	return row
}

func (r *EditorRow) ApplyProbe(msg EditorsProbe) {
	r.found, r.auto, r.probed = msg.Found, msg.Auto, true
	if r.custom && slices.Contains(r.found, r.typed) {
		r.custom, r.picked, r.typed = false, r.typed, ""
	}
}

// Line is the command the row stores.
func (r EditorRow) Line() string {
	if r.custom {
		return strings.TrimSpace(r.typed)
	}
	return r.picked
}

func (r EditorRow) Custom() bool { return r.custom }

func (r EditorRow) Typing() bool { return r.typing }

func (r *EditorRow) cycle(step int) {
	custom := len(r.found) + 1
	position := custom
	if !r.custom {
		position = slices.Index(r.found, r.picked) + 1
	}
	position = (position + step + custom + 1) % (custom + 1)
	r.custom, r.picked = position == custom, ""
	if position > 0 && !r.custom {
		r.picked = r.found[position-1]
	}
}

func (r EditorRow) Label() string {
	switch {
	case r.custom && r.Line() == "":
		return "custom"
	case r.custom:
		return "custom · " + r.Line()
	case r.picked != "":
		return r.picked
	case !r.probed:
		return "auto"
	case r.auto == "":
		return "auto · none found"
	}
	return "auto · " + r.auto
}

func (s *State) openEditorTyping() {
	input := textinput.New()
	input.Placeholder = "a command, such as code -n"
	input.CharLimit = 400
	input.Prompt = ""
	input.SetValue(s.Editor.typed)
	input.Focus()
	s.Editor.input = input
	s.Editor.typing = true
}

func (s *State) editorTypingKey(msg tea.KeyMsg) tea.Cmd {
	row := &s.Editor
	switch msg.String() {
	case "enter":
		row.typed = strings.TrimSpace(row.input.Value())
		row.typing = false
		s.Dirty = true
		return nil
	case "esc":
		row.typing = false
		return nil
	}
	var cmd tea.Cmd
	row.input, cmd = row.input.Update(msg)
	return cmd
}
