package ui

import (
	"os"
	"path/filepath"
	"strings"
)

const maxPathSuggestions = 5

type pathComplete struct {
	suggestions []string
	index       int
	chosen      bool
}

func (pc *pathComplete) reset() {
	pc.suggestions = nil
	pc.index = 0
	pc.chosen = false
}

func (pc *pathComplete) active() bool { return len(pc.suggestions) > 0 }

func (pc *pathComplete) move(delta int) {
	if !pc.active() {
		return
	}
	pc.index = (pc.index + delta + len(pc.suggestions)) % len(pc.suggestions)
	pc.chosen = true
}

func (pc *pathComplete) selected() string {
	if !pc.active() {
		return ""
	}
	return pc.suggestions[pc.index]
}

func (pc *pathComplete) recompute(typed string) {
	pc.suggestions = completeDirs(typed)
	pc.index = 0
	pc.chosen = false
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

func (m *Model) applyPathSuggestion() {
	path := m.pathSugg.selected() + "/"
	switch m.mode {
	case modeForm:
		m.form.dir.SetValue(path)
		m.form.dir.CursorEnd()
		m.form.dirAuto = false
	case modeGroupForm:
		m.groupForm.path.SetValue(path)
		m.groupForm.path.CursorEnd()
		m.groupForm.pathAuto = false
	}
	m.pathSugg.recompute(path)
}
