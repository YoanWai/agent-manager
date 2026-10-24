package dialogs

import (
	"cmp"
	"path/filepath"

	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// GroupFormSurface is the New Group card. It owns the fields, the parent
// picker and the path completer, and asks the root for the work that needs
// the workspace or the effect lane with messages: a submit and a base step.
// The directory and base each parent lends a child are read when it opens,
// as the picker's rows were.
type GroupFormSurface struct {
	GroupForm
	pc        pathcomplete.State
	base      string
	remote    Remote
	parents   []spawn.GroupOption
	parent    int
	lends     map[string]Lend
	worktrees []string
	styles    spawn.Styles
	root      GroupFormRoot
}

// Remote is the SSH connection and group a card creates on: the group is
// fixed and the directory is the host's to resolve.
type Remote struct{ Host, Group string }

func (r Remote) on() bool { return r.Host != "" }

type Lend struct{ Dir, Base string }

type GroupFormRoot interface {
	CardWidth(cols int) int
	ReportErr(text string)
}

type GroupFormSubmitMsg struct {
	Request GroupFormRequest
	Base    string
	Remote  Remote
}

// GroupBaseAskMsg asks the root to step the open form's base choice, which
// reads the repo's branches.
type GroupBaseAskMsg struct{ Gen, Delta int }

// FormOptions is what the root seeds a card with: the workspace reads that
// would otherwise need a root, and the tokens it paints with.
type FormOptions struct {
	Gen       int
	Dir       string
	Cols      int
	Parents   []spawn.GroupOption
	Parent    int
	Lends     map[string]Lend
	Remote    Remote
	Worktrees []string
	Styles    spawn.Styles
}

func NewGroupFormSurface(root GroupFormRoot, opts FormOptions) *GroupFormSurface {
	s := &GroupFormSurface{
		GroupForm: NewGroupForm(opts.Dir, opts.Gen),
		pc:        pathcomplete.State{Generation: uint64(opts.Gen) << 32},
		remote:    opts.Remote,
		parents:   opts.Parents,
		parent:    opts.Parent,
		lends:     opts.Lends,
		worktrees: opts.Worktrees,
		styles:    opts.Styles,
		root:      root,
	}
	if s.remote.on() {
		s.AimRemote(s.remote.Host)
	}
	s.SyncWidths(groupFieldWidth(root.CardWidth(opts.Cols)))
	return s
}

func groupFieldWidth(cardWidth int) int {
	return CardInnerWidth(cardWidth) - spawn.LabelColumn - 3
}

func (s *GroupFormSurface) Parents() []spawn.GroupOption { return s.parents }

func (s *GroupFormSurface) Parent() int { return s.parent }

// SelectParent highlights row index without moving the inherited path, for
// a click that only places the cursor.
func (s *GroupFormSurface) SelectParent(index int) { s.parent = index }

func (s *GroupFormSurface) Selected() string {
	if s.parent >= 0 && s.parent < len(s.parents) {
		return s.parents[s.parent].Path
	}
	return ""
}

// ParentPath is the group the form creates under: the connection's
// selected group, or the picker's.
func (s *GroupFormSurface) ParentPath() string {
	if s.remote.on() {
		return s.remote.Group
	}
	return s.Selected()
}

// RemoteTarget is the connection the card creates on, empty for a local
// group.
func (s *GroupFormSurface) RemoteTarget() Remote { return s.remote }

// Base is the group's own base choice, empty for auto.
func (s *GroupFormSurface) Base() string { return s.base }

func (s *GroupFormSurface) BaseRef() *string { return &s.base }

func (s *GroupFormSurface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	return true, s.run(h, s.GroupForm.Key(msg, dialogPaths{&s.pc}, s.ParentPath(), len(s.worktrees)))
}

func (s *GroupFormSurface) Click(h surface.Host, line int) tea.Cmd {
	return s.run(h, s.GroupForm.Click(line, dialogPaths{&s.pc}, len(s.worktrees)))
}

func (s *GroupFormSurface) Wheel(h surface.Host, arrow tea.KeyMsg) tea.Cmd {
	step := 1
	if arrow.Type == tea.KeyUp {
		step = -1
	}
	return s.run(h, s.GroupForm.Wheel(step))
}

func (s *GroupFormSurface) run(h surface.Host, key GroupFormKey) tea.Cmd {
	switch key.Action {
	case GroupFormCancel:
		h.Pop()
		return nil
	case GroupFormComplete:
		return s.apply()
	case GroupFormStepParent:
		s.StepParent(key.Delta)
		return nil
	case GroupFormPickParent:
		s.StepParent(key.Entry - s.parent)
		return nil
	case GroupFormPickPath:
		if s.pc.Pick(key.Entry) {
			return s.apply()
		}
	case GroupFormStepBase:
		ask := GroupBaseAskMsg{Gen: s.Gen(), Delta: key.Delta}
		return func() tea.Msg { return ask }
	case GroupFormInvalid:
		s.root.ReportErr(key.Err)
		return nil
	case GroupFormSubmit:
		submit := GroupFormSubmitMsg{Request: key.Request, Base: s.base, Remote: s.remote}
		return func() tea.Msg { return submit }
	}
	// A connection's directory is the host's: nothing here completes it.
	if key.Suggest && !s.remote.on() {
		return tea.Batch(key.Cmd, s.pc.Request(pathcomplete.Group, s.Dir(), pathcomplete.SystemReader{}))
	}
	return key.Cmd
}

// StepParent moves within the expanded picker, wrapping at the ends, and
// re-resolves the path while it is still the parent's default.
func (s *GroupFormSurface) StepParent(delta int) {
	if len(s.parents) == 0 {
		return
	}
	s.parent = (s.parent + delta + len(s.parents)) % len(s.parents)
	if s.DirAuto() {
		s.SetDir(s.lends[s.Selected()].Dir)
	}
}

func (s *GroupFormSurface) apply() tea.Cmd {
	path := s.pc.Selected() + string(filepath.Separator)
	s.TakePath(path)
	return s.pc.Request(pathcomplete.Group, path, pathcomplete.SystemReader{})
}

func (s *GroupFormSurface) View(h surface.Host, _ string) string {
	cols, _ := h.Size()
	s.SyncWidths(groupFieldWidth(s.root.CardWidth(cols)))
	parent := s.ParentPath()
	view := GroupFormView{
		GlyphMode: s.styles.GlyphMode,
		Field: func(label, value string, focused bool) string {
			return spawn.FormField(s.styles, label, value, focused)
		},
		Input:     s.styles.Input,
		Badge:     groupBadge(s.styles),
		Parent:    parent,
		Picker:    func() string { return spawn.ViewGroupOptions(s.parents, s.parent, s.styles) },
		Subtle:    s.styles.Subtle,
		Value:     s.styles.Value,
		Worktrees: s.worktrees,
		Base:      BaseChoice(s.styles.Subtle, s.styles.Value, s.base, s.lends[parent].Base),
	}
	if s.pc.Active() {
		view.Suggestions = func() string { return s.pc.View(pathStyles(s.styles)) }
		view.SuggestHints = pathSuggestHints(s.pc.Chosen)
	}
	card := s.GroupForm.View(view)
	return h.CardSized(s.root.CardWidth(cols), card.Title, card.Body, card.Hints)
}

// Footer is empty: the card paints its own hints.
func (s *GroupFormSurface) Footer() [][2]string { return nil }

func (s *GroupFormSurface) Owner() surface.Owner { return surface.Manager }

// Dismissible: the form holds a typed draft, so a stray click keeps it.
func (s *GroupFormSurface) Dismissible() bool { return false }

func (s *GroupFormSurface) Leave(surface.Host) {}

func (s *GroupFormSurface) PathTarget() pathcomplete.Target { return pathcomplete.Group }
func (s *GroupFormSurface) PathValue() (string, bool)       { return s.Dir(), true }
func (s *GroupFormSurface) TakePath(path string)            { s.GroupForm.TakePath(path) }
func (s *GroupFormSurface) Paths() *pathcomplete.State      { return &s.pc }

func groupBadge(st spawn.Styles) func(string) string {
	return func(text string) string { return lipgloss.NewStyle().Foreground(st.Accent2).Render(text) }
}

func pathStyles(st spawn.Styles) pathcomplete.Styles {
	return pathcomplete.Styles{GlyphMode: st.GlyphMode, Muted: st.Muted, Accent: st.Accent, Accent2: st.Accent2}
}

func pathSuggestHints(chosen bool) [][2]string {
	if chosen {
		return [][2]string{{"↑↓", "pick"}, {"↵/tab", "complete"}, {"esc", "close"}}
	}
	return [][2]string{{"↑↓", "pick"}, {"tab", "complete"}, {"↵", "create"}, {"esc", "close"}}
}

// BaseChoice renders a group's base picker: its own ref, or auto and the
// parent's choice that auto inherits.
func BaseChoice(subtle, value lipgloss.Style, base, inherited string) string {
	choice := subtle.Render("◂ ") + value.Render(cmp.Or(base, "auto")) + subtle.Render(" ▸")
	if base == "" && inherited != "" {
		choice += subtle.Render("  " + inherited + " from parent")
	}
	return choice
}
