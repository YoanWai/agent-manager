package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// handleDiffKey owns the whole keymap in fullscreen review mode.
func (m *Model) handleDiffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.diff.notice = ""
	if m.diff.annotating {
		return m.handleAnnotateKey(msg)
	}
	if m.diff.sendConfirm {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter", "y":
			m.diff.sendConfirm = false
			return m.sendAnnotations()
		case "esc", "q":
			m.diff.sendConfirm = false
		}
		return m, nil
	}
	height := m.diffCodeHeight()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc":
		return m, m.closeDiff()
	case "?":
		m.openHelp()
	case "up", "k":
		m.moveDiffCursor(-1, height)
	case "down", "j":
		m.moveDiffCursor(1, height)
	case "ctrl+d":
		m.moveDiffCursor(height/2, height)
	case "ctrl+u":
		m.moveDiffCursor(-height/2, height)
	case "pgup":
		m.moveDiffCursor(-height, height)
	case "pgdown", "pgdn":
		m.moveDiffCursor(height, height)
	case "g":
		m.diff.cursorLine = 0
		m.diff.scroll = 0
	case "G":
		if fd := m.currentFileDiff(); fd != nil {
			m.diff.cursorLine = m.diffRowCount(fd) - 1
			m.moveDiffCursor(0, height)
		}
	case "J", "tab":
		return m, m.switchDiffFile(1)
	case "K", "shift+tab":
		return m, m.switchDiffFile(-1)
	case "n":
		m.jumpChange(1)
	case "N", "shift+n":
		m.jumpChange(-1)
	case "s":
		return m, m.cycleDiffScope()
	case "r":
		m.openRepoPick()
	case "b":
		return m, m.openBranchPick()
	case "B":
		return m, m.openBasePick()
	case "u":
		lineIdx := m.cursorDiffLine()
		m.diff.sideBySide = !m.diff.sideBySide
		m.setCursorDiffLine(lineIdx)
	case "f":
		return m, m.toggleCodeOnly()
	case " ", "space":
		return m, m.toggleReviewed()
	case "c":
		m.openAnnotate()
	case "d":
		return m, m.discardOrToggleAnnotation()
	case "C":
		if m.draftAnnotationCount() == 0 {
			m.errBar.text = "no comments to send - press c on a line first"
		} else {
			m.diff.sendConfirm = true
		}
	case "o", "f3":
		return m.openDiffFile()
	}
	return m, nil
}
