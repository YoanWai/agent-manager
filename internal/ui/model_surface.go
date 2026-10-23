package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
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

// A surface is a layer over the base screen that takes input before it:
// focus, and the cards over the list, Review or a focused session. The root
// walks the stack from the top for keys, mouse, frame and footer, so no
// other code asks which overlay is open. See docs/adr/0001-surface-stack.md.
//
// A surface owns its state and reaches the root only through surfaceHost,
// so it can be built and driven in a test without a Model. Mode stays the
// base layer while surfaces move onto the stack one by one.
type surface interface {
	// Key handles a key that reached this layer. A modal surface consumes
	// every key; handled false lets the key fall to the layer below.
	Key(h surfaceHost, msg tea.KeyMsg) (handled bool, cmd tea.Cmd)
	// Click acts on the surface's card body at its painted line.
	Click(h surfaceHost, line int) tea.Cmd
	// Wheel steps the surface the way ↑↓ do.
	Wheel(h surfaceHost, arrow tea.KeyMsg) tea.Cmd
	// View paints the surface over the frame below it.
	View(h surfaceHost, below string) string
	// Footer is the hint pairs the surface adds ahead of the layer's own.
	Footer() [][2]string
	// Owner is who reads plain keys while this surface is on top.
	Owner() inputOwner
	// Dismissible is whether esc and a click off the card close it, and
	// whether ctrl+c does where the pane under the card is the agent's.
	// A surface holding typed input says no, so a stray click keeps a draft.
	Dismissible() bool
	// Leave runs once when the surface comes off the stack, by pop or by
	// leaving the mode under it: the one place its cleanup lives.
	Leave(h surfaceHost)
}

// pathCompleting is the optional capability of a surface with a directory
// field: tab completes a path into it. The root asks the top surface for it
// instead of knowing which cards have one.
type pathCompleting interface {
	// pathTarget names the field for the shared completer and its reads.
	pathTarget() pathSuggestionTarget
	// dir is the field's current text.
	dir() (string, bool)
	// takePath puts a completed directory into the field.
	takePath(path string)
	// paths is the surface's completer state.
	paths() *pathComplete
}

// mouseTaker is the optional capability of a surface that is not a card:
// it takes every mouse event itself, and the root draws no card edge or
// backdrop around it.
type mouseTaker interface {
	Mouse(h surfaceHost, msg tea.MouseMsg) tea.Cmd
}

// surfaceHost is what a surface reaches on the root. The cap and the type
// rules of model_host_test.go apply: a surface that needs more returns a
// command for a root adapter instead.
type surfaceHost interface {
	// keys are the session key table, for the keys a surface shares with
	// focus such as detach.
	keys() keybind.Table
	// selected is the session under the cursor.
	selected() (store.Session, bool)
	// cardSized paints a card and records its body for clicks.
	cardSized(width int, title, body string, hint [][2]string) string
	// size is the terminal's columns and rows.
	size() (width, height int)
	// pasteFocused types text into the focused pane, unsent.
	pasteFocused(sess store.Session, text string) tea.Cmd
	// leaveFocus returns from a focused session to the list.
	leaveFocus() tea.Cmd
	// pop takes the top surface off the stack, running its Leave.
	pop()
}

// inputOwner is who receives the keys no layer claims for itself.
type inputOwner uint8

const (
	ownerManager inputOwner = iota
	ownerAgent
)

func (m *Model) keys() keybind.Table { return m.services.keys }

func (m *Model) pasteFocused(sess store.Session, text string) tea.Cmd {
	m.dispatchInput(inputRequest{kind: inputPaste, session: sess, text: text, echo: true})
	return m.nextEffectCmd()
}

// push puts s on top of the stack.
func (m *Model) push(s surface) { m.surfaces = append(m.surfaces, s) }

// pop takes the top surface off the stack and runs its Leave.
func (m *Model) pop() {
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
		m.pop()
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
