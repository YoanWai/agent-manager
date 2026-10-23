package ui

// noticesSurfaceOf is the messages modal on the stack, which a test reads the
// cursor and scroll from.
func noticesSurfaceOf(m *Model) *noticesSurface {
	s, ok := surfaceOf[*noticesSurface](m)
	if !ok {
		panic("the messages modal is not open")
	}
	return s
}
