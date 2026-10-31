package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	focuswatch "github.com/YoanWai/agent-manager/internal/ui/focus/watch"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uipreview "github.com/YoanWai/agent-manager/internal/ui/preview"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) routeFocusMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case linkOpenErrMsg:
		m.reportErr(msg.err.Error())
		return routed(m, nil)

	case linkPageMsg:
		return routed(m, uiterminal.ShowLinkPage(msg.url))

	case uifocus.CopiedMessage:
		// The clipboard writer runs off the update loop and can take hundreds
		// of milliseconds, long enough for a click elsewhere to drop the
		// highlight this count belongs to.
		if !m.focus.pane.ApplyCopied(msg.Generation, msg.Chars) {
			return routed(m, nil)
		}
		m.clearErr()
		return routed(m, nil)

	case focusruntime.ScrollMsg:
		return routed(m, m.focus.pane.ApplyScroll(uifocus.ScrollMessage(msg), focusFrameHost{m}))

	case focusruntime.FrameMsg:
		return routed(m, m.focus.pane.ApplyFrame(uifocus.FrameMessage(msg), focusFrameHost{m}))
	}
	return nil, nil, false
}

// focusSelected enters focus mode: keys go to the selected session's pane
// while the manager, its rail and its live preview stay on screen. The
// dead-pane probe and the finished acknowledgement run on the effect lane; the
// completion enters focus only while this selection holds.
func (m *Model) focusSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if sess.Archived {
		return m.attachSelected()
	}
	// Cleared on the press, as attach does, so a notice the lane posts before
	// entry, such as a dropped detach request, stays up.
	m.clearErr()
	m.enqueueEffect(focusruntime.ProbeRequest{SessionID: sess.ID, Generation: m.gens.foreground}, 0, false)
	return m, m.nextEffectCmd()
}

// caretAtInputStart asks Focus whether the agent's caret sits at the head of
// its prompt, with the tool's prompt rules and the watcher's write count
// resolved here.
func (m *Model) caretAtInputStart(sessID, tool string) bool {
	if m.services.engine == nil {
		return false
	}
	return m.focus.pane.CaretAtInputStart(uifocus.CaretContext{
		SessionID: sessID,
		Preview:   m.workspace.preview,
		CaughtUp:  m.focus.runtime.CaughtUp(m.focus.pane.Pane().Writes),
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

// leaveFocus returns to the list. Mouse reporting stays on: handing it back to
// the terminal here would let a wheel notch scroll the manager out of view, so
// the list swallows the wheel instead.
func (m *Model) leaveFocus() tea.Cmd {
	if report := m.focus.pane.Leave(); report != "" {
		m.sendFocusReport(report, false)
	}
	return m.leaveFocusMode()
}

func (m *Model) leaveFocusMode() tea.Cmd {
	m.clearSurfaces()
	// A run opened before the session was entered would pair with the very
	// click that comes back here, focusing it again instead of leaving.
	m.rail.ResetClickHistory()
	m.flushPendingNotice()
	m.mouse.flickAt = time.Now()
	return nil
}

// handleFocusKey forwards focused input to the pane except for session actions
// and paging a normal-screen pane's history. Every plain character, q
// included, still reaches the agent.
func (m *Model) handleFocusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m.focus.pane.KeyPress(focusHost{m}, msg)
}

// handleFocusMouse runs the mouse over a focused pane.
func (m *Model) handleFocusMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// The package cannot name the root, so a nil model is this one.
	model, cmd := m.focus.pane.MouseUnder(focusHost{m}, msg)
	if model == nil {
		model = m
	}
	return model, cmd
}

// syncPointerShape shows the hand while the mouse rests on a link a click in
// the focused pane would open.
func (m *Model) syncPointerShape() {
	top, _ := m.top()
	_, focusOnTop := top.(*uifocus.Surface)
	uiterminal.ShowHand(focusOnTop && m.focus.pane.OverLink())
}

// paneCaretRow resolves the root-owned status fact that a tool's terminal
// cursor is parked below its own painted composer. Focus owns crop geometry
// after this value crosses the boundary.
func (m *Model) paneCaretRow() int {
	if !m.focused() {
		return -1
	}
	parks := false
	if sess, ok := m.selected(); ok && m.services.engine != nil {
		parks = m.services.engine.ParksItsCaret(sess.Tool)
	}
	return m.focus.pane.CaretRow(m.workspace.preview, parks)
}

func (m *Model) sendFocusReport(report string, echo bool) {
	sess, ok := m.selected()
	if !ok || !m.focus.runtime.Ready() {
		return
	}
	command, args := tmux.GuardedMouseReport(tmux.PaneTarget(sess.ID), report)
	m.dispatchInput(focusruntime.Request{Kind: focusruntime.Mouse, Session: sess, Command: command, Args: args, Echo: echo})
}

// pasteFocused is the seam tests swap to observe pastes into the pane.
var pasteFocused = func(driver *tmux.Driver, id, text string) error {
	return driver.Paste(id, text)
}

// foregroundTakenSinceDetach reports whether, since the detach, the user opened
// a view, a rail prompt or a resize, or asked to enter a session.
func (m *Model) foregroundTakenSinceDetach() bool {
	if m.base != baseList || len(m.surfaces) > 0 || m.rail.Busy() {
		return true
	}
	for _, job := range m.effects.Main.Pending {
		switch job.Request.(type) {
		case focusruntime.ProbeRequest, uilifecycle.AttachRequest:
			return true
		}
	}
	return false
}

// openReviewForReturn opens Review and remembers the session to come back to.
func (m *Model) openReviewForReturn(sessionID string) tea.Cmd {
	cmd := m.openDiff()
	if m.base == baseReview {
		m.reviewNav.Set(uireviewsource.Return{Kind: uireviewsource.ReturnAttach, SessionID: sessionID})
	}
	return cmd
}

// openEditorForReturn opens the editor over the session's directory.
func (m *Model) openEditorForReturn(sessionID string) tea.Cmd {
	_, cmd := m.openEditorWithReaderForReturn(newSystemTerminalDirectoryReader(m), sessionID)
	return cmd
}

// watchLandedWrite points the watcher at what a counted write reached, so the
// pane's echo shows without waiting out the frame budget.
func (m *Model) watchLandedWrite(sessionID string) {
	if m.effects.Quitting || !m.focused() || !m.focus.runtime.Ready() {
		return
	}
	if selected, ok := m.selected(); ok && selected.ID == sessionID {
		m.focus.runtime.Retry()
		m.watchSelection()
	}
}

// recordPrompt writes a prompt receipt onto its row.
func (m *Model) recordPrompt(sessionID, prompt string) {
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == sessionID {
			m.workspace.sessions[i].LastPrompt = prompt
		}
	}
}

func (m *Model) focusRegionCmd(sessID string, offset int) tea.Cmd {
	return m.focusRegionRequestCmd(uifocus.RegionRequest{
		SessionID: sessID,
		Offset:    offset,
		Rows:      m.focusPaneRows(),
	})
}

func (m *Model) focusRegionRequestCmd(request uifocus.RegionRequest) tea.Cmd {
	watch := m.focus.runtime.Watch
	return func() tea.Msg {
		return focusruntime.Scroll(watch, request.SessionID, request.Offset, request.Rows)
	}
}

func copyTextCmd(text string, done func(chars int) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		if err := clipboard.WriteText(text); err != nil {
			return errMsg{err}
		}
		return done(utf8.RuneCountInString(text))
	}
}

// focusRoot is the root as a focused session reads it: the key it forwards to
// the agent, the mouse over the pane, and the fence a key sets.
type focusRoot struct{ *Model }

func (r focusRoot) Key(msg tea.KeyMsg) tea.Cmd {
	_, cmd := r.handleFocusKey(msg)
	return cmd
}

func (r focusRoot) Mouse(msg tea.MouseMsg) tea.Cmd {
	_, cmd := r.focusMouse(msg)
	return cmd
}

func (r focusRoot) TakeForeground() { r.takeForeground() }

// focusSavedRoot is the root as the saved prompts list reads it: the live key
// tables, the card's body width, and the paste a pick makes.
type focusSavedRoot struct{ *Model }

func (r focusSavedRoot) Keys() keybind.Table { return r.keys() }

// PasteFocused pastes a pick into the selected session, which is the focused
// one while the list is up.
func (r focusSavedRoot) PasteFocused(pick spawn.SavedPick) tea.Cmd {
	sess, ok := r.selected()
	if !ok || !pick.Picked {
		return nil
	}
	return r.pasteFocused(sess, pick.Saved.Text)
}

func (r focusSavedRoot) LeaveFocus() tea.Cmd { return r.leaveFocus() }

func newFocusSurface(m *Model) *uifocus.Surface { return uifocus.NewSurface(focusRoot{m}) }

// focused is whether a session is focused, with or without a card over it.
func (m *Model) focused() bool {
	_, ok := surfaceOf[*uifocus.Surface](m)
	return ok
}

func (m *Model) openFocusSaved() tea.Cmd {
	s := uifocus.OpenSaved(focusSavedRoot{m}, uint64(m.nextComposerGen()), m.spawnStyles())
	m.push(s)
	return uipersist.SavedCmd(m.services.store, uipersist.SavedForFocus, s.Gen(), 0)
}

func (m *Model) loadFocusSaved(msg uipersist.SavedMsg) {
	if saved, ok := surfaceOf[*uifocus.Saved](m); ok && saved.Gen() == msg.Gen {
		saved.Load(spawnSaved(msg.Rows), msg.Err)
	}
}

// enterFocus opens the focused surface over the selected session.
func (m *Model) enterFocus(sess store.Session) tea.Cmd {
	m.clearSurfaces()
	m.push(newFocusSurface(m))
	if m.prefs.fullLayout {
		m.layout.panes.PinFullFocus(paneSizing{m}, sess.ID, m.layout.width, m.listBodyHeight())
	}
	m.focus.runtime.Retry()
	m.watchSelection()
	keepPaneFacts := m.focus.runtime.KeepsPaneFacts(&m.focus.pane, sess.ID)
	m.focus.pane.Enter(uifocus.EnterContext{SessionID: sess.ID, KeepPaneFacts: keepPaneFacts})
	return tea.Batch(tea.EnableMouseAllMotion, m.cursorBlink())
}

// applyFocusEffect enters focus once the dead-pane probe has run.
func (m *Model) applyFocusEffect(request focusruntime.ProbeRequest, err error) tea.Cmd {
	return focusruntime.ApplyProbe(focusProbeHost{m}, request, err)
}

// applyDetachEffect opens what a session asked for before it detached.
func (m *Model) applyDetachEffect(request focusruntime.DetachRequest, result focusruntime.Detach, err error) tea.Cmd {
	return focusruntime.ApplyDetach(focusDetachHost{m}, request, result, err)
}

// applyInputEffect lands one queued write to a focused pane.
func (m *Model) applyInputEffect(result focusruntime.Result, err error) {
	focusruntime.ApplyInput(focusInputHost{m}, result, err)
}

func (m *Model) applyAckEffect(err error) {
	if err != nil {
		m.reportErr(err.Error())
		return
	}
	m.requestRefresh()
}

// dispatchInput queues one write to a focused session's pane.
func (m *Model) dispatchInput(request focusruntime.Request) {
	request = request.Copied()
	if request.Kind != focusruntime.Prompt && !request.Counted {
		request.Counted = m.focus.runtime.CountWrite(request.Session.ID)
	}
	m.enqueueEffect(focusruntime.InputRequest{Value: request}, 0, false)
}

// newFocusWatch opens the root's one pane watcher, whose frames and lost
// clients arrive through send as messages.
func newFocusWatch(driver *tmux.Driver, send func(tea.Msg)) *focuswatch.Watcher {
	return focusruntime.NewWatcher(driver, focusruntime.Sink{
		Frame: func(frame focuswatch.Frame) { send(focusruntime.FrameMessage(frame)) },
		Lost:  func(err error) { send(errMsg{err}) },
	})
}

// viewFullFocusFrame is a session opened from the full screen list: the
// captured pane owns the whole terminal body and the list waits behind it.
func (m *Model) viewFullFocusFrame() string {
	footer := m.footer()
	bodyHeight := m.listBodyHeight()
	// This frame paints no rail, so a click lands on no row: the list frame's
	// hits would otherwise select a row nobody pointed at.
	m.layout.displayedRail = uirail.Frame{}
	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, uitheme.Paint(line, m.layout.width, uitheme.BackdropHex()))
	}
	frame = append(frame, uitheme.Paint(uitheme.Hrule(m.layout.width), m.layout.width, uitheme.BackdropHex()))
	frame = append(frame, uitheme.Paint(m.focusFactsLine(m.layout.width), m.layout.width, uitheme.BackdropHex()))
	frame = append(frame, uitheme.Paint(m.focusEdge(m.layout.width), m.layout.width, uitheme.BackdropHex()))
	paneRows := m.previewLines(m.layout.width, bodyHeight, strings.Repeat(" ", contentGutter))
	frame = append(frame, paintContent(paneRows, m.layout.width, bodyHeight, uitheme.BackdropHex())...)
	frame = append(frame, uitheme.Paint(m.focusEdge(m.layout.width), m.layout.width, uitheme.BackdropHex()))
	m.placeKeys(footer.Hits, 0, len(frame))
	for _, line := range uitheme.SplitLines(footer.Text) {
		frame = append(frame, uitheme.Paint(line, m.layout.width, uitheme.BackdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}

// focusFactsLine says which session a full screen frame is showing, where
// nothing else on it does.
func (m *Model) focusFactsLine(width int) string {
	sess, ok := m.selected()
	if !ok {
		return ""
	}
	facts := uifocus.FactsSession{
		Glyph:    m.sessionGlyph(sess),
		Name:     m.displayName(sess),
		Tool:     sess.Tool,
		Status:   sess.Status,
		Activity: uiframe.RelSince(uipreview.LastActivity(sess.CreatedAt, sess.LastStatusAt)),
		Dir:      uiframe.ShortHome(m.sessionDir(sess), m.env.homeDir),
		Branch:   sess.WorktreeBranch,
		Started:  uiframe.RelSince(sess.CreatedAt),
		Queued:   m.workspace.queuedMessages[sess.ID],
	}
	if facts.Branch != "" {
		facts.Branch = m.glyphs().BranchGlyph + " " + facts.Branch
	}
	if m.workspace.procFor == sess.ID && m.workspace.proc.OK {
		facts.CPU = fmt.Sprintf("%.1f%%", m.workspace.proc.CPUPercent)
		facts.RAM = uiframe.HumanBytes(m.workspace.proc.RSS)
	}
	return uifocus.FactsLine(width, facts, focusFactsHost{})
}

// focusFactsHost is the root as the facts line reads a state word and its tone.
type focusFactsHost struct{}

func (focusFactsHost) StatusLabel(value string) string { return statusLabel(value) }

func (focusFactsHost) StatusColor(value string) string { return string(statusColor(value)) }

// focusEdge is the hairline holding the full screen pane off what sits above
// and below it, in the pane's own tone once it has a box to trace.
func (m *Model) focusEdge(width int) string {
	return uifocus.Edge(width, m.focus.pane.FrameBox().Valid)
}

// focusTopRule is the hairline that caps the focused pane in the split,
// naming the session's keys.
func focusTopRule(width int, keys keybind.Table) string {
	return uifocus.TopRuleFor(width, keys)
}

var (
	_ uifocus.Root      = focusRoot{}
	_ uifocus.SavedRoot = focusSavedRoot{}
)
