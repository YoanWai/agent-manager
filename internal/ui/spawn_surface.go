package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// spawnSurface is the New Session form over the list. It owns the form, its
// prompt, its directory completion and its group picker. It holds a draft,
// so only esc, a spawn that lands, or a screen taking the list's place
// closes it.
type spawnSurface struct {
	formDialog
}

// spawnSurfaceHost is what the form reaches on the root beyond the stack.
type spawnSurfaceHost interface {
	handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd)
	handleFormClick(line int) (tea.Model, tea.Cmd)
	formWheel(step int) tea.Cmd
	viewForm() string
}

func newSpawnSurface(form formDialog) *spawnSurface { return &spawnSurface{formDialog: form} }

// form is the open New Session form, nil when none is on the stack.
func (m *Model) form() *spawnSurface {
	s, _ := surfaceOf[*spawnSurface](m)
	return s
}

// formFor is the open form when it is the one composerGen came from: a
// completion lands on the form that asked or is dropped.
func (m *Model) formFor(composerGen int) (*spawnSurface, bool) {
	s := m.form()
	return s, s != nil && s.prompt.gen == composerGen
}

// formHolds is whether the form composerGen came from still shows draft
// and no image: a spawn of exactly that prompt may take its place.
func (m *Model) formHolds(composerGen int, draft string) bool {
	f, ok := m.formFor(composerGen)
	return ok && f.prompt.input.Value() == draft && len(f.prompt.attachments) == 0
}

// closeForm takes the form off the stack wherever it sits: a spawn can land
// under a card opened since.
func (m *Model) closeForm() {
	for i := len(m.surfaces) - 1; i >= 0; i-- {
		if s, ok := m.surfaces[i].(*spawnSurface); ok {
			m.surfaces = append(m.surfaces[:i:i], m.surfaces[i+1:]...)
			s.Leave(m)
			return
		}
	}
}

func (s *spawnSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	_, cmd := h.(spawnSurfaceHost).handleFormKey(msg)
	return true, cmd
}

func (s *spawnSurface) Click(h surfaceHost, line int) tea.Cmd {
	_, cmd := h.(spawnSurfaceHost).handleFormClick(line)
	return cmd
}

func (s *spawnSurface) Wheel(h surfaceHost, arrow tea.KeyMsg) tea.Cmd {
	step := 1
	if arrow.Type == tea.KeyUp {
		step = -1
	}
	return h.(spawnSurfaceHost).formWheel(step)
}

// View is the form's card, which takes the frame while it is up.
func (s *spawnSurface) View(h surfaceHost, _ string) string { return h.(spawnSurfaceHost).viewForm() }

// Footer is empty: the card names its own keys.
func (s *spawnSurface) Footer() [][2]string { return nil }

func (s *spawnSurface) Owner() inputOwner { return ownerManager }

// Dismissible: the name, directory and prompt are a draft, so a stray
// click keeps them.
func (s *spawnSurface) Dismissible() bool { return false }

// Leave lets go of the prompt's images; a spawn has already taken the ones
// it carries.
func (s *spawnSurface) Leave(surfaceHost) { s.prompt.release() }

func (s *spawnSurface) pathTarget() pathSuggestionTarget { return pathSuggestionForm }

func (s *spawnSurface) dir() (string, bool) { return s.Dir(), true }

func (s *spawnSurface) takePath(path string) { s.TakePath(path) }

func (s *spawnSurface) paths() *pathComplete { return &s.complete }
