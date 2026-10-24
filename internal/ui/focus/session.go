package focus

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Host is the read side of a focused session's root: the selected row, the
// rows the pane paints, the manager's own keys, and whether the watcher that
// routes a report is up.
type Host interface {
	// Session is the selected row, false when the cursor sits on a group.
	Session() (id string, ok bool)
	// PaneRows is the rows of pane content the focused view paints.
	PaneRows() int
	// Bindings are the manager's keys as the pressed key resolves them.
	Bindings(key string) Bindings
	// RuntimeReady reports whether a watcher exists to route a report through.
	RuntimeReady() bool
}

// KeyHost is what one key press asks of the root beyond Host.
type KeyHost interface {
	Host
	// Queued writes one key press to the session's pane.
	Queued(id string, write Write) tea.Cmd
	// Region asks the watcher for one scrollback viewport.
	Region(request *RegionRequest) tea.Cmd
	// Reported sends a guarded mouse report into the pane's application.
	Reported(report string, echo bool)
	// Editor and Review hand the pane's view to the manager, and Saved opens
	// the saved prompts list over it.
	Editor() (tea.Model, tea.Cmd)
	Review(id string) (tea.Model, tea.Cmd)
	Saved() tea.Cmd
	// Leave ends focus, LeaveMode returns to the list behind it.
	Leave() tea.Cmd
	LeaveMode() tea.Cmd
}

// MouseHost is what one mouse event asks of the root beyond Host.
type MouseHost interface {
	Host
	Reported(report string, echo bool)
	Copied(request CopyRequest) tea.Cmd
	Opened(url string) (tea.Model, tea.Cmd)
	Region(request *RegionRequest) tea.Cmd
	LeaveMode() tea.Cmd
}

// Bindings are the manager's keys as the focused pane reads them.
type Bindings struct {
	Detach bool
	Editor bool
	Review bool
	Saved  bool
	// ArrowStep makes ← at the head of the prompt mean back to the list.
	ArrowStep bool
	// AtInputHead is the agent's caret sitting at the head of its prompt.
	AtInputHead bool
}

// WriteKind is the transport one write to a pane takes.
type WriteKind uint8

const (
	// WriteKeys is one key press the view already encoded: raw bytes, or a
	// tmux key name. The root addresses it to the pane.
	WriteKeys WriteKind = iota
	// WriteMouse is a guarded mouse report the application asked for.
	WriteMouse
	// WritePaste is pasted text, which travels over its own transport.
	WritePaste
)

// Write is one write to the focused pane, before the root gives it a session
// and a generation.
type Write struct {
	Kind    WriteKind
	Raw     []byte
	Name    string
	Command string
	Args    []string
	Text    string
	Submit  bool
	Echo    bool
}

// Key is one key press: the pane's, or one of the manager's. The host
// resolves the key table, because the manager's keys are live and the pane
// cannot read them.
func (s *Model) KeyPress(h KeyHost, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	id, ok := h.Session()
	if !ok {
		return nil, h.Leave()
	}
	keys := h.Bindings(msg.String())
	if keys.Saved {
		return nil, h.Saved()
	}
	result := s.Key(msg, KeyContext{
		SessionID:   id,
		Rows:        h.PaneRows(),
		Detach:      keys.Detach,
		Editor:      keys.Editor,
		Review:      keys.Review,
		ArrowStep:   keys.ArrowStep,
		AtInputHead: msg.Type == tea.KeyLeft && !msg.Alt && keys.AtInputHead,
	})
	switch result.Action {
	case LeaveFocus:
		if result.SendReport != "" {
			h.Reported(result.SendReport, false)
		}
		return nil, h.LeaveMode()
	case ScrollPane:
		return nil, h.Region(result.Region)
	case OpenEditor:
		return h.Editor()
	case OpenReview:
		return h.Review(id)
	}
	var resume tea.Cmd
	if result.Region != nil {
		resume = h.Region(result.Region)
	}
	if msg.Paste {
		return nil, tea.Batch(resume, h.Queued(id, Write{Kind: WritePaste, Text: string(msg.Runes), Echo: true}))
	}
	raw, name, ok := EncodeKey(msg)
	if !ok {
		return nil, resume
	}
	return nil, tea.Batch(resume, h.Queued(id, Write{Kind: WriteKeys, Raw: raw, Name: name, Submit: result.Submit, Echo: true}))
}

// MouseUnder is one mouse event over the pane, and the rail under it.
func (s *Model) MouseUnder(h MouseHost, msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	id, ok := h.Session()
	if !ok {
		return nil, nil
	}
	result := s.Mouse(msg, MouseContext{
		SessionID:    id,
		Rows:         h.PaneRows(),
		RuntimeReady: h.RuntimeReady(),
	})
	if result.SendReport != "" {
		h.Reported(result.SendReport, result.Input)
	}
	if result.Leave {
		return nil, h.LeaveMode()
	}
	if result.OpenURL != "" {
		return h.Opened(result.OpenURL)
	}
	if result.Copy != nil {
		return nil, h.Copied(*result.Copy)
	}
	return nil, h.Region(result.Region)
}

// CopiedMessage is a finished clipboard write, in the generation it was taken
// in so a press elsewhere can drop it.
type CopiedMessage struct {
	Generation uint64
	Chars      int
}

// ApplyCopiedMessage records a finished clipboard write the generation still
// owns.
func (s *Model) ApplyCopiedMessage(msg CopiedMessage) bool {
	return s.ApplyCopied(msg.Generation, msg.Chars)
}

// ScrollMessage is one scrollback viewport read for the focused pane.
type ScrollMessage struct {
	SessionID string
	Offset    int
	Rows      int
	Preview   string
	OK        bool
}

// ScrollHost is what landing a scrollback viewport asks of the root.
type ScrollHost interface {
	Session() (id string, ok bool)
	PaneRows() int
	Region(*RegionRequest) tea.Cmd
	UsePreview(preview string)
}

// ApplyScroll lands one scrollback viewport: the next one it asks for, or the
// preview the view paints while the pane is scrolled back.
func (s *Model) ApplyScroll(msg ScrollMessage, h ScrollHost) tea.Cmd {
	id, _ := h.Session()
	result := s.ApplyRegion(RegionResult{
		SessionID: msg.SessionID,
		Offset:    msg.Offset,
		Rows:      msg.Rows,
		Preview:   msg.Preview,
		OK:        msg.OK,
	}, id, h.PaneRows())
	if result.Next != nil {
		return h.Region(result.Next)
	}
	if result.Apply {
		h.UsePreview(result.Preview)
	}
	return nil
}

// FrameMessage is a pane snapshot the watcher pushed, with the cursor cell so
// the view shows where typing lands.
type FrameMessage struct {
	SessionID   string
	Preview     string
	CursorX     int
	CursorY     int
	CursorOK    bool
	PaneStateOK bool
	Mouse       bool
	Motion      bool
	SGR         bool
	Alt         bool
	History     int
	Writes      uint64
}

// FrameHost is what landing a watcher frame asks of the root.
type FrameHost interface {
	Session() (id string, ok bool)
	UsePreview(preview string)
}

// ApplyFrame lands one watcher frame the focused pane pushed.
func (s *Model) ApplyFrame(msg FrameMessage, h FrameHost) tea.Cmd {
	id, _ := h.Session()
	result := s.ApplyPane(PaneUpdate{
		SessionID: msg.SessionID,
		Mouse:     msg.Mouse,
		Alt:       msg.Alt,
		Motion:    msg.Motion,
		SGR:       msg.SGR,
		History:   msg.History,
		Cursor: Cursor{
			X: msg.CursorX, Y: msg.CursorY,
			Visible: msg.CursorOK, PositionKnown: msg.PaneStateOK,
		},
		Writes: msg.Writes,
	}, id)
	if result.UsePreview {
		h.UsePreview(msg.Preview)
	}
	return nil
}
