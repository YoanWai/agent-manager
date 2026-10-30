package capture

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type CaretMsg struct{}

// CaretInterval is the caret's half period, matching the rate most terminals
// blink their own.
const CaretInterval = 530 * time.Millisecond

// CaretTick re-arms the caret timer.
func CaretTick() tea.Cmd {
	return tea.Tick(CaretInterval, func(time.Time) tea.Msg { return CaretMsg{} })
}

type CaretRoot interface {
	Focused() bool
	BlinkCaret()
}

// RouteCaret applies a caret blink, reporting whether msg was one. The timer
// runs only while a session is focused; every other mode lets it die.
func RouteCaret(root CaretRoot, msg tea.Msg) (tea.Cmd, bool) {
	if _, ok := msg.(CaretMsg); !ok {
		return nil, false
	}
	if !root.Focused() {
		return nil, true
	}
	root.BlinkCaret()
	return CaretTick(), true
}
