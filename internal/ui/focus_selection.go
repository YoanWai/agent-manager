package ui

import (
	"strings"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) handleFocusMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	result := m.focusPane.Mouse(msg, uifocus.MouseContext{
		SessionID:    sess.ID,
		Rows:         m.focusPaneRows(),
		RuntimeReady: m.focusRuntime.watch != nil,
	})
	return m.applyFocusMouse(result)
}

func (m *Model) applyFocusMouse(result uifocus.MouseResult) (tea.Model, tea.Cmd) {
	if result.SendReport != "" {
		m.sendFocusReport(result.SendReport)
	}
	if result.Leave {
		return m, m.leaveFocusMode()
	}
	if result.OpenURL != "" {
		return m, openLinkCmd(result.OpenURL)
	}
	if result.Copy != nil {
		return m, copyFocusSelectionCmd(*result.Copy)
	}
	if result.Region != nil {
		return m, m.focusRegionRequestCmd(*result.Region)
	}
	return m, nil
}

func copyFocusSelectionCmd(request uifocus.CopyRequest) tea.Cmd {
	return copyTextCmd(request.Text, func(chars int) tea.Msg {
		return focusCopiedMsg{chars: chars, gen: request.Generation}
	})
}

func copyTextCmd(text string, done func(chars int) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if err := clipboard.WriteText(text); err != nil {
			return errMsg{err}
		}
		return done(utf8.RuneCountInString(text))
	}
}

type focusCopiedMsg struct {
	chars int
	gen   uint64
}

// paneCaretRow resolves the root-owned status fact that a tool's terminal
// cursor is parked below its own painted composer. Focus owns crop geometry
// after this value crosses the boundary.
func (m *Model) paneCaretRow() int {
	pane := m.focusPane.Pane()
	if m.mode != modeFocus || !pane.Cursor.Visible || m.focusPane.ScrolledBack() {
		return -1
	}
	caret := pane.Cursor.Y
	if sess, ok := m.selected(); ok && m.services.engine != nil && m.services.engine.ParksItsCaret(sess.Tool) {
		rows := strings.Split(strings.TrimSuffix(m.workspace.preview, "\n"), "\n")
		if caret < len(rows) && caretParkedBelowContent(rows, caret, pane.Cursor.X) {
			return -1
		}
	}
	return caret
}

func caretParkedBelowContent(rows []string, caret, column int) bool {
	if column != 0 {
		return false
	}
	for y := caret; y < len(rows); y++ {
		if strings.TrimSpace(ansi.Strip(rows[y])) != "" {
			return false
		}
	}
	for y := caret - 1; y >= 0; y-- {
		if strings.TrimSpace(ansi.Strip(rows[y])) != "" {
			return true
		}
	}
	return false
}
