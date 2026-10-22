package dialogs

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Fork names a fork of one session. It owns the source it shows, the name
// field and the dialog generation; the root validates the source, keeps the
// captured row and runs the fork the submit asks for.
type Fork struct {
	sourceName  string
	sourceGroup string
	name        textinput.Model
	// gen counts opened dialogs, so a completion only closes the dialog it
	// was submitted from; reopening a fork on the same source mints a new
	// one that an older in-flight completion leaves alone.
	gen int
}

// Open starts a fork of the named source, its name field seeded from it.
func (d *Fork) Open(sourceName, sourceGroup string) {
	name := newField("fork name", 60)
	name.SetValue(sourceName + "-fork")
	name.CursorEnd()
	name.Focus()
	*d = Fork{sourceName: sourceName, sourceGroup: sourceGroup, name: name, gen: d.gen + 1}
}

// Gen is the generation of the dialog now open.
func (d Fork) Gen() int { return d.gen }

// Name is the fork name as a submit would send it.
func (d Fork) Name() string { return cleanName(d.name.Value()) }

// SetName replaces the typed name.
func (d *Fork) SetName(name string) { d.name.SetValue(name) }

// ForkAction is what a key asks the root to do.
type ForkAction uint8

const (
	ForkStay ForkAction = iota
	ForkCancel
	// ForkInvalid carries the error the root reports.
	ForkInvalid
	// ForkSubmit carries the name and the generation it was submitted from.
	ForkSubmit
)

// ForkKey is a key's outcome; Cmd is the name field's own command.
type ForkKey struct {
	Action ForkAction
	Name   string
	Gen    int
	Err    string
	Cmd    tea.Cmd
}

func (d *Fork) Key(msg tea.KeyMsg) ForkKey {
	switch msg.String() {
	case "esc":
		return ForkKey{Action: ForkCancel}
	case "enter":
		name := d.Name()
		if name == "" {
			return ForkKey{Action: ForkInvalid, Err: "name cannot be empty"}
		}
		return ForkKey{Action: ForkSubmit, Name: name, Gen: d.gen}
	}
	var cmd tea.Cmd
	d.name, cmd = d.name.Update(msg)
	return ForkKey{Cmd: cmd}
}

// ForkStyles are the root's text styles and field renderers the card uses.
type ForkStyles struct {
	BranchGlyph string
	Value       lipgloss.Style
	Group       lipgloss.Style
	// Field lays out a labelled form row; Input renders a text field with
	// the root's cursor marker.
	Field func(label, value string, focused bool) string
	Input func(textinput.Model) string
}

func (d Fork) View(styles ForkStyles) Card {
	body := "  source  " + styles.Value.Render(d.sourceName) + "\n" +
		"  group   " + styles.Group.Render(displayGroup(d.sourceGroup)) + "\n" +
		styles.Field("name", styles.Input(d.name), true)
	return Card{Title: styles.BranchGlyph + " Fork Session", Body: body, Hints: [][2]string{{"↵", "create"}, {"esc", "cancel"}}}
}

// cleanName trims a typed name and keeps it one path segment.
func cleanName(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "/", "-")
}
