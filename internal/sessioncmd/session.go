package sessioncmd

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
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

type Group struct {
	Path      string `json:"path" jsonschema:"full group path, slash separated; pass this value as a group argument"`
	Directory string `json:"directory,omitempty" jsonschema:"group's default working directory, inherited by sessions created in it"`
	Worktree  string `json:"worktree,omitempty" jsonschema:"group's spawn-in-worktree choice: on, off, or empty to inherit"`
	Archived  bool   `json:"archived" jsonschema:"whether the group is archived"`
	Sessions  int    `json:"sessions" jsonschema:"number of active agent sessions directly in this group"`
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
	newGit       func() (*git.Driver, error)
	archiveOwner ArchiveOwner
}

func NewSessions(configDir string, words Vocabulary) *Sessions {
	return newSessions(configDir, words, tmux.New, git.New)
}

func NewSessionsWithBackend(backend *Backend, words Vocabulary) *Sessions {
	if backend == nil {
		panic("session command backend is required")
	}
	return &Sessions{
		commands: commands{words: words, backend: backend},
		newGit:   backend.gitDriver,
	}
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
	defer runtime.Close()
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
	defer runtime.Close()
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
		if runtime.cfg.Tools[sess.Tool].Shell || sess.Archived {
			continue
		}
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
	defer runtime.Close()
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

// GroupRemoval is what deleting a group did, since a fleet that filed work
// under one wants to know where its sessions went.
type GroupRemoval struct {
	Removed []string `json:"removed" jsonschema:"group paths that no longer exist, the named group and anything nested under it"`
	Moved   []string `json:"moved,omitempty" jsonschema:"ids of sessions that were filed under those groups and now sit in the root group"`
}

// DeleteGroup removes a group and the groups nested under it. Sessions
// filed there move to the root rather than going with it: an agent
// tidying up the group it opened for a finished fleet is not asking to
// destroy whatever is still running in it.
func (s *Sessions) DeleteGroup(sessionID, path string) (GroupRemoval, error) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return GroupRemoval{}, errors.New("group path is empty; the root group cannot be deleted")
	}
	runtime, err := s.open()
	if err != nil {
		return GroupRemoval{}, err
	}
	defer runtime.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return GroupRemoval{}, err
	}
	removed, moved, err := runtime.store.RemoveGroup(path)
	if errors.Is(err, store.ErrGroupNotFound) {
		return GroupRemoval{}, fmt.Errorf("group %q does not exist; call %s for current paths", path, runtime.words.ListGroups)
	}
	if err != nil {
		return GroupRemoval{}, err
	}
	return GroupRemoval{Removed: removed, Moved: moved}, nil
}

func (s *Sessions) Create(sessionID string, opts CreateSessionOptions) (Session, error) {
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return Session{}, err
	}
	toolName := strings.TrimSpace(opts.Tool)
	if toolName == "" {
		// A spawn with no tool named runs whatever the caller runs, which a
		// terminal cannot supply: its tool is the user's shell. Guessing an
		// agent for it would start a CLI nobody asked for.
		if runtime.cfg.Tools[caller.Tool].Shell {
			return Session{}, fmt.Errorf("a terminal runs a shell, not an agent CLI, so there is none to inherit; name one with %s (configured tools are %s)", runtime.words.SpawnTool, strings.Join(agentToolNames(runtime), ", "))
		}
		toolName = caller.Tool
	}
	tool, known := runtime.cfg.Tools[toolName]
	if !known {
		return Session{}, fmt.Errorf("tool %q is not configured; configured tools are %s", toolName, strings.Join(agentToolNames(runtime), ", "))
	}
	if tool.Shell {
		return Session{}, fmt.Errorf("tool %q opens a shell, not an agent; use %s for that", toolName, runtime.words.CreateTerminal)
	}
	group, dir, err := runtime.createTarget(caller, opts.Group, opts.Directory)
	if err != nil {
		return Session{}, err
	}
	prompt := strings.TrimSpace(opts.Prompt)
	if strings.HasPrefix(prompt, "-") && tool.PromptFlag == "" {
		return Session{}, fmt.Errorf(`prompt cannot start with "-" for %s, which takes its prompt as a bare argument and would read it as a flag`, toolName)
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
	dir, worktree, err := s.prepareWorktree(dir, name, wantWorktree, opts.Worktree != nil)
	if err != nil {
		return Session{}, err
	}
	// Every failure from here on has to hand back the worktree it just
	// made: AddWorktree refuses a path that already exists, so a leaked
	// one turns the caller's retry into a name collision it cannot explain.
	discard := func() {
		if worktree.repo == "" {
			return
		}
		if driver, err := s.newGit(); err == nil {
			_, _ = driver.RemoveWorktreeIfClean(worktree.repo, dir, worktree.branch)
		}
	}

	plan := launch.Assemble(toolName, tool, prompt, autoNamed)
	var lifecycleGit *git.Driver
	if worktree.repo != "" {
		lifecycleGit, err = s.newGit()
		if err != nil {
			discard()
			return Session{}, err
		}
	}
	lifecycle, err := s.lifecycle(runtime, lifecycleGit)
	if err != nil {
		discard()
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
		WorktreeRepo:   worktree.repo,
		WorktreeBranch: worktree.branch,
		PendingInputs:  plan.PendingInputs,
		LaunchPrompt:   plan.LaunchPrompt,
	}
	launched, err := lifecycle.Launch(LaunchRequest{
		Session:          sess,
		Tool:             tool,
		BaseCommand:      plan.Command,
		RollbackWorktree: worktree.repo != "",
	})
	if err != nil {
		return Session{}, err
	}
	return runtime.sessionInfo(launched.Session, true, false), nil
}

type worktreeTarget struct {
	repo   string
	branch string
}

// prepareWorktree opens the session's own checkout when one is wanted.
// A directory that cannot host one is only an error when the caller asked
// for a worktree by name; an inherited default degrades to a plain spawn,
// which is what the New Session form does rather than refusing to launch.
func (s *Sessions) prepareWorktree(dir, name string, wanted, explicit bool) (string, worktreeTarget, error) {
	if !wanted {
		return dir, worktreeTarget{}, nil
	}
	driver, err := s.newGit()
	if err != nil {
		if explicit {
			return "", worktreeTarget{}, fmt.Errorf("worktree sessions need git installed: %w", err)
		}
		return dir, worktreeTarget{}, nil
	}
	root, err := driver.RepoRoot(dir)
	if err != nil {
		if explicit {
			return "", worktreeTarget{}, fmt.Errorf("%s cannot host a worktree: %w; pass worktree false to spawn there anyway", dir, err)
		}
		return dir, worktreeTarget{}, nil
	}
	path, branch, err := driver.AddWorktree(root, name)
	if err != nil {
		return "", worktreeTarget{}, err
	}
	return path, worktreeTarget{repo: root, branch: branch}, nil
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

type SendResult struct {
	MessageID     int64 `json:"message_id" jsonschema:"pass to message_status to see whether it arrived"`
	QueuePosition int   `json:"queue_position" jsonschema:"this message's place in the recipient's queue; 1 means it is next"`
	ManagerAwake  bool  `json:"manager_awake" jsonschema:"whether Agent Manager is running to deliver it; a message queued while it is closed waits until it opens again"`
}

// maxMessageBytes bounds one message. An instruction to another agent is
// prose; the queue and rate caps count messages, and this is what keeps one
// of them from being a file paste that fills the recipient's prompt.
const maxMessageBytes = 8000

// Send queues a message for another agent. It is deliberately not typed
// into the pane here: several tools keep their input line drawn under an
// approval dialog, so a message sent the moment it is written would answer
// that dialog. The manager's poller types it in once the target is at rest.
func (s *Sessions) Send(sessionID, targetID, message string) (SendResult, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return SendResult{}, errors.New("message is empty")
	}
	if len(message) > maxMessageBytes {
		return SendResult{}, fmt.Errorf("message is %d bytes, over the %d byte limit; shorten it to the instruction and point the agent at a file or a task for the detail", len(message), maxMessageBytes)
	}
	runtime, err := s.open()
	if err != nil {
		return SendResult{}, err
	}
	defer runtime.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return SendResult{}, err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return SendResult{}, err
	}
	if target.ID == caller.ID {
		return SendResult{}, errors.New("a session cannot message itself")
	}
	if err := runtime.deliverable(target); err != nil {
		return SendResult{}, err
	}
	now := time.Now()
	id, err := runtime.store.Enqueue(store.InboxMessage{
		SessionID:   target.ID,
		SenderID:    caller.ID,
		SenderName:  caller.Name,
		Body:        message,
		Fingerprint: fingerprint(message),
		SentAt:      now,
	}, store.DefaultInboxLimits)
	if err != nil {
		return SendResult{}, err
	}
	// Answering is the acknowledgement: whatever this session was sent by
	// the agent it is now writing to has plainly been read.
	if err := runtime.store.MarkRead(caller.ID, target.ID, now); err != nil {
		return SendResult{}, err
	}
	queued, err := runtime.store.QueuedCount(target.ID)
	if err != nil {
		return SendResult{}, err
	}
	awake, err := runtime.managerAwake(now)
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{MessageID: id, QueuePosition: queued, ManagerAwake: awake}, nil
}

// MessageStatus reports what happened to a message this session sent.
func (s *Sessions) MessageStatus(sessionID string, messageID int64) (MessageState, error) {
	runtime, err := s.open()
	if err != nil {
		return MessageState{}, err
	}
	defer runtime.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return MessageState{}, err
	}
	msg, err := runtime.store.Message(messageID, caller.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageState{}, fmt.Errorf("message %d was not sent by this session, or has aged out of the log", messageID)
	}
	if err != nil {
		return MessageState{}, err
	}
	state := MessageState{
		MessageID: msg.ID,
		SessionID: msg.SessionID,
		Body:      msg.Body,
	}
	switch {
	// A drop stamps the delivery column as well, so that a message nothing
	// will ever type leaves the queue, and is read first for that reason.
	case !msg.DroppedAt.IsZero():
		state.State = "dropped"
		state.Reason = "Agent Manager could not type it into the pane, and never retries a message; send it again"
	case !msg.ReadAt.IsZero():
		state.State = "answered"
		state.DeliveredAt = msg.DeliveredAt.Format(time.RFC3339)
	case !msg.DeliveredAt.IsZero():
		state.State = "delivered"
		state.DeliveredAt = msg.DeliveredAt.Format(time.RFC3339)
	default:
		state.State = "queued"
		held, err := runtime.heldReason(msg.SessionID)
		if err != nil {
			return MessageState{}, err
		}
		if held != "" {
			state.State = "held"
			state.Reason = held
		}
	}
	return state, nil
}

// heldReason names why a queued message is not moving when the recipient is
// what stops it: a session the manager no longer visits, or a screen the
// delivery gate refuses. The gate is the poller's own, run here against the
// recipient's current pane, so a sender is never promised a hold the manager
// does not keep. A recipient merely mid-turn is not held: that clears itself.
func (r *runtime) heldReason(sessionID string) (string, error) {
	target, err := r.store.Get(sessionID)
	if err != nil {
		return "", err
	}
	if target.Archived {
		return fmt.Sprintf("session %s is archived, so Agent Manager no longer polls it and nothing will type this in; restore it with %s", sessionID, r.words.Restore), nil
	}
	if !r.driver.Exists(target.ID) {
		return fmt.Sprintf("session %s is not running, so nothing will type this in; revive it with %s", sessionID, r.words.Revive), nil
	}
	if r.cfg.Tools[target.Tool].ActivityCutoff == "" {
		return fmt.Sprintf("Agent Manager cannot tell when %s is ready, so nothing will type this in: %s", sessionID, unreadableTool(r.cfg, target.Tool)), nil
	}
	// The poller types into a resting session, and an errored one is not
	// resting: it is showing whatever stopped it, often a limit its agent
	// cannot clear on its own.
	if target.Status == status.Errored {
		return fmt.Sprintf("session %s is errored, so nothing will type this in until it recovers; read its screen with %s", sessionID, r.words.Read), nil
	}
	pane, err := r.driver.CapturePane(target.ID)
	if err != nil {
		return "", err
	}
	engine, err := status.NewEngine(r.cfg)
	if err != nil {
		return "", err
	}
	if engine.TypingHold(target.Tool, ansi.Strip(pane)) != status.Waiting {
		return "", nil
	}
	return fmt.Sprintf("session %s is sitting on a dialog, and nothing is typed into a session while one is on its screen; read that screen and answer it", sessionID), nil
}

type MessageState struct {
	MessageID   int64  `json:"message_id"`
	SessionID   string `json:"session_id" jsonschema:"session the message was addressed to"`
	Body        string `json:"body"`
	State       string `json:"state" jsonschema:"queued (waiting for the agent to be at rest), held (nothing will type it in as things stand: the recipient is sitting on a dialog, archived or not running, and reason says which), delivered (typed into its prompt), dropped (it never reached the prompt and is not retried), or answered (it has since messaged back)"`
	DeliveredAt string `json:"delivered_at,omitempty" jsonschema:"RFC3339 time the message reached the prompt"`
	Reason      string `json:"reason,omitempty" jsonschema:"why the message is in that state, and what to do about it"`
}

// managerAwake reports whether a manager has polled recently enough to
// still be delivering. Queued messages only move while it runs.
func (r *runtime) managerAwake(now time.Time) (bool, error) {
	return r.store.ManagerAwake(now, r.cfg.PollInterval.Duration)
}

// fingerprint collapses whitespace before hashing so a retry that only
// re-wraps its text is still recognised as the same message.
func fingerprint(message string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(message), " ")))
	return hex.EncodeToString(sum[:])
}

func (s *Sessions) Read(sessionID, targetID string) (SessionScreen, error) {
	runtime, err := s.open()
	if err != nil {
		return SessionScreen{}, err
	}
	defer runtime.Close()
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
	defer runtime.Close()
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
	lifecycle, err := s.lifecycle(runtime, nil)
	if err != nil {
		return Session{}, err
	}
	killed, err := lifecycle.Kill(target)
	if err != nil {
		return Session{}, err
	}
	return runtime.sessionInfo(killed, false, false), nil
}

// Revive relaunches a dead session under its old id, keeping its name,
// group and history, and resuming the conversation it held where the tool
// can.
func (s *Sessions) Revive(sessionID, targetID string) (Session, error) {
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return Session{}, err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return Session{}, err
	}
	lifecycle, err := s.lifecycle(runtime, nil)
	if err != nil {
		return Session{}, err
	}
	revived, err := lifecycle.Revive(target, PaneSize{})
	if err != nil {
		return Session{}, err
	}
	return runtime.sessionInfo(revived.Session, true, false), nil
}

// Archive parks a session out of the active list, or restores it. A live
// pane keeps running; archiving only changes where the row is filed, and
// the last screen is captured first so an archived row still shows one.
func (s *Sessions) Archive(sessionID, targetID string, archived bool) (Session, error) {
	request := ArchiveRequest{CallerID: sessionID, TargetID: targetID, Archived: archived, Words: s.words}
	if s.archiveOwner != nil {
		return s.archiveOwner.Archive(request)
	}
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.Close()
	lifecycle, err := s.lifecycle(runtime, nil)
	if err != nil {
		return Session{}, err
	}
	return lifecycle.SetArchivedForSession(request.CallerID, request.TargetID, request.Archived, request.Words)
}
