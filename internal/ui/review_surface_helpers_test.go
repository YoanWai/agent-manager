package ui

import (
	"testing"

	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
)

// pickerOpen reports a review picker on the stack.
func (m *Model) pickerOpen() bool {
	_, ok := surfaceOf[*reviewPicker](m)
	return ok
}

// pickerOf is the mounted picker's state, for a test that reads its rows or
// cursor the way the surface does.
func (m *Model) pickerOf(t *testing.T) *uireview.Picker {
	t.Helper()
	s, ok := surfaceOf[*reviewPicker](m)
	if !ok {
		t.Fatal("no review picker is open")
	}
	return &s.picker
}

// viewPicker paints the mounted picker's card alone.
func (m *Model) viewPicker(t *testing.T) string {
	t.Helper()
	s, ok := surfaceOf[*reviewPicker](m)
	if !ok {
		t.Fatal("no review picker is open")
	}
	return s.View(m, "")
}
