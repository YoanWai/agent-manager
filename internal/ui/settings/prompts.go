package settings

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Prompt is a saved prompt: a name, and the text that goes in front of a task.
type Prompt struct {
	Name string
	Text string
}

// PromptSave is an edit the root writes: Previous is the name the prompt
// had, empty for a new one.
type PromptSave struct {
	Previous string
	Prompt   Prompt
}

// PromptsRead is a catalog read the root ran for the panel. Gen is the
// panel's when it asked. Mutation marks the read after a save or delete,
// and Committed that the write landed before the read; Selected names the
// row to land on.
type PromptsRead struct {
	Gen       uint64
	Rows      []Prompt
	Err       error
	Mutation  bool
	Committed bool
	Selected  string
}

// Prompts is the saved prompts panel Settings opens over its rows: the
// list, the editor for one prompt, and the delete confirmation.
type Prompts struct {
	// gen fences a read or write against the panel state that asked: an
	// editor opened since, or a newer read.
	gen                                   uint64
	open, editing, confirm, loading, busy bool
	refreshFailed                         bool
	cursor, focus                         int
	rows                                  []Prompt
	previous                              string
	// first is the list row the window starts at; lines is what each
	// painted body line holds, a list row or, in the editor, the field.
	first int
	lines presentation.Lines[int]
	name  textinput.Model
	text  textarea.Model
}

// Open says the panel covers the Settings rows.
func (p *Prompts) Open() bool { return p.open }

// Gen is the generation a read or write the panel asks for carries.
func (p *Prompts) Gen() uint64 { return p.gen }

// Editing says the panel's editor holds a prompt being typed.
func (p *Prompts) Editing() bool { return p.open && p.editing }

// Listing says the panel shows its list, which the wheel scrolls.
func (p *Prompts) Listing() bool { return p.open && !p.editing && !p.confirm }

func (s *State) openPrompts() Result {
	s.Prompts = Prompts{gen: s.Prompts.gen + 1, open: true, loading: true}
	return Result{Notice: clearNotice(), LoadPrompts: true}
}

// LoadedPrompts applies a catalog read: a failed one keeps an editor open
// so its typing survives, and a landed write closes it. A read the panel
// has moved on from changes nothing.
func (s *State) LoadedPrompts(read PromptsRead) Notice {
	p := &s.Prompts
	if !p.open || read.Gen != p.gen {
		return Notice{}
	}
	p.loading, p.busy = false, false
	if read.Err != nil {
		switch {
		case read.Committed:
			p.editing, p.confirm = false, false
			p.previous = read.Selected
			p.rows = nil
			p.refreshFailed = true
			return errorNotice("Changes saved. Refresh failed. " + read.Err.Error())
		case !read.Mutation:
			p.rows = nil
			p.refreshFailed = true
			return errorNotice("Reading saved prompts: " + read.Err.Error())
		}
		return errorNotice(read.Err.Error())
	}
	p.refreshFailed = false
	p.rows = read.Rows
	p.cursor = min(p.cursor, max(0, len(p.rows)-1))
	for i, row := range p.rows {
		if row.Name == read.Selected {
			p.cursor = i
			break
		}
	}
	if read.Mutation {
		p.editing, p.confirm = false, false
	}
	return clearNotice()
}

func (s *State) editPrompt(isNew bool) Result {
	p := &s.Prompts
	p.gen++
	p.loading = false
	p.editing, p.confirm = true, false
	p.previous = ""
	p.name = textinput.New()
	p.name.Placeholder = "name"
	p.name.CharLimit = 60
	p.text = textarea.New()
	p.text.CharLimit = 64 * 1024
	// No cap on lines: a long prompt is pasted whole.
	p.text.MaxHeight = 0
	p.text.ShowLineNumbers = false
	p.text.Placeholder = "instructions"
	if !isNew && p.cursor < len(p.rows) {
		row := p.rows[p.cursor]
		p.previous = row.Name
		p.name.SetValue(row.Name)
		p.text.SetValue(row.Text)
	}
	p.focus = 0
	return Result{Notice: clearNotice(), Cmd: p.name.Focus()}
}

func (p *Prompts) save() Result {
	p.busy = true
	return Result{Notice: clearNotice(), SavePrompt: &PromptSave{
		Previous: p.previous,
		Prompt:   Prompt{Name: p.name.Value(), Text: p.text.Value()},
	}}
}

func (p *Prompts) remove() Result {
	if p.cursor >= len(p.rows) {
		return Result{}
	}
	p.busy = true
	return Result{Notice: clearNotice(), DeletePrompt: p.rows[p.cursor].Name}
}

func (s *State) reloadPrompts() Result {
	s.Prompts.gen++
	s.Prompts.loading = true
	return Result{Notice: clearNotice(), LoadPrompts: true}
}

// promptsKey runs a key on the open panel. A key while a write is on its
// way changes nothing, so the write's answer finds what it left.
func (s *State) promptsKey(msg tea.KeyMsg) Result {
	p := &s.Prompts
	if p.busy {
		return Result{}
	}
	key := msg.String()
	if p.confirm {
		switch key {
		case "esc":
			p.confirm = false
		case "enter":
			return p.remove()
		}
		return Result{}
	}
	if p.editing {
		switch key {
		case "esc":
			p.editing = false
			return s.reloadPrompts()
		case "ctrl+s":
			return p.save()
		case "tab", "shift+tab":
			p.focus = 1 - p.focus
			return Result{Cmd: p.focusEditor()}
		}
		return Result{Cmd: p.typeEditor(msg)}
	}
	switch key {
	case "esc":
		p.open = false
		return Result{Notice: clearNotice()}
	case "n":
		return s.editPrompt(true)
	case "r":
		if p.refreshFailed && !p.loading {
			return s.reloadPrompts()
		}
	case "up", "k":
		p.cursor = max(0, p.cursor-1)
	case "down", "j":
		p.cursor = min(max(0, len(p.rows)-1), p.cursor+1)
	case "enter", "e":
		if !p.loading && len(p.rows) > 0 {
			return s.editPrompt(false)
		}
	case "d":
		if !p.loading && len(p.rows) > 0 {
			p.confirm = true
			return Result{Notice: clearNotice()}
		}
	}
	return Result{}
}

func (p *Prompts) focusEditor() tea.Cmd {
	p.name.Blur()
	p.text.Blur()
	if p.focus == 0 {
		return p.name.Focus()
	}
	return p.text.Focus()
}

func (p *Prompts) typeEditor(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	if p.focus == 0 {
		p.name, cmd = p.name.Update(msg)
		return cmd
	}
	p.text, cmd = p.text.Update(msg)
	return cmd
}

// SelectedName is the row a write lands on once the catalog is read back.
func (p PromptSave) SelectedName() string { return strings.TrimSpace(p.Prompt.Name) }
