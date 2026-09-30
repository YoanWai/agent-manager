package focus

import tea "github.com/charmbracelet/bubbletea"

type KeyAction uint8

const (
	ForwardKey KeyAction = iota
	LeaveFocus
	OpenEditor
	OpenReview
)

// KeyContext contains root-owned routing facts already resolved for one key.
type KeyContext struct {
	SessionID   string
	Rows        int
	Detach      bool
	Editor      bool
	Review      bool
	ArrowStep   bool
	AtInputHead bool
}

// KeyResult is the Focus policy decision. Root executes the selected action,
// forwards the original key, and performs an optional region capture.
type KeyResult struct {
	Action     KeyAction
	Submit     bool
	Region     *RegionRequest
	SendReport string
}

func (m *Model) Key(msg tea.KeyMsg, ctx KeyContext) KeyResult {
	if ctx.Detach {
		return m.leaveResult()
	}
	if ctx.Editor {
		return KeyResult{Action: OpenEditor}
	}
	if ctx.Review {
		m.clearSelection()
		return KeyResult{Action: OpenReview}
	}
	if msg.Type == tea.KeyLeft && !msg.Alt && ctx.ArrowStep && ctx.AtInputHead {
		return m.leaveResult()
	}

	m.cursorOn = true
	result := KeyResult{
		Action: ForwardKey,
		Submit: msg.Type == tea.KeyEnter && !msg.Alt,
	}
	if m.scroll != 0 {
		m.scroll = 0
		result.Region = m.requestRegion(ctx.SessionID, ctx.Rows)
	}
	return result
}

func (m *Model) leaveResult() KeyResult {
	return KeyResult{Action: LeaveFocus, SendReport: m.Leave()}
}
