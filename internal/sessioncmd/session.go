package sessioncmd

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

// worktreeSetting is the store key holding the global spawn-in-worktree
// default the Agent Manager settings screen writes.
const worktreeSetting = "worktree_default"

type Session struct {
	ID        string `json:"id" jsonschema:"agent session id"`
	Name      string `json:"name" jsonschema:"session name shown in Agent Manager"`
	Tool      string `json:"tool" jsonschema:"agent CLI the session runs"`
	Group     string `json:"group" jsonschema:"group path holding the session; empty is the root"`
	Directory string `json:"directory" jsonschema:"session's current working directory, or its launch directory when stopped"`
	Status    string `json:"status" jsonschema:"Agent Manager status: starting, working, waiting, finished, idle or dead"`
	Running   bool   `json:"running" jsonschema:"whether the session currently has a live tmux pane"`
	Archived  bool   `json:"archived" jsonschema:"whether the session is archived out of the active list"`
	Branch    string `json:"branch,omitempty" jsonschema:"branch of the worktree Agent Manager created for this session, when it has one"`
	Self      bool   `json:"self" jsonschema:"whether this row is the calling session itself"`
}

type SessionScreen struct {
	Session Session `json:"session"`
	Output  string  `json:"output" jsonschema:"plain text currently visible in the session pane"`
}

type Group struct {
	Path      string `json:"path" jsonschema:"full group path, slash separated; pass this value as a group argument"`
	Directory string `json:"directory,omitempty" jsonschema:"group's default working directory, inherited by sessions created in it"`
	Worktree  string `json:"worktree,omitempty" jsonschema:"group's spawn-in-worktree choice: on, off, or empty to inherit"`
	Archived  bool   `json:"archived" jsonschema:"whether the group is archived"`
	Sessions  int    `json:"sessions" jsonschema:"number of sessions directly in this group"`
}

type CreateSessionOptions struct {
	Tool string
	Name string
	// Nil inherits the calling session's group; a pointer to an empty string
	// deliberately targets the root group.
	Group     *string
	Directory string
	Prompt    string
	// Nil inherits the group's spawn-in-worktree choice, then the global
	// setting.
	Worktree *bool
}

type Sessions struct {
	commands
	newGit func() (*git.Driver, error)
}

func NewSessions(configDir string) *Sessions {
	return newSessions(configDir, tmux.New, git.New)
}

func newSessions(configDir string, newDriver func() (*tmux.Driver, error), newGit func() (*git.Driver, error)) *Sessions {
	return &Sessions{
		commands: commands{configDir: configDir, newDriver: newDriver},
		newGit:   newGit,
	}
}

// agent resolves a target id to an agent session, refusing the ids that
// belong to terminals so an agent never types a sentence at a shell.
func (r *runtime) agent(id string) (store.Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return store.Session{}, errors.New("session_id is empty; call list_sessions to get one")
	}
	sess, err := r.store.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Session{}, fmt.Errorf("session %s does not exist; call list_sessions for current ids", id)
	}
	if err != nil {
		return store.Session{}, err
	}
	if r.cfg.Tools[sess.Tool].Shell {
		return store.Session{}, fmt.Errorf("session %s is a terminal, not an agent; use the terminal tools for it", id)
	}
	return sess, nil
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

func (s *Sessions) Groups(sessionID string) ([]Group, error) {
	runtime, err := s.open()
	if err != nil {
		return nil, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return nil, err
	}
	stored, err := runtime.store.Groups()
	if err != nil {
		return nil, err
	}
	sessions, err := runtime.store.ListSessions(true)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(stored))
	for _, sess := range sessions {
		counts[sess.Group]++
	}
	groups := make([]Group, 0, len(stored))
	for _, group := range stored {
		groups = append(groups, Group{
			Path:      group.Name,
			Directory: group.Path,
			Worktree:  group.Worktree,
			Archived:  group.Archived,
			Sessions:  counts[group.Name],
		})
	}
	return groups, nil
}

// CreateGroup adds a group under an existing parent, so sessions spawned
// later can be filed into it. Directory becomes the group's inherited
// default working directory.
func (s *Sessions) CreateGroup(sessionID, path, directory string) (Group, error) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return Group{}, errors.New("group path is empty")
	}
	runtime, err := s.open()
	if err != nil {
		return Group{}, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return Group{}, err
	}
	existing, err := runtime.store.Groups()
	if err != nil {
		return Group{}, err
	}
	for _, group := range existing {
		if group.Name == path {
			return Group{}, fmt.Errorf("group %q already exists", path)
		}
	}
	if parent := parentGroup(path); parent != "" {
		known := false
		for _, group := range existing {
			if group.Name == parent {
				known = true
				break
			}
		}
		if !known {
			return Group{}, fmt.Errorf("parent group %q does not exist; create it first", parent)
		}
	}
	resolved := ""
	if strings.TrimSpace(directory) != "" {
		resolved, err = resolveTerminalDirectory(directory)
		if err != nil {
			return Group{}, err
		}
	}
	if err := runtime.store.CreateGroup(path, resolved); err != nil {
		return Group{}, err
	}
	return Group{Path: path, Directory: resolved}, nil
}

func (s *Sessions) Create(sessionID string, opts CreateSessionOptions) (Session, error) {
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.store.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return Session{}, err
	}
	toolName := strings.TrimSpace(opts.Tool)
	if toolName == "" {
		toolName = caller.Tool
	}
	tool, known := runtime.cfg.Tools[toolName]
	if !known {
		return Session{}, fmt.Errorf("tool %q is not configured; configured tools are %s", toolName, strings.Join(agentToolNames(runtime), ", "))
	}
	if tool.Shell {
		return Session{}, fmt.Errorf("tool %q opens a shell, not an agent; use create_terminal for that", toolName)
	}
	group, dir, err := runtime.createTarget(caller, opts.Group, opts.Directory)
	if err != nil {
		return Session{}, err
	}
	prompt := strings.TrimSpace(opts.Prompt)
	if strings.HasPrefix(prompt, "-") {
		return Session{}, errors.New(`prompt cannot start with "-": the tool would read it as a flag`)
	}
	name := strings.TrimSpace(opts.Name)
	autoNamed := name == ""
	id := uuid.NewString()[:8]
	if autoNamed {
		name = toolName + "-" + id[:4]
	}

	wantWorktree, err := runtime.worktreeWanted(group, opts.Worktree)
	if err != nil {
		return Session{}, err
	}
	worktreeRepo, worktreeBranch := "", ""
	if wantWorktree {
		driver, err := s.newGit()
		if err != nil {
			return Session{}, fmt.Errorf("worktree sessions need git installed: %w", err)
		}
		root, err := driver.RepoRoot(dir)
		if err != nil {
			return Session{}, fmt.Errorf("%s cannot host a worktree: %w", dir, err)
		}
		path, branch, err := driver.AddWorktree(root, name)
		if err != nil {
			return Session{}, err
		}
		dir, worktreeRepo, worktreeBranch = path, root, branch
	}

	plan := launch.Assemble(tool, prompt, autoNamed)
	manager := hooks.NewManager(s.configDir)
	command, env, err := launch.Environment(manager, toolName, tool, plan.Command, id)
	if err != nil {
		return Session{}, err
	}
	sess := store.Session{
		ID:             id,
		Name:           name,
		Tool:           toolName,
		Cwd:            dir,
		Group:          group,
		Status:         status.Starting,
		AgentSessionID: plan.AgentSessionID,
		WorktreeRepo:   worktreeRepo,
		WorktreeBranch: worktreeBranch,
		PendingInputs:  plan.PendingInputs,
	}
	if err := runtime.driver.Create(sess.ID, sess.Cwd, command, env, 0, 0); err != nil {
		return Session{}, err
	}
	if err := runtime.store.CreateSession(sess); err != nil {
		_ = runtime.driver.Kill(sess.ID)
		return Session{}, err
	}
	_ = runtime.driver.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name))
	return runtime.sessionInfo(sess, true, false), nil
}

// worktreeWanted resolves whether a spawn opens its own worktree: an
// explicit choice wins, then the nearest ancestor group with one, then
// the global setting.
func (r *runtime) worktreeWanted(group string, explicit *bool) (bool, error) {
	if explicit != nil {
		return *explicit, nil
	}
	groups, err := r.store.Groups()
	if err != nil {
		return false, err
	}
	choice := make(map[string]string, len(groups))
	for _, candidate := range groups {
		choice[candidate.Name] = candidate.Worktree
	}
	for current := group; current != ""; current = parentGroup(current) {
		switch choice[current] {
		case "on":
			return true, nil
		case "off":
			return false, nil
		}
	}
	setting, err := r.store.Setting(worktreeSetting)
	if err != nil {
		return false, err
	}
	return setting == "on", nil
}

func agentToolNames(r *runtime) []string {
	names := make([]string, 0, len(r.cfg.Tools))
	for _, name := range r.cfg.ToolNames() {
		if !r.cfg.Tools[name].Shell {
			names = append(names, name)
		}
	}
	return names
}

// Send types a message into another agent's pane, the same way the quick
// prompt bar does, so the agent reads it as its next turn.
func (s *Sessions) Send(sessionID, targetID, message string) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("message is empty")
	}
	runtime, err := s.open()
	if err != nil {
		return err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return err
	}
	if !runtime.driver.Exists(target.ID) {
		return fmt.Errorf("session %s is not running; revive it with revive_session first", target.ID)
	}
	return runtime.driver.SendText(target.ID, message)
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
	if runtime.driver.Exists(target.ID) {
		return Session{}, fmt.Errorf("session %s is still running; revive only applies to dead sessions", target.ID)
	}
	tool, known := runtime.cfg.Tools[target.Tool]
	if !known {
		return Session{}, fmt.Errorf("tool %s is no longer configured", target.Tool)
	}
	if _, err := resolveTerminalDirectory(target.Cwd); err != nil {
		return Session{}, fmt.Errorf("working directory no longer exists: %s", target.Cwd)
	}
	base := launch.ReviveCommand(tool, target.AgentSessionID)
	command, env, err := launch.Environment(hooks.NewManager(s.configDir), target.Tool, tool, base, target.ID)
	if err != nil {
		return Session{}, err
	}
	if err := runtime.driver.Create(target.ID, target.Cwd, command, env, 0, 0); err != nil {
		return Session{}, err
	}
	_ = runtime.driver.SetLabel(target.ID, sessionLabel(target.Group, target.Name))
	if err := runtime.store.UpdateStatus(target.ID, tool.DefaultStatus); err != nil {
		return Session{}, err
	}
	if err := runtime.store.SetAcked(target.ID, false); err != nil {
		return Session{}, err
	}
	target.Status = tool.DefaultStatus
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
