package pathcomplete

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// Root is what completing a suggestion asks of the manager: the top surface
// with a directory field, and the probe a spawn form's new directory earns.
type Root interface {
	PathCompleting() (Field, bool)
	FormPathChanged() tea.Cmd
}

// Apply completes the highlighted suggestion into the field that asked, then
// starts the read the new value earns. A spawn form's field also probes the
// directory it now names.
func Apply(root Root, reader Reader) tea.Cmd {
	field, ok := root.PathCompleting()
	if !ok {
		return nil
	}
	path := field.Paths().Selected() + string(filepath.Separator)
	field.TakePath(path)
	completion := field.Paths().Request(field.PathTarget(), path, reader)
	if field.PathTarget() == Form {
		return tea.Batch(completion, root.FormPathChanged())
	}
	return completion
}
