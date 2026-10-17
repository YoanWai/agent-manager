package ui

import (
	"cmp"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	tea "github.com/charmbracelet/bubbletea"
)

// groupFormDialog keeps what the root shares with the New Group form: the
// path completion under its path field and the group base choice, which the
// root probes. The fields, focus, keys and validation are uidialogs.GroupForm.
type groupFormDialog struct {
	uidialogs.GroupForm
	paths pathComplete
	base  string
}

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

func (m *Model) syncGroupFormFieldWidths() {
	m.groupForm.SyncWidths(m.formValueWidth() - 3)
}

func (m *Model) openGroupForm() {
	m.rebuildGroupOptions(m.contextGroup())
	m.groupForm = groupFormDialog{
		GroupForm: uidialogs.NewGroupForm(m.capturedGroupDefaultDir(m.selectedGroupPath()), m.nextComposerGen()),
		paths:     m.groupForm.paths.fresh(),
	}
	m.syncGroupFormFieldWidths()
	m.mode = modeGroupForm
	m.clearErr()
}

func (m *Model) handleGroupFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := m.groupForm.Key(msg, dialogPaths{&m.groupForm.paths}, m.selectedGroupPath(), len(groupWorktreeOptions))
	return m, m.runGroupFormKey(key, systemDirectoryPreflight{git: m.services.gitDrv})
}

// runGroupFormKey executes what a group form key asked of the root.
func (m *Model) runGroupFormKey(key uidialogs.GroupFormKey, reader directoryPreflight) tea.Cmd {
	switch key.Action {
	case uidialogs.GroupFormCancel:
		m.mode = modeList
	case uidialogs.GroupFormComplete:
		return m.applyPathSuggestion()
	case uidialogs.GroupFormStepParent:
		return m.moveGroupCursor(key.Delta)
	case uidialogs.GroupFormStepBase:
		var cmd tea.Cmd
		m.groupForm.base, cmd = m.stepGroupBase(groupBaseForm, uint64(m.groupForm.Gen()), m.groupFormDir(), m.groupForm.base, key.Delta)
		return cmd
	case uidialogs.GroupFormInvalid:
		m.reportErr(key.Err)
	case uidialogs.GroupFormSubmit:
		request := key.Request
		m.clearErr()
		m.dispatchGroup(groupRequest{
			path: request.Path, worktree: groupWorktreeValue(request.Worktree), base: m.groupForm.base, gen: request.Gen,
			rawDir: request.Dir, fallbacks: m.groupDirCandidates(request.Parent), dirReader: reader,
		})
		return m.nextEffectCmd()
	}
	if key.Suggest {
		return tea.Batch(key.Cmd, m.groupForm.paths.request(pathSuggestionGroup, m.groupForm.Dir(), systemPathSuggestionReader{}))
	}
	return key.Cmd
}

// groupFormDir is the default path the group form would save, resolved
// the way the group worker resolves it.
func (m *Model) groupFormDir() string {
	return m.capturedAbsolutePath(m.groupForm.Dir(), m.capturedGroupDefaultDir(m.selectedGroupPath()))
}

func (m *Model) submitGroupForm() (tea.Model, tea.Cmd) {
	return m.submitGroupFormWithReader(systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) submitGroupFormWithReader(reader directoryPreflight) (tea.Model, tea.Cmd) {
	return m, m.runGroupFormKey(m.groupForm.Submit(m.selectedGroupPath()), reader)
}

// dispatchGroup queues the group's store write on the effect lane; the row
// materialization and reveal snapshot land when it completes.
func (m *Model) dispatchGroup(request groupRequest) {
	request.draftName = m.groupForm.Name()
	request.draftDir = m.groupForm.Dir()
	m.enqueueEffect(request, 0, false)
}

func (m *Model) viewGroupForm() string {
	parent := m.selectedGroupPath()
	view := uidialogs.GroupFormView{
		Field:     formField,
		Input:     textInputView,
		Badge:     groupBadge,
		Parent:    parent,
		Picker:    m.viewGroupPicker,
		Subtle:    subtleStyle(),
		Value:     valueStyle(),
		Worktrees: groupWorktreeOptions,
		Base:      groupBaseChoice(m.groupForm.base, m.groupBase(parent)),
	}
	if m.groupForm.paths.active() {
		view.Suggestions = m.groupForm.paths.view
		view.SuggestHints = pathSuggestHint(m.groupForm.paths.chosen)
	}
	return m.drawCard(m.groupForm.View(view))
}

// groupBaseChoice renders a group's base picker: its own ref, or auto and
// the parent's choice that auto inherits.
func groupBaseChoice(base, inherited string) string {
	choice := subtleStyle().Render("◂ ") + valueStyle().Render(cmp.Or(base, "auto")) + subtleStyle().Render(" ▸")
	if base == "" && inherited != "" {
		choice += subtleStyle().Render("  " + inherited + " from parent")
	}
	return choice
}
