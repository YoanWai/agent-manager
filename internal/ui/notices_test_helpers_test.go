package ui

import (
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
)

// noticesSurfaceOf is the messages modal on the stack, which a test reads the
// cursor and scroll from.
func noticesSurfaceOf(m *Model) *uinotices.Surface {
	s, ok := surfaceOf[*uinotices.Surface](m)
	if !ok {
		panic("the messages modal is not open")
	}
	return s
}
