package ui

import tea "github.com/charmbracelet/bubbletea"

// screen is what is on the terminal: the base mode and the surfaces over
// it, read from the top. Model embeds it, so m.mode and m.surfaces read as
// before while the two stay one fact.
type screen struct {
	mode     mode
	surfaces []surface
}

// A surface is a layer that sits over the current mode and takes input
// before it: the saved prompts list over a focused session is the first.
// The root walks the stack from the top for keys, mouse and the frame, so
// no other code asks which overlay is open. See docs/adr/0001-surface-stack.md.
//
// Mode stays the base layer while surfaces move onto the stack one by one;
// a surface never reads m.mode to decide what it does, only to be opened.
type surface interface {
	// Key handles a key that reached this layer. A modal surface consumes
	// every key; handled false lets the key fall to the layer below.
	Key(m *Model, msg tea.KeyMsg) (handled bool, cmd tea.Cmd)
	// Click acts on the surface's card body at its painted line.
	Click(m *Model, line int) tea.Cmd
	// Wheel steps the surface the way ↑↓ do.
	Wheel(m *Model, arrow tea.KeyMsg) tea.Cmd
	// View paints the surface over the frame below it.
	View(m *Model, below string) string
	// Footer is the hint pairs the surface adds ahead of the layer's own.
	Footer() [][2]string
	// Owner is who reads plain keys while this surface is on top.
	Owner() inputOwner
}

// inputOwner is who receives the keys no layer claims for itself.
type inputOwner uint8

const (
	ownerManager inputOwner = iota
	ownerAgent
)

// pushSurface puts s on top of the stack.
func (m *Model) pushSurface(s surface) { m.surfaces = append(m.surfaces, s) }

// popSurface removes the top surface, if any.
func (m *Model) popSurface() {
	if n := len(m.surfaces); n > 0 {
		m.surfaces = m.surfaces[:n-1]
	}
}

// clearSurfaces drops every surface; leaving the mode they sit on does this.
func (m *Model) clearSurfaces() { m.surfaces = nil }

// topSurface is the surface that takes input first.
func (m *Model) topSurface() (surface, bool) {
	if n := len(m.surfaces); n > 0 {
		return m.surfaces[n-1], true
	}
	return nil, false
}

// inputOwner is who reads plain keys now: the top surface decides, and with
// none open the base mode does. Focus hands them to the agent.
func (m *Model) inputOwner() inputOwner {
	if top, ok := m.topSurface(); ok {
		return top.Owner()
	}
	if m.mode == modeFocus {
		return ownerAgent
	}
	return ownerManager
}

// agentOwnsKeyboard is true while keys go straight to the focused pane.
func (m *Model) agentOwnsKeyboard() bool { return m.inputOwner() == ownerAgent }

// surfaceKey offers msg to the stack from the top down, and reports whether
// a surface took it.
func (m *Model) surfaceKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	for i := len(m.surfaces) - 1; i >= 0; i-- {
		if handled, cmd := m.surfaces[i].Key(m, msg); handled {
			return cmd, true
		}
	}
	return nil, false
}

// surfaceView paints every surface over frame, bottom up.
func (m *Model) surfaceView(frame string) string {
	for _, s := range m.surfaces {
		frame = s.View(m, frame)
	}
	return frame
}
