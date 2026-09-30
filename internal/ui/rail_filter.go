package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	tea "github.com/charmbracelet/bubbletea"
)

// cycleStatusFilter advances the list status filter (all → attention → …).
// Modes live in statusFilterCycle so new ones only need a const and a
// matches case; this handler stays the same.
func (m *Model) cycleStatusFilter() tea.Cmd {
	previousKey := ""
	if entry, ok := m.selectedRow(); ok {
		previousKey = rowKey(entry)
	}
	m.rail.statusFilter = m.rail.statusFilter.next()
	m.rebuildRows()
	return m.afterListFilter(previousKey)
}

// toggleEmptyGroups hides or restores group rows whose subtree has no
// sessions in the active list. It never changes the store, and the archived
// view ignores the filter, so the key is refused there rather than flipping
// a setting nothing on screen reports.
func (m *Model) toggleEmptyGroups() tea.Cmd {
	if m.rail.showArchived {
		return nil
	}
	previousKey := ""
	if entry, ok := m.selectedRow(); ok {
		previousKey = rowKey(entry)
	}
	m.rail.hideEmptyGroups = !m.rail.hideEmptyGroups
	m.rebuildRows()
	return m.afterListFilter(previousKey)
}

// afterListFilter keeps the preview tied to the selection when a filter
// change leaves the cursor on the same row, and refreshes it when not.
func (m *Model) afterListFilter(previousKey string) tea.Cmd {
	currentKey := ""
	if entry, ok := m.selectedRow(); ok {
		currentKey = rowKey(entry)
	}
	if currentKey == previousKey {
		return nil
	}

	m.workspace.preview = ""
	m.workspace.proc = sysstat.ProcStat{}
	m.workspace.procFor = ""
	m.focusPane.previewGen++
	m.syncPollInput()
	if _, ok := m.selected(); ok {
		return m.schedulePreview()
	}
	return nil
}

// statusFilter is which status set the list shows. The zero value is all
// sessions. To add a mode: define a const, append it to statusFilterCycle,
// and handle it in label and matches.
type statusFilter int

const (
	statusFilterAll statusFilter = iota
	statusFilterAttention
)

// statusFilterCycle is the order `w` walks. Append new modes before the
// wrap back to all happens at the end of next().
var statusFilterCycle = []statusFilter{
	statusFilterAll,
	statusFilterAttention,
}

func (f statusFilter) next() statusFilter {
	for i, mode := range statusFilterCycle {
		if mode == f {
			return statusFilterCycle[(i+1)%len(statusFilterCycle)]
		}
	}
	return statusFilterAll
}

func (f statusFilter) active() bool {
	return f != statusFilterAll
}

// label is the short badge word for the header and empty state, empty when
// the filter shows every status.
func (f statusFilter) label() string {
	switch f {
	case statusFilterAttention:
		return "attention"
	default:
		return ""
	}
}

// matches reports whether a session status is visible under this filter.
func (f statusFilter) matches(st string) bool {
	switch f {
	case statusFilterAttention:
		switch st {
		case status.Waiting, status.Finished, status.Errored:
			return true
		default:
			return false
		}
	default:
		return true
	}
}
