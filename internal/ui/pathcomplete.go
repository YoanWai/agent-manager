package ui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const maxPathSuggestions = 5

type pathComplete struct {
	suggestions []string
	index       int
	chosen      bool
	generation  uint64
}

func (pc *pathComplete) reset() {
	pc.generation++
	pc.suggestions = nil
	pc.index = 0
	pc.chosen = false
}

func (pc *pathComplete) active() bool { return len(pc.suggestions) > 0 }

// move advances within the suggestions without wrapping. It returns false
// when the cursor is already at the requested edge so the containing form can
// continue focus navigation instead of trapping the user in this list.
func (pc *pathComplete) move(delta int) bool {
	if !pc.active() {
		return false
	}
	if !pc.chosen {
		if delta < 0 {
			return false
		}
		pc.index = 0
		pc.chosen = true
		return true
	}
	next := pc.index + delta
	if next < 0 || next >= len(pc.suggestions) {
		return false
	}
	pc.index = next
	pc.chosen = true
	return true
}

func (pc *pathComplete) selected() string {
	if !pc.active() {
		return ""
	}
	return pc.suggestions[pc.index]
}

type pathSuggestionTarget uint8

const (
	pathSuggestionForm pathSuggestionTarget = iota
	pathSuggestionGroup
	pathSuggestionRename
)

type pathSuggestionsRequest struct {
	target     pathSuggestionTarget
	generation uint64
	typed      string
}

type pathSuggestionsMsg struct {
	request     pathSuggestionsRequest
	suggestions []string
}

type pathSuggestionReader interface {
	complete(typed string) []string
}

type systemPathSuggestionReader struct{}

func (systemPathSuggestionReader) complete(typed string) []string { return completeDirs(typed) }

func pathSuggestionsCmd(request pathSuggestionsRequest, reader pathSuggestionReader) tea.Cmd {
	return func() tea.Msg {
		return pathSuggestionsMsg{request: request, suggestions: reader.complete(request.typed)}
	}
}

func (m *Model) requestPathSuggestions(target pathSuggestionTarget, typed string) tea.Cmd {
	return m.requestPathSuggestionsWithReader(target, typed, systemPathSuggestionReader{})
}

func (m *Model) requestPathSuggestionsWithReader(target pathSuggestionTarget, typed string, reader pathSuggestionReader) tea.Cmd {
	m.pathSugg.reset()
	request := pathSuggestionsRequest{target: target, generation: m.pathSugg.generation, typed: typed}
	return pathSuggestionsCmd(request, reader)
}

func (m *Model) handlePathSuggestions(msg pathSuggestionsMsg) (tea.Model, tea.Cmd) {
	request := msg.request
	if request.generation != m.pathSugg.generation {
		return m, nil
	}
	var current string
	switch request.target {
	case pathSuggestionForm:
		if m.mode != modeForm {
			return m, nil
		}
		current = m.form.dir.Value()
	case pathSuggestionGroup:
		if m.mode != modeGroupForm {
			return m, nil
		}
		current = m.groupForm.path.Value()
	case pathSuggestionRename:
		if m.mode != modeRename || !m.rename.isGroup {
			return m, nil
		}
		current = m.rename.dir.Value()
	default:
		return m, nil
	}
	if current != request.typed {
		return m, nil
	}
	m.pathSugg.suggestions = msg.suggestions
	m.pathSugg.index = 0
	m.pathSugg.chosen = false
	return m, nil
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

// completeDirs matches shell completion: everything after the last slash
// is a partial name matched against directories inside its parent.
func completeDirs(typed string) []string {
	typed = expandHome(strings.TrimSpace(typed))
	if typed == "" || !strings.Contains(typed, "/") {
		return nil
	}
	parent, partial := filepath.Split(typed)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	partialLower := strings.ToLower(partial)
	var matches []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(partial, ".") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(name), partialLower) {
			continue
		}
		if !isDirEntry(parent, entry) {
			continue
		}
		matches = append(matches, filepath.Join(parent, name))
		if len(matches) == maxPathSuggestions {
			break
		}
	}
	return matches
}

// isDirEntry treats symlinks to directories as directories (e.g. /tmp on macOS).
func isDirEntry(parent string, entry os.DirEntry) bool {
	if entry.IsDir() {
		return true
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(parent, entry.Name()))
	return err == nil && info.IsDir()
}

func (m *Model) applyPathSuggestion() tea.Cmd {
	path := m.pathSugg.selected() + "/"
	var target pathSuggestionTarget
	switch m.mode {
	case modeForm:
		m.form.dir.SetValue(path)
		m.form.dir.CursorEnd()
		m.form.dirAuto = false
		target = pathSuggestionForm
	case modeGroupForm:
		m.groupForm.path.SetValue(path)
		m.groupForm.path.CursorEnd()
		m.groupForm.pathAuto = false
		target = pathSuggestionGroup
	case modeRename:
		m.rename.dir.SetValue(path)
		m.rename.dir.CursorEnd()
		target = pathSuggestionRename
	}
	completion := m.requestPathSuggestions(target, path)
	if target == pathSuggestionForm {
		return tea.Batch(completion, m.formWorktreeProbeCmd(false))
	}
	return completion
}
