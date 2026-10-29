package dialogs

import (
	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// RenameSurface is the rename typed into its row. It paints no card: the
// rail and the detail column draw its fields in place, and the list holds
// still under it. It keeps what the root shares with the dialog: the path
// completion under the group's path field and the group base choice, which
// the root probes.
type RenameSurface struct {
	Rename
	complete pathcomplete.State
	Base     string
	Env      string
	root     RenameRoot
	// pressed is a left press on the group edit's fields, which a release
	// over them completes as a click.
	pressed bool
}

// RenameRoot is what the row reaches on the root beyond surface.Host. The
// request a submit validated stays the root's, so it builds one from the
// RenameRequest the dialog hands it.
type RenameRoot interface {
	ReportErr(text string)
	// ApplyPathSuggestion fills a completed directory into the top surface's
	// path field and reads the completions under it.
	ApplyPathSuggestion() tea.Cmd
	// StepBase steps the group base choice, probing the default path this
	// row would save.
	StepBase(s *RenameSurface, delta int) (string, tea.Cmd)
	QueueRename(request RenameRequest, base, env string) tea.Cmd
	// WorktreeCount is how many worktree choices the group field cycles
	// through, which the root's list of labels decides.
	WorktreeCount() int
	// EnvProfiles are the defined environment profiles, in picker order.
	EnvProfiles() []string
	// FieldsLine is the group edit's field line under the pointer, which the
	// root paints in the detail column.
	FieldsLine(x, y int) (line int, ok bool)
}

// NewRenameSurface opens the row on a rename the root seeded, its base choice
// and the root it reaches beyond the stack.
func NewRenameSurface(root RenameRoot, rename Rename, base, env string) *RenameSurface {
	return &RenameSurface{Rename: rename, Base: base, Env: env, root: root}
}

func (s *RenameSurface) Root() RenameRoot { return s.root }

// dialogPaths hands the rename and the group form their own completer as the
// PathSuggestions the fields read.
type dialogPaths struct{ *pathcomplete.State }

func (p dialogPaths) Active() bool        { return p.State.Active() }
func (p dialogPaths) Chosen() bool        { return p.State.Chosen }
func (p dialogPaths) Move(delta int) bool { return p.State.Move(delta) }
func (p dialogPaths) Reset()              { p.State.Reset() }

func (s *RenameSurface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	return true, s.run(h, s.Rename.Key(msg, dialogPaths{&s.complete}, s.root.WorktreeCount()))
}

// Click acts on a click on line of the group edit's fields, which the root
// paints in the detail column.
func (s *RenameSurface) Click(h surface.Host, line int) tea.Cmd {
	return s.run(h, s.Rename.Click(line, dialogPaths{&s.complete}, s.root.WorktreeCount()))
}

func (s *RenameSurface) run(h surface.Host, key RenameKey) tea.Cmd {
	switch key.Action {
	case RenameCancel:
		h.Pop()
		return nil
	case RenameComplete:
		return s.root.ApplyPathSuggestion()
	case RenamePickPath:
		if s.complete.Pick(key.Entry) {
			return s.root.ApplyPathSuggestion()
		}
		return nil
	case RenameStepBase:
		var cmd tea.Cmd
		s.Base, cmd = s.root.StepBase(s, key.Delta)
		return cmd
	case RenameStepEnv:
		s.Env = spawn.StepEnv(s.root.EnvProfiles(), s.Env, key.Delta)
		return nil
	case RenameInvalid:
		s.root.ReportErr(key.Err)
		return nil
	case RenameSubmit:
		return s.root.QueueRename(key.Request, s.Base, s.Env)
	}
	if key.Suggest {
		return tea.Batch(key.Cmd, s.complete.Request(pathcomplete.Rename, s.Dir(), pathcomplete.SystemReader{}))
	}
	return key.Cmd
}

func (s *RenameSurface) Wheel(surface.Host, tea.KeyMsg) tea.Cmd { return nil }

// Mouse answers a left click on the group edit's fields; the rest of the
// screen holds still while a rename is typed.
func (s *RenameSurface) Mouse(h surface.Host, msg tea.MouseMsg) tea.Cmd {
	line, onFields := s.root.FieldsLine(msg.X, msg.Y)
	onFields = onFields && s.IsGroup()
	switch msg.Action {
	case tea.MouseActionPress:
		s.pressed = onFields && msg.Button == tea.MouseButtonLeft
	case tea.MouseActionRelease:
		// X10 reporting names no button on any release, so only the press
		// tells a left click from a right one.
		pressed := s.pressed
		s.pressed = false
		if pressed && onFields {
			return s.Click(h, line)
		}
	}
	return nil
}

// View leaves the frame as it is: the row and the detail column draw the
// fields themselves.
func (s *RenameSurface) View(_ surface.Host, below string) string { return below }

func (s *RenameSurface) Footer() [][2]string { return s.Hints() }

func (s *RenameSurface) Owner() surface.Owner { return surface.Manager }

// Dismissible: the name and path are a draft, so a stray click or ctrl+c
// keeps them; esc cancels. The row also holds the list still, so the mouse
// treats it as no card at all.
func (s *RenameSurface) Dismissible() bool { return false }

func (s *RenameSurface) Leave(surface.Host) {}

func (s *RenameSurface) PathTarget() pathcomplete.Target { return pathcomplete.Rename }

// PathValue is the path field of a group edit; a session rename has none.
func (s *RenameSurface) PathValue() (string, bool) { return s.Dir(), s.IsGroup() }

func (s *RenameSurface) TakePath(path string) { s.SetDir(path) }

func (s *RenameSurface) Paths() *pathcomplete.State { return &s.complete }
