// Package pathcomplete is directory completion for a path field: the
// suggestions a field offers, the highlight over them, and the read that
// fills them. The New Session form, the New Group form and a group rename
// each hold one State and hand the root only the typed text.
package pathcomplete

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const maxSuggestions = 5

// State is one field's completion: what a read answered, the highlight, and
// the generation a read is dropped against.
type State struct {
	Suggestions []string
	Index       int
	Chosen      bool
	Generation  uint64
}

// Styles are the root's presentation tokens, copied for one paint.
type Styles struct {
	GlyphMode       presentation.GlyphMode
	Muted           lipgloss.Style
	Accent, Accent2 lipgloss.Color
}

// Target names the field a read was started from, so its answer lands on
// that field and nowhere else.
type Target uint8

const (
	Form Target = iota
	Group
	Rename
)

// Request is the read a field started, carried by its answer so the field
// can tell whether it is still the one that asked.
type Request struct {
	Target     Target
	Generation uint64
	Typed      string
}

// Msg is a read's answer, arriving on the update path.
type Msg struct {
	Request     Request
	Suggestions []string
}

// Reader answers what a path field could complete to.
type Reader interface {
	Complete(typed string) []string
}

// SystemReader completes against the local filesystem.
type SystemReader struct{}

func (SystemReader) Complete(typed string) []string { return completeDirs(typed) }

func suggestionsCmd(request Request, reader Reader) tea.Cmd {
	return func() tea.Msg {
		return Msg{Request: request, Suggestions: reader.Complete(request.Typed)}
	}
}

// Field is a surface with a directory field: tab completes a path into it.
// The root asks the top surface for it instead of knowing which cards have
// one.
type Field interface {
	// PathTarget names the field for the shared completer and its reads.
	PathTarget() Target
	// PathValue is the field's current text, false when the surface shows
	// no path field.
	PathValue() (string, bool)
	// TakePath puts a completed directory into the field.
	TakePath(path string)
	// Paths is the field's completer state.
	Paths() *State
}

// Host reports the value of the path field a read was started from, and
// false once that field is off screen.
type Host interface {
	PathField(target Target) (string, bool)
}

// Reset clears the list and starts a generation, so a read still in flight
// outranks nothing: a reopened dialog never takes a scan its previous
// opening asked for.
func (s *State) Reset() {
	s.Generation++
	s.Suggestions = nil
	s.Index = 0
	s.Chosen = false
}

func (s *State) Active() bool { return len(s.Suggestions) > 0 }

// Move advances within the suggestions without wrapping. It returns false
// when the cursor is already at the requested edge so the containing form can
// continue focus navigation instead of trapping the user in this list.
func (s *State) Move(delta int) bool {
	if !s.Active() {
		return false
	}
	if !s.Chosen {
		if delta < 0 {
			return false
		}
		s.Index = 0
		s.Chosen = true
		return true
	}
	next := s.Index + delta
	if next < 0 || next >= len(s.Suggestions) {
		return false
	}
	s.Index = next
	s.Chosen = true
	return true
}

// Pick highlights suggestion i, read off the painted list, and reports
// false when a newer read has since shortened the list past it.
func (s *State) Pick(i int) bool {
	if i < 0 || i >= len(s.Suggestions) {
		return false
	}
	s.Index, s.Chosen = i, true
	return true
}

func (s *State) Selected() string {
	if !s.Active() {
		return ""
	}
	return s.Suggestions[s.Index]
}

// Request starts a fresh read for typed, retiring any read in flight.
func (s *State) Request(target Target, typed string, reader Reader) tea.Cmd {
	s.Reset()
	request := Request{Target: target, Generation: s.Generation, Typed: typed}
	return suggestionsCmd(request, reader)
}

// Handle takes a read's suggestions only while its field still holds what
// was typed.
func (s *State) Handle(h Host, msg Msg) {
	request := msg.Request
	if request.Generation != s.Generation {
		return
	}
	current, open := h.PathField(request.Target)
	if !open || current != request.Typed {
		return
	}
	s.Suggestions = msg.Suggestions
	s.Index = 0
	s.Chosen = false
}

// View renders the directory-completion dropdown under a focused path
// field.
func (s *State) View(styles Styles) string {
	var b strings.Builder
	for i, path := range s.Suggestions {
		marker := "  "
		style := styles.Muted
		if i == s.Index {
			marker = lipgloss.NewStyle().Foreground(styles.Accent).Render(presentation.GlyphsFor(styles.GlyphMode).SelectionGlyph + " ")
			style = lipgloss.NewStyle().Foreground(styles.Accent2).Bold(true)
		}
		b.WriteString("      " + marker + style.Render(presentation.TruncateTail(path, 40)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ExpandHome replaces a leading ~ with the user's home directory.
func ExpandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~"+string(filepath.Separator)) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

// completeDirs matches shell completion: everything after the last slash
// (or the platform's own separator) is a partial name matched against
// directories inside its parent.
func completeDirs(typed string) []string {
	typed = ExpandHome(strings.TrimSpace(typed))
	if typed == "" || !strings.ContainsAny(typed, "/"+string(filepath.Separator)) {
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
		if len(matches) == maxSuggestions {
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
