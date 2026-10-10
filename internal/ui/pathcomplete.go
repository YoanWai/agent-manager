package ui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// fresh is an empty completer whose reads outrank every read pc started, so
// a reopened dialog never takes a scan its previous opening asked for.
func (pc pathComplete) fresh() pathComplete { return pathComplete{generation: pc.generation + 1} }

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

// completer is the path completer of the dialog a read was started from.
func (m *Model) completer(target pathSuggestionTarget) *pathComplete {
	switch target {
	case pathSuggestionGroup:
		return &m.groupForm.paths
	case pathSuggestionRename:
		return &m.rename.paths
	}
	return &m.form.paths
}

func (m *Model) requestPathSuggestions(target pathSuggestionTarget, typed string) tea.Cmd {
	return m.completer(target).request(target, typed, systemPathSuggestionReader{})
}

// request starts a fresh read for typed, retiring any read in flight.
func (pc *pathComplete) request(target pathSuggestionTarget, typed string, reader pathSuggestionReader) tea.Cmd {
	pc.reset()
	request := pathSuggestionsRequest{target: target, generation: pc.generation, typed: typed}
	return pathSuggestionsCmd(request, reader)
}

// pathCompleteHost reports the value of the path field a read was started from,
// and false once that field is off screen.
type pathCompleteHost interface {
	pathSuggestionField(target pathSuggestionTarget) (string, bool)
}

func (m *Model) pathSuggestionField(target pathSuggestionTarget) (string, bool) {
	switch target {
	case pathSuggestionForm:
		if m.mode != modeForm {
			return "", false
		}
		return m.form.dir.Value(), true
	case pathSuggestionGroup:
		if m.mode != modeGroupForm {
			return "", false
		}
		return m.groupForm.path.Value(), true
	case pathSuggestionRename:
		if m.mode != modeRename || !m.rename.isGroup {
			return "", false
		}
		return m.rename.dir.Value(), true
	}
	return "", false
}

// handle takes a read's suggestions only while its field still holds what
// was typed.
func (pc *pathComplete) handle(h pathCompleteHost, msg pathSuggestionsMsg) {
	request := msg.request
	if request.generation != pc.generation {
		return
	}
	current, open := h.pathSuggestionField(request.target)
	if !open || current != request.typed {
		return
	}
	pc.suggestions = msg.suggestions
	pc.index = 0
	pc.chosen = false
}

// viewPathSuggestions is the dropdown under the rename dialog's path field.
func (m *Model) viewPathSuggestions() string { return m.rename.paths.view() }

// view renders the directory-completion dropdown under a focused path
// field.
func (pc *pathComplete) view() string {
	var b strings.Builder
	for i, path := range pc.suggestions {
		marker := "  "
		style := mutedStyle
		if i == pc.index {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			style = lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
		}
		b.WriteString("      " + marker + style.Render(truncateTail(path, 40)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
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
	var path string
	var target pathSuggestionTarget
	switch m.mode {
	case modeForm:
		path = m.form.paths.selected() + "/"
		m.form.dir.SetValue(path)
		m.form.dir.CursorEnd()
		m.form.dirAuto = false
		target = pathSuggestionForm
	case modeGroupForm:
		path = m.groupForm.paths.selected() + "/"
		m.groupForm.path.SetValue(path)
		m.groupForm.path.CursorEnd()
		m.groupForm.pathAuto = false
		target = pathSuggestionGroup
	case modeRename:
		path = m.rename.paths.selected() + "/"
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
