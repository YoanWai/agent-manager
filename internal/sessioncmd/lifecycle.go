package sessioncmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/google/uuid"
)

type PaneSize struct {
	Width  int
	Height int
}

type LaunchRequest struct {
	Session          store.Session
	Tool             config.Tool
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
	Sessions   []store.Session
	GroupPath  string
	Archived   bool
	LabelError error
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
	_, _ = l.runtime.Git.RemoveWorktreeIfClean(sess.WorktreeRepo, sess.Cwd, sess.WorktreeBranch)
}

// Launch creates the pane before its row and rolls the pane back if the row
// cannot be persisted. A label failure is returned separately because the UI
// reports it while agent commands historically treat it as cosmetic.
func (l *Lifecycle) Launch(request LaunchRequest) (LaunchResult, error) {
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
	command, env, err := launch.Environment(l.runtime.Hooks, sess.Tool, request.Tool, request.BaseCommand, sess.ID)
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
	create := l.runtime.Store.CreateSession
	if request.BesideSessionID != "" {
		create = func(row store.Session) error {
			return l.runtime.Store.CreateSessionBeside(row, request.BesideSessionID)
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
		LabelError: l.setLabel(sess.ID, sessionLabel(sess.Group, sess.Name)),
	}, nil
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

func (l *Lifecycle) Revive(sess store.Session, pane PaneSize) (RelaunchResult, error) {
	if err := l.requireHooks(); err != nil {
		return RelaunchResult{}, err
	}
	tool, ok := l.runtime.Config.Tools[sess.Tool]
	if !ok {
		return RelaunchResult{}, fmt.Errorf("tool %s is no longer configured", sess.Tool)
	}
	if _, err := resolveTerminalDirectory(sess.Cwd); err != nil {
		return RelaunchResult{}, fmt.Errorf("working directory no longer exists: %s", sess.Cwd)
	}
	if tool.Shell && l.runtime.Driver.Exists(sess.ID) {
		return RelaunchResult{}, fmt.Errorf("%s is a shell; its pane is already open", sess.Name)
	}
	degraded := sess.AgentSessionID == "" && tool.ResumeByIDCommand != "" && tool.ResumePickerCommand == ""
	if l.runtime.Driver.Exists(sess.ID) {
		launchedAt, err := RelaunchInPane(l.runtime.Driver, l.runtime.Store, l.runtime.Hooks, sess, tool)
		if err != nil {
			return RelaunchResult{}, err
		}
		if sess.AgentSessionID == "" && tool.ResumePickerKeys != "" {
			InjectPickerKeys(l.runtime.Driver, sess.ID, tool)
		}
		sess.Status = status.Starting
		sess.AgentLaunchedAt = launchedAt
		return RelaunchResult{Session: sess, LaunchedAt: launchedAt, ReusedPane: true, Degraded: degraded}, nil
	}
	if err := SnapshotRelaunch(l.runtime.Store, sess, tool, sess.AgentSessionID); err != nil {
		return RelaunchResult{}, err
	}
	return l.launchExisting(sess, tool, launch.ReviveCommand(tool, sess.AgentSessionID), pane, degraded, true, func(at time.Time) error {
		return l.runtime.Store.SetAgentLaunchedAt(sess.ID, at)
	})
}

func (l *Lifecycle) Restart(sess store.Session, pane PaneSize) (RelaunchResult, error) {
	if err := l.requireHooks(); err != nil {
		return RelaunchResult{}, err
	}
	tool, ok := l.runtime.Config.Tools[sess.Tool]
	if !ok {
		return RelaunchResult{}, fmt.Errorf("tool %s is no longer configured", sess.Tool)
	}
	if _, err := resolveTerminalDirectory(sess.Cwd); err != nil {
		return RelaunchResult{}, fmt.Errorf("working directory no longer exists: %s", sess.Cwd)
	}
	if l.runtime.Driver.Exists(sess.ID) {
		if _, err := l.Kill(sess); err != nil {
			return RelaunchResult{}, err
		}
	}
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

func (l *Lifecycle) launchExisting(sess store.Session, tool config.Tool, baseCommand string, pane PaneSize, degraded, injectPicker bool, bind func(time.Time) error) (RelaunchResult, error) {
	command, env, err := launch.Environment(l.runtime.Hooks, sess.Tool, tool, baseCommand, sess.ID)
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
	launchedAt := time.Now()
	if err := bind(launchedAt); err != nil {
		_ = l.runtime.Driver.Kill(sess.ID)
		return RelaunchResult{}, err
	}
	if err := l.runtime.Store.SetTmuxSocket(sess.ID, l.runtime.Driver.SocketPath()); err != nil {
		_ = l.runtime.Driver.Kill(sess.ID)
		return RelaunchResult{}, err
	}
	labelErr := l.setLabel(sess.ID, sessionLabel(sess.Group, sess.Name))
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
	if _, err := runtime.caller(callerID); err != nil {
		return Session{}, err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return Session{}, err
	}
	if target.ID == callerID && archived {
		return Session{}, errors.New("a session cannot archive itself")
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
		return ArchiveResult{Sessions: sessions, GroupPath: selection.GroupPath, Archived: archived}
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

func (l *Lifecycle) RestoreForHuman(selection ArchiveSelection, pane PaneSize) (ArchiveResult, error) {
	restored := make([]store.Session, 0, len(selection.Sessions))
	var labelErr error
	partial := func() ArchiveResult {
		return ArchiveResult{Sessions: restored, GroupPath: selection.GroupPath, LabelError: labelErr}
	}
	for _, sess := range selection.Sessions {
		if !l.runtime.Driver.Exists(sess.ID) {
			result, err := l.Revive(sess, pane)
			if err != nil {
				return partial(), err
			}
			sess = result.Session
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
		if err := l.runtime.Driver.Kill(sess.ID); err != nil {
			return result, err
		}
		for _, remove := range []func(string) error{
			l.runtime.Hooks.Remove,
			l.runtime.Hooks.RemoveName,
			l.runtime.Hooks.RemoveReviewRepo,
			l.runtime.Hooks.RemoveReviewBase,
			l.runtime.Hooks.RemoveReviewScope,
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
	removed, err := l.runtime.Git.RemoveWorktreeIfClean(sess.WorktreeRepo, sess.Cwd, sess.WorktreeBranch)
	if err != nil {
		result.Notice = "worktree cleanup: " + err.Error()
	} else if !removed {
		result.Notice = "worktree kept (has work): " + sess.Cwd
	}
}
