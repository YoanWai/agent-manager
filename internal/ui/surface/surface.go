// Package surface is the stack's contract: what a layer over the base screen
// answers and what it reaches on the root. It is a leaf, so a feature package
// can put its own surface on the root's stack without importing the root.
package surface

import tea "github.com/charmbracelet/bubbletea"

// Surface is a layer over the base screen that takes input before it:
// focus, and the cards over the list, Review or a focused session. The root
// walks the stack from the top for keys, mouse, frame and footer, so no
// other code asks which overlay is open. See docs/architecture.md.
//
// A surface owns its state and reaches the root only through Host, so it
// can be built and driven in a test without the root.
type Surface interface {
	// Key handles a key that reached this layer. A modal surface consumes
	// every key; handled false lets the key fall to the layer below.
	Key(h Host, msg tea.KeyMsg) (handled bool, cmd tea.Cmd)
	Click(h Host, line int) tea.Cmd
	Wheel(h Host, arrow tea.KeyMsg) tea.Cmd
	View(h Host, below string) string
	// Footer is the hint pairs the surface adds ahead of the layer's own.
	Footer() [][2]string
	Owner() Owner
	// Dismissible is whether esc and a click off the card close it, and
	// whether ctrl+c does where the pane under the card is the agent's.
	// A surface holding typed input says no, so a stray click keeps a draft.
	Dismissible() bool
	// Leave runs once when the surface comes off the stack, by pop or by
	// leaving the screen under it: the one place its cleanup lives.
	Leave(h Host)
}

// Host is what every surface reaches on the root. A surface that needs
// more declares its own narrow host beside it and asserts to it.
type Host interface {
	// CardSized paints a card and records its body for clicks.
	CardSized(width int, title, body string, hint [][2]string) string
	Size() (width, height int)
	Pop()
}

type Owner uint8

const (
	Manager Owner = iota
	Agent
)
