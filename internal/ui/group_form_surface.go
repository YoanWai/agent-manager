package ui

import (
	"errors"
	"path/filepath"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// groupFormSurface is the New Group card. It owns the fields, the parent
// picker and the path completer, and asks the root for the work that needs
// the workspace or the effect lane with messages: a submit and a base step.
// The directory and base each parent lends a child are read when it opens,
// as the picker's rows were.
type groupFormSurface struct {
	form    uidialogs.GroupForm
	pc      pathComplete
	base    string
	remote  formRemote
	parents []groupOption
	parent  int
	lends   map[string]groupLend
	styles  spawn.Styles
}

// groupLend is what a group gives a child group made under it.
type groupLend struct {
	dir  string
	base string
}

// groupFormSubmitMsg asks the root to create the group a submit validated.
type groupFormSubmitMsg struct {
	request uidialogs.GroupFormRequest
	base    string
	remote  formRemote
}

// groupBaseAskMsg asks the root to step the open form's base choice, which
// reads the repo's branches.
type groupBaseAskMsg struct{ gen, delta int }

func newGroupFormSurface(gen int, dir string, cols int, parents []groupOption, parent int, lends map[string]groupLend, remote formRemote, styles spawn.Styles) *groupFormSurface {
	s := &groupFormSurface{
		form:    uidialogs.NewGroupForm(dir, gen),
		pc:      pathComplete{generation: uint64(gen) << 32},
		remote:  remote,
		parents: parents,
		parent:  parent,
		lends:   lends,
		styles:  styles,
	}
	if remote.on() {
		s.form.AimRemote(remote.host)
	}
	s.form.SyncWidths(groupFieldWidth(cols))
	return s
}

func (m *Model) openGroupForm() {
	gen, cols, styles := m.nextComposerGen(), m.layout.width, m.spawnStyles()
	if target, ok := m.remoteTarget(); ok {
		m.push(newGroupFormSurface(gen, "", cols, nil, 0, nil, target, styles))
	} else {
		parents, at := m.groupOptions(m.contextGroup())
		lends := make(map[string]groupLend, len(parents))
		for _, opt := range parents {
			lends[opt.path] = groupLend{dir: m.capturedAncestorGroupDir(opt.path), base: m.groupBase(opt.path)}
		}
		m.push(newGroupFormSurface(gen, m.capturedGroupDefaultDir(parents[at].path), cols, parents, at, lends, formRemote{}, styles))
	}
	m.clearErr()
}

// groupFieldWidth is the columns the name and path fields take in a card on
// a terminal cols wide.
func groupFieldWidth(cols int) int {
	return cardInnerWidth(cardWidthFor(cols)) - spawn.LabelColumn - 3
}

// selected is the group the picker highlights.
func (s *groupFormSurface) selected() string {
	if s.parent >= 0 && s.parent < len(s.parents) {
		return s.parents[s.parent].path
	}
	return ""
}

// parentPath is the group the form creates under: the connection's
// selected group, or the picker's.
func (s *groupFormSurface) parentPath() string {
	if s.remote.on() {
		return s.remote.group
	}
	return s.selected()
}

func (s *groupFormSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	return true, s.run(h, s.form.Key(msg, dialogPaths{&s.pc}, s.parentPath(), len(groupWorktreeOptions)))
}

func (s *groupFormSurface) Click(h surfaceHost, line int) tea.Cmd {
	return s.run(h, s.form.Click(line, dialogPaths{&s.pc}, len(groupWorktreeOptions)))
}

func (s *groupFormSurface) Wheel(h surfaceHost, arrow tea.KeyMsg) tea.Cmd {
	step := 1
	if arrow.Type == tea.KeyUp {
		step = -1
	}
	return s.run(h, s.form.Wheel(step))
}

// run executes what a form key asked of the surface and the root.
func (s *groupFormSurface) run(h surfaceHost, key uidialogs.GroupFormKey) tea.Cmd {
	switch key.Action {
	case uidialogs.GroupFormCancel:
		h.Pop()
		return nil
	case uidialogs.GroupFormComplete:
		return s.apply()
	case uidialogs.GroupFormStepParent:
		s.stepParent(key.Delta)
		return nil
	case uidialogs.GroupFormPickParent:
		s.stepParent(key.Entry - s.parent)
		return nil
	case uidialogs.GroupFormPickPath:
		if s.pc.pick(key.Entry) {
			return s.apply()
		}
	case uidialogs.GroupFormStepBase:
		ask := groupBaseAskMsg{gen: s.form.Gen(), delta: key.Delta}
		return func() tea.Msg { return ask }
	case uidialogs.GroupFormInvalid:
		err := errors.New(key.Err)
		return func() tea.Msg { return errMsg{err: err} }
	case uidialogs.GroupFormSubmit:
		submit := groupFormSubmitMsg{request: key.Request, base: s.base, remote: s.remote}
		return func() tea.Msg { return submit }
	}
	// A connection's directory is the host's: nothing here completes it.
	if key.Suggest && !s.remote.on() {
		return tea.Batch(key.Cmd, s.pc.request(pathSuggestionGroup, s.form.Dir(), systemPathSuggestionReader{}))
	}
	return key.Cmd
}

// stepParent moves within the expanded picker, wrapping at the ends, and
// re-resolves the path while it is still the parent's default.
func (s *groupFormSurface) stepParent(delta int) {
	if len(s.parents) == 0 {
		return
	}
	s.parent = (s.parent + delta + len(s.parents)) % len(s.parents)
	if s.form.DirAuto() {
		s.form.SetDir(s.lends[s.selected()].dir)
	}
}

func (s *groupFormSurface) apply() tea.Cmd {
	path := s.pc.selected() + string(filepath.Separator)
	s.form.TakePath(path)
	return s.pc.request(pathSuggestionGroup, path, systemPathSuggestionReader{})
}

func (s *groupFormSurface) View(h surfaceHost, _ string) string {
	cols, _ := h.Size()
	s.form.SyncWidths(groupFieldWidth(cols))
	parent := s.parentPath()
	view := uidialogs.GroupFormView{
		GlyphMode: s.styles.GlyphMode,
		Field: func(label, value string, focused bool) string {
			return spawn.FormField(s.styles, label, value, focused)
		},
		Input:     textInputView,
		Badge:     groupBadge,
		Parent:    parent,
		Picker:    func() string { return viewGroupOptions(s.parents, s.parent, s.styles.GlyphMode) },
		Subtle:    subtleStyle(),
		Value:     valueStyle(),
		Worktrees: groupWorktreeOptions,
		Base:      groupBaseChoice(s.base, s.lends[parent].base),
	}
	if s.pc.active() {
		view.Suggestions = func() string { return s.pc.view(s.styles.GlyphMode) }
		view.SuggestHints = pathSuggestHint(s.pc.chosen)
	}
	card := s.form.View(view)
	return h.CardSized(cardWidthFor(cols), card.Title, card.Body, card.Hints)
}

// Footer is empty: the card paints its own hints.
func (s *groupFormSurface) Footer() [][2]string { return nil }

func (s *groupFormSurface) Owner() inputOwner { return ownerManager }

// Dismissible: the form holds a typed draft, so a stray click keeps it.
func (s *groupFormSurface) Dismissible() bool { return false }

func (s *groupFormSurface) Leave(surfaceHost) {}

func (s *groupFormSurface) pathTarget() pathSuggestionTarget { return pathSuggestionGroup }
func (s *groupFormSurface) dir() (string, bool)              { return s.form.Dir(), true }
func (s *groupFormSurface) takePath(path string)             { s.form.TakePath(path) }
func (s *groupFormSurface) paths() *pathComplete             { return &s.pc }
