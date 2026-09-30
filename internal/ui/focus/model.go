package focus

// Model owns the interaction state of the focused pane. Runtime resources
// such as the tmux control client, clipboard, URL opener, and IME writer stay
// with the root UI and execute the typed outcomes returned here.
type Model struct {
	sessionID string
	pane      paneState
	frame     displayedFrame

	selection selection
	pending   pendingClick
	forward   forwardedGesture

	copied  int
	copyGen uint64

	cursorOn bool
	scroll   int

	fetchInFlight bool

	previewGen uint64
	watchedGen uint64
}

type paneState struct {
	sessionID string
	mouse     bool
	motion    bool
	sgr       bool
	history   int
	cursor    Cursor
}

type displayedFrame struct {
	box       Box
	lines     []string
	rowOffset int
}

// Box is the pane rectangle from the frame currently on screen.
type Box struct {
	X      int
	Y      int
	Width  int
	Height int
	Valid  bool
}

// Cursor is the pane cursor reported by tmux. PositionKnown remains true
// when the application hides the cursor but tmux still reports its cell.
type Cursor struct {
	X             int
	Y             int
	Visible       bool
	PositionKnown bool
}

// PaneState is an immutable copy of the watcher facts root adapters need.
type PaneState struct {
	SessionID string
	Mouse     bool
	Motion    bool
	SGR       bool
	History   int
	Cursor    Cursor
}

// Status is the Focus-owned status-line state.
type Status struct {
	ScrollOffset int
	CopiedChars  int
}

// EnterContext describes the selected runtime target without handing the
// child a watcher or control client.
type EnterContext struct {
	SessionID     string
	KeepPaneFacts bool
}

// PaneUpdate is the value snapshot delivered by the root watcher.
type PaneUpdate struct {
	SessionID string
	Mouse     bool
	Motion    bool
	SGR       bool
	History   int
	Cursor    Cursor
}

// PaneResult says whether a watcher update belongs to the current selection
// and whether its live preview may replace the frame being read.
type PaneResult struct {
	UsePreview bool
}

func (m *Model) Enter(ctx EnterContext) {
	m.sessionID = ctx.SessionID
	m.clearSelection()
	m.pending = pendingClick{}
	m.forward = forwardedGesture{}
	m.cursorOn = true
	m.scroll = 0
	m.fetchInFlight = false
	if !ctx.KeepPaneFacts || m.pane.sessionID != ctx.SessionID {
		m.pane.mouse = false
		m.pane.motion = false
		m.pane.sgr = false
		m.pane.history = 0
	}
	if m.pane.sessionID != ctx.SessionID {
		m.pane.cursor = Cursor{}
	}
}

// Leave clears gesture and selection state. A returned report releases an
// Alt-forwarded button that the pane application still considers held.
func (m *Model) Leave() string {
	release := m.endForwardedGesture()
	m.clearSelection()
	m.pending = pendingClick{}
	m.sessionID = ""
	return release
}

func (m *Model) ApplyPane(update PaneUpdate, currentSessionID string) PaneResult {
	if currentSessionID == "" || update.SessionID != currentSessionID {
		return PaneResult{}
	}
	m.pane.sessionID = update.SessionID
	m.pane.mouse = update.Mouse
	m.pane.motion = update.Motion
	m.pane.sgr = update.SGR
	m.pane.history = max(update.History, 0)
	if m.pane.mouse && m.scroll != 0 && m.pane.history == 0 {
		m.scroll = 0
	}
	if m.scroll != 0 {
		return PaneResult{}
	}
	m.pane.cursor = update.Cursor
	return PaneResult{UsePreview: true}
}

func (m Model) Pane() PaneState {
	return PaneState{
		SessionID: m.pane.sessionID,
		Mouse:     m.pane.mouse,
		Motion:    m.pane.motion,
		SGR:       m.pane.sgr,
		History:   m.pane.history,
		Cursor:    m.pane.cursor,
	}
}

func (m Model) Status() Status {
	return Status{ScrollOffset: m.scroll, CopiedChars: m.copied}
}

func (m Model) FrameBox() Box { return m.frame.box }

func (m Model) ScrolledBack() bool { return m.scroll > 0 }

func (m Model) CursorOn() bool { return m.cursorOn }

func (m *Model) Blink() { m.cursorOn = !m.cursorOn }

func (m *Model) clearSelection() {
	m.selection = selection{}
	m.copied = 0
	m.copyGen++
}

func (m *Model) ApplyCopied(generation uint64, chars int) bool {
	if generation != m.copyGen {
		return false
	}
	m.copied = chars
	return true
}

func (m Model) PreviewGeneration() uint64 { return m.previewGen }

func (m *Model) MovePreview() uint64 {
	m.previewGen++
	return m.previewGen
}

func (m Model) AcceptPreview(generation uint64) bool {
	return generation == 0 || generation == m.previewGen
}

func (m Model) PreviewSettled(generation uint64) bool {
	return generation == m.previewGen
}

// ObservePoll reports whether selection stayed still since the preceding
// poll, then advances the observation point.
func (m *Model) ObservePoll() bool {
	rested := m.previewGen == m.watchedGen
	m.watchedGen = m.previewGen
	return rested
}
