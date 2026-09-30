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
	Sessions  []store.Session
	GroupPath string
	Archived  bool
}

// Lifecycle owns the ordered store, tmux, hook, and worktree effects shared
// by the interactive manager and session-scoped commands. Its runtime is
// borrowed; closing resources remains the composing process's responsibility.
type Lifecycle struct {
	runtime Runtime
}

func NewLifecycle(runtime Runtime) (*Lifecycle, error) {
	if err := validateRuntime(runtime); err != nil {
		return nil, err
	}
	return &Lifecycle{runtime: runtime}, nil
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
		_ = l.runtime.Driver.Kill(sess.ID)
		_ = l.runtime.Hooks.Remove(sess.ID)
		discard()
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
		LabelError: l.runtime.Driver.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name)),
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
	degraded := sess.AgentSessionID == "" && tool.ResumeByIDCommand != "" && tool.ResumePickerCommand == ""
	if l.runtime.Driver.Exists(sess.ID) {
		launchedAt, err := RelaunchInPane(l.runtime.Driver, l.runtime.Store, l.runtime.Hooks, sess, tool)
		if err != nil {
			return RelaunchResult{}, err
		}
		if sess.AgentSessionID == "" && tool.ResumePickerKeys != "" {
			InjectPickerKeys(l.runtime.Driver, sess.ID, tool.InputPrefix, tool.ResumePickerKeys)
		}
		sess.Status = status.Starting
		sess.AgentLaunchedAt = launchedAt
		return RelaunchResult{Session: sess, LaunchedAt: launchedAt, ReusedPane: true, Degraded: degraded}, nil
	}
	if err := SnapshotRelaunch(l.runtime.Store, sess, tool, sess.AgentSessionID); err != nil {
		return RelaunchResult{}, err
	}
	return l.launchExisting(sess, tool, launch.ReviveCommand(tool, sess.AgentSessionID), pane, degraded, func(at time.Time) error {
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
	if _, err := l.Kill(sess); err != nil {
		return RelaunchResult{}, err
	}
	baseCommand, agentSessionID := tool.Command, ""
	if tool.SessionIDFlag != "" {
		agentSessionID = uuid.NewString()
		baseCommand += " " + tool.SessionIDFlag + " " + agentSessionID
	}
	if err := SnapshotRelaunch(l.runtime.Store, sess, tool, agentSessionID); err != nil {
		return RelaunchResult{}, err
	}
	result, err := l.launchExisting(sess, tool, baseCommand, pane, false, func(at time.Time) error {
		return l.runtime.Store.RestartAgent(sess.ID, agentSessionID, at)
	})
	result.Conversation = agentSessionID
	return result, err
}

func (l *Lifecycle) launchExisting(sess store.Session, tool config.Tool, baseCommand string, pane PaneSize, degraded bool, bind func(time.Time) error) (RelaunchResult, error) {
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
	labelErr := l.runtime.Driver.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name))
	if err := l.runtime.Store.UpdateStatus(sess.ID, status.Starting); err != nil {
		return RelaunchResult{}, err
	}
	if err := l.runtime.Store.SetAcked(sess.ID, false); err != nil {
		return RelaunchResult{}, err
	}
	if sess.AgentSessionID == "" && tool.ResumePickerKeys != "" {
		InjectPickerKeys(l.runtime.Driver, sess.ID, tool.InputPrefix, tool.ResumePickerKeys)
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
	for _, sess := range selection.Sessions {
		if !l.runtime.Driver.Exists(sess.ID) {
			continue
		}
		pane, err := l.runtime.Driver.CapturePane(sess.ID)
		if err != nil {
			return ArchiveResult{}, err
		}
		if pane != "" {
			if err := l.runtime.Snapshot(sess.ID, pane); err != nil {
				return ArchiveResult{}, err
			}
		}
	}
	archived := make([]store.Session, 0, len(selection.Sessions))
	for _, sess := range selection.Sessions {
		killed, err := l.Kill(sess)
		if err != nil {
			return ArchiveResult{}, err
		}
		killed.Archived = true
		archived = append(archived, killed)
	}
	if selection.GroupPath != "" {
		if err := l.runtime.Store.SetGroupArchived(selection.GroupPath, true); err != nil {
			return ArchiveResult{}, err
		}
	} else {
		for _, sess := range archived {
			if err := l.runtime.Store.SetArchived(sess.ID, true); err != nil {
				return ArchiveResult{}, err
			}
		}
	}
	return ArchiveResult{Sessions: archived, GroupPath: selection.GroupPath, Archived: true}, nil
}

func (l *Lifecycle) RestoreForHuman(selection ArchiveSelection, pane PaneSize) (ArchiveResult, error) {
	restored := make([]store.Session, 0, len(selection.Sessions))
	for _, sess := range selection.Sessions {
		if !l.runtime.Driver.Exists(sess.ID) {
			result, err := l.Revive(sess, pane)
			if err != nil {
				return ArchiveResult{}, err
			}
			sess = result.Session
		}
		sess.Archived = false
		restored = append(restored, sess)
		if selection.GroupPath == "" {
			if err := l.runtime.Store.SetArchived(sess.ID, false); err != nil {
				return ArchiveResult{}, err
			}
		}
	}
	if selection.GroupPath != "" {
		if err := l.runtime.Store.SetGroupArchived(selection.GroupPath, false); err != nil {
			return ArchiveResult{}, err
		}
	}
	return ArchiveResult{Sessions: restored, GroupPath: selection.GroupPath}, nil
}
