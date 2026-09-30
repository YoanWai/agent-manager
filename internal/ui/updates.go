package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.handleMsg(msg)
	if mm, ok := model.(*Model); ok {
		mm.flushPendingNotice()
		return mm, tea.Batch(cmd, mm.syncMouseCapture())
	}
	return model, tea.Batch(cmd, m.syncMouseCapture())
}

func (m *Model) handleMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Resuming from a tmux attach re-sends the current size unchanged; only
		// a real resize needs the per-session tmux resize calls, so an
		// unchanged size skips them and keeps detach latency flat.
		if msg.Width == m.width && msg.Height == m.height {
			return m, nil
		}
		m.width = msg.Width
		m.height = msg.Height
		// Re-assert the terminal backdrop: a reattach or a fresh outer
		// terminal delivers a size message and may carry stale colors.
		SyncTerminalBackground()
		m.publishPaneSize()
		m.resizeSessions()
		if m.fullFocus() {
			if sess, ok := m.selected(); ok {
				m.pinFullFocusPane(sess.ID)
			}
		}
		if m.mode == modeForm {
			m.syncFormFieldWidths()
		} else if m.mode == modeGroupForm {
			m.syncGroupFormFieldWidths()
		}
		return m, nil

	case bannerTickMsg:
		m.startup.bannerPhase++
		return m, m.bannerTick()

	case bannerShimmerMsg:
		m.startup.bannerPhase = 0
		return m, m.bannerTick()

	case browserOpenMsg:
		m.handleBrowserOpen(msg)
		return m, nil

	case startupTickMsg:
		if !m.needsLoaderTick() {
			m.startup.startupAnimating = false
			return m, nil
		}
		m.startup.startupPhase++
		return m, m.startupTick()

	case previewTickMsg:
		// Only the list keeps a live pane on screen; review and the modal
		// screens have no preview to feed, so they skip the capture and
		// just keep the timer alive.
		sess, ok := m.selected()
		if !ok || (m.mode != modeList && m.mode != modeRename && m.mode != modeFocus) {
			return m, m.previewTick()
		}
		// A session with a control client already pushes every frame; a
		// tick capture on top of that is work whose result is discarded.
		if m.focusRuntime.watch != nil && m.focusRuntime.watch.serving(sess.ID) {
			return m, m.previewTick()
		}
		return m, tea.Batch(m.previewCmd(sess, m.focusPane.PreviewGeneration()), m.previewTick())

	case refreshMsg:
		m.startup.booting = false
		m.ageError()
		// The focused session can die or vanish under us; fall back to the
		// list rather than typing into nothing.
		sessions := m.dropRecentlyRemoved(m.keepPendingLaunches(msg.sessions, msg.listedAt), msg.listedAt)
		stripDeletedGroups(&msg, m.ledger.goneGroups)
		var focusExit tea.Cmd
		if m.mode == modeFocus {
			if sess, ok := m.selected(); !ok || sessionGone(sessions, sess.ID) {
				focusExit = m.leaveFocus()
			}
		}
		m.workspace.sessions = sessions
		m.workspace.tmuxSocket = msg.tmuxSocket
		m.workspace.leadingManager = msg.leadingManager
		m.workspace.panes = msg.panes
		m.workspace.groups = msg.groups
		m.workspace.groupPaths = msg.groupPaths
		m.workspace.groupWorktrees = msg.groupWorktrees
		m.workspace.archivedGroups = msg.archivedGroups
		m.workspace.agents = msg.agents
		m.workspace.queuedMessages = msg.queuedMessages
		if m.workspace.paneLines == nil {
			m.workspace.paneLines = map[string]string{}
		}
		for id, line := range msg.paneLines {
			m.workspace.paneLines[id] = line
		}
		if m.workspace.panePrompts == nil {
			m.workspace.panePrompts = map[string]string{}
		}
		for id, prompt := range msg.panePrompts {
			if prompt != "" {
				m.workspace.panePrompts[id] = prompt
			}
		}
		m.commitTypedPrompt()
		if msg.snapOK {
			m.workspace.snap = msg.snap
			m.updateNetRates(msg.snap)
		}
		// Sessions left from a previous run carry that run's window size,
		// which the cache knows nothing about; seedPaneGeom adopts their
		// real geometry on the first pass, so nothing resets the cache here.
		if !m.startup.sessionsSized && m.width > 0 && len(m.workspace.sessions) > 0 {
			m.startup.sessionsSized = true
		}
		m.publishPaneSize()
		// The preview box changes height for more reasons than a terminal
		// resize: the quick bar opening, the status line appearing, a new
		// badge in the header. A pane shorter than the box paints a dead
		// band under its output, so every pass grows what falls short.
		// The call is free when nothing moved: it diffs against paneGeom.
		if m.startup.sessionsSized && m.width > 0 {
			m.resizeSessions()
		}
		m.settleInstall()
		m.rebuildRows()
		if msg.focusID != "" {
			focused, ok := m.selected()
			// Moving the cursor under a focused pane would leave the
			// keyboard pinned to the session the user was in while the
			// list claims another. The click steps back to the list, and
			// only once its session turns out to have a row to land on.
			if m.focusSession(msg.focusID) && m.mode == modeFocus && (!ok || focused.ID != msg.focusID) {
				focusExit = m.leaveFocus()
			}
		}
		reviewStatuses := m.reviewStatusesCmd()
		// A pass that ran with a stale selection (a session created this
		// tick, or one a notification click just chose) carries the wrong
		// preview; resync and fetch it directly.
		if sess, ok := m.selected(); ok && sess.ID != msg.procFor {
			m.syncPollInput()
			gen := m.focusPane.MovePreview()
			return m, tea.Batch(focusExit, m.previewCmd(sess, gen), m.diffRefreshCmd(), reviewStatuses, m.startStartupTick())
		}
		m.workspace.proc = msg.proc
		m.workspace.procFor = msg.procFor
		m.setPreview(msg.procFor, msg.preview)
		// A selection that has not moved since the last pass is at rest,
		// so this covers the startup case where no settle ever fired.
		if m.focusPane.ObservePoll() {
			m.watchSelection()
		}
		return m, tea.Batch(focusExit, m.diffRefreshCmd(), reviewStatuses, m.startStartupTick())

	case updateMsg:
		if msg.manual {
			m.finishNoticeRefresh()
		}
		if msg.failed && len(msg.releases) == 0 {
			if msg.manual && msg.err != nil {
				m.errBar.text = "refresh failed: " + msg.err.Error()
			}
			return m, nil
		}
		m.applyNotices(func() {
			m.update.latest = msg.latest
			m.update.url = msg.url
			m.update.releases = msg.releases
			m.update.checked = true
			m.indexReleaseRanges()
		})
		if msg.manual && msg.err != nil {
			m.errBar.text = "refresh failed: " + msg.err.Error()
		}
		return m, nil

	case updateAppliedMsg:
		m.update.applying = false
		if len(msg.result.Releases) > 0 {
			m.keepNoticeSelection(func() {
				m.update.latest = msg.result.Latest
				m.update.url = msg.result.URL
				m.update.releases = msg.result.Releases
				m.update.checked = true
				m.indexReleaseRanges()
			})
		}
		if msg.err != nil {
			m.errBar.text = "update failed: " + msg.err.Error()
			return m, nil
		}
		if msg.upToDate {
			m.keepNoticeSelection(func() {
				m.update.latest = ""
				m.update.url = ""
				if len(msg.result.Releases) == 0 {
					m.update.releases = nil
					m.update.checked = true
				}
				m.indexReleaseRanges()
			})
			m.reportDone("already up to date")
			return m, nil
		}
		m.update.restartPath = msg.path
		return m, tea.Quit

	case updateTickMsg:
		return m, tea.Batch(m.checkForUpdate, m.checkFeed, m.updateTick())

	case feedMsg:
		if msg.manual {
			m.finishNoticeRefresh()
		}
		if !msg.failed || len(msg.messages) > 0 {
			m.applyNotices(func() { m.notices.feedMessages = msg.messages })
		}
		if msg.manual && msg.err != nil {
			m.errBar.text = "refresh failed: " + msg.err.Error()
		}
		return m, nil

	case pasteSweepMsg:
		if msg.err != nil {
			m.errBar.text = "clearing old pasted images: " + msg.err.Error()
		}
		return m, nil

	case pasteSweepTickMsg:
		return m, tea.Batch(m.sweepPastes, m.pasteSweepTick())

	case replyCopiedMsg:
		if msg.unreadable {
			m.reportWarn(fmt.Sprintf("no reply to read in %s: a %s pane is not read that way", msg.name, msg.tool))
			return m, nil
		}
		if msg.chars == 0 {
			m.errBar.text = "nothing to copy from " + msg.name
			return m, nil
		}
		if msg.unbounded {
			m.reportWarn(fmt.Sprintf("copied %d chars from %s: %s marks no turn start here, so this is the whole pane",
				msg.chars, msg.name, msg.tool))
			return m, nil
		}
		m.reportDone(fmt.Sprintf("copied %d chars from %s", msg.chars, msg.name))
		return m, nil

	case previewSettleMsg:
		if !m.focusPane.PreviewSettled(msg.gen) {
			return m, nil
		}
		sess, ok := m.selected()
		if !ok {
			return m, nil
		}
		// The cursor has come to rest: this is where the control client is
		// worth opening.
		m.watchSelection()
		return m, m.previewCmd(sess, msg.gen)

	case cursorBlinkMsg:
		if m.mode != modeFocus {
			return m, nil
		}
		m.focusPane.Blink()
		return m, m.cursorBlink()

	case linkOpenErrMsg:
		m.errBar.text = msg.err.Error()
		return m, nil

	case linkPageMsg:
		return m, showLinkPage(msg.url)

	case launchCommandCopiedMsg:
		m.handleLaunchCommandCopied(msg)
		return m, nil

	case focusCopiedMsg:
		// The clipboard writer runs off the update loop and can take
		// hundreds of milliseconds, long enough for a click elsewhere to
		// drop the highlight this count belongs to.
		if !m.focusPane.ApplyCopied(msg.gen, msg.chars) {
			return m, nil
		}
		m.errBar.text = ""
		return m, nil

	case focusScrollMsg:
		currentID := ""
		if sess, ok := m.selected(); ok {
			currentID = sess.ID
		}
		result := m.focusPane.ApplyRegion(uifocus.RegionResult{
			SessionID: msg.sessID,
			Offset:    msg.offset,
			Rows:      msg.rows,
			Preview:   msg.preview,
			OK:        msg.ok,
		}, currentID, m.focusPaneRows())
		if result.Next != nil {
			return m, m.focusRegionRequestCmd(*result.Next)
		}
		if result.Apply {
			m.workspace.preview = result.Preview
		}
		return m, nil

	case focusPreviewMsg:
		currentID := ""
		if sess, ok := m.selected(); ok {
			currentID = sess.ID
		}
		result := m.focusPane.ApplyPane(uifocus.PaneUpdate{
			SessionID: msg.sessID,
			Mouse:     msg.paneMouse,
			Motion:    msg.paneMotion,
			SGR:       msg.paneSGR,
			History:   msg.historySize,
			Cursor: uifocus.Cursor{
				X: msg.cursorX, Y: msg.cursorY,
				Visible: msg.cursorOK, PositionKnown: msg.paneStateOK,
			},
		}, currentID)
		if result.UsePreview {
			m.workspace.preview = msg.preview
		}
		return m, nil

	case previewMsg:
		if !m.focusPane.AcceptPreview(msg.gen) {
			return m, nil
		}
		if sess, ok := m.selected(); ok && sess.ID == msg.sessID {
			m.setPreview(msg.sessID, msg.preview)
			m.workspace.proc = msg.proc
			m.workspace.procFor = msg.sessID
		}
		return m, nil

	case diffLoadedMsg:
		return m, m.handleDiffLoaded(msg)

	case diffFileLoadedMsg:
		return m, m.handleDiffFileLoaded(msg)

	case diffFilesLoadedMsg:
		var cmds []tea.Cmd
		for _, loaded := range msg {
			if cmd := m.handleDiffFileLoaded(loaded); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)

	case diffHLMsg:
		m.handleDiffHL(msg)
		return m, nil

	case diffProbeMsg:
		return m, m.handleDiffProbe(msg)

	case reviewStatusesLoadedMsg:
		m.handleReviewStatusesLoaded(msg)
		return m, nil

	case reviewStateSavedMsg:
		m.handleReviewStateSaved(msg)
		return m, nil

	case reviewCommentHandledMsg:
		m.handleReviewCommentHandled(msg)
		return m, nil

	case reviewSendFinishedMsg:
		m.handleReviewSendFinished(msg)
		return m, nil

	case errMsg:
		m.errBar.text = msg.err.Error()
		return m, nil

	case forkedInSourceMsg:
		return m.handleForkedInSource(msg)

	case pasteImageMsg:
		return m.handlePasteImageMsg(msg)

	case pasteTextMsg:
		return m.handlePasteTextMsg(msg)

	case attachDoneMsg:
		// An agent that repainted the terminal background for itself leaves
		// it on ours; the resume's WindowSizeMsg skips its own sync when the
		// size is unchanged, so the detach restores the theme's here.
		SyncTerminalBackground()
		// The attach client sized the window to the full terminal and tmux
		// keeps that size on detach; pin it back to the current layout's
		// box so the capture is not clipped on the right.
		if m.focusRuntime.geom != nil {
			delete(m.focusRuntime.geom, msg.sessID)
		}
		width, height := m.paneTargetSize()
		m.poller.reflowSessions([]string{msg.sessID}, func() {
			_ = m.services.tmux.Resize(msg.sessID, width, height)
		})
		if m.focusRuntime.geom == nil {
			m.focusRuntime.geom = map[string][2]int{}
		}
		m.focusRuntime.geom[msg.sessID] = [2]int{width, height}
		if msg.err != nil {
			m.errBar.text = msg.err.Error()
			m.requestRefresh()
			return m, nil
		}
		// Ctrl+R and F3 inside the session leave a marker before
		// detaching; consume it here and carry it out for the session just
		// attached.
		request, err := m.services.tmux.PendingRequest()
		if err != nil {
			m.errBar.text = err.Error()
		} else if request != "" {
			// A failed clear leaves the marker set, which would replay the
			// request on every later detach, so surface it and stay in the
			// list rather than letting the request reset m.errBar.text and
			// hide it.
			if clearErr := m.services.tmux.ClearRequest(); clearErr != nil {
				m.errBar.text = clearErr.Error()
				m.requestRefresh()
				return m, nil
			}
			// Both requests act on the row under the cursor, and the cursor
			// is not where the request came from: a poll handled ahead of
			// this message rebuilds the rows, and a filter can drop the
			// session that detached out of the list entirely.
			m.focusSession(msg.sessID)
			sess, ok := m.selected()
			if !ok || sess.ID != msg.sessID {
				m.errBar.text = "the session that asked for it has left the list"
				m.requestRefresh()
				return m, nil
			}
			switch request {
			case tmux.RequestReview:
				cmd := m.openDiff()
				if m.mode == modeDiff {
					m.diff.reattachID = sess.ID
				}
				return m, cmd
			case tmux.RequestEditor:
				_, cmd := m.openEditor()
				// The request cost the session its client, so the manager
				// goes back into it once the editor is up, or once a
				// terminal editor closes. A refused launch returns no
				// command and stays in the list with its reason.
				if cmd != nil {
					m.editorReturnID = sess.ID
				}
				return m, cmd
			}
		}
		m.requestRefresh()
		return m, nil

	case diffFileCheckedMsg:
		return m.handleDiffFileChecked(msg)

	case editorDoneMsg:
		var resume tea.Cmd
		if msg.tookScreen {
			// The terminal comes back from an editor the way it comes back
			// from an attach: painted in the editor's background, and
			// without the mouse reporting focus mode armed on the way in.
			SyncTerminalBackground()
			if m.mode == modeFocus {
				resume = tea.EnableMouseCellMotion
			}
		}
		if msg.err != nil {
			// Going back into the session would hide the only account of
			// what went wrong, so a failed editor keeps the list.
			m.errBar.text = msg.err.Error()
			m.editorReturnID = ""
			return m, resume
		}
		if msg.name != "" {
			m.reportDone("opened " + msg.path + " in " + msg.name)
		}
		if id := m.editorReturnID; id != "" {
			m.editorReturnID = ""
			return m, tea.Batch(resume, m.reattach(id, m.diff.gen))
		}
		return m, resume

	case relaunchedMsg:
		if msg.err != nil {
			m.reportLaunchError(msg.err, nil)
			return m, nil
		}
		m.bindReviveLocally(msg.sessID, msg.launchedAt)
		m.rebuildRows()
		m.requestRefresh()
		return m, nil

	case reattachPreparedMsg:
		if msg.diffGen != m.diff.gen || m.diff.active {
			return m, nil
		}
		if msg.err != nil {
			m.errBar.text = msg.err.Error()
			return m, nil
		}
		m.errBar.text = msg.warn
		return m, execTerminalProcess(m.services.tmux.AttachCommand(msg.sessID), func(err error) tea.Msg {
			return attachDoneMsg{sessID: msg.sessID, err: err}
		})

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case autoscrollMsg:
		return m.handleAutoscroll(msg)

	case tea.KeyMsg:
		model, cmd := m.handleKey(msg)
		m.syncPollInput()
		return model, cmd
	}
	return m, nil
}
