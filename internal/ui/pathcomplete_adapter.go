package ui

import (
	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// completer is the path completer of the dialog a read was started from.
// With that dialog no longer on top, a read lands on a blank completer
// whose generation no read carries, and is dropped.
func (m *Model) completer(target pathcomplete.Target) *pathcomplete.State {
	if s, ok := m.topPathCompleting(); ok && s.PathTarget() == target {
		return s.Paths()
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
	if s, ok := c.topPathCompleting(); ok && s.PathTarget() == target {
		return s.PathValue()
	}
	return "", false
}

// pathCompleteStyles copies the live palette for one paint of the list.
func pathCompleteStyles(mode presentation.GlyphMode) pathcomplete.Styles {
	return pathcomplete.Styles{
		GlyphMode: mode,
		Muted:     uitheme.MutedStyle(),
		Accent:    uitheme.ColorAccent(),
		Accent2:   uitheme.ColorAccent2(),
	}
}

// topPathCompleting is the top surface, when it has a directory field.
func (m *Model) topPathCompleting() (pathcomplete.Field, bool) {
	top, ok := m.top()
	if !ok {
		return nil, false
	}
	s, ok := top.(pathcomplete.Field)
	return s, ok
}

// pathRoot is the root as the completer reaches it.
type pathRoot struct{ *Model }

func (r pathRoot) PathCompleting() (pathcomplete.Field, bool) {
	return r.m.topPathCompleting()
}

func (r pathRoot) FormPathChanged() tea.Cmd { return r.m.formWorktreeProbeCmd(false) }

func (m *Model) applyPathSuggestion() tea.Cmd {
	return pathcomplete.Apply(pathRoot{m}, pathcomplete.SystemReader{})
}

var _ pathcomplete.Host = pathCompleter{}
