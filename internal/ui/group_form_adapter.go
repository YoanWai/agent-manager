package ui

import (
	"cmp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

// handleGroupFormSubmit runs what the form's submit asked: a remote call
// for a connection's group, otherwise the store write on the effect lane.
func (m *Model) handleGroupFormSubmit(msg groupFormSubmitMsg, reader directoryPreflight) tea.Cmd {
	request := msg.request
	m.clearErr()
	if target := msg.remote; target.on() {
		return m.queueRemote(remoteRequest{op: remoteCreateGroup, host: target.host, name: request.Path,
			groupDir: strings.TrimSpace(request.Dir), composerGen: request.Gen})
	}
	m.enqueueEffect(groupRequest{
		path: request.Path, worktree: groupWorktreeValue(request.Worktree), base: msg.base, gen: request.Gen,
		rawDir: request.Dir, fallbacks: m.groupDirCandidates(request.Parent), dirReader: reader,
		draftName: request.Name, draftDir: request.Dir,
	}, 0, false)
	return m.nextEffectCmd()
}

// handleGroupBaseAsk steps the open form's base through the repo's
// branches, probing the directory the group would save.
func (m *Model) handleGroupBaseAsk(msg groupBaseAskMsg) tea.Cmd {
	s, ok := surfaceOf[*groupFormSurface](m)
	if !ok || s.form.Gen() != msg.gen {
		return nil
	}
	var cmd tea.Cmd
	s.base, cmd = m.stepGroupBase(groupBaseForm, uint64(msg.gen), m.groupFormDir(s), s.base, msg.delta)
	return cmd
}

// groupFormDir is the default path the group form would save, resolved
// the way the group worker resolves it.
func (m *Model) groupFormDir(s *groupFormSurface) string {
	return m.capturedAbsolutePath(s.form.Dir(), m.capturedGroupDefaultDir(s.selected()))
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
