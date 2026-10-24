package ui

import (
	"os"
	"path/filepath"

	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	tea "github.com/charmbracelet/bubbletea"
)

type reviewLoadMsg = uireviewsource.LoadMsg

type reviewFilesResult = uireviewsource.FilesResult

func reviewTarget(sess store.Session) uireview.Target { return uireviewsource.Target(sess) }

// reviewGroupBase is the base the target's group sets for its branch scope.
// A base picked in review wins over it.
func (m *Model) reviewGroupBase(targetID string) string {
	for _, sess := range m.workspace.sessions {
		if sess.ID == targetID {
			return m.groupBase(sess.Group)
		}
	}
	return ""
}

func (m *Model) reviewGit() uireviewsource.Git {
	return uireviewsource.SystemGit{Driver: m.services.gitDrv}
}

func (m *Model) reviewLoadCmd(req uireview.LoadRequest) tea.Cmd {
	return uireviewsource.LoadCmd(m.reviewGit(), m.services.store, req, m.reviewGroupBase(req.Target.ID))
}

func (m *Model) reviewFileCmd(req uireview.FileRequest) tea.Cmd {
	return uireviewsource.FileCmd(m.reviewGit(), req)
}

func (m *Model) reviewFilesCmd(requests []uireview.FileRequest) tea.Cmd {
	return uireviewsource.FilesCmd(m.reviewGit(), requests)
}

func (m *Model) reviewProbeCmd(req uireview.ProbeRequest) tea.Cmd {
	return uireviewsource.ProbeCmd(m.reviewGit(), m.services.store, req, m.reviewGroupBase(req.Target.ID))
}

func (m *Model) reviewHighlightCmd(req uireview.HighlightRequest) tea.Cmd {
	return uireviewsource.HighlightCmd(req)
}

// reviewSaveCmd persists one captured review state through the effect
// lane; the worker owns the merge, so no Update path touches SQLite here.
// Dispatch is enqueue-only: the lane's trailing nextEffectCmd starts it.
func (m *Model) reviewSaveCmd(req uireview.SaveRequest) tea.Cmd {
	m.enqueueEffect(reviewEffectRequest{
		op:       reviewOpSave,
		targetID: req.TargetID,
		repoRoot: req.RepoRoot,
		state:    req.State,
	}, 0, false)
	return nil
}

// reviewStatusCmd reads comment statuses through the effect lane, so it
// observes every Review mutation accepted before it without a private
// write fence.
func (m *Model) reviewStatusCmd(req uireview.StatusRequest) tea.Cmd {
	if m.services.store == nil {
		return nil
	}
	m.enqueueEffect(reviewEffectRequest{
		op:         reviewOpStatus,
		targetID:   req.TargetID,
		repoRoot:   req.RepoRoot,
		generation: req.Generation,
	}, 0, false)
	return nil
}

// reviewHandleCmd marks one comment handled through the effect lane; the
// worker owns the store write and the found/ambiguous outcome.
func (m *Model) reviewHandleCmd(req uireview.HandleCommentRequest) tea.Cmd {
	m.enqueueEffect(reviewEffectRequest{
		op:        reviewOpHandle,
		targetID:  req.TargetID,
		repoRoot:  req.RepoRoot,
		commentID: req.CommentID,
		handled:   req.Handled,
		previous:  req.Previous,
	}, 0, false)
	return nil
}

// reviewSendCmd persists the round and types the prompt through the effect
// lane, so the existence preflight, state merge, text delivery, rollback,
// and ack clear run in one ordered job behind every earlier Review
// mutation.
func (m *Model) reviewSendCmd(req uireview.SendRequest) tea.Cmd {
	session, _ := m.diffSession()
	m.enqueueEffect(reviewEffectRequest{
		op:            reviewOpSend,
		targetID:      req.Target.ID,
		targetName:    req.Target.Name,
		repoRoot:      req.RepoRoot,
		state:         req.State,
		previousState: req.PreviousState,
		prompt:        req.Prompt,
		commentIDs:    req.CommentIDs,
		previousRound: req.PreviousRound,
		round:         req.Round,
		count:         req.Count,
		sendTarget:    captureReviewSendTarget(session),
	}, 0, false)
	return nil
}

func reviewFileCheckCmd(req uireview.FileCheckRequest, editor editorResolution, foregroundGen uint64) tea.Cmd {
	return func() tea.Msg {
		_, err := os.Stat(req.Path)
		result := uireview.FileCheckResult{Request: req, Err: err}
		var launch editorLaunch
		if err == nil {
			launch = editor.prepare(req.Path)
		}
		return editorFileCheckedMsg{result: result, editor: launch, foregroundGen: foregroundGen}
	}
}

func (m *Model) reviewCommands(requests uireview.Requests) tea.Cmd {
	var cmds []tea.Cmd
	if requests.Load != nil {
		cmds = append(cmds, m.reviewLoadCmd(*requests.Load))
	}
	if len(requests.Files) > 0 {
		cmds = append(cmds, m.reviewFileCmd(requests.Files[0]))
		if len(requests.Files) > 1 {
			cmds = append(cmds, m.reviewFilesCmd(requests.Files[1:]))
		}
	}
	if requests.Highlight != nil {
		cmds = append(cmds, m.reviewHighlightCmd(*requests.Highlight))
	}
	if requests.Save != nil && m.services.store != nil {
		cmds = append(cmds, m.reviewSaveCmd(*requests.Save))
	}
	if requests.Status != nil && m.services.store != nil {
		cmds = append(cmds, m.reviewStatusCmd(*requests.Status))
	}
	if requests.Handle != nil && m.services.store != nil {
		cmds = append(cmds, m.reviewHandleCmd(*requests.Handle))
	}
	if requests.Send != nil && m.services.store != nil && m.services.tmux != nil {
		cmds = append(cmds, m.reviewSendCmd(*requests.Send))
	}
	if requests.FileCheck != nil {
		cmds = append(cmds, reviewFileCheckCmd(*requests.FileCheck, m.captureEditorResolution(), m.gens.foreground))
	}
	if requests.WidgetCmd != nil {
		cmds = append(cmds, requests.WidgetCmd)
	}
	if requests.StartupTick {
		cmds = append(cmds, m.startStartupTick())
	}
	return tea.Batch(cmds...)
}

func reviewStateFromStore(state store.ReviewState) uireview.SavedState {
	return uireviewsource.StateFromStore(state)
}

func reviewStateToStore(state uireview.SavedState) store.ReviewState {
	return uireviewsource.StateToStore(state)
}

func reviewOpenPathError(path string, err error) string {
	if os.IsNotExist(err) {
		return "file no longer exists: " + path
	}
	return "checking file " + filepath.Clean(path) + ": " + err.Error()
}

func (m *Model) routeReviewMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case reviewPickerLoadedMsg:
		return routed(m, m.handleReviewPickerLoaded(msg))

	case uireview.PickedMsg:
		return routed(m, m.applyReviewPick(msg))

	case reviewPreferencesMsg:
		return routed(m.handleReviewPreferences(msg))

	case reviewLoadMsg:
		if msg.Normalize != nil {
			m.enqueueEffect(reviewEffectRequest{op: reviewOpNormalize, targetID: msg.Normalize.TargetID, repoRoot: msg.Normalize.RepoRoot, state: msg.Normalize.State}, 0, false)
		}
		return routed(m, tea.Batch(m.handleReviewLoad(msg.Result), m.nextEffectCmd()))

	case uireview.FileResult:
		return routed(m, m.handleReviewFile(msg))

	case reviewFilesResult:
		var cmds []tea.Cmd
		for _, loaded := range msg {
			if cmd := m.handleReviewFile(loaded); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return routed(m, tea.Batch(cmds...))

	case uireview.HighlightResult:
		m.review.ApplyHighlight(msg)
		return routed(m, nil)

	case uireview.ProbeResult:
		return routed(m, m.handleReviewProbe(msg))

	case uireview.StatusResult:
		return routed(m, m.handleReviewStatus(msg))

	case uireview.SaveResult:
		return routed(m, m.handleReviewSave(msg))

	case uireview.HandleCommentResult:
		return routed(m, m.handleReviewComment(msg))

	case uireview.SendResult:
		return routed(m, m.handleReviewSend(msg))
	}
	return nil, nil, false
}

type reviewReturnKind = uireviewsource.ReturnKind

const (
	reviewReturnList   = uireviewsource.ReturnList
	reviewReturnFocus  = uireviewsource.ReturnFocus
	reviewReturnAttach = uireviewsource.ReturnAttach
)

type reviewReturn = uireviewsource.Return

func (m *Model) diffSession() (store.Session, bool) {
	for _, sess := range m.workspace.sessions {
		if sess.ID == m.review.SessionID() {
			return sess, true
		}
	}
	return store.Session{}, false
}

func (m *Model) closeDiff() tea.Cmd {
	ret := m.reviewNav.Take()
	gen := m.review.Close()
	m.base = baseList
	m.clearSurfaces()
	switch ret.Kind {
	case reviewReturnAttach:
		return m.reattach(ret.SessionID, gen)
	case reviewReturnFocus:
		if !m.focusSession(ret.SessionID) {
			m.reportErr("the review origin is no longer listed")
			return nil
		}
		_, cmd := m.focusSelected()
		return cmd
	default:
		return nil
	}
}

type reviewPreferencesReader = uireviewsource.Preferences

type reviewPreferencesMsg = uireviewsource.PreferencesMsg

func (m *Model) openDiff() tea.Cmd { return m.openDiffWithReader(m.services.store) }

func (m *Model) openDiffWithReader(reader reviewPreferencesReader) tea.Cmd {
	if m.services.gitDrv == nil {
		m.reportErr("git not found in PATH, " + deps.Hint("git"))
		return nil
	}
	sess, ok := m.selected()
	if !ok {
		m.reportErr("select a session to diff")
		return nil
	}
	preferred, picked := m.ledger.lifecycle.PickedRepos[sess.ID]
	m.reviewNav.Set(reviewReturn{Kind: reviewReturnList})
	m.clearSurfaces()
	m.base = baseReview
	m.clearErr()
	target := reviewTarget(sess)
	target.Cwd = m.sessionDir(sess)
	request := m.review.Open(target, git.ScopeUncommitted, preferred)
	return uireviewsource.PreferencesCmd(reader, sess, request, preferred, picked)
}

func (m *Model) handleReviewPreferences(msg reviewPreferencesMsg) (tea.Model, tea.Cmd) {
	if m.effects.Quitting || !m.review.Active() || m.review.Generation() != msg.Request.Generation || m.review.SessionID() != msg.Request.Target.ID {
		return m, nil
	}
	current, ok := m.diffSession()
	if !ok || !current.CreatedAt.Equal(msg.Session.CreatedAt) || !current.LaunchTime().Equal(msg.Session.LaunchTime()) || current.TmuxSocket != msg.Session.TmuxSocket {
		return m, nil
	}
	request := m.review.Open(msg.Request.Target, msg.Scope, msg.Preferred)
	if msg.Err != nil {
		m.reportErr("reading review preferences: " + msg.Err.Error())
	}
	return m, tea.Batch(m.reviewLoadCmd(request), m.startStartupTick())
}

func (m *Model) handleDiffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ctx := uireview.KeyContext{CodeHeight: m.diffCodeHeight(), PersistenceAvailable: m.services.store != nil}
	if sess, ok := m.diffSession(); ok && m.isShell(sess.Tool) {
		ctx.IsShell = true
		ctx.SendError = shellPromptHint(sess.Name)
	}
	result := m.review.Key(msg, ctx)
	if result.Error != "" {
		m.reportErr(result.Error)
	}
	var navigation tea.Cmd
	switch result.Navigation {
	case uireview.NavigationExit:
		navigation = m.closeDiff()
	case uireview.NavigationHelp:
		m.openHelp()
	case uireview.NavigationRepoPicker:
		m.openRepoPick()
	case uireview.NavigationBranchPicker:
		navigation = m.openBranchPick()
	case uireview.NavigationBasePicker:
		navigation = m.openBasePick()
	}
	return m, tea.Batch(navigation, m.reviewCommands(result.Requests))
}

func (m *Model) applyReviewResult(result uireview.ApplyResult) tea.Cmd {
	if !result.Accepted {
		return nil
	}
	if result.Error != "" {
		m.reportErr(result.Error)
	}
	if result.ForgetPreferredRepo != "" && m.ledger.lifecycle.PickedRepos[m.review.SessionID()] == result.ForgetPreferredRepo {
		delete(m.ledger.lifecycle.PickedRepos, m.review.SessionID())
	}
	if result.Refresh {
		m.requestRefresh()
	}
	return m.reviewCommands(result.Requests)
}

func (m *Model) handleReviewLoad(result uireview.LoadResult) tea.Cmd {
	return m.applyReviewResult(m.review.ApplyLoad(result))
}

func (m *Model) handleReviewFile(result uireview.FileResult) tea.Cmd {
	return m.applyReviewResult(m.review.ApplyFile(result))
}

func (m *Model) handleReviewProbe(result uireview.ProbeResult) tea.Cmd {
	request, ok := m.review.ApplyProbe(result)
	if !ok {
		return nil
	}
	return m.reviewLoadCmd(request)
}

func (m *Model) cycleDiffScope() tea.Cmd {
	request, ok := m.review.CycleScope()
	if !ok {
		return nil
	}
	return tea.Batch(m.reviewLoadCmd(request), m.startStartupTick())
}

func (m *Model) diffRefreshCmd() tea.Cmd {
	if m.services.gitDrv == nil {
		return nil
	}
	request, ok := m.review.RefreshRequest()
	if !ok {
		return nil
	}
	return m.reviewProbeCmd(request)
}

func (m *Model) reviewStatusesCmd() tea.Cmd {
	if m.services.store == nil {
		return nil
	}
	request, ok := m.review.StatusRequest()
	if !ok {
		return nil
	}
	return m.reviewStatusCmd(request)
}

func (m *Model) loadCurrentDiffFile() tea.Cmd {
	return m.reviewCommands(m.review.EnsureCurrentFile())
}

func (m *Model) ensureHighlight() tea.Cmd {
	request, ok := m.review.EnsureHighlight()
	if !ok {
		return nil
	}
	return m.reviewHighlightCmd(request)
}

func (m *Model) handleReviewStatus(result uireview.StatusResult) tea.Cmd {
	return m.applyReviewResult(m.review.ApplyStatus(result))
}

func (m *Model) handleReviewSave(result uireview.SaveResult) tea.Cmd {
	return m.applyReviewResult(m.review.ApplySave(result))
}

func (m *Model) handleReviewComment(result uireview.HandleCommentResult) tea.Cmd {
	return m.applyReviewResult(m.review.ApplyHandle(result))
}

func (m *Model) handleReviewSend(result uireview.SendResult) tea.Cmd {
	return m.applyReviewResult(m.review.ApplySend(result))
}
