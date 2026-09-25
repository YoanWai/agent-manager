package sessioncmd

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/x/ansi"
)

type Session struct {
	ID        string `json:"id" jsonschema:"agent session id"`
	Name      string `json:"name" jsonschema:"session name shown in Agent Manager"`
	Tool      string `json:"tool" jsonschema:"agent CLI the session runs"`
	Group     string `json:"group" jsonschema:"group path holding the session; empty is the root"`
	Directory string `json:"directory" jsonschema:"session's current working directory, or its launch directory when stopped"`
	Status    string `json:"status" jsonschema:"Agent Manager status: starting, working, waiting, finished, idle, errored or dead"`
	Running   bool   `json:"running" jsonschema:"whether the session currently has a live tmux pane"`
	Archived  bool   `json:"archived" jsonschema:"whether the session is archived out of the active list"`
	Branch    string `json:"branch,omitempty" jsonschema:"branch of the worktree Agent Manager created for this session, when it has one"`
	Self      bool   `json:"self" jsonschema:"whether this row is the calling session itself"`
}

type SessionScreen struct {
	Session Session `json:"session"`
	Output  string  `json:"output" jsonschema:"plain text currently visible in the session pane"`
}

type Sessions struct {
	commands
	newGit func() (*git.Driver, error)
}

func NewSessions(configDir string, words Vocabulary) *Sessions {
	return newSessions(configDir, words, tmux.New, git.New)
}

func newSessions(configDir string, words Vocabulary, newDriver func() (*tmux.Driver, error), newGit func() (*git.Driver, error)) *Sessions {
	return &Sessions{
		commands: commands{configDir: configDir, words: words, newDriver: newDriver, loadConfig: config.LoadDir},
		newGit:   newGit,
	}
}

// agent resolves a target id to an agent session, refusing the ids that
// belong to terminals so an agent never types a sentence at a shell.
func (r *runtime) agent(id string) (store.Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return store.Session{}, fmt.Errorf("session_id is empty; call %s to get one", r.words.ListSessions)
	}
	sess, err := r.store.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Session{}, fmt.Errorf("session %s does not exist; call %s for current ids", id, r.words.ListSessions)
	}
	if err != nil {
		return store.Session{}, err
	}
	if r.cfg.Tools[sess.Tool].Shell {
		return store.Session{}, fmt.Errorf("session %s is a terminal, not an agent; use the terminal tools for it", id)
	}
	return sess, nil
}

// deliverable refuses a target the manager would never type into. An
// archived session keeps its pane, so it looks reachable, but the poller
// skips archived rows and a message queued for one would sit unread until
// the queue filled up.
func (r *runtime) deliverable(target store.Session) error {
	if !r.driver.Exists(target.ID) {
		return fmt.Errorf("session %s is not running; revive it with %s first", target.ID, r.words.Revive)
	}
	if target.Archived {
		return fmt.Errorf("session %s is archived, so Agent Manager no longer polls it; restore it with %s first", target.ID, r.words.Restore)
	}
	if r.cfg.Tools[target.Tool].ActivityCutoff == "" {
		return fmt.Errorf("Agent Manager cannot tell when %s is ready to read a message: %s", target.ID, unreadableTool(r.cfg, target.Tool))
	}
	return nil
}

// unreadableTool says why the poller cannot judge a tool's readiness; a
// session outliving the build that shipped its tool is the common case.
func unreadableTool(cfg config.Config, name string) string {
	if _, known := cfg.Tools[name]; !known {
		return fmt.Sprintf("%q is not a CLI it supports", name)
	}
	return fmt.Sprintf("%q marks no input box for it to read", name)
}

func (r *runtime) sessionInfo(sess store.Session, running, self bool) Session {
	dir := sess.Cwd
	if running {
		if current, err := r.driver.PaneCurrentPath(sess.ID); err == nil {
			dir = current
		}
	}
	return Session{
		ID:        sess.ID,
		Name:      sess.Name,
		Tool:      sess.Tool,
		Group:     sess.Group,
		Directory: dir,
		Status:    sess.Status,
		Running:   running,
		Archived:  sess.Archived,
		Branch:    sess.WorktreeBranch,
		Self:      self,
	}
}

func (s *Sessions) List(sessionID string) ([]Session, error) {
	runtime, err := s.open()
	if err != nil {
		return nil, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return nil, err
	}
	stored, err := runtime.store.ListSessions(true)
	if err != nil {
		return nil, err
	}
	panes, err := runtime.driver.Panes()
	if err != nil {
		return nil, err
	}
	sessions := make([]Session, 0)
	for _, sess := range stored {
		if runtime.cfg.Tools[sess.Tool].Shell {
			continue
		}
		_, running := panes[sess.ID]
		sessions = append(sessions, runtime.sessionInfo(sess, running, sess.ID == sessionID))
	}
	return sessions, nil
}

func (s *Sessions) Read(sessionID, targetID string) (SessionScreen, error) {
	runtime, err := s.open()
	if err != nil {
		return SessionScreen{}, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return SessionScreen{}, err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return SessionScreen{}, err
	}
	if !runtime.driver.Exists(target.ID) {
		snapshot, err := runtime.store.Snapshot(target.ID)
		if err != nil {
			return SessionScreen{}, err
		}
		if snapshot == "" {
			return SessionScreen{}, fmt.Errorf("session %s is not running and has no captured screen", target.ID)
		}
		return SessionScreen{
			Session: runtime.sessionInfo(target, false, target.ID == sessionID),
			Output:  strings.TrimRight(ansi.Strip(snapshot), "\r\n"),
		}, nil
	}
	output, err := runtime.driver.CapturePane(target.ID)
	if err != nil {
		return SessionScreen{}, err
	}
	return SessionScreen{
		Session: runtime.sessionInfo(target, true, target.ID == sessionID),
		Output:  strings.TrimRight(ansi.Strip(output), "\r\n"),
	}, nil
}

// Kill stops a session's pane and leaves its row dead, keeping the last
// screen so the manager can still show it and a revive can resume the
// conversation it held.
func (s *Sessions) Kill(sessionID, targetID string) (Session, error) {
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return Session{}, err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return Session{}, err
	}
	if target.ID == sessionID {
		return Session{}, errors.New("a session cannot kill itself")
	}
	if runtime.driver.Exists(target.ID) {
		if pane, err := runtime.driver.CapturePane(target.ID); err == nil && pane != "" {
			if err := runtime.store.SetSnapshot(target.ID, pane); err != nil {
				return Session{}, err
			}
		}
		if err := runtime.driver.Kill(target.ID); err != nil {
			return Session{}, err
		}
	}
	// The agent dies without running its session-end hook, so a leftover
	// status file would otherwise decide what a revived session reads as.
	if err := hooks.NewManager(s.configDir).Remove(target.ID); err != nil {
		return Session{}, err
	}
	if err := runtime.store.UpdateStatus(target.ID, status.Dead); err != nil {
		return Session{}, err
	}
	target.Status = status.Dead
	return runtime.sessionInfo(target, false, false), nil
}

// Revive relaunches a dead session under its old id, keeping its name,
// group and history, and resuming the conversation it held where the tool
// can.
func (s *Sessions) Revive(sessionID, targetID string) (Session, error) {
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return Session{}, err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return Session{}, err
	}
	tool, known := runtime.cfg.Tools[target.Tool]
	if !known {
		return Session{}, fmt.Errorf("tool %s is no longer configured", target.Tool)
	}
	if _, err := resolveTerminalDirectory(target.Cwd); err != nil {
		return Session{}, fmt.Errorf("working directory no longer exists: %s", target.Cwd)
	}
	// Reviving inside the surviving pane keeps the scrollback its last life
	// left there.
	if runtime.driver.Exists(target.ID) {
		if _, err := RelaunchInPane(runtime.driver, runtime.store, hooks.NewManager(s.configDir), target, tool); err != nil {
			return Session{}, err
		}
		if target.AgentSessionID == "" && tool.ResumePickerKeys != "" {
			InjectPickerKeys(runtime.driver, target.ID, tool.InputPrefix, tool.ResumePickerKeys)
		}
		target.Status = status.Starting
		return runtime.sessionInfo(target, true, false), nil
	}
	if err := SnapshotRelaunch(runtime.store, target, tool, target.AgentSessionID); err != nil {
		return Session{}, err
	}
	base := launch.ReviveCommand(tool, target.AgentSessionID)
	command, env, err := launch.Environment(hooks.NewManager(s.configDir), target.Tool, tool, base, target.ID)
	if err != nil {
		return Session{}, err
	}
	if err := runtime.createPane(target.ID, target.Cwd, command, env); err != nil {
		return Session{}, err
	}
	launchedAt := time.Now()
	if err := runtime.store.SetAgentLaunchedAt(target.ID, launchedAt); err != nil {
		_ = runtime.driver.Kill(target.ID)
		return Session{}, err
	}
	// The row now lives on this manager's server, wherever it ran before.
	// A row that cannot take the stamp is gone, and its fresh pane goes with
	// it rather than outliving the session it was opened for.
	if err := runtime.store.SetTmuxSocket(target.ID, runtime.driver.SocketPath()); err != nil {
		_ = runtime.driver.Kill(target.ID)
		return Session{}, err
	}
	_ = runtime.driver.SetLabel(target.ID, sessionLabel(target.Group, target.Name))
	if err := runtime.store.UpdateStatus(target.ID, status.Starting); err != nil {
		return Session{}, err
	}
	if err := runtime.store.SetAcked(target.ID, false); err != nil {
		return Session{}, err
	}
	if target.AgentSessionID == "" && tool.ResumePickerKeys != "" {
		InjectPickerKeys(runtime.driver, target.ID, tool.InputPrefix, tool.ResumePickerKeys)
	}
	target.Status = status.Starting
	target.AgentLaunchedAt = launchedAt
	return runtime.sessionInfo(target, true, false), nil
}

// Archive parks a session out of the active list, or restores it. A live
// pane keeps running; archiving only changes where the row is filed, and
// the last screen is captured first so an archived row still shows one.
func (s *Sessions) Archive(sessionID, targetID string, archived bool) (Session, error) {
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return Session{}, err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return Session{}, err
	}
	if target.ID == sessionID && archived {
		return Session{}, errors.New("a session cannot archive itself")
	}
	running := runtime.driver.Exists(target.ID)
	if archived && running {
		if pane, err := runtime.driver.CapturePane(target.ID); err == nil && pane != "" {
			if err := runtime.store.SetSnapshot(target.ID, pane); err != nil {
				return Session{}, err
			}
		}
	}
	if err := runtime.store.SetArchived(target.ID, archived); err != nil {
		return Session{}, err
	}
	target.Archived = archived
	return runtime.sessionInfo(target, running, false), nil
}
