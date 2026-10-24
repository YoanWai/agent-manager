package spawn

import (
	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// Surface is the New Session form over the list. It owns the form, its
// directory completion, its group picker and the connection it spawns into.
// The prompt box stays on the root, which holds it in composer, and composer
// imports this package. It holds a draft, so only esc, a spawn that lands,
// or a screen taking the list's place closes it.
type Surface struct {
	Form
	Picker
	Complete pathcomplete.State
	// Remote is the connection the spawn goes to; the zero value is this
	// machine.
	Remote Remote
	root   SurfaceRoot
}

type Remote struct{ Host, Group string }

// On is whether the spawn goes to a connection rather than this machine.
func (r Remote) On() bool { return r.Host != "" }

type SurfaceRoot interface {
	FormKey(msg tea.KeyMsg) tea.Cmd
	FormClick(line int) tea.Cmd
	FormWheel(step int) tea.Cmd
	FormView() string
	TakeForeground()
	// ReleasePrompt lets go of the prompt box's images; a spawn has already
	// taken the ones it carries.
	ReleasePrompt()
}

func NewSurface(root SurfaceRoot, form Form, picker Picker, complete pathcomplete.State) *Surface {
	return &Surface{Form: form, Picker: picker, Complete: complete, root: root}
}

func (s *Surface) Root() SurfaceRoot { return s.root }

// AimRemote points the form at a connection: the group is fixed, the
// directory is the host's to resolve, and the worktree inherits its default
// until picked.
func (s *Surface) AimRemote(target Remote) {
	s.Remote = target
	s.Groups = []GroupOption{{Path: target.Group}}
	s.GroupIndex = 0
	s.TakePath("")
	s.SetDirPlaceholder("the group's directory on " + target.Host)
	s.SeedWorktree(false)
}

func (s *Surface) Key(_ surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	s.root.TakeForeground()
	return true, s.root.FormKey(msg)
}

func (s *Surface) Click(_ surface.Host, line int) tea.Cmd { return s.root.FormClick(line) }

func (s *Surface) Wheel(_ surface.Host, arrow tea.KeyMsg) tea.Cmd {
	step := 1
	if arrow.Type == tea.KeyUp {
		step = -1
	}
	return s.root.FormWheel(step)
}

// View is the form's card, which takes the frame while it is up.
func (s *Surface) View(surface.Host, string) string { return s.root.FormView() }

// Footer is empty: the card names its own keys.
func (s *Surface) Footer() [][2]string { return nil }

func (s *Surface) Owner() surface.Owner { return surface.Manager }

// Dismissible: the name, directory and prompt are a draft, so a stray
// click keeps them.
func (s *Surface) Dismissible() bool { return false }

// Leave lets go of the prompt's images; a spawn has already taken the ones
// it carries.
func (s *Surface) Leave(surface.Host) { s.root.ReleasePrompt() }

func (s *Surface) PathTarget() pathcomplete.Target { return pathcomplete.Form }

func (s *Surface) PathValue() (string, bool) { return s.Dir(), true }

func (s *Surface) Paths() *pathcomplete.State { return &s.Complete }
