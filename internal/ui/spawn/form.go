// Package spawn is the New Session form and the model choice it shares
// with the quick bar: their state, keys, validation and view. The root
// opens the form, owns its prompt box, path completer and group picker,
// and runs the Request it builds.
package spawn

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Field is a row of the form.
type Field int

const (
	FieldName Field = iota
	FieldTool
	FieldProfile
	FieldModel
	FieldEffort
	FieldDir
	FieldWorktree
	FieldPrompt
	FieldSaved
	FieldGroup
	// fieldBase labels the read-only base line, which never takes focus.
	fieldBase
)

// Prompt is the root's prompt box, which holds pasted images.
type Prompt interface {
	Focus()
	Blur()
	// Fit sizes the box: its width, and the rows it shows before scrolling.
	Fit(width, rows int)
	View() string
	// StepRow moves the caret a row, false at an edge row.
	StepRow(msg tea.KeyMsg) (tea.Cmd, bool)
	// ChipKey takes the keys an image chip owns, false for every other.
	ChipKey(msg tea.KeyMsg) (tea.Cmd, bool)
	Type(msg tea.KeyMsg) tea.Cmd
	// Release lets go of the images, since nothing names them any more.
	Release()
	Pasting() bool
	// Message is the prompt with each chip as the path of its image.
	Message() string
}

// Paths is the root's directory completer under the dir field.
type Paths interface {
	Active() bool
	Chosen() bool
	// Move steps the highlight, false at an edge.
	Move(delta int) bool
	Reset()
	Suggest(typed string) tea.Cmd
	View() string
	Hint() [][2]string
}

// FormHost is what the form reads from the root: the choice facts, the
// status bar, and the prompt box and path completer it owns.
type FormHost interface {
	ChoiceHost
	ClearErr()
	Prompt() Prompt
	Paths() Paths
}

// Request is a spawn the form or the quick bar asks the root to run.
type Request struct {
	Tool string
	// Name is empty for a session that takes a generated name.
	Name   string
	Group  string
	Prompt string
	// Saved is the saved prompt's text, which goes in front of Prompt.
	Saved string
	Pick  Pick
	// Dir is the directory the form names; the quick bar leaves it to
	// the group.
	Dir string
	// Worktree is an explicit choice when WorktreeSet, else the group's
	// default decides.
	Worktree    bool
	WorktreeSet bool
}

// FormRequest is root work a form key or click asks for once the form has
// taken its own share. At most one is set.
type FormRequest struct {
	Close     bool
	ApplyPath bool
	// Group steps the group picker, which the group form and the move
	// dialog share; PickGroup moves it to GroupEntry.
	Group      int
	PickGroup  bool
	GroupEntry int
	// Toggle flips the worktree choice once the directory is known to host
	// one; Probe only asks.
	Toggle bool
	Probe  bool
	// Catalog names the CLI the form moved to, whose choice rows are new.
	Catalog string
	// Spawn leaves Group to the root, whose picker holds it.
	Spawn *Request
}

// Form is the New Session form.
type Form struct {
	name         textinput.Model
	dir          textinput.Model
	dirAuto      bool
	toolNames    []string
	toolIndex    int
	worktree     bool
	worktreeAuto bool
	// defaultsTouched protects an explicit tool or worktree choice from an
	// external settings refresh that began when the form opened.
	defaultsTouched bool
	focus           Field
	choice          Choice
	// hits maps each painted body line to what a click there does.
	hits []hit
	// saved is the catalog as the form read it, savedIndex the pick in it
	// (0 for none), and savedList the list ctrl+o opens on it.
	saved        []Saved
	savedIndex   int
	savedLoading bool
	savedErr     string
	savedList    SavedList
}

// TextField is a one-line input with a placeholder and a length cap.
func TextField(placeholder string, limit int) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.CharLimit = limit
	return in
}

// NewForm opens on the name field with tools[toolIndex] picked.
func NewForm(h ChoiceHost, tools []string, toolIndex int) Form {
	name := TextField("my-session", 60)
	name.Focus()
	return Form{
		name:      name,
		dir:       TextField("", 400),
		dirAuto:   true,
		toolNames: tools,
		toolIndex: toolIndex,
		focus:     FieldName,
		choice:    NewChoice(h, tools[toolIndex]),
		// The root reads the catalog as the form opens.
		savedLoading: true,
	}
}

// SetSaved takes the catalog read, or why it failed.
func (d *Form) SetSaved(rows []Saved, err error) {
	d.savedLoading = false
	if err != nil {
		d.savedErr = "Reading saved prompts: " + err.Error()
		return
	}
	d.saved = rows
	if d.savedIndex > len(d.saved) {
		d.savedIndex = 0
	}
}

// SavedText is the picked saved prompt's text, empty for none.
func (d *Form) SavedText() string {
	if d.savedIndex <= 0 || d.savedIndex > len(d.saved) {
		return ""
	}
	return d.saved[d.savedIndex-1].Text
}

func (d *Form) stepSaved(delta int) {
	count := len(d.saved) + 1
	d.savedIndex = (d.savedIndex + delta + count) % count
}

func (d *Form) openSavedList() {
	d.savedList = OpenSavedList(d.saved, false, true)
}

func (d *Form) savedKey(msg tea.KeyMsg) tea.Cmd {
	cmd, pick := d.savedList.Key(msg)
	if pick.Picked {
		d.savedIndex = pick.Index + 1
	}
	return cmd
}

// SyncWidths fits the field widgets to the card so long values scroll
// instead of clipping at the card edge. Inputs reserve 3 columns: their
// "> " prompt plus the cursor cell that renders past the last character.
func (d *Form) SyncWidths(inner int) {
	d.name.Width = inner - 3
	d.dir.Width = inner - 3
	// textinput recomputes its scroll window only inside Update/SetValue/
	// SetCursor, so a width change alone would render a stale window until
	// the next keystroke.
	d.name.SetCursor(d.name.Position())
	d.dir.SetCursor(d.dir.Position())
	d.choice.filter.Width = inner - 3
	d.choice.filter.SetCursor(d.choice.filter.Position())
}

// Tool is the CLI the form spawns.
func (d *Form) Tool() string {
	if len(d.toolNames) == 0 {
		return ""
	}
	return d.toolNames[d.toolIndex]
}

// Tools are the CLIs the tool row steps through, and the picked one's index.
func (d *Form) Tools() ([]string, int) { return d.toolNames, d.toolIndex }

// SetTools takes a newer tool list and pick, leaving the choice rows be.
func (d *Form) SetTools(names []string, index int) {
	d.toolNames, d.toolIndex = names, index
}

// RestartChoice starts the choice rows over on the current CLI.
func (d *Form) RestartChoice(h ChoiceHost) { d.choice = NewChoice(h, d.Tool()) }

// Choice is the model, effort and profile the form launches with.
func (d *Form) Choice() *Choice { return &d.choice }

// Name is the typed session name.
func (d *Form) Name() string { return d.name.Value() }

// SetName types a session name.
func (d *Form) SetName(value string) { d.name.SetValue(value) }

// Dir is the typed directory.
func (d *Form) Dir() string { return d.dir.Value() }

// DirAuto says the directory is still the group's default.
func (d *Form) DirAuto() bool { return d.dirAuto }

// SetDir fills the directory without making it the user's own.
func (d *Form) SetDir(value string) { d.dir.SetValue(value) }

// TakePath fills in a completed path, which the user then owns.
func (d *Form) TakePath(path string) {
	d.dir.SetValue(path)
	d.dir.CursorEnd()
	d.dirAuto = false
}

// Worktree is the worktree toggle.
func (d *Form) Worktree() bool { return d.worktree }

// WorktreeAuto says the toggle is still the default.
func (d *Form) WorktreeAuto() bool { return d.worktreeAuto }

// SeedWorktree sets the toggle to a default, which an explicit choice
// later replaces.
func (d *Form) SeedWorktree(on bool) {
	d.worktree = on
	d.worktreeAuto = true
}

// DefaultWorktree moves a toggle that is still the default.
func (d *Form) DefaultWorktree(on bool) {
	if d.worktreeAuto {
		d.worktree = on
	}
}

// SetWorktree is an explicit worktree choice, which later defaults leave be.
func (d *Form) SetWorktree(on bool) {
	d.worktree = on
	d.worktreeAuto = false
	d.defaultsTouched = true
}

// DefaultsTouched says the user chose a tool or worktree since opening.
func (d *Form) DefaultsTouched() bool { return d.defaultsTouched }

// Focused is the field with focus.
func (d *Form) Focused() Field { return d.focus }

// HandleKey takes a key the root has made typed text where it was pasted.
func (d *Form) HandleKey(h FormHost, msg tea.KeyMsg) (tea.Cmd, FormRequest) {
	if d.savedList.Open() {
		return d.savedKey(msg), FormRequest{}
	}
	if d.focus == FieldModel {
		if cmd, handled := d.handleModelKey(h, msg); handled {
			return cmd, FormRequest{}
		}
	}
	paths, prompt := h.Paths(), h.Prompt()
	dirSuggesting := d.focus == FieldDir && paths.Active()
	promptFocused := d.focus == FieldPrompt
	switch msg.String() {
	case "esc":
		if dirSuggesting {
			paths.Reset()
			return nil, FormRequest{}
		}
		// The form is gone, and with it the only text naming the images it
		// was holding.
		prompt.Release()
		return nil, FormRequest{Close: true}
	case "tab":
		if dirSuggesting {
			return nil, FormRequest{ApplyPath: true}
		}
		d.FocusStep(h, 1)
		return nil, FormRequest{}
	case "shift+tab":
		d.FocusStep(h, -1)
		return nil, FormRequest{}
	case "up":
		if promptFocused {
			if cmd, stepped := prompt.StepRow(msg); stepped {
				return cmd, FormRequest{}
			}
		}
		if dirSuggesting {
			if !paths.Move(-1) {
				d.FocusStep(h, -1)
			}
		} else {
			d.FocusStep(h, -1)
		}
		return nil, FormRequest{}
	case "down":
		if promptFocused {
			if cmd, stepped := prompt.StepRow(msg); stepped {
				return cmd, FormRequest{}
			}
		}
		if dirSuggesting {
			if !paths.Move(1) {
				d.FocusStep(h, 1)
			}
		} else {
			d.FocusStep(h, 1)
		}
		return nil, FormRequest{}
	case "left":
		if d.focus == FieldTool {
			return nil, d.cycleTool(h, -1)
		}
		if d.focus == FieldSaved {
			d.stepSaved(-1)
			return nil, FormRequest{}
		}
		if d.stepChoice(h, -1) {
			return nil, FormRequest{}
		}
		if d.focus == FieldWorktree {
			return nil, FormRequest{Toggle: true}
		}
		if d.focus == FieldGroup {
			return nil, FormRequest{Group: -1}
		}
	case "right":
		if d.focus == FieldTool {
			return nil, d.cycleTool(h, 1)
		}
		if d.focus == FieldSaved {
			d.stepSaved(1)
			return nil, FormRequest{}
		}
		if d.stepChoice(h, 1) {
			return nil, FormRequest{}
		}
		if d.focus == FieldWorktree {
			return nil, FormRequest{Toggle: true}
		}
		if d.focus == FieldGroup {
			return nil, FormRequest{Group: 1}
		}
	case "ctrl+o":
		if d.focus == FieldSaved {
			d.openSavedList()
			return nil, FormRequest{}
		}
	case "enter":
		if dirSuggesting && paths.Chosen() {
			return nil, FormRequest{ApplyPath: true}
		}
		if d.focus == FieldSaved {
			d.openSavedList()
			return nil, FormRequest{}
		}
		return nil, d.Submit(h)
	}

	if promptFocused {
		if cmd, handled := prompt.ChipKey(msg); handled {
			return cmd, FormRequest{}
		}
	}

	var cmd tea.Cmd
	switch d.focus {
	case FieldName:
		d.name, cmd = d.name.Update(msg)
	case FieldDir:
		d.dir, cmd = d.dir.Update(msg)
		d.dirAuto = false
		return tea.Batch(cmd, paths.Suggest(d.dir.Value())), FormRequest{Probe: true}
	case FieldPrompt:
		cmd = prompt.Type(msg)
	case FieldEffort:
		if d.choice.effortTyped(h, d.Tool()) {
			cmd = d.choice.TypeEffort(h, d.Tool(), msg)
		}
	}
	return cmd, FormRequest{}
}

// handleModelKey lets the keys the list does not take fall through.
func (d *Form) handleModelKey(h FormHost, msg tea.KeyMsg) (tea.Cmd, bool) {
	toolName, ch := d.Tool(), &d.choice
	list := ch.suggestions(h, toolName, ch.query())
	open := ch.sugg.open && len(list) > 0
	pick := func() { ch.pickModel(h, toolName, list[ch.sugg.index].model.Key()) }
	var cmd tea.Cmd
	switch msg.String() {
	case "esc":
		if !ch.sugg.open {
			return nil, false
		}
		ch.sugg = modelSuggest{}
		return nil, true
	case "tab":
		if !open {
			return nil, false
		}
		pick()
		return nil, true
	case "enter":
		if !open || !ch.sugg.chosen {
			return nil, false
		}
		pick()
		return nil, true
	case "up", "down":
		delta := 1
		if msg.String() == "up" {
			delta = -1
		}
		if !open || !ch.sugg.move(len(list), delta) {
			d.FocusStep(h, delta)
		}
		return nil, true
	case "shift+tab":
		return nil, false
	case "left", "right":
		ch.filter, cmd = ch.filter.Update(msg)
		return cmd, true
	}
	ch.filter, cmd = ch.filter.Update(msg)
	ch.filtering = true
	if strings.TrimSpace(ch.filter.Value()) == "" {
		ch.pickModel(h, toolName, "")
	}
	ch.sugg = modelSuggest{open: true}
	return cmd, true
}

// Click acts on the painted body line a click landed on.
func (d *Form) Click(h FormHost, line int) FormRequest {
	if line < 0 || line >= len(d.hits) {
		return FormRequest{}
	}
	hit := d.hits[line]
	toolName, ch := d.Tool(), &d.choice
	switch {
	case hit.field == FieldSaved && hit.entry >= 0:
		if pick := d.savedList.Click(hit.entry); pick.Picked {
			d.savedIndex = pick.Index + 1
		}
		d.FocusField(h, FieldSaved)
		return FormRequest{}
	case hit.field == FieldModel && hit.entry >= 0:
		ch.PickEntry(h, toolName, hit.entry)
		return FormRequest{}
	case hit.field == FieldGroup && hit.entry >= 0:
		return FormRequest{PickGroup: true, GroupEntry: hit.entry}
	case hit.field != d.focus:
		if slices.Contains(d.Fields(h), hit.field) {
			d.FocusField(h, hit.field)
		}
		return FormRequest{}
	}
	switch hit.field {
	case FieldSaved:
		d.stepSaved(1)
	case FieldTool:
		return d.cycleTool(h, 1)
	case FieldWorktree:
		return FormRequest{Toggle: true}
	default:
		d.stepChoice(h, 1)
	}
	return FormRequest{}
}

func (d *Form) stepChoice(h ChoiceHost, delta int) bool {
	toolName, ch := d.Tool(), &d.choice
	switch {
	case d.focus == FieldProfile:
		ch.CycleProfile(h, toolName, delta)
	case d.focus == FieldEffort && !ch.effortTyped(h, toolName):
		ch.CycleEffort(h, toolName, delta)
	default:
		return false
	}
	return true
}

// Fields holds a choice row only where the CLI has something to pick.
func (d *Form) Fields(h ChoiceHost) []Field {
	toolName, ch := d.Tool(), &d.choice
	fields := []Field{FieldName, FieldTool}
	if HasProfiles(h, toolName) {
		fields = append(fields, FieldProfile)
	}
	if ModelListed(h, toolName) {
		fields = append(fields, FieldModel)
	}
	if shown, active := ch.EffortShown(h, toolName); shown && active {
		fields = append(fields, FieldEffort)
	}
	fields = append(fields, FieldDir, FieldWorktree)
	if len(d.saved) > 0 {
		fields = append(fields, FieldSaved)
	}
	return append(fields, FieldPrompt, FieldGroup)
}

// FocusStep moves focus delta fields along, wrapping at the ends.
func (d *Form) FocusStep(h FormHost, delta int) {
	fields := d.Fields(h)
	at := max(slices.Index(fields, d.focus), 0)
	d.FocusField(h, fields[(at+delta+len(fields))%len(fields)])
}

// FocusField puts focus on field.
func (d *Form) FocusField(h FormHost, field Field) {
	h.Paths().Reset()
	if field != FieldSaved {
		d.savedList.Close()
	}
	d.focus = field
	d.name.Blur()
	d.dir.Blur()
	prompt := h.Prompt()
	prompt.Blur()
	d.choice.Blur()
	switch field {
	case FieldName:
		d.name.Focus()
	case FieldModel:
		d.choice.filter.Focus()
		d.choice.openModelList(h, d.Tool())
	case FieldEffort:
		d.choice.typedEffort.Focus()
	case FieldDir:
		d.dir.Focus()
	case FieldPrompt:
		prompt.Focus()
	}
}

// cycleTool starts the choice rows over; the root fits them to the card and
// asks the new CLI what it offers.
func (d *Form) cycleTool(h ChoiceHost, delta int) FormRequest {
	if len(d.toolNames) == 0 {
		return FormRequest{}
	}
	d.toolIndex = (d.toolIndex + delta + len(d.toolNames)) % len(d.toolNames)
	d.defaultsTouched = true
	toolName := d.Tool()
	d.choice = NewChoice(h, toolName)
	return FormRequest{Catalog: toolName}
}

// Submit validates the form and builds its spawn; the root fills in the
// group and its launch inputs, and checks the directory on the effect lane.
func (d *Form) Submit(h FormHost) FormRequest {
	if len(d.toolNames) == 0 {
		h.ReportErr("no tools configured")
		return FormRequest{Close: true}
	}
	toolName := d.toolNames[d.toolIndex]
	prompt := h.Prompt()
	if prompt.Pasting() {
		h.ReportErr("still reading the pasted image - try again in a moment")
		return FormRequest{}
	}

	name := strings.TrimSpace(d.name.Value())
	// Chips become the paths of the images they stand for, so a first task
	// reaches the agent with its screenshot named where it was pasted.
	message := prompt.Message()
	picked, err := d.choice.Launch(h, toolName, d.choice.filter.Value())
	if err != nil {
		h.ReportErr(err.Error())
		return FormRequest{}
	}
	return FormRequest{Spawn: &Request{
		Tool:        toolName,
		Name:        name,
		Prompt:      message,
		Saved:       d.SavedText(),
		Pick:        picked,
		Dir:         d.dir.Value(),
		Worktree:    d.worktree,
		WorktreeSet: true,
	}}
}
