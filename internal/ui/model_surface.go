package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	uisurface "github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// screen is what is on the terminal: the base screen and the surfaces over
// it, read from the top.
type screen struct {
	base     baseScreen
	surfaces []surface
}

// baseScreen is the screen under the stack: the list, or Review in its
// place.
type baseScreen uint8

const (
	baseList baseScreen = iota
	baseReview
)

// screenID names where the user is, for a completion that must land on the
// screen it was asked from: the base, whether a session is focused, and
// how many surfaces are up, so a card opened since the request keeps it.
type screenID struct {
	base    baseScreen
	focused bool
	depth   int
}

func (m *Model) screenID() screenID {
	return screenID{base: m.base, focused: m.focused(), depth: len(m.surfaces)}
}

// The stack's contract lives in ui/surface, so a feature package can put
// its own surface on the stack.
type (
	surface     = uisurface.Surface
	surfaceHost = uisurface.Host
	inputOwner  = uisurface.Owner
)

const (
	ownerManager = uisurface.Manager
	ownerAgent   = uisurface.Agent
)

// mouseTaker is the optional capability of a surface that is not a card:
// it takes every mouse event itself, and the root draws no card edge or
// backdrop around it.
type mouseTaker interface {
	Mouse(h surfaceHost, msg tea.MouseMsg) tea.Cmd
}

func (m *Model) keys() keybind.Table { return m.services.keys }

func (m *Model) pasteFocused(sess store.Session, text string) tea.Cmd {
	m.dispatchInput(focusruntime.Request{Kind: focusruntime.Paste, Session: sess, Text: text, Echo: true})
	return m.nextEffectCmd()
}

// push puts s on top of the stack.
func (m *Model) push(s surface) { m.surfaces = append(m.surfaces, s) }

// Pop takes the top surface off the stack and runs its Leave.
func (m *Model) Pop() {
	n := len(m.surfaces)
	if n == 0 {
		return
	}
	top := m.surfaces[n-1]
	m.surfaces = m.surfaces[:n-1]
	top.Leave(m)
}

// clearSurfaces pops every surface, top first; leaving the screen they sit
// on does this.
func (m *Model) clearSurfaces() {
	for len(m.surfaces) > 0 {
		m.Pop()
	}
}

// top is the surface that takes input first.
func (m *Model) top() (surface, bool) {
	if n := len(m.surfaces); n > 0 {
		return m.surfaces[n-1], true
	}
	return nil, false
}

// surfaceOf finds the mounted surface of one type, for a completion that
// must land on the surface that asked and nowhere else.
func surfaceOf[S surface](m *Model) (S, bool) {
	for i := len(m.surfaces) - 1; i >= 0; i-- {
		if s, ok := m.surfaces[i].(S); ok {
			return s, true
		}
	}
	var none S
	return none, false
}

// inputOwner is who reads plain keys now: the top surface decides, and with
// none open the base mode does. Focus hands them to the agent.
func (m *Model) inputOwner() inputOwner {
	if top, ok := m.top(); ok {
		return top.Owner()
	}
	return m.baseOwner()
}

// baseOwner is who reads plain keys under the cards: the agent while a
// session is focused, else the manager.
func (m *Model) baseOwner() inputOwner {
	if m.focused() {
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
