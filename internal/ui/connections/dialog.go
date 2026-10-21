package connections

import (
	"strings"

	remote "github.com/YoanWai/agent-manager/internal/remote/value"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Dialog fields, in focus order; each paints on its own body line.
const (
	NameField = iota
	DestinationField
	BinaryField
	fieldCount
)

// Dialog adds an SSH connection or edits one: its fields, keys, view and
// validation. The root opens it and runs the save on the effect lane.
type Dialog struct {
	name        textinput.Model
	destination textinput.Model
	binary      textinput.Model
	focus       int
	// saving is set while the save enter asked for is on the effect lane,
	// so a second enter cannot add the connection twice.
	saving bool
	// editing is the name of the connection being edited; empty adds one.
	editing string
	// gen tells this dialog from the one opened before it, so a save that
	// completes late cannot close a dialog the user reopened.
	gen uint64
}

// Open starts the dialog empty, or filled from the connection it edits.
func (d *Dialog) Open(editing remote.Connection) {
	name := field("build-box", 40, editing.Name)
	destination := field("user@host or an ~/.ssh/config alias", 255, editing.Destination)
	binary := field("found on the login PATH", 1024, editing.Binary)
	name.Focus()
	*d = Dialog{name: name, destination: destination, binary: binary, editing: editing.Name, gen: d.gen + 1}
}

func field(placeholder string, limit int, value string) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.CharLimit = limit
	in.SetValue(value)
	in.CursorEnd()
	return in
}

func (d *Dialog) Gen() uint64 { return d.gen }

func (d *Dialog) Focus() int { return d.focus }

// Editing is the name of the connection being edited; empty adds one.
func (d *Dialog) Editing() string { return d.editing }

// SaveFailed takes enter again after the save of dialog gen failed.
func (d *Dialog) SaveFailed(gen uint64) {
	if d.gen == gen {
		d.saving = false
	}
}

// DialogAction is what a key on the dialog asks the root for.
type DialogAction uint8

const (
	DialogNone DialogAction = iota
	DialogCancel
	// DialogSave carries a connection that passed validation.
	DialogSave
	// DialogRefused carries why enter saved nothing.
	DialogRefused
)

type DialogKey struct {
	Action DialogAction
	Cmd    tea.Cmd
	// Next is the connection to save, replacing Editing when that is set.
	Next    remote.Connection
	Editing string
	Gen     uint64
	Refusal string
}

// Key handles a key; others are the connections the one saved would sit
// beside, including the one being edited.
func (d *Dialog) Key(msg tea.KeyMsg, others []remote.Connection) DialogKey {
	switch msg.String() {
	case "esc":
		return DialogKey{Action: DialogCancel}
	case "tab", "down":
		d.focusStep(1)
		return DialogKey{}
	case "shift+tab", "up":
		d.focusStep(-1)
		return DialogKey{}
	case "enter":
		if d.saving {
			return DialogKey{Action: DialogRefused, Refusal: "this connection is already being saved"}
		}
		return d.submit(others)
	}
	var cmd tea.Cmd
	in := d.field(d.focus)
	*in, cmd = in.Update(msg)
	return DialogKey{Cmd: cmd}
}

// Click focuses the field painted on the body's line; the note under the
// fields takes none.
func (d *Dialog) Click(line int) {
	if line >= 0 && line < fieldCount {
		d.focusOn(line)
	}
}

func (d *Dialog) field(index int) *textinput.Model {
	switch index {
	case NameField:
		return &d.name
	case DestinationField:
		return &d.destination
	}
	return &d.binary
}

func (d *Dialog) focusStep(delta int) {
	d.focusOn((d.focus + delta + fieldCount) % fieldCount)
}

func (d *Dialog) focusOn(index int) {
	d.focus = index
	for index := range fieldCount {
		d.field(index).Blur()
	}
	d.field(d.focus).Focus()
}

func (d *Dialog) submit(list []remote.Connection) DialogKey {
	next := remote.Connection{
		Name:        strings.TrimSpace(d.name.Value()),
		Destination: strings.TrimSpace(d.destination.Value()),
		Binary:      strings.TrimSpace(d.binary.Value()),
	}
	var others []remote.Connection
	for _, conn := range list {
		if conn.Name != d.editing {
			others = append(others, conn)
		}
	}
	if err := remote.ValidateConnection(next, others); err != nil {
		return DialogKey{Action: DialogRefused, Refusal: err.Error()}
	}
	d.saving = true
	return DialogKey{Action: DialogSave, Next: next, Editing: d.editing, Gen: d.gen}
}

// DialogView is the root's field layout and styles.
type DialogView struct {
	Field func(label, value string, focused bool) string
	Input func(textinput.Model) string
	Muted lipgloss.Style
}

func (d *Dialog) View(view DialogView) uidialogs.Card {
	title := "⇄ New SSH Connection"
	if d.editing != "" {
		title = "⇄ Edit SSH Connection"
	}
	body := view.Field("name", view.Input(d.name), d.focus == NameField) +
		view.Field("ssh to", view.Input(d.destination), d.focus == DestinationField) +
		view.Field("binary", view.Input(d.binary), d.focus == BinaryField) + "\n" +
		view.Muted.Render("  Its sessions show under this name. The host needs agent-manager, on") + "\n" +
		view.Muted.Render("  its login PATH or at the binary path (/… or ~/…), and a key this") + "\n" +
		view.Muted.Render("  machine's ssh can use without a prompt.")
	return uidialogs.Card{Title: title, Body: body, Hints: [][2]string{{"tab/↑↓", "move"}, {"↵", "save"}, {"esc", "cancel"}}}
}
