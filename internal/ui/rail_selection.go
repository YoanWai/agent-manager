package ui

import (
	"github.com/YoanWai/agent-manager/internal/sysstat"
	tea "github.com/charmbracelet/bubbletea"
)

// moveCursor shifts the selection by delta, wrapping at either end, and
// schedules a debounced preview fetch. Key-repeat only bumps the gen; a
// single capture runs after the cursor settles so holding j/k cannot pile
// up tmux work.
func (m *Model) moveCursor(delta int) tea.Cmd {
	if len(m.rail.rows) == 0 {
		return nil
	}
	next := m.rail.cursor + delta
	if next < 0 {
		next = len(m.rail.rows) - 1
	}
	if next >= len(m.rail.rows) {
		next = 0
	}
	return m.selectRow(next)
}

// scrollCursor moves the cursor like moveCursor but stops at either end.
// The keyboard wraps on purpose; one flick of the wheel is several notches,
// so wrapping there would fling the selection to the far end mid-gesture
// and aim every key after it somewhere the user never looked.
func (m *Model) scrollCursor(delta int) tea.Cmd {
	if len(m.rail.rows) == 0 {
		return nil
	}
	return m.selectRow(min(max(m.rail.cursor+delta, 0), len(m.rail.rows)-1))
}

// selectRow moves the cursor straight to index, the shared tail moveCursor
// and a row click both need: reset the stale preview and schedule a fresh
// one. A click on the row already selected is a no-op, same as a wheel
// notch that would not move the cursor.
func (m *Model) selectRow(index int) tea.Cmd {
	if index < 0 || index >= len(m.rail.rows) || index == m.rail.cursor {
		return nil
	}
	m.rail.cursor = index
	m.workspace.preview = ""
	m.workspace.proc = sysstat.ProcStat{}
	m.workspace.procFor = ""
	if _, ok := m.selected(); !ok {
		return nil
	}
	m.focusPane.previewGen++
	return m.schedulePreview()
}

// selectRowByKey moves the cursor to the row with this identity, reporting
// false once a rebuild has dropped it from the list.
func (m *Model) selectRowByKey(key string) bool {
	index := m.rowIndexByKey(key)
	if index < 0 {
		return false
	}
	m.selectRow(index)
	return true
}
