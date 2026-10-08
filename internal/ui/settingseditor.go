package ui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// editorRow is the Settings row for the command the editor key runs. It
// steps through automatic, each GUI editor found on PATH, what $VISUAL and
// $EDITOR name, and a custom line, which is the only entry that is typed.
type editorRow struct {
	found  []string
	auto   string
	probed bool
	picked string
	custom bool
	typed  string
	typing bool
	input  textinput.Model
}

type editorsProbedMsg struct {
	found []string
	auto  string
	// generation is the Settings visit that asked, so a late answer does
	// not land on a later visit's row.
	generation uint64
}

// probe walks PATH, which is I/O that Update must not do.
func (r editorResolution) probe() editorsProbedMsg {
	var found []string
	if r.lookup != nil {
		for _, name := range r.gui {
			if _, err := r.lookup(name); err == nil {
				found = append(found, name)
			}
		}
	}
	for _, line := range []string{r.visual, r.editor} {
		if line = strings.TrimSpace(line); line != "" && !slices.Contains(found, line) {
			found = append(found, line)
		}
	}
	r.configured = ""
	return editorsProbedMsg{found: found, auto: r.resolve()}
}

func (m *Model) probeEditorsCmd() tea.Cmd {
	resolution, generation := detectedEditors(), m.settings.gen
	return func() tea.Msg {
		msg := resolution.probe()
		msg.generation = generation
		return msg
	}
}

// applyEditorsProbe keeps the answer for the rows later loads rebuild, and
// fills the row of the visit that asked.
func (m *Model) applyEditorsProbe(msg editorsProbedMsg) {
	m.settings.cache.editors = &msg
	if m.mode == modeSettings && msg.generation == m.settings.gen {
		m.settings.dialog.editor.applyProbe(msg)
	}
}

func (m *Model) cachedEditorRow() editorRow {
	row := newEditorRow(m.settings.cache.value(editorSetting))
	if m.settings.cache.editors != nil {
		row.applyProbe(*m.settings.cache.editors)
	}
	return row
}

// A stored line reads as custom until the probe can say it names a found
// editor.
func newEditorRow(stored string) editorRow {
	return editorRow{custom: stored != "", typed: stored}
}

func (r *editorRow) applyProbe(msg editorsProbedMsg) {
	r.found, r.auto, r.probed = msg.found, msg.auto, true
	if r.custom && slices.Contains(r.found, r.typed) {
		r.custom, r.picked, r.typed = false, r.typed, ""
	}
}

func (r editorRow) line() string {
	if r.custom {
		return strings.TrimSpace(r.typed)
	}
	return r.picked
}

func (r *editorRow) cycle(step int) {
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

func (r editorRow) label() string {
	switch {
	case r.custom && r.line() == "":
		return "custom"
	case r.custom:
		return "custom · " + r.line()
	case r.picked != "":
		return r.picked
	case !r.probed:
		return "auto"
	case r.auto == "":
		return "auto · none found"
	}
	return "auto · " + r.auto
}

func (m *Model) openEditorTyping() {
	input := textField("a command, such as code -n", 400)
	input.Prompt = ""
	input.SetValue(m.settings.dialog.editor.typed)
	input.Focus()
	m.settings.dialog.editor.input = input
	m.settings.dialog.editor.typing = true
}

func (m *Model) handleEditorTypingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	row := &m.settings.dialog.editor
	switch msg.String() {
	case "enter":
		row.typed = strings.TrimSpace(row.input.Value())
		row.typing = false
		m.settings.dialog.dirty = true
		return m, nil
	case "esc":
		row.typing = false
		return m, nil
	}
	var cmd tea.Cmd
	row.input, cmd = row.input.Update(msg)
	return m, cmd
}
