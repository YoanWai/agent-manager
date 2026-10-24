package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
	sessiondialogs "github.com/YoanWai/agent-manager/internal/ui/session/dialogs"
	uiseditor "github.com/YoanWai/agent-manager/internal/ui/session/editor"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	tea "github.com/charmbracelet/bubbletea"
	"os/exec"
	"time"
)

func (m *Model) archiveSelected() (tea.Model, tea.Cmd) {
	row, ok := m.selectedLifecycleRow()
	if !ok {
		return m, nil
	}
	return m.runSessionIntent(sessiondialogs.Archive(m.sessionHost(), row))
}

func (m *Model) restoreSelected() (tea.Model, tea.Cmd) {
	row, ok := m.selectedLifecycleRow()
	if !ok {
		return m, nil
	}
	return m.runSessionIntent(sessiondialogs.Restore(m.sessionHost(), row))
}

func (m *Model) killSelected() (tea.Model, tea.Cmd) {
	row, ok := m.selectedLifecycleRow()
	if !ok {
		return m, nil
	}
	return m.runSessionIntent(sessiondialogs.Kill(m.sessionHost(), row))
}

func (m *Model) killAllLive() (tea.Model, tea.Cmd) {
	return m.runSessionIntent(sessiondialogs.KillAll(m.sessionHost()))
}

func (m *Model) restartSelected() (tea.Model, tea.Cmd) {
	row, ok := m.selectedLifecycleRow()
	if !ok {
		return m, nil
	}
	return m.runSessionIntent(sessiondialogs.Restart(m.sessionHost(), row))
}

func (m *Model) reviveSelected() (tea.Model, tea.Cmd) {
	row, ok := m.selectedLifecycleRow()
	if !ok {
		return m, nil
	}
	return m.runSessionIntent(sessiondialogs.Revive(m.sessionHost(), row))
}

func (m *Model) reviveAllDead() (tea.Model, tea.Cmd) {
	return m.runSessionIntent(sessiondialogs.ReviveAll(m.listedSessions()))
}

func (m *Model) prepareDelete() {
	row, ok := m.selectedLifecycleRow()
	if !ok {
		return
	}
	m.runSessionIntent(sessiondialogs.Delete(m.sessionHost(), row))
}

// cancelEndSelected keeps the selected session past its turn, withdrawing the
// archive or kill it asked for.
func (m *Model) cancelEndSelected() (tea.Model, tea.Cmd) {
	row, hasRow := m.selectedLifecycleRow()
	id, name, ok := sessiondialogs.CancelEnd(row, hasRow)
	if !ok {
		return m, nil
	}
	m.enqueueEffect(uilifecycle.AfterTurnRequest{ID: id, Name: name, Cancel: true}, 0, false)
	return m, nil
}

// endAfterTurns archives or kills each session whose turn ended after it asked
// for that. Dispatch is enqueue-only: Update's trailing nextEffectCmd starts
// it.
func (m *Model) endAfterTurns(ids []string) {
	for _, id := range ids {
		m.enqueueEffect(uilifecycle.AfterTurnRequest{ID: id, Watched: m.focus.runtime.Watching()}, 0, false)
	}
}

func (m *Model) applyAfterTurn(request uilifecycle.AfterTurnRequest, result uilifecycle.AfterTurnResult, err error) tea.Cmd {
	apply := uilifecycle.AfterTurn(request, result, err)
	var command tea.Cmd
	if apply.Change != nil {
		command = m.applyLifecycleEffect(apply.ChangeRequest, result.Lifecycle, apply.Err)
	} else {
		if apply.Err != nil {
			m.reportErr(apply.Err.Error())
		}
		m.rewatchSurvivor(request.Watched, apply.Survivor)
	}
	if apply.ClearID != "" {
		m.clearAfterTurn(apply.ClearID)
		if apply.Done != "" {
			m.reportDone(apply.Done + " stays once its turn ends")
		}
	}
	return command
}

// applyLifecycleEffect reconciles the loaded list with what a session action
// did: the statuses that changed, the rows that left or came back, the panes
// this run launched, and what the completion reports.
func (m *Model) applyLifecycleEffect(request uilifecycle.Request, result uilifecycle.Result, err error) tea.Cmd {
	change := uilifecycle.Changes(request, result, err)
	for _, each := range change.Statuses {
		m.setSessionStatus(each.ID, each.Status)
	}
	for _, id := range change.Forget {
		m.forgetLaunch(id)
		delete(m.workspace.panes, id)
	}
	if edit := change.Edit; edit != nil {
		if edit.Archive {
			m.markArchivedLocally(edit)
		} else {
			m.markRestoredLocally(edit)
		}
	}
	for _, sess := range change.Deleted {
		m.ledger.lifecycle.ForgetSession(sess.ID)
		m.removeSessionLocally(sess.ID)
	}
	if len(change.RemovedGroups) > 0 {
		m.queueRail(m.rail.ForgetGroups(change.RemovedGroups).Mutations, 0, true)
		m.pruneGroupsLocally(change.RemovedGroups)
	}
	restart := confirmAction(request.Target.Action) == uidialogs.ConfirmRestart
	for _, each := range change.Launches {
		m.markFreshPane(each.ID)
		m.ledger.lifecycle.RecordLaunch(each.ID, time.Now())
		if restart {
			m.bindRestartLocally(each.ID, each.Result.Conversation, each.Result.LaunchedAt)
		} else {
			m.bindReviveLocally(each.ID, each.Result.LaunchedAt)
		}
	}
	if len(change.Launches) > 0 {
		m.focus.runtime.Retry()
	}
	m.rewatchSurvivor(request.Watched, change.Survivor)
	m.rebuildRows()
	for _, notice := range change.Notices {
		m.reportErr(notice)
	}
	if change.Warning != nil {
		m.reportErr(change.Warning.Error())
	}
	m.reportRefusal(change.Refusal)
	return nil
}

// setSessionStatus lands one row's status from a finished run.
// setSessionStatus lands one row's status from a finished run.
func (m *Model) setSessionStatus(id, status string) {
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == id {
			m.workspace.sessions[i].Status = status
		}
	}
}

func (m *Model) reportRefusal(refusal *uilifecycle.Refusal) {
	if refusal == nil {
		return
	}
	if !refusal.ViaLaunchHint || !m.listClear() {
		m.reportErr(refusal.Err.Error())
		return
	}
	// An async completion must not steal a newly opened modal or its
	// attachments.
	m.reportLaunchError(refusal.Err)
	if refusal.Retry == nil {
		return
	}
	if fix, ok := m.launchHintFix(); ok {
		fix.Retry = *refusal.Retry
	}
}

// clearAfterTurn drops the request a finished session no longer carries, so
// the row stops showing the archive or kill it withdrew.
func (m *Model) clearAfterTurn(id string) {
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == id {
			m.workspace.sessions[i].AfterTurn, m.workspace.sessions[i].AfterTurnAt = "", time.Time{}
		}
	}
	m.rebuildRows()
}

// markArchivedLocally flags the confirmed archive in the loaded rows, so they
// leave the active view on this frame instead of first showing the dead state
// their kill just gave them. A group archive also flags the group itself and
// every group under it, which is what hides the whole subtree from the active
// view.
func (m *Model) markArchivedLocally(edit *uilifecycle.ArchiveEdit) {
	changed := false
	for _, sess := range uilifecycle.ArchiveRows(m.workspace.sessions, edit) {
		m.ledger.lifecycle.MarkSession(sess.ID, uilifecycle.GoneMark{Archived: true})
		changed = true
	}
	if edit.Group != "" {
		for _, path := range append([]string{edit.Group}, uilifecycle.Subgroups(m.workspace.groups, edit.Group)...) {
			if m.workspace.archivedGroups == nil {
				m.workspace.archivedGroups = map[string]bool{}
			}
			m.workspace.archivedGroups[path] = true
			m.ledger.lifecycle.MarkGroup(path, uilifecycle.GoneMark{Archived: true})
		}
		changed = true
	}
	if changed {
		m.rebuildRows()
	}
}

// markRestoredLocally mirrors a completed restore in the loaded rows and group
// flags, so what came back changes views on this frame rather than waiting for
// the next poll.
func (m *Model) markRestoredLocally(edit *uilifecycle.ArchiveEdit) {
	for _, sess := range uilifecycle.RestoreRows(m.workspace.sessions, edit) {
		m.ledger.lifecycle.MarkSession(sess.ID, uilifecycle.GoneMark{Archived: false})
	}
	if edit.Group != "" {
		for _, path := range append([]string{edit.Group}, uilifecycle.Subgroups(m.workspace.groups, edit.Group)...) {
			delete(m.workspace.archivedGroups, path)
			m.ledger.lifecycle.MarkGroup(path, uilifecycle.GoneMark{Archived: false})
		}
	} else {
		for id := range edit.Rows {
			for _, sess := range m.workspace.sessions {
				if sess.ID != id {
					continue
				}
				for _, path := range uilifecycle.Ancestors(sess.Group) {
					delete(m.workspace.archivedGroups, path)
					m.ledger.lifecycle.MarkGroup(path, uilifecycle.GoneMark{Archived: false})
				}
			}
		}
	}
	m.rebuildRows()
}

// removeSessionLocally takes a deleted row off the loaded list right away, so
// it leaves the screen on this frame instead of waiting for the next poll to
// confirm what the store already knows.
func (m *Model) removeSessionLocally(id string) {
	m.workspace.sessions = uilifecycle.RemoveSession(m.workspace.sessions, id)
	m.ledger.lifecycle.MarkSession(id, uilifecycle.GoneMark{Deleted: true})
	m.rebuildRows()
}

// pruneGroupsLocally drops the removed group paths from the loaded tree, so a
// deleted group's header goes with its sessions instead of hanging around
// empty until the next poll. Each path is recorded for the stale listing
// filter, which is what keeps an in-flight poll from restoring it.
func (m *Model) pruneGroupsLocally(removed []string) {
	for _, path := range removed {
		m.ledger.lifecycle.MarkGroup(path, uilifecycle.GoneMark{Deleted: true})
		delete(m.workspace.groupPaths, path)
		delete(m.workspace.groupWorktrees, path)
		delete(m.workspace.groupBases, path)
		delete(m.workspace.archivedGroups, path)
	}
	m.workspace.groups = uilifecycle.DropGroups(m.workspace.groups, removed)
	m.rebuildRows()
}

// bindRestartLocally mirrors the store write in the loaded rows, so the list
// redraws on the new conversation before the next poll re-reads it.
func (m *Model) bindRestartLocally(id, agentSessionID string, launchedAt time.Time) {
	uilifecycle.BindRestart(m.workspace.sessions, id, agentSessionID, launchedAt)
}

// bindReviveLocally mirrors the store write in the loaded rows, so the startup
// loader can paint before the next poll re-reads them.
func (m *Model) bindReviveLocally(id string, launchedAt time.Time) {
	uilifecycle.BindRevive(m.workspace.sessions, id, launchedAt)
	if sel, ok := m.selected(); ok && sel.ID == id {
		// A killed or archived row still holds its last pane; that would count
		// as painted and hide the loader.
		m.workspace.preview = ""
	}
}

func (m *Model) rewatchSurvivor(watched string, survivor bool) {
	if !survivor {
		return
	}
	if sess, ok := m.selected(); ok && sess.ID == watched && !sess.Archived {
		m.watchSelection()
	}
}

// sessionWindowProjectedLive combines the last poll with launches accepted
// since that poll. The lifecycle worker checks tmux again before it mutates;
// this projection only decides which confirmation the UI presents.
func (m *Model) sessionWindowProjectedLive(id string) bool {
	if m.workspace.panes[id].PID > 0 {
		return true
	}
	_, launched := m.ledger.lifecycle.Launched[id]
	return launched
}

// selectedLifecycleRow is the selected row as the session keys read it.
func (m *Model) selectedLifecycleRow() (sessiondialogs.Row, bool) {
	entry, ok := m.selectedRow()
	if !ok {
		return sessiondialogs.Row{}, false
	}
	switch {
	case entry.isRoot():
		return sessiondialogs.Row{Kind: sessiondialogs.RootRow}, true
	case entry.isGroup:
		return sessiondialogs.Row{Kind: sessiondialogs.GroupRow, Group: entry.group}, true
	}
	return sessiondialogs.Row{Kind: sessiondialogs.SessionRow, Group: entry.group, Sess: entry.sess}, true
}

// runSessionIntent executes what a session key decided: the card it opens, the
// batch it queues, or the line it reports.
func (m *Model) runSessionIntent(intent sessiondialogs.Intent) (tea.Model, tea.Cmd) {
	if intent.Nothing() {
		return m, nil
	}
	if intent.Notice != "" {
		m.reportErr(intent.Notice)
		return m, nil
	}
	if intent.ClearsErr {
		m.clearErr()
	}
	if intent.Run {
		m.queueLifecycleCard(intent.Card, intent.AllowLive, intent.EmptyNotice)
		return m, m.nextEffectCmd()
	}
	m.openConfirm(cardTarget(intent.Card))
	return m, nil
}

// cardTarget is a decision's card as the confirmation's apply reads it.
func cardTarget(card sessiondialogs.Card) confirmTarget {
	return confirmTarget{
		isGroup:      card.Group,
		archivedOnly: card.ArchivedOnly,
		path:         card.Path,
		label:        card.Label,
		sessions:     card.Sessions,
		action:       card.Action,
		batch:        card.Batch,
		selection:    cardSelection(card.Selection),
	}
}

// packageTarget is a confirmation as the lifecycle package runs it.
func packageTarget(target confirmTarget) uilifecycle.Target {
	return uilifecycle.Target{
		Action:       lifecycleAction(target.action),
		Sessions:     target.sessions,
		Path:         groupPath(target),
		Batch:        target.batch,
		ArchivedOnly: target.archivedOnly,
		Selection:    packageSelection(target.selection),
	}
}

// confirmAction is the package's action as the card spells it: the card names
// delete with its zero value.
func confirmAction(action uilifecycle.Action) uidialogs.ConfirmAction {
	if action == uilifecycle.Remove {
		return uidialogs.ConfirmDelete
	}
	return uidialogs.ConfirmAction(action)
}

func lifecycleAction(action uidialogs.ConfirmAction) uilifecycle.Action {
	if action == uidialogs.ConfirmDelete {
		return uilifecycle.Remove
	}
	return uilifecycle.Action(action)
}

func packageSelection(selection lifecycleSelection) uilifecycle.Selection {
	return uilifecycle.Selection{Kind: uilifecycle.SelectionKind(selection.kind), RootID: selection.rootID, ArchivedOnly: selection.archivedOnly}
}

// queueLifecycle queues a confirmation as the lifecycle package runs it.
func (m *Model) queueLifecycle(target confirmTarget, allowLive bool, emptyNotice string) {
	target.sessions = append([]store.Session(nil), target.sessions...)
	w, h := m.paneTargetSize()
	m.enqueueEffect(uilifecycle.Request{Target: packageTarget(target), Pane: sessioncmd.PaneSize{Width: w, Height: h}, Watched: m.focus.runtime.Watching(), AllowLive: allowLive, EmptyNotice: emptyNotice}, 0, false)
}

func cardSelection(selection sessiondialogs.Selection) lifecycleSelection {
	return lifecycleSelection{kind: lifecycleSelectionKind(selection.Kind), rootID: selection.RootID, archivedOnly: selection.ArchivedOnly}
}

func groupPath(confirm confirmTarget) string {
	if confirm.isGroup {
		return confirm.path
	}
	return ""
}

// queueLifecycleCard queues an action a decision returned.
func (m *Model) queueLifecycleCard(card sessiondialogs.Card, allowLive bool, emptyNotice string) {
	target := cardTarget(card)
	target.sessions = append([]store.Session(nil), target.sessions...)
	w, h := m.paneTargetSize()
	m.enqueueEffect(uilifecycle.Request{Target: packageTarget(target), Pane: sessioncmd.PaneSize{Width: w, Height: h}, Watched: m.focus.runtime.Watching(), AllowLive: allowLive, EmptyNotice: emptyNotice}, 0, false)
}

// lifecycleServices is the effect services as a lifecycle run reaches them.
func (s effectServices) lifecycleServices() uilifecycle.Services {
	services := uilifecycle.Services{
		Lifecycle: s.lifecycle,
		Store:     s.store,
		Driver:    s.driver,
		Reflow:    s.reflowSessions,
	}
	// A nil watcher stored in the interface would read as present.
	if s.watch != nil {
		services.Watch = s.watch
	}
	return services
}

// reflowSessions holds the panes a lifecycle run touches, the lane contract
// that keeps an input job from writing into a session it is killing.
func (s effectServices) reflowSessions(ids []string, work func()) error { return s.reflow(ids, work) }

func (m *Model) attachSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	// The dead-pane probe and the finished acknowledgement run inside the
	// attach effect; a dead session reports back from its completion.
	m.clearErr()
	return m, m.attachCmd(sess.ID)
}

// acknowledgeSelected marks the selected finished session idle and acked
// without entering it.
func (m *Model) acknowledgeSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	id, apply := uilifecycle.Acknowledge(sess, ok)
	if !apply {
		return m, nil
	}
	m.clearErr()
	m.enqueueEffect(focusruntime.AckRequest{SessionID: id}, 0, false)
	return m, m.nextEffectCmd()
}

// fence is the screen a request was made from, in the shape a queued request
// carries, so the package that runs the work never holds a root type.
func (m *Model) fence() uilifecycle.Fence {
	return uilifecycle.Fence{Base: uint8(m.base), Focused: m.focused(), Depth: len(m.surfaces)}
}

func (m *Model) attachCmd(id string) tea.Cmd {
	m.enqueueEffect(uilifecycle.AttachRequest{ID: id, ForegroundGen: m.gens.foreground, Screen: m.fence()}, 0, false)
	return m.nextEffectCmd()
}

func (m *Model) reattach(id string, diffGen int) tea.Cmd {
	m.enqueueEffect(uilifecycle.AttachRequest{ID: id, Reattach: true, Generation: diffGen, ForegroundGen: m.gens.foreground, Screen: m.fence()}, 0, false)
	return m.nextEffectCmd()
}

// copyReplySelected puts the selected session's newest reply on the system
// clipboard without entering it.
func (m *Model) copyReplySelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok || entry.isGroup || m.services.engine == nil || m.services.tmux == nil {
		return m, nil
	}
	sess := entry.sess
	m.clearErr()
	// A shell has no reply, and its scrollback is the user's own commands
	// and their output rather than anything an agent said.
	if m.isShell(sess.Tool) {
		m.reportErr(uisession.ShellPromptHint(sess.Name))
		return m, nil
	}
	reader := uilifecycle.ReplyReader{Driver: m.services.tmux, Engine: m.services.engine, History: quoteHistoryLines}
	return m, func() tea.Msg {
		reply, err := reader.Copy(sess)
		if err != nil {
			return errMsg{err}
		}
		if reply.Text == "" {
			return replyCopiedMsg{name: sess.Name, tool: sess.Tool, unreadable: reply.Unreadable}
		}
		return copyTextCmd(reply.Text, func(chars int) tea.Msg {
			return replyCopiedMsg{chars: chars, name: sess.Name, tool: sess.Tool, unbounded: reply.Unbounded}
		})()
	}
}

// replyCopiedMsg reports a finished copy. No chars means the turn held
// nothing; unreadable means the tool draws no region a reply can be read
// from, which no amount of retrying will change; unbounded means nothing
// in the pane said where the turn began, so the copy is the whole screen
// rather than one answer.
type replyCopiedMsg struct {
	chars      int
	name       string
	tool       string
	unreadable bool
	unbounded  bool
}

type attachDoneMsg struct {
	sessID string
	err    error
}

// routeSessionMsg routes the messages a session key's work finishes with.
func (m *Model) routeSessionMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case replyCopiedMsg:
		switch notice, severity := uisession.ReplyReport(msg.name, msg.tool, msg.chars, msg.unreadable, msg.unbounded); severity {
		case uisession.Done:
			m.reportDone(notice)
		case uisession.Warn:
			m.reportWarn(notice)
		default:
			m.reportErr(notice)
		}
		return routed(m, nil)

	case uiinstall.CopiedMsg:
		cmd, _ := m.ledger.install.Route(installRoot{m}, msg)
		return routed(m, cmd)

	case attachDoneMsg:
		return routed(m, m.attachDone(msg))
	}
	return nil, nil, false
}

// attachDone finishes an attach the terminal handed back: the background the
// agent left, the pane size the attach client changed, and the marker the
// session wrote before detaching.
func (m *Model) attachDone(msg attachDoneMsg) tea.Cmd {
	// An agent that repainted the terminal background for itself leaves it on
	// ours; the resume's WindowSizeMsg skips its own sync when the size is
	// unchanged, so the detach restores the theme's here.
	SyncTerminalColors()
	// The attach client sized the window to the full terminal and tmux keeps
	// that size on detach; pin it back to the current layout's box so the
	// capture is not clipped on the right.
	m.adoptPaneGeom(msg.sessID)
	if msg.err != nil {
		m.reportErr(msg.err.Error())
		m.requestRefresh()
		return nil
	}
	// Ctrl+R and F3 inside the session leave a marker before detaching; the
	// lane reads it once and clears it, and the completion carries it out for
	// the session just attached.
	m.enqueueEffect(focusruntime.DetachRequest{SessionID: msg.sessID}, 0, false)
	return nil
}

// lookPath and startEditor are the seams tests swap to control which editors
// this machine has and to observe the launch instead of running it.
var (
	lookPath    = exec.LookPath
	startEditor = func(cmd *exec.Cmd) error {
		if err := cmd.Start(); err != nil {
			return err
		}
		// The manager runs for days at a time; without this every o would
		// leave the finished editor process behind holding its pipes.
		go func() { _ = cmd.Wait() }()
		return nil
	}
)

type editorDoneMsg struct {
	name       string
	path       string
	err        error
	tookScreen bool
	returnTo   editorReturnTarget
}

type editorReturnTarget struct {
	sessionID     string
	foregroundGen uint64
	screen        screenID
}

// resumes reports whether a returning editor goes back into the session it
// was opened from: nothing the user did since may have moved the screen on.
func (t editorReturnTarget) resumes(foregroundGen uint64, current screenID, quitting bool) bool {
	return t.sessionID != "" && !quitting && t.foregroundGen == foregroundGen && t.screen == current
}

type editorFileCheckedMsg struct {
	result        uireview.FileCheckResult
	editor        uisession.Launch
	foregroundGen uint64
}

func (m *Model) openEditor() (tea.Model, tea.Cmd) {
	return m.openEditorWithReaderForReturn(newSystemTerminalDirectoryReader(m), "")
}

func (m *Model) openEditorWithReaderForReturn(reader terminalDirectoryReader, returnID string) (tea.Model, tea.Cmd) {
	request, ok := m.captureTerminalDirectory(terminalDirectoryEditor)
	if !ok {
		return m, nil
	}
	request.returnID = returnID
	request.editor = m.captureEditorResolution()
	return m, terminalDirectoryCmd(request, reader)
}

func (m *Model) openDiffFile() (tea.Model, tea.Cmd) {
	request := m.review.OpenFileRequest()
	if request == nil {
		return m, nil
	}
	return m, reviewFileCheckCmd(*request, m.captureEditorResolution(), m.gens.foreground)
}

func (m *Model) handleDiffFileChecked(msg editorFileCheckedMsg) (tea.Model, tea.Cmd) {
	if m.effects.Quitting || m.base != baseReview || msg.foregroundGen != m.gens.foreground {
		return m, nil
	}
	path, accepted := m.review.ApplyFileCheck(msg.result)
	if !accepted {
		return m, nil
	}
	if msg.result.Err != nil {
		m.reportErr(reviewOpenPathError(path, msg.result.Err))
		return m, nil
	}
	return m.launchEditor(msg.editor, editorReturnTarget{})
}

func (m *Model) launchEditor(editor uisession.Launch, returnTo editorReturnTarget) (tea.Model, tea.Cmd) {
	if editor.Command == nil {
		m.reportErr("no editor found: pick one in Settings > editor")
		return m, nil
	}
	m.clearErr()
	if editor.TakesScreen() {
		return m, uiterminal.ExecProcess(editor.Command, func(err error) tea.Msg {
			return editorDoneMsg{err: err, tookScreen: true, returnTo: returnTo}
		})
	}
	return m, startEditorCmd(editor.Command, editor.Name, editor.Path, returnTo)
}

// Starting a process is exec, which Update must not do: a slow launch
// would hold the next keystroke.
func startEditorCmd(cmd *exec.Cmd, name, path string, returnTo editorReturnTarget) tea.Cmd {
	return func() tea.Msg {
		if err := startEditor(cmd); err != nil {
			return editorDoneMsg{err: err, returnTo: returnTo}
		}
		return editorDoneMsg{name: name, path: path, returnTo: returnTo}
	}
}

func (m *Model) captureEditorResolution() editorResolution {
	return uiseditor.Resolution(m.services.editor, lookPath)
}

func (m *Model) routeEditorMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case editorFileCheckedMsg:
		return routed(m.handleDiffFileChecked(msg))

	case editorDoneMsg:
		var resume tea.Cmd
		if msg.tookScreen {
			// The terminal comes back from an editor the way it comes back
			// from an attach: painted in the editor's background, and
			// without the mouse reporting focus mode armed on the way in.
			SyncTerminalColors()
			if m.focused() {
				resume = tea.EnableMouseCellMotion
			}
		}
		if msg.err != nil {
			// Going back into the session would hide the only account of
			// what went wrong, so a failed editor keeps the list.
			m.reportErr(msg.err.Error())
			return routed(m, resume)
		}
		if msg.name != "" {
			m.reportDone("opened " + msg.path + " in " + msg.name)
		}
		if target := msg.returnTo; target.resumes(m.gens.foreground, m.screenID(), m.effects.Quitting) {
			return routed(m, tea.Batch(resume, m.reattach(target.sessionID, m.review.Generation())))
		}
		return routed(m, resume)
	}
	return nil, nil, false
}

// The root keeps the resolution's type here: terminal.go, review_adapter.go
// and settings_adapter.go are wired to it, and their call sites belong to
// other prefixes.
type editorResolution = uiseditor.EditorResolution

type launchOptions struct {
	rollbackWorktree bool
}

func (m *Model) launchNewSession(sess store.Session, tool config.Tool, baseCommand string, opts launchOptions) error {
	paneWidth, paneHeight := m.paneTargetSize()
	launched, err := m.services.lifecycle.Launch(sessioncmd.LaunchRequest{
		Session:          sess,
		Tool:             tool,
		BaseCommand:      baseCommand,
		Pane:             sessioncmd.PaneSize{Width: paneWidth, Height: paneHeight},
		RollbackWorktree: opts.rollbackWorktree,
	})
	if err != nil {
		return err
	}
	m.markFreshPane(sess.ID)
	sess = launched.Session
	m.recordLaunch(sess.ID)
	m.workspace.sessions = append(m.workspace.sessions, sess)
	m.rebuildRows()
	return launched.LabelError
}

// recordLaunch marks a row this run launched, so a poll that listed the store
// before it cannot blink the row off screen.
func (m *Model) recordLaunch(id string) { m.ledger.lifecycle.RecordLaunch(id, time.Now()) }

// forgetLaunch drops a row from the pending set, so a session the user has
// since sent away is not carried back onto the tree by a poll that listed the
// store before it was launched.
func (m *Model) forgetLaunch(id string) { m.ledger.lifecycle.ForgetLaunch(id) }

// reconcileListing reconciles a stale listing's group rows and archive flags
// against what this run has just done.
func (m *Model) reconcileListing(msg *refreshMsg) {
	listing := m.ledger.lifecycle.ReconcileListing(uilifecycle.Listing{
		ListedAt:       msg.listedAt,
		Groups:         msg.groups,
		GroupPaths:     msg.groupPaths,
		GroupWorktrees: msg.groupWorktrees,
		GroupBases:     msg.groupBases,
		ArchivedGroups: msg.archivedGroups,
	})
	msg.groups = listing.Groups
	msg.groupPaths = listing.GroupPaths
	msg.groupWorktrees = listing.GroupWorktrees
	msg.groupBases = listing.GroupBases
	msg.archivedGroups = listing.ArchivedGroups
}

// applyAttachEffect hands the terminal to a session once its pane checked
// out. The fences are compared here, on the completion rather than the
// request, because the user may have moved the screen while it waited.
func (m *Model) applyAttachEffect(request uilifecycle.AttachRequest, result uilifecycle.AttachResult, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	if request.Reattach && (request.Generation != m.review.Generation() || m.review.Active()) {
		return nil
	}
	if m.effects.Quitting || request.ForegroundGen != m.gens.foreground || request.Screen != m.fence() {
		return nil
	}
	if result.Warning != nil {
		m.reportErr(result.Warning.Error())
	}
	return uiterminal.ExecProcess(m.services.tmux.AttachCommand(request.ID), func(err error) tea.Msg { return attachDoneMsg{sessID: request.ID, err: err} })
}

// launchLedger is what a manager run remembers about the sessions it starts
// and ends, outliving the cards that asked: each piece's state and runtime
// live in its package.
type launchLedger struct {
	// composerSeq numbers the prompt boxes this run has opened.
	composerSeq int
	lifecycle   uilifecycle.State
	spawner     uilaunch.State
	install     uiinstall.State
}
