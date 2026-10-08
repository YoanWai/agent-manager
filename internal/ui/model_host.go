package ui

// Host methods several feature types declare. Each feature's own host
// interface names the subset it uses.

func (m *Model) size() (width, height int) { return m.layout.width, m.layout.height }

func (m *Model) currentMode() mode { return m.mode }

// setMode switches the root's active mode; a dialog closes through it.
func (m *Model) setMode(next mode) { m.mode = next }
