package ui

import (
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	focuswatch "github.com/YoanWai/agent-manager/internal/ui/focus/watch"
	tea "github.com/charmbracelet/bubbletea"
)

type focusRuntimeState struct {
	watch             *focuswatch.Watcher
	imeCursor         *cursorAnchor
	lastPaneSizes     map[string][2]int
	lastPanePIDs      map[string]int
	lastPublishedSize [2]int
}

func (m *Model) routeFocusMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case linkOpenErrMsg:
		m.reportErr(msg.err.Error())
		return routed(m, nil)

	case linkPageMsg:
		return routed(m, showLinkPage(msg.url))

	case focusCopiedMsg:
		// The clipboard writer runs off the update loop and can take
		// hundreds of milliseconds, long enough for a click elsewhere to
		// drop the highlight this count belongs to.
		if !m.focus.pane.ApplyCopied(msg.gen, msg.chars) {
			return routed(m, nil)
		}
		m.clearErr()
		return routed(m, nil)

	case focusScrollMsg:
		currentID := ""
		if sess, ok := m.selected(); ok {
			currentID = sess.ID
		}
		result := m.focus.pane.ApplyRegion(uifocus.RegionResult{
			SessionID: msg.sessID,
			Offset:    msg.offset,
			Rows:      msg.rows,
			Preview:   msg.preview,
			OK:        msg.ok,
		}, currentID, m.focusPaneRows())
		if result.Next != nil {
			return routed(m, m.focusRegionRequestCmd(*result.Next))
		}
		if result.Apply {
			m.workspace.preview = result.Preview
		}
		return routed(m, nil)

	case focusPreviewMsg:
		currentID := ""
		if sess, ok := m.selected(); ok {
			currentID = sess.ID
		}
		result := m.focus.pane.ApplyPane(uifocus.PaneUpdate{
			SessionID: msg.sessID,
			Mouse:     msg.paneMouse,
			Alt:       msg.paneAlt,
			Motion:    msg.paneMotion,
			SGR:       msg.paneSGR,
			History:   msg.historySize,
			Cursor: uifocus.Cursor{
				X: msg.cursorX, Y: msg.cursorY,
				Visible: msg.cursorOK, PositionKnown: msg.paneStateOK,
			},
			Writes: msg.writes,
		}, currentID)
		if result.UsePreview {
			m.workspace.preview = msg.preview
		}
		return routed(m, nil)
	}
	return nil, nil, false
}

// focusKeyCommand encodes one key press as a tmux send-keys command for
// the focused session. ok is false for keys tmux cannot represent, which
// are dropped.
func focusKeyCommand(target string, msg tea.KeyMsg) (string, bool) {
	raw, name, ok := uifocus.EncodeKey(msg)
	if !ok {
		return "", false
	}
	if name != "" {
		return tmux.SendKeyName(target, name), true
	}
	return tmux.SendKeysHex(target, raw), true
}

// focusSelected enters focus mode: keys go to the selected session's pane
// while the manager, its rail and its live preview stay on screen. The
// dead-pane probe and the finished acknowledgement run on the effect
// lane; the completion enters focus only while this selection holds.
func (m *Model) focusSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if sess.Archived {
		return m.attachSelected()
	}
	// Cleared on the press, as attach does, so a notice the lane posts
	// before entry, such as a dropped detach request, stays up.
	m.clearErr()
	m.enqueueEffect(focusRequest{sessionID: sess.ID, generation: m.gens.foreground}, 0, false)
	return m, m.nextEffectCmd()
}

// caretAtInputStart asks Focus whether the agent's caret sits at the head
// of its prompt, with the tool's prompt rules and the watcher's write count
// resolved here.
func (m *Model) caretAtInputStart(sessID, tool string) bool {
	if m.services.engine == nil {
		return false
	}
	watch := m.focus.runtime.watch
	return m.focus.pane.CaretAtInputStart(uifocus.CaretContext{
		SessionID: sessID,
		Preview:   m.workspace.preview,
		CaughtUp:  watch == nil || watch.CaughtUp(m.focus.pane.Pane().Writes),
		Rules:     focusPromptRules{engine: m.services.engine, tool: tool},
	})
}

// focusPromptRules binds the status engine's prompt rules to one tool.
type focusPromptRules struct {
	engine *status.Engine
	tool   string
}

func (r focusPromptRules) InputPrefix(row string) (string, bool) {
	return r.engine.InputPrefix(r.tool, row)
}

func (r focusPromptRules) TextBeforeCaret(row string, caretX int) bool {
	return status.HasTextBeforeCaret(r.engine, r.tool, row, caretX)
}

func (r focusPromptRules) ComposerIsEmpty(row string) bool {
	return r.engine.ComposerIsEmpty(r.tool, row)
}

func (r focusPromptRules) MatchesActivityCutoff(row string) bool {
	return r.engine.MatchesActivityCutoff(r.tool, row)
}

func (r focusPromptRules) ParksItsCaret() bool { return r.engine.ParksItsCaret(r.tool) }

// leaveFocus returns to the list. Mouse reporting stays on: handing it back
// to the terminal here would let a wheel notch scroll the manager out of
// view, so the list swallows the wheel instead.
func (m *Model) leaveFocus() tea.Cmd {
	if report := m.focus.pane.Leave(); report != "" {
		m.sendFocusReport(report, false)
	}
	return m.leaveFocusMode()
}

func (m *Model) leaveFocusMode() tea.Cmd {
	m.mode = modeList
	m.closeFocusSaved()
	// A run opened before the session was entered would pair with the very
	// click that comes back here, focusing it again instead of leaving.
	m.rail.ResetClickHistory()
	m.flushPendingNotice()
	return nil
}

// focusPagesScrollback reads the Focus policy for the footer and status
// line, which name the page keys only while they page.
func (m *Model) focusPagesScrollback(sess store.Session) bool {
	return m.focus.pane.PagesScrollback(sess.ID)
}

// handleFocusKey forwards focused input to the pane except for session
// actions and paging a normal-screen pane's history. Every plain
// character, q included, still reaches the agent.
func (m *Model) handleFocusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, m.leaveFocus()
	}
	if m.focus.saved.list.Open() {
		return m.handleFocusSavedKey(msg)
	}
	if m.services.keys.Binding(keybind.SavedPrompt).Has(msg.String()) {
		return m, m.openFocusSaved()
	}
	result := m.focus.pane.Key(msg, uifocus.KeyContext{
		SessionID:   sess.ID,
		Rows:        m.focusPaneRows(),
		Detach:      m.services.keys.Binding(keybind.Detach).Has(msg.String()),
		Editor:      m.services.keys.Binding(keybind.Editor).Has(msg.String()),
		Review:      m.services.keys.Binding(keybind.Review).Has(msg.String()),
		ArrowStep:   m.prefs.arrowStep,
		AtInputHead: msg.Type == tea.KeyLeft && !msg.Alt && m.caretAtInputStart(sess.ID, sess.Tool),
	})
	switch result.Action {
	case uifocus.LeaveFocus:
		if result.SendReport != "" {
			m.sendFocusReport(result.SendReport, false)
		}
		return m, m.leaveFocusMode()
	case uifocus.ScrollPane:
		if result.Region == nil {
			return m, nil
		}
		return m, m.focusRegionRequestCmd(*result.Region)
	case uifocus.OpenEditor:
		return m.openEditor()
	case uifocus.OpenReview:
		cmd := m.openDiff()
		if m.mode == modeDiff {
			m.reviewNav.ret = reviewReturn{kind: reviewReturnFocus, sessionID: sess.ID}
		}
		return m, cmd
	}
	var resume tea.Cmd
	if result.Region != nil {
		resume = m.focusRegionRequestCmd(*result.Region)
	}
	if msg.Paste {
		m.dispatchInput(inputRequest{kind: inputPaste, session: sess, text: string(msg.Runes), echo: true})
		return m, tea.Batch(resume, m.nextEffectCmd())
	}
	command, ok := focusKeyCommand(tmux.PaneTarget(sess.ID), msg)
	if !ok {
		return m, resume
	}
	m.dispatchInput(inputRequest{kind: inputKeys, session: sess, command: command, submit: result.Submit, echo: true})

	return m, tea.Batch(resume, m.nextEffectCmd())
}

// pasteFocused is the seam tests swap to observe pastes into the pane.
var pasteFocused = func(driver *tmux.Driver, id, text string) error {
	return driver.Paste(id, text)
}

func (m *Model) handleFocusMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	result := m.focus.pane.Mouse(msg, uifocus.MouseContext{
		SessionID:    sess.ID,
		Rows:         m.focusPaneRows(),
		RuntimeReady: m.focus.runtime.watch != nil,
	})
	return m.applyFocusMouse(result)
}

func (m *Model) applyFocusMouse(result uifocus.MouseResult) (tea.Model, tea.Cmd) {
	if result.SendReport != "" {
		m.sendFocusReport(result.SendReport, result.Input)
	}
	if result.Leave {
		return m, m.leaveFocusMode()
	}
	if result.OpenURL != "" {
		return m, openLinkCmd(result.OpenURL)
	}
	if result.Copy != nil {
		return m, copyFocusSelectionCmd(*result.Copy)
	}
	if result.Region != nil {
		return m, m.focusRegionRequestCmd(*result.Region)
	}
	return m, nil
}

func copyFocusSelectionCmd(request uifocus.CopyRequest) tea.Cmd {
	return copyTextCmd(request.Text, func(chars int) tea.Msg {
		return focusCopiedMsg{chars: chars, gen: request.Generation}
	})
}

func copyTextCmd(text string, done func(chars int) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if err := clipboard.WriteText(text); err != nil {
			return errMsg{err}
		}
		return done(utf8.RuneCountInString(text))
	}
}

type focusCopiedMsg struct {
	chars int
	gen   uint64
}

// paneCaretRow resolves the root-owned status fact that a tool's terminal
// cursor is parked below its own painted composer. Focus owns crop geometry
// after this value crosses the boundary.
func (m *Model) paneCaretRow() int {
	if m.mode != modeFocus {
		return -1
	}
	parks := false
	if sess, ok := m.selected(); ok && m.services.engine != nil {
		parks = m.services.engine.ParksItsCaret(sess.Tool)
	}
	return m.focus.pane.CaretRow(m.workspace.preview, parks)
}

type focusScrollMsg struct {
	sessID  string
	offset  int
	rows    int
	preview string
	ok      bool
}

func (m *Model) sendFocusReport(report string, echo bool) {
	sess, ok := m.selected()
	watch := m.focus.runtime.watch
	if !ok || watch == nil {
		return
	}
	command, args := tmux.GuardedMouseReport(tmux.PaneTarget(sess.ID), report)
	m.dispatchInput(inputRequest{kind: inputMouse, session: sess, command: command, args: args, echo: echo})
}

func (m *Model) focusRegionCmd(sessID string, offset int) tea.Cmd {
	return m.focusRegionRequestCmd(uifocus.RegionRequest{
		SessionID: sessID,
		Offset:    offset,
		Rows:      m.focusPaneRows(),
	})
}

func (m *Model) focusRegionRequestCmd(request uifocus.RegionRequest) tea.Cmd {
	watch := m.focus.runtime.watch
	return func() tea.Msg {
		if watch == nil {
			return focusScrollMsg{sessID: request.SessionID, offset: request.Offset, rows: request.Rows}
		}
		out, ok := watch.History(request.SessionID, request.Offset+request.Rows)
		return focusScrollMsg{
			sessID:  request.SessionID,
			offset:  request.Offset,
			rows:    request.Rows,
			preview: uifocus.BottomWindow(out, request.Rows, request.Offset),
			ok:      ok,
		}
	}
}
