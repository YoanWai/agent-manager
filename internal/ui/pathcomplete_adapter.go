package ui

import (
	"path/filepath"

	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
)

// completer is the path completer of the dialog a read was started from.
// With that dialog no longer on top, a read lands on a blank completer
// whose generation no read carries, and is dropped.
func (m *Model) completer(target pathcomplete.Target) *pathcomplete.State {
	if s, ok := m.topPathCompleting(); ok && s.pathTarget() == target {
		return s.paths()
	}
	return &pathcomplete.State{}
}

func (m *Model) requestPathSuggestions(target pathcomplete.Target, typed string) tea.Cmd {
	return m.completer(target).Request(target, typed, pathcomplete.SystemReader{})
}

// pathCompleter is the root as the completer reads it: the value of the path
// field a read was started from, false once that field is off screen.
type pathCompleter struct{ *Model }

func (c pathCompleter) PathField(target pathcomplete.Target) (string, bool) {
	if s, ok := c.topPathCompleting(); ok && s.pathTarget() == target {
		return s.dir()
	}
	return "", false
}

// pathCompleteStyles copies the live palette for one paint of the list.
func pathCompleteStyles(mode presentation.GlyphMode) pathcomplete.Styles {
	return pathcomplete.Styles{
		GlyphMode: mode,
		Muted:     mutedStyle(),
		Accent:    colorAccent(),
		Accent2:   colorAccent2(),
	}
}

// topPathCompleting is the top surface, when it has a directory field.
func (m *Model) topPathCompleting() (pathCompleting, bool) {
	top, ok := m.top()
	if !ok {
		return nil, false
	}
	s, ok := top.(pathCompleting)
	return s, ok
}

func (m *Model) applyPathSuggestion() tea.Cmd {
	s, ok := m.topPathCompleting()
	if !ok {
		return nil
	}
	path := s.paths().Selected() + string(filepath.Separator)
	s.takePath(path)
	completion := m.requestPathSuggestions(s.pathTarget(), path)
	if s.pathTarget() == pathcomplete.Form {
		return tea.Batch(completion, m.formWorktreeProbeCmd(false))
	}
	return completion
}

var _ pathcomplete.Host = pathCompleter{}
