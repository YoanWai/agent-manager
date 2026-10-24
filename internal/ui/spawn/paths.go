package spawn

import (
	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
	tea "github.com/charmbracelet/bubbletea"
)

// PathField is the form's directory field as the shared completer drives
// it: the state the form holds, the target its reads are tagged with, and
// whether the field offers suggestions at all.
type PathField struct {
	State  *pathcomplete.State
	Target pathcomplete.Target
	// Off asks for no suggestions, which a connection's directory takes:
	// the host owns that path.
	Off bool
	// Styles paint the open list.
	Styles pathcomplete.Styles
}

func (p PathField) Active() bool { return p.State.Active() }

func (p PathField) Chosen() bool { return p.State.Chosen }

func (p PathField) Move(delta int) bool { return p.State.Move(delta) }

func (p PathField) Reset() { p.State.Reset() }

func (p PathField) Suggest(typed string) tea.Cmd {
	if p.Off {
		return nil
	}
	return p.State.Request(p.Target, typed, pathcomplete.SystemReader{})
}

func (p PathField) View() string { return p.State.View(p.Styles) }

func (p PathField) Hint() [][2]string { return PathHint(p.State.Chosen) }

// PathHint names the keys that act on the open list: the complete key only
// creates once nothing is highlighted.
func PathHint(chosen bool) [][2]string {
	if chosen {
		return [][2]string{{"↑↓", "pick"}, {"↵/tab", "complete"}, {"esc", "close"}}
	}
	return [][2]string{{"↑↓", "pick"}, {"tab", "complete"}, {"↵", "create"}, {"esc", "close"}}
}
