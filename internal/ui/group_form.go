package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	gfName = iota
	gfParent
	gfPath
	gfWorktree
	gfBase
	gfCount
)

// groupWorktreeOptions are the picker states for a group's spawn-in-worktree
// choice; index 0 stores as "" so the group keeps inheriting.
var groupWorktreeOptions = []string{"inherit", "on", "off"}

func groupWorktreeValue(index int) string {
	switch index {
	case 1:
		return "on"
	case 2:
		return "off"
	}
	return ""
}

func groupWorktreeIndex(value string) int {
	switch value {
	case "on":
		return 1
	case "off":
		return 2
	}
	return 0
}

type groupForm struct {
	paths         pathComplete
	name          textinput.Model
	path          textinput.Model
	pathAuto      bool
	worktreeIndex int
	base          string
	focus         int
	// gen tells this group form from the one that stood in the same place
	// before it, so a completion cannot close a form the user since reopened.
	gen int
}

// groupFormDialog is the New Group form: its fields, keys, view,
// validation and the group request enter builds. The root opens it and
// runs what its groupFormRequest asks for.
type groupFormDialog struct{ groupForm }

// groupFormHost is what the group form reads from the root: the status
// bar, the path completer under its path field, the parent picker it
// shares with the New Session form, and the parent's defaults.
type groupFormHost interface {
	reportErr(text string)
	clearErr()
	selectedGroupPath() string
	viewGroupPicker() string
	groupBase(group string) string
	groupDirCandidates(group string) []string
}

// groupFormRequest is root work a group form key asks for once the form
// has taken its own share. At most one is set.
type groupFormRequest struct {
	close     bool
	applyPath bool
	// parent steps the shared group picker.
	parent int
	// base steps the base choice through the branches of the group's repo.
	base   int
	create *groupRequest
}

func (m *Model) syncGroupFormFieldWidths() {
	m.groupForm.syncWidths(m.formValueWidth() - 3)
}

func (d *groupFormDialog) syncWidths(width int) {
	d.name.Width = width
	d.path.Width = width
	d.name.SetCursor(d.name.Position())
	d.path.SetCursor(d.path.Position())
}

func (m *Model) openGroupForm() {
	name := textField("group-name", 60)
	name.Focus()
	m.groupForm = groupFormDialog{groupForm{
		paths:    m.groupForm.paths.fresh(),
		name:     name,
		path:     textField("default working directory", 400),
		pathAuto: true,
		focus:    gfName,
		gen:      m.nextComposerGen(),
	}}
	m.rebuildGroupOptions(m.contextGroup())
	m.groupForm.path.SetValue(m.capturedGroupDefaultDir(m.selectedGroupPath()))
	m.syncGroupFormFieldWidths()
	m.mode = modeGroupForm
	m.clearErr()
}

func (m *Model) handleGroupFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, request := m.groupForm.handleKey(m, msg)
	return m, tea.Batch(cmd, m.runGroupFormRequest(request, systemDirectoryPreflight{git: m.services.gitDrv}))
}

// runGroupFormRequest executes what a group form key asked of the root.
func (m *Model) runGroupFormRequest(request groupFormRequest, reader directoryPreflight) tea.Cmd {
	switch {
	case request.close:
		m.mode = modeList
	case request.applyPath:
		return m.applyPathSuggestion()
	case request.parent != 0:
		return m.moveGroupCursor(request.parent)
	case request.base != 0:
		var cmd tea.Cmd
		m.groupForm.base, cmd = m.stepGroupBase(groupBaseForm, uint64(m.groupForm.gen), m.groupFormDir(), m.groupForm.base, request.base)
		return cmd
	case request.create != nil:
		create := *request.create
		create.dirReader = reader
		m.clearErr()
		m.dispatchGroup(create)
		return m.nextEffectCmd()
	}
	return nil
}

func (d *groupFormDialog) handleKey(h groupFormHost, msg tea.KeyMsg) (tea.Cmd, groupFormRequest) {
	paths := &d.paths
	pathSuggesting := d.focus == gfPath && paths.active()
	switch msg.String() {
	case "esc":
		if pathSuggesting {
			paths.reset()
			return nil, groupFormRequest{}
		}
		return nil, groupFormRequest{close: true}
	case "tab":
		if pathSuggesting {
			return nil, groupFormRequest{applyPath: true}
		}
		d.focusStep(h, 1)
		return nil, groupFormRequest{}
	case "shift+tab":
		d.focusStep(h, -1)
		return nil, groupFormRequest{}
	case "up":
		if pathSuggesting {
			if !paths.move(-1) {
				d.focusStep(h, -1)
			}
		} else {
			d.focusStep(h, -1)
		}
		return nil, groupFormRequest{}
	case "down":
		if pathSuggesting {
			if !paths.move(1) {
				d.focusStep(h, 1)
			}
		} else {
			d.focusStep(h, 1)
		}
		return nil, groupFormRequest{}
	case "left":
		if d.focus == gfWorktree {
			count := len(groupWorktreeOptions)
			d.worktreeIndex = (d.worktreeIndex + count - 1) % count
			return nil, groupFormRequest{}
		}
		if d.focus == gfBase {
			return nil, groupFormRequest{base: -1}
		}
		if d.focus == gfParent {
			return nil, groupFormRequest{parent: -1}
		}
	case "right":
		if d.focus == gfWorktree {
			d.worktreeIndex = (d.worktreeIndex + 1) % len(groupWorktreeOptions)
			return nil, groupFormRequest{}
		}
		if d.focus == gfBase {
			return nil, groupFormRequest{base: 1}
		}
		if d.focus == gfParent {
			return nil, groupFormRequest{parent: 1}
		}
	case "enter":
		if pathSuggesting && paths.chosen {
			return nil, groupFormRequest{applyPath: true}
		}
		return nil, d.submit(h)
	}

	var cmd tea.Cmd
	switch d.focus {
	case gfName:
		d.name, cmd = d.name.Update(msg)
	case gfPath:
		d.path, cmd = d.path.Update(msg)
		d.pathAuto = false
		cmd = tea.Batch(cmd, paths.request(pathSuggestionGroup, d.path.Value(), systemPathSuggestionReader{}))
	}
	return cmd, groupFormRequest{}
}

func (d *groupFormDialog) focusStep(h groupFormHost, delta int) {
	d.paths.reset()
	d.focus = (d.focus + delta + gfCount) % gfCount
	d.name.Blur()
	d.path.Blur()
	switch d.focus {
	case gfName:
		d.name.Focus()
	case gfPath:
		d.path.Focus()
	}
}

// groupFormDir is the default path the group form would save, resolved
// the way the group worker resolves it.
func (m *Model) groupFormDir() string {
	return m.capturedAbsolutePath(m.groupForm.path.Value(), m.capturedGroupDefaultDir(m.selectedGroupPath()))
}

func (m *Model) submitGroupForm() (tea.Model, tea.Cmd) {
	return m.submitGroupFormWithReader(systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) submitGroupFormWithReader(reader directoryPreflight) (tea.Model, tea.Cmd) {
	return m, m.runGroupFormRequest(m.groupForm.submit(m), reader)
}

// submit validates the name and builds the group; the root checks the
// directory on the effect lane.
func (d *groupFormDialog) submit(h groupFormHost) groupFormRequest {
	name := strings.TrimSpace(d.name.Value())
	name = strings.ReplaceAll(name, "/", "-")
	if name == "" {
		h.reportErr("group name cannot be empty")
		return groupFormRequest{}
	}
	parent := h.selectedGroupPath()
	full := name
	if parent != "" {
		full = parent + "/" + name
	}
	worktree := groupWorktreeValue(d.worktreeIndex)
	request := groupRequest{
		path: full, worktree: worktree, base: d.base, gen: d.gen,
		draftName: d.name.Value(), draftDir: d.path.Value(),
		rawDir: d.path.Value(), fallbacks: h.groupDirCandidates(parent),
	}
	return groupFormRequest{create: &request}
}

// dispatchGroup queues the group's store write on the effect lane; the row
// materialization and reveal snapshot land when it completes.
func (m *Model) dispatchGroup(request groupRequest) {
	request.draftName = m.groupForm.name.Value()
	request.draftDir = m.groupForm.path.Value()
	m.enqueueEffect(request, 0, false)
}
