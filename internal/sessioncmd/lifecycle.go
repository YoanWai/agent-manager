package sessioncmd

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/termbrowser"
	"github.com/google/uuid"
)

type PaneSize struct {
	Width  int
	Height int
}

type LaunchRequest struct {
	Session          store.Session
	Tool             config.CLI
	BaseCommand      string
	Pane             PaneSize
	BesideSessionID  string
	RollbackWorktree bool
}

type LaunchResult struct {
	Session    store.Session
	LabelError error
}

type RelaunchResult struct {
	Session      store.Session
	LaunchedAt   time.Time
	ReusedPane   bool
	Degraded     bool
	LabelError   error
	Conversation string
}

type ArchiveSelection struct {
	Sessions  []store.Session
	GroupPath string
}

type ArchiveResult struct {
	GroupChanged bool
	Sessions     []store.Session
	GroupPath    string
	Archived     bool
	LabelError   error
}

type DeleteSelection struct {
	Sessions     []store.Session
	GroupPath    string
	ArchivedOnly bool
}

type DeleteResult struct {
	Deleted       []store.Session
	RemovedGroups []string
	Notice        string
}

// Lifecycle owns the ordered store, tmux, hook, and worktree effects shared
// by the interactive manager and session-scoped commands. Its runtime is
// borrowed; closing resources remains the composing process's responsibility.
type Lifecycle struct {
	runtime  Runtime
	setLabel func(string, string) error
	killPane func(string) error
	// browserEngine stands in for terminal-browser in tests. Nil runs the
	// real one on the runtime's tmux server.
	browserEngine browserEngine
}

func NewLifecycle(runtime Runtime) (*Lifecycle, error) {
	if err := validateRuntime(runtime); err != nil {
		return nil, err
	}
	return &Lifecycle{runtime: runtime, setLabel: runtime.Driver.SetLabel, killPane: runtime.Driver.Kill}, nil
}

func (l *Lifecycle) requireHooks() error {
	if l.runtime.Hooks == nil {
		return errors.New("lifecycle operation requires a hook manager")
	}
	return nil
}

func (l *Lifecycle) paneSize(requested PaneSize) (PaneSize, error) {
	if requested.Width > 0 && requested.Height > 0 {
		return requested, nil
	}
	width, height, err := l.runtime.Store.PaneSize()
	return PaneSize{Width: width, Height: height}, err
}

func (l *Lifecycle) discardWorktree(sess store.Session) {
	if sess.WorktreeRepo == "" || l.runtime.Git == nil {
		return
	}
	base, err := groupBase(l.runtime.Store, sess.Group)
	if err != nil {
		return
	}
	_, _ = l.runtime.Git.RemoveWorktreeIfClean(sess.WorktreeRepo, sess.Cwd, sess.WorktreeBranch, base)
}

// WorktreeRequest asks Spawn for the session's own checkout of the
// repository its Cwd is in.
type WorktreeRequest struct {
	Base  string
	Fetch bool
}

type SpawnRequest struct {
	Session  store.Session
	Tool     config.CLI
	Plan     launch.Plan
	Pane     PaneSize
	Worktree *WorktreeRequest
}

// Spawn is the one path a new session takes, from the manager and from the
// CLI: it opens the worktree the request asks for, fills the row from the
// launch plan, and launches it, handing the worktree back if that fails.
func (l *Lifecycle) Spawn(request SpawnRequest) (LaunchResult, error) {
	if err := l.ValidatePlacement(request.Session); err != nil {
		return LaunchResult{}, err
	}
	sess := request.Session
	if request.Worktree != nil {
		if l.runtime.Git == nil {
			return LaunchResult{}, errors.New("worktree sessions need git installed")
		}
		root, err := l.runtime.Git.RepoRoot(sess.Cwd)
		if err != nil {
			return LaunchResult{}, err
		}
		if request.Worktree.Fetch {
			// Offline or refused, the worktree branches from the last fetch instead.
			_ = l.runtime.Git.FetchBase(root, request.Worktree.Base)
		}
		path, branch, err := l.runtime.Git.AddWorktree(root, sess.Name, request.Worktree.Base)
		if err != nil {
			return LaunchResult{}, err
		}
		sess.Cwd, sess.WorktreeRepo, sess.WorktreeBranch = path, root, branch
	}
	// Starting until the agent first draws to its pane, so the row shows a
	// launch state immediately; the poller flips it.
	sess.Status = status.Starting
	sess.AgentSessionID = request.Plan.AgentSessionID
	sess.PendingInputs = request.Plan.PendingInputs
	sess.LaunchPrompt = request.Plan.LaunchPrompt
	return l.Launch(LaunchRequest{
		Session:          sess,
		Tool:             request.Tool,
		BaseCommand:      request.Plan.Command,
		Pane:             request.Pane,
		RollbackWorktree: sess.WorktreeRepo != "",
	})
}

// Launch creates the pane before its row and rolls the pane back if the row
// cannot be persisted. A label failure is returned separately because the UI
// reports it while agent commands historically treat it as cosmetic.
func (l *Lifecycle) Launch(request LaunchRequest) (LaunchResult, error) {
	if err := l.ValidatePlacement(request.Session); err != nil {
		if request.RollbackWorktree {
			l.discardWorktree(request.Session)
		}
		return LaunchResult{}, err
	}
	if err := l.requireHooks(); err != nil {
		return LaunchResult{}, err
	}
	sess := request.Session
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now()
	}
	if sess.LastStatusAt.IsZero() {
		sess.LastStatusAt = sess.CreatedAt
	}
	discard := func() {
		if request.RollbackWorktree {
			l.discardWorktree(sess)
		}
	}
	profile, err := l.runtime.Store.LaunchEnv(sess.Environment)
	if err != nil {
		discard()
		return LaunchResult{}, err
	}
	command, env, err := launch.Environment(l.runtime.Hooks, sess.Tool, request.Tool, request.BaseCommand, sess.ID, sess.Cwd, time.Time{}, profile)
	if err != nil {
		discard()
		return LaunchResult{}, err
	}
	size, err := l.paneSize(request.Pane)
	if err != nil {
		discard()
		return LaunchResult{}, err
	}
	if err := l.runtime.Driver.Create(sess.ID, sess.Cwd, command, env, size.Width, size.Height); err != nil {
		discard()
		return LaunchResult{}, err
	}
	sess.TmuxSocket = l.runtime.Driver.SocketPath()
	create := l.runtime.Store.CreateActiveSession
	if request.BesideSessionID != "" {
		create = func(row store.Session) error {
			return l.runtime.Store.CreateActiveSessionBeside(row, request.BesideSessionID)
		}
	}
	if err := create(sess); err != nil {
		killErr := l.killPane(sess.ID)
		_ = l.runtime.Hooks.Remove(sess.ID)
		discard()
		if killErr != nil {
			return LaunchResult{}, errors.Join(err, fmt.Errorf("pane %s is still running and has no row: %w", sess.ID, killErr))
		}
		return LaunchResult{}, err
	}
	if request.BesideSessionID != "" {
		stored, err := l.runtime.Store.Get(sess.ID)
		if err != nil {
			return LaunchResult{}, err
		}
		sess = stored
	}
	return LaunchResult{
		Session:    sess,
		LabelError: l.setLabel(sess.ID, SessionLabel(sess.Group, sess.Name)),
	}, nil
}

// releaseBrowser frees a live browser row's agent-browser session before its
// pane is killed. Other rows have none.
func (l *Lifecycle) releaseBrowser(sess store.Session) {
	if sess.Tool != config.BrowserTool || !l.runtime.Driver.Exists(sess.ID) {
		return
	}
	engine := l.browserEngine
	if engine == nil {
		engine = termbrowser.NewOnServer(l.runtime.Driver.SocketPath())
	}
	releaseBrowserAgent(engine, sess.ID)
}

// Kill leaves the row and conversation intact for revive. Pane capture is
// best effort, but a captured frame must be persisted before the pane dies.
func (l *Lifecycle) Kill(sess store.Session) (store.Session, error) {
	if err := l.requireHooks(); err != nil {
		return store.Session{}, err
	}
	if l.runtime.Driver.Exists(sess.ID) {
		if pane, err := l.runtime.Driver.CapturePane(sess.ID); err == nil && pane != "" {
			if err := l.runtime.Snapshot(sess.ID, pane); err != nil {
				return store.Session{}, err
			}
		}
		l.releaseBrowser(sess)
		if err := l.runtime.Driver.Kill(sess.ID); err != nil {
			return store.Session{}, err
		}
	}
	if err := l.runtime.Hooks.Remove(sess.ID); err != nil {
		return store.Session{}, err
	}
	if err := l.runtime.Store.UpdateStatus(sess.ID, status.Dead); err != nil {
		return store.Session{}, err
	}
	sess.Status = status.Dead
	return sess, nil
}

// Revive relaunches a dead session or an agent that quit to its shell. The
// row is read again first, since the listed copy can predate the last
// conversation the agent reported, and the agent's last conversation is
// settled before the revive command is built.
func (l *Lifecycle) Revive(listed store.Session, pane PaneSize) (RelaunchResult, error) {
	if err := l.requireHooks(); err != nil {
		return RelaunchResult{}, err
	}
	sess, err := l.runtime.Store.RequireSession(listed.ID)
	if err != nil {
		return RelaunchResult{}, err
	}
	if sess.Archived {
		return RelaunchResult{}, fmt.Errorf("session %s is archived; restore it before reviving", sess.Name)
	}
	return l.revive(sess, pane)
}

func (l *Lifecycle) revive(sess store.Session, pane PaneSize) (RelaunchResult, error) {
	var err error
	tool, ok := l.runtime.Config.Pane(sess.Tool)
	if !ok {
		return RelaunchResult{}, fmt.Errorf("%s is no longer configured", sess.Tool)
	}
	if _, err := resolveTerminalDirectory(sess.Cwd); err != nil {
		return RelaunchResult{}, fmt.Errorf("working directory no longer exists: %s", sess.Cwd)
	}
	if sess.Tool == config.TerminalTool && l.runtime.Driver.Exists(sess.ID) {
		return RelaunchResult{}, fmt.Errorf("%s is a shell; its pane is already open", sess.Name)
	}
	degraded := func(sess store.Session) bool {
		return sess.AgentSessionID == "" && tool.ResumeByIDCommand != "" && tool.ResumePickerCommand == ""
	}
	if l.runtime.Driver.Exists(sess.ID) {
		relaunched, err := RelaunchInPane(l.runtime.Driver, l.runtime.Store, l.runtime.Hooks, sess.ID, tool)
		if err != nil {
			return RelaunchResult{}, err
		}
		if relaunched.AgentSessionID == "" && tool.ResumePickerKeys != "" {
			InjectPickerKeys(l.runtime.Driver, relaunched.ID, tool)
		}
		relaunched.Status = status.Starting
		return RelaunchResult{Session: relaunched, LaunchedAt: relaunched.AgentLaunchedAt, ReusedPane: true, Degraded: degraded(relaunched)}, nil
	}
	if sess, err = settleBeforeRevive(l.runtime.Store, l.runtime.Hooks, sess, tool.SessionReport, true); err != nil {
		return RelaunchResult{}, err
	}
	tool = tool.WithChoice(sess.Choice)
	if err := SnapshotRelaunch(l.runtime.Store, sess, tool, sess.AgentSessionID); err != nil {
		return RelaunchResult{}, err
	}
	return l.launchExisting(sess, tool, relaunchCommand(tool, sess), pane, degraded(sess), true, func(at time.Time) error {
		return l.runtime.Store.SetAgentLaunchedAt(sess.ID, at)
	})
}

func (l *Lifecycle) Restart(listed store.Session, pane PaneSize) (RelaunchResult, error) {
	if err := l.requireHooks(); err != nil {
		return RelaunchResult{}, err
	}
	sess, err := l.runtime.Store.RequireSession(listed.ID)
	if err != nil {
		return RelaunchResult{}, err
	}
	if sess.Archived {
		return RelaunchResult{}, fmt.Errorf("session %s is archived; restore it before restarting", sess.Name)
	}
	tool, ok := l.runtime.Config.Pane(sess.Tool)
	if !ok {
		return RelaunchResult{}, fmt.Errorf("%s is no longer configured", sess.Tool)
	}
	if _, err := resolveTerminalDirectory(sess.Cwd); err != nil {
		return RelaunchResult{}, fmt.Errorf("working directory no longer exists: %s", sess.Cwd)
	}
	if sess.Tool == config.BrowserTool {
		return l.restartBrowser(sess, pane)
	}
	if l.runtime.Driver.Exists(sess.ID) {
		if _, err := l.Kill(sess); err != nil {
			return RelaunchResult{}, err
		}
	}
	tool = tool.WithChoice(sess.Choice)
	baseCommand, agentSessionID := tool.Command, ""
	if tool.SessionIDFlag != "" {
		agentSessionID = uuid.NewString()
		baseCommand += " " + tool.SessionIDFlag + " " + agentSessionID
	}
	if err := SnapshotRelaunch(l.runtime.Store, sess, tool, agentSessionID); err != nil {
		return RelaunchResult{}, err
	}
	result, err := l.launchExisting(sess, tool, baseCommand, pane, false, false, func(at time.Time) error {
		return l.runtime.Store.RestartAgent(sess.ID, agentSessionID, at)
	})
	if err == nil {
		if result.Session.AgentSessionID != "" {
			result.Session.RetiredAgentSessionID = result.Session.AgentSessionID
		}
		result.Session.AgentSessionID = agentSessionID
	}
	result.Conversation = agentSessionID
	return result, err
}

// restartBrowser ends a browser that is still running and revives it, so
// the page it last showed opens again. A browser holds no conversation to
// retire.
func (l *Lifecycle) restartBrowser(sess store.Session, pane PaneSize) (RelaunchResult, error) {
	if l.runtime.Driver.Exists(sess.ID) {
		running, err := AgentRunning(l.runtime.Driver, sess.ID)
		if err != nil {
			return RelaunchResult{}, err
		}
		if running {
			if _, err := l.Kill(sess); err != nil {
				return RelaunchResult{}, err
			}
		}
	}
	return l.revive(sess, pane)
}

func (l *Lifecycle) launchExisting(sess store.Session, tool config.CLI, baseCommand string, pane PaneSize, degraded, injectPicker bool, bind func(time.Time) error) (result RelaunchResult, launchErr error) {
	launchedAt := time.Now()
	profile, err := l.runtime.Store.LaunchEnv(sess.Environment)
	if err != nil {
		return RelaunchResult{}, err
	}
	command, env, err := launch.Environment(l.runtime.Hooks, sess.Tool, tool, baseCommand, sess.ID, sess.Cwd, launchedAt, profile)
	if err != nil {
		return RelaunchResult{}, err
	}
	size, err := l.paneSize(pane)
	if err != nil {
		return RelaunchResult{}, err
	}
	if err := l.runtime.Driver.Create(sess.ID, sess.Cwd, command, env, size.Width, size.Height); err != nil {
		return RelaunchResult{}, err
	}
	defer func() {
		if launchErr != nil {
			_, cleanupErr := l.stopFailedLaunch(sess)
			if cleanupErr != nil {
				launchErr = errors.Join(launchErr, fmt.Errorf("stop failed launch of %s: %w", sess.Name, cleanupErr))
			}
		}
	}()
	if err := bind(launchedAt); err != nil {
		return RelaunchResult{}, err
	}
	if err := l.runtime.Store.SetTmuxSocket(sess.ID, l.runtime.Driver.SocketPath()); err != nil {
		return RelaunchResult{}, err
	}
	labelErr := l.setLabel(sess.ID, SessionLabel(sess.Group, sess.Name))
	if err := l.runtime.Store.UpdateStatus(sess.ID, status.Starting); err != nil {
		return RelaunchResult{}, err
	}
	if err := l.runtime.Store.SetAcked(sess.ID, false); err != nil {
		return RelaunchResult{}, err
	}
	if injectPicker && sess.AgentSessionID == "" && tool.ResumePickerKeys != "" {
		InjectPickerKeys(l.runtime.Driver, sess.ID, tool)
	}
	sess.Status = status.Starting
	sess.AgentLaunchedAt = launchedAt
	return RelaunchResult{Session: sess, LaunchedAt: launchedAt, Degraded: degraded, LabelError: labelErr}, nil
}

// SetArchivedForSession implements agent-command archive policy: one target,
// no self-archive, and no pane termination.
func (l *Lifecycle) SetArchivedForSession(callerID, targetID string, archived bool, words Vocabulary) (Session, error) {
	runtime := runtime{cfg: l.runtime.Config, words: words, store: l.runtime.Store, driver: l.runtime.Driver}
	if _, err := runtime.optionalCaller(callerID); err != nil {
		return Session{}, err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return Session{}, err
	}
	if target.ID == callerID && archived {
		return Session{}, fmt.Errorf("a session archives itself with %s, which waits for this turn to end", words.ArchiveSelf)
	}
	running := l.runtime.Driver.Exists(target.ID)
	if archived && running {
		if pane, err := l.runtime.Driver.CapturePane(target.ID); err == nil && pane != "" {
			if err := l.runtime.Snapshot(target.ID, pane); err != nil {
				return Session{}, err
			}
		}
	}
	if err := l.runtime.Store.SetArchived(target.ID, archived); err != nil {
		return Session{}, err
	}
	target.Archived = archived
	return runtime.sessionInfo(target, running, false), nil
}

// ArchiveForHuman preserves the interactive action: capture every live pane
// before ending any of them, then archive the selected rows or group.
func (l *Lifecycle) ArchiveForHuman(selection ArchiveSelection) (ArchiveResult, error) {
	partial := func(sessions []store.Session, archived bool) ArchiveResult {
		return ArchiveResult{Sessions: sessions, GroupPath: selection.GroupPath, Archived: archived, GroupChanged: archived && selection.GroupPath != ""}
	}
	for _, sess := range selection.Sessions {
		if !l.runtime.Driver.Exists(sess.ID) {
			continue
		}
		pane, err := l.runtime.Driver.CapturePane(sess.ID)
		if err != nil {
			return partial(nil, false), err
		}
		if pane != "" {
			if err := l.runtime.Snapshot(sess.ID, pane); err != nil {
				return partial(nil, false), err
			}
		}
	}
	archived := make([]store.Session, 0, len(selection.Sessions))
	for _, sess := range selection.Sessions {
		killed := sess
		if l.runtime.Driver.Exists(sess.ID) {
			var err error
			killed, err = l.Kill(sess)
			if err != nil {
				return partial(archived, false), err
			}
		}
		archived = append(archived, killed)
	}
	if selection.GroupPath != "" {
		if err := l.runtime.Store.SetGroupArchived(selection.GroupPath, true); err != nil {
			return partial(archived, false), err
		}
		for i := range archived {
			archived[i].Archived = true
		}
	} else {
		for i, sess := range archived {
			if err := l.runtime.Store.SetArchived(sess.ID, true); err != nil {
				return partial(archived, false), err
			}
			archived[i].Archived = true
		}
	}
	return partial(archived, true), nil
}

func (l *Lifecycle) RestoreForHuman(selection ArchiveSelection, pane PaneSize) (result ArchiveResult, operationErr error) {
	restored := make([]store.Session, 0, len(selection.Sessions))
	started := map[string]bool{}
	groupChanged := false
	var labelErr error
	partial := func() ArchiveResult {
		return ArchiveResult{Sessions: restored, GroupPath: selection.GroupPath, LabelError: labelErr, GroupChanged: groupChanged}
	}
	// A failed archive write must not leave newly started panes running
	// outside the active inventory.
	defer func() {
		if operationErr == nil {
			return
		}
		for i, sess := range restored {
			if !started[sess.ID] || !sess.Archived {
				continue
			}
			killed, err := l.stopFailedLaunch(sess)
			restored[i] = killed
			if err != nil {
				operationErr = errors.Join(operationErr, fmt.Errorf("stop failed restore of %s: %w", sess.Name, err))
			}
		}
		result = partial()
	}()
	for _, sess := range selection.Sessions {
		if !l.runtime.Driver.Exists(sess.ID) {
			if err := l.requireHooks(); err != nil {
				return partial(), err
			}
			current, err := l.runtime.Store.RequireSession(sess.ID)
			if err != nil {
				return partial(), err
			}
			result, err := l.revive(current, pane)
			if err != nil {
				return partial(), err
			}
			sess = result.Session
			started[sess.ID] = true
			labelErr = errors.Join(labelErr, result.LabelError)
		}
		restored = append(restored, sess)
		if selection.GroupPath == "" {
			if err := l.runtime.Store.SetArchived(sess.ID, false); err != nil {
				return partial(), err
			}
			restored[len(restored)-1].Archived = false
		}
	}
	if selection.GroupPath != "" {
		if err := l.runtime.Store.SetGroupArchived(selection.GroupPath, false); err != nil {
			return partial(), err
		}
		groupChanged = true
		for i := range restored {
			restored[i].Archived = false
		}
	}
	return partial(), nil
}

// DeleteForHuman owns the destructive effects after the UI has confirmed
// them. Child terminals go first so a partial failure never leaves a row
// pointing at a parent that is already gone.
func (l *Lifecycle) DeleteForHuman(selection DeleteSelection) (DeleteResult, error) {
	if err := l.requireHooks(); err != nil {
		return DeleteResult{}, err
	}
	result := DeleteResult{Deleted: make([]store.Session, 0, len(selection.Sessions))}
	for _, sess := range deleteChildrenFirst(selection.Sessions) {
		l.releaseBrowser(sess)
		if err := l.runtime.Driver.Kill(sess.ID); err != nil {
			return result, err
		}
		for _, remove := range []func(string) error{
			l.runtime.Hooks.Remove,
			l.runtime.Hooks.RemoveName,
			l.runtime.Hooks.RemoveReviewRepo,
			l.runtime.Hooks.RemoveReviewBase,
			l.runtime.Hooks.RemoveReviewScope,
			l.runtime.Hooks.RemoveSettings,
			l.runtime.Hooks.RemoveAgentFiles,
		} {
			if err := remove(sess.ID); err != nil {
				return result, err
			}
		}
		if err := l.runtime.Store.Delete(sess.ID); err != nil {
			return result, err
		}
		result.Deleted = append(result.Deleted, sess)
		l.cleanupDeletedWorktree(sess, &result)
	}
	if selection.GroupPath == "" {
		return result, nil
	}
	var err error
	if selection.ArchivedOnly {
		result.RemovedGroups, err = l.runtime.Store.PruneArchivedGroups(selection.GroupPath)
	} else {
		result.RemovedGroups, err = l.runtime.Store.DeleteGroup(selection.GroupPath)
	}
	return result, err
}

func deleteChildrenFirst(sessions []store.Session) []store.Session {
	ordered := make([]store.Session, 0, len(sessions))
	for _, sess := range sessions {
		if sess.ParentID != "" {
			ordered = append(ordered, sess)
		}
	}
	for _, sess := range sessions {
		if sess.ParentID == "" {
			ordered = append(ordered, sess)
		}
	}
	return ordered
}

func (l *Lifecycle) cleanupDeletedWorktree(sess store.Session, result *DeleteResult) {
	if sess.WorktreeRepo == "" || l.runtime.Git == nil {
		return
	}
	sessions, err := l.runtime.Store.ListSessions(true)
	if err != nil {
		result.Notice = "worktree cleanup: " + err.Error()
		return
	}
	for _, other := range sessions {
		if other.Cwd == sess.Cwd {
			result.Notice = "worktree kept (used by another session): " + sess.Cwd
			return
		}
	}
	base, err := groupBase(l.runtime.Store, sess.Group)
	if err != nil {
		result.Notice = "worktree cleanup: " + err.Error()
		return
	}
	removed, err := l.runtime.Git.RemoveWorktreeIfClean(sess.WorktreeRepo, sess.Cwd, sess.WorktreeBranch, base)
	if err != nil {
		result.Notice = "worktree cleanup: " + err.Error()
	} else if !removed {
		result.Notice = "worktree kept (has work): " + sess.Cwd
	}
}

// EndAfterTurnResult names what EndAfterTurn did, so the manager can mirror
// it in memory without reading the rows back.
type EndAfterTurnResult struct {
	Action   string
	Sessions []store.Session
}

// EndAfterTurn archives or kills a session whose turn ended after it asked
// for that, taking the same steps the archive and kill keys take. A request
// the row no longer carries, or a session that started a new turn since the
// pass that reported it, is left alone and returns an empty Action.
func (l *Lifecycle) EndAfterTurn(id string) (EndAfterTurnResult, error) {
	if err := l.requireHooks(); err != nil {
		return EndAfterTurnResult{}, err
	}
	sess, err := l.runtime.Store.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		return EndAfterTurnResult{}, nil
	}
	if err != nil {
		return EndAfterTurnResult{}, err
	}
	if sess.AfterTurn == "" || !slices.Contains(status.Resting, sess.Status) {
		return EndAfterTurnResult{}, nil
	}
	children, err := l.runtime.Store.Children(sess.ID)
	if err != nil {
		return EndAfterTurnResult{}, err
	}
	sessions := append([]store.Session{sess}, children...)
	result := EndAfterTurnResult{Action: sess.AfterTurn}
	if sess.AfterTurn == store.AfterTurnArchive {
		archived, err := l.ArchiveForHuman(ArchiveSelection{Sessions: sessions})
		if err != nil {
			return EndAfterTurnResult{}, err
		}
		result.Sessions = archived.Sessions
	} else {
		for _, each := range sessions {
			killed, err := l.Kill(each)
			if err != nil {
				return EndAfterTurnResult{}, err
			}
			result.Sessions = append(result.Sessions, killed)
		}
	}
	if err := l.runtime.Store.ClearAfterTurn(id); err != nil {
		return EndAfterTurnResult{}, err
	}
	for i := range result.Sessions {
		if result.Sessions[i].ID == id {
			result.Sessions[i].AfterTurn, result.Sessions[i].AfterTurnAt = "", time.Time{}
		}
	}
	return result, nil
}

// Capture borrows the same bound resources and operation policies with an
// independently supplied configuration and snapshot writer for one dispatch.
// The caller owns copying configuration values it may later mutate.
func (l *Lifecycle) Capture(cfg config.Config, snapshot func(string, string) error) *Lifecycle {
	captured := *l
	captured.runtime.Config = cfg
	captured.runtime.Snapshot = snapshot
	return &captured
}

// Failed launches must not overwrite the screen saved when the row was archived.
func (l *Lifecycle) stopFailedLaunch(sess store.Session) (store.Session, error) {
	if l.runtime.Driver.Exists(sess.ID) {
		if err := l.killPane(sess.ID); err != nil {
			return sess, err
		}
	}
	hookErr := l.runtime.Hooks.Remove(sess.ID)
	statusErr := l.runtime.Store.UpdateStatus(sess.ID, status.Dead)
	if statusErr == nil {
		sess.Status = status.Dead
	}
	return sess, errors.Join(hookErr, statusErr)
}
