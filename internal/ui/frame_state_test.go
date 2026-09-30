package ui

import "testing"

func TestViewDoesNotPublishCursorState(t *testing.T) {
	m := &Model{}
	m.focusRuntime.imeCursor = &cursorAnchor{}
	m.focusRuntime.imeCursor.set(8, 4, true)
	if got := m.View(); got != "loading..." {
		t.Fatalf("initial view = %q", got)
	}
	col, row, ok := m.focusRuntime.imeCursor.get()
	if col != 8 || row != 4 || !ok {
		t.Fatalf("View changed published cursor to (%d, %d, %t)", col, row, ok)
	}
}

func TestUpdatePublishesPreparedFrame(t *testing.T) {
	m := shotModel()
	m.prepareFrame()
	before := m.View()
	m.mode = modeHelp
	if got := m.View(); got != before {
		t.Fatal("View rendered state that had not been prepared")
	}
	updated, _ := m.Update(struct{}{})
	if updated != m {
		t.Fatal("Update replaced the root model")
	}
	if got := m.View(); got == before || got == "loading..." {
		t.Fatal("Update did not publish the changed frame")
	}
}

func preparedView(m *Model) string {
	m.prepareFrame()
	return m.View()
}
