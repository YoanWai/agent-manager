package sessioncmd

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
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
