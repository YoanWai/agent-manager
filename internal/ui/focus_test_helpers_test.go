package ui

import (
	"strings"
	"testing"

	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	focusScrollStep = 3
	leftButton      = 0
)

type paneCursor struct {
	x, y       int
	ok         bool
	positionOK bool
}

func paneAt(t *testing.T, lines ...string) *Model {
	t.Helper()
	preview := strings.Join(lines, "\n") + "\n"
	m := &Model{mode: modeFocus}
	m.workspace.preview = preview
	m.focus.pane.Enter(uifocus.EnterContext{SessionID: "s1"})
	setFocusPaneID(m, "s1")
	prepareFocusFrame(m, preview, 40, len(lines), 10, 5, -1)
	return m
}

// focusedShotPane focuses the selected session of a model built without
// tmux, painting rows at a known box so the root mouse path can be driven
// in terminal coordinates.
func focusedShotPane(t *testing.T, width, x, y int, rows ...string) (*Model, string) {
	t.Helper()
	m := shotModel()
	m.mode = modeFocus
	sess, ok := m.selected()
	if !ok {
		t.Fatal("test setup: no selected session")
	}
	preview := strings.Join(rows, "\n") + "\n"
	m.workspace.preview = preview
	m.focus.pane.Enter(uifocus.EnterContext{SessionID: sess.ID})
	setFocusPaneID(m, sess.ID)
	prepareFocusFrame(m, preview, width, len(rows), x, y, -1)
	return m, sess.ID
}

func setFocusPaneID(m *Model, id string) {
	updateFocusPane(m, id, func(*uifocus.PaneUpdate) {})
}

func setFocusCursor(m *Model, cursor paneCursor) {
	id := m.focus.pane.Pane().SessionID
	if id == "" {
		if sess, ok := m.selected(); ok {
			id = sess.ID
		} else {
			id = "s1"
		}
	}
	updateFocusPane(m, id, func(update *uifocus.PaneUpdate) {
		update.Cursor = uifocus.Cursor{
			X: cursor.x, Y: cursor.y,
			Visible: cursor.ok, PositionKnown: cursor.positionOK,
		}
	})
}

func setFocusPaneFacts(m *Model, id string, mouse, motion, sgr bool, history int, cursor paneCursor) {
	updateFocusPane(m, id, func(update *uifocus.PaneUpdate) {
		update.Mouse = mouse
		update.Motion = motion
		update.SGR = sgr
		update.History = history
		update.Cursor = uifocus.Cursor{
			X: cursor.x, Y: cursor.y,
			Visible: cursor.ok, PositionKnown: cursor.positionOK,
		}
	})
}

func updateFocusPane(m *Model, id string, change func(*uifocus.PaneUpdate)) {
	pane := m.focus.pane.Pane()
	update := uifocus.PaneUpdate{
		SessionID: id,
		Mouse:     pane.Mouse,
		Alt:       pane.Alt,
		Motion:    pane.Motion,
		SGR:       pane.SGR,
		History:   pane.History,
		Cursor:    pane.Cursor,
	}
	change(&update)
	m.focus.pane.ApplyPane(update, id)
}

func prepareFocusFrame(m *Model, preview string, width, height, x, y, caretRow int) uifocus.FrameContent {
	return m.focus.pane.PrepareFrame(uifocus.FrameContext{
		Preview:        preview,
		Width:          width,
		Height:         height,
		OriginX:        x,
		OriginY:        y,
		CaretRow:       caretRow,
		CursorStyle:    lipgloss.NewStyle().Foreground(colorBg).Background(colorAccent),
		SelectionStyle: lipgloss.NewStyle().Foreground(colorBg).Background(colorAccent2),
	})
}

func setFocusScrollOffset(t *testing.T, m *Model, id string, offset, rows int) {
	t.Helper()
	updateFocusPane(m, id, func(update *uifocus.PaneUpdate) { update.History = offset })
	for m.focus.pane.Status().ScrollOffset < offset {
		m.focus.pane.Scroll(-1, id, rows)
	}
	status := m.focus.pane.Status()
	m.focus.pane.ApplyRegion(uifocus.RegionResult{
		SessionID: id,
		Offset:    status.ScrollOffset,
		Rows:      rows,
		OK:        true,
	}, id, rows)
}

func (m *Model) scrollFocus(delta int) tea.Cmd {
	sess, ok := m.selected()
	if !ok || m.mode != modeFocus {
		return nil
	}
	request := m.focus.pane.Scroll(delta, sess.ID, m.focusPaneRows())
	if request == nil {
		return nil
	}
	return m.focusRegionRequestCmd(*request)
}

func (m *Model) wheelFocus(up bool, x, y int) tea.Cmd {
	sess, ok := m.selected()
	if !ok {
		return nil
	}
	result := m.focus.pane.Mouse(tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress,
		Button: map[bool]tea.MouseButton{true: tea.MouseButtonWheelUp, false: tea.MouseButtonWheelDown}[up],
	}, uifocus.MouseContext{
		SessionID: sess.ID, Rows: m.focusPaneRows(), RuntimeReady: m.focus.runtime.watch != nil,
	})
	_, cmd := m.applyFocusMouse(result)
	return cmd
}

func (m *Model) scrolledBack() bool { return m.focus.pane.ScrolledBack() }

func (m *Model) cursorCell(int) (row, col int, ok bool) {
	sess, selected := m.selected()
	if !selected {
		sess.ID = m.focus.pane.Pane().SessionID
	}
	column, screenRow, ok := m.focus.pane.CursorAnchor(sess.ID)
	if !ok {
		return 0, 0, false
	}
	box := m.focus.pane.FrameBox()
	return screenRow - box.Y - 1, column - box.X - 1, true
}
