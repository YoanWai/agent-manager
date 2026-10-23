package ui

import (
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// spawnFormRoot is the root as the New Session form reaches it, holding the
// prompt box that cannot live in the package: ui/composer imports ui/spawn,
// so the form's prompt box stays on the root.
type spawnFormRoot struct {
	*Model
	prompt *composer.Composer
}

func (r spawnFormRoot) FormKey(msg tea.KeyMsg) tea.Cmd {
	_, cmd := r.handleFormKey(msg)
	return cmd
}

func (r spawnFormRoot) FormClick(line int) tea.Cmd {
	_, cmd := r.handleFormClick(line)
	return cmd
}

func (r spawnFormRoot) FormWheel(step int) tea.Cmd { return r.formWheel(step) }

func (r spawnFormRoot) FormView() string { return r.viewForm() }

func (r spawnFormRoot) TakeForeground() { r.takeForeground() }

func (r spawnFormRoot) ReleasePrompt() { r.prompt.Release() }

// newSpawnSurface builds the New Session form over the root's own prompt
// box, once the root has the completer state it starts from.
func (m *Model) newSpawnSurface(prompt *composer.Composer, complete pathcomplete.State, form spawn.Form, picker spawn.Picker) *spawn.Surface {
	return spawn.NewSurface(spawnFormRoot{m, prompt}, form, picker, complete)
}

// form is the open New Session form, nil when none is on the stack.
func (m *Model) form() *spawn.Surface {
	s, _ := surfaceOf[*spawn.Surface](m)
	return s
}

// formPromptFor is the prompt box of the open New Session form, nil when
// none is up.
func (m *Model) formPromptFor() *composer.Composer {
	f := m.form()
	if f == nil {
		return nil
	}
	return f.Root().(spawnFormRoot).prompt
}

// formFor is the open form when it is the one composerGen came from.
func (m *Model) formFor(composerGen int) (*spawn.Surface, bool) {
	s := m.form()
	return s, s != nil && s.Root().(spawnFormRoot).prompt.Gen == composerGen
}

// formHolds is whether the form composerGen came from still shows draft
// and no image: a spawn of exactly that prompt may take its place.
func (m *Model) formHolds(composerGen int, draft string) bool {
	f, ok := m.formFor(composerGen)
	if !ok {
		return false
	}
	prompt := f.Root().(spawnFormRoot).prompt
	return prompt.Input.Value() == draft && len(prompt.Attachments) == 0
}

// formDraftHolds is formHolds for a spawn that also named a name and a
// directory: only the form that dispatched it may be closed.
func (m *Model) formDraftHolds(composerGen int, draft, name, dir string) bool {
	f, ok := m.formFor(composerGen)
	return ok && m.formHolds(composerGen, draft) && f.Name() == name && f.Dir() == dir
}

// formPromptForGen is the prompt box of the form composerGen came from.
func (m *Model) formPromptForGen(composerGen int) (*composer.Composer, bool) {
	f, ok := m.formFor(composerGen)
	if !ok {
		return nil, false
	}
	return f.Root().(spawnFormRoot).prompt, true
}

// closeForm takes the form off the stack wherever it sits: a spawn can land
// under a card opened since.
func (m *Model) closeForm() {
	for i := len(m.surfaces) - 1; i >= 0; i-- {
		if s, ok := m.surfaces[i].(*spawn.Surface); ok {
			m.surfaces = append(m.surfaces[:i:i], m.surfaces[i+1:]...)
			s.Leave(m)
			return
		}
	}
}

// spawnedPrompt is the form's prompt box as spawn.Prompt.
func spawnedPrompt(f *spawn.Surface, target composer.ID, host composer.Host) promptBox {
	return promptBox{c: f.Root().(spawnFormRoot).prompt, target: target, host: host}
}

// spawnedPaths is the form's completer as spawn.Paths.
func (m *Model) spawnedPaths(f *spawn.Surface) pathField {
	// A connection's directory is the host's: nothing here completes it.
	return pathField{
		mode:   m.prefs.glyphMode,
		pc:     &f.Complete,
		target: pathcomplete.Form,
		off:    f.Remote.On(),
	}
}

var _ spawn.SurfaceRoot = spawnFormRoot{}

// pathField lends a feature package the root's path completer.
type pathField struct {
	mode   presentation.GlyphMode
	pc     *pathcomplete.State
	target pathcomplete.Target
	// off asks for no suggestions.
	off bool
}

func (p pathField) Active() bool { return p.pc.Active() }

func (p pathField) Chosen() bool { return p.pc.Chosen }

func (p pathField) Move(delta int) bool { return p.pc.Move(delta) }

func (p pathField) Reset() { p.pc.Reset() }

func (p pathField) Suggest(typed string) tea.Cmd {
	if p.off {
		return nil
	}
	return p.pc.Request(p.target, typed, pathcomplete.SystemReader{})
}

func (p pathField) View() string { return p.pc.View(pathCompleteStyles(p.mode)) }

func (p pathField) Hint() [][2]string { return pathSuggestHint(p.pc.Chosen) }
