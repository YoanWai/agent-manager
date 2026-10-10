package sessioncmd

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// managerAwake reports whether a manager has polled recently enough to
// still be delivering. Queued messages only move while it runs.
func (r *runtime) managerAwake(now time.Time) (bool, error) {
	return r.store.ManagerAwake(now)
}

// commands is the shared plumbing of every managed-pane command: the
// manager's config directory, the words the calling front speaks, and the
// tmux driver behind its socket.
type commands struct {
	configDir string
	words     Vocabulary
	backend   *Backend
	newDriver func() (*tmux.Driver, error)
	// loadConfig is config.Default outside the tests, which inject fake CLIs.
	loadConfig func() (config.Config, error)
}

type runtime struct {
	cfg    config.Config
	words  Vocabulary
	store  *store.Store
	driver *tmux.Driver
	close  func() error
}

func (r *runtime) Close() error {
	if r.close == nil {
		return nil
	}
	return r.close()
}

func (c *commands) open() (*runtime, error) {
	if c.backend != nil {
		return c.backend.commands(c.words)
	}
	cfg, err := c.loadConfig()
	if err != nil {
		return nil, err
	}
	driver, err := c.newDriver()
	if err != nil {
		return nil, err
	}
	st, err := openStore(c.configDir)
	if err != nil {
		return nil, err
	}
	keys, err := st.SessionKeys()
	if err != nil {
		st.Close()
		return nil, err
	}
	driver.SetSessionKeys(keys)
	return &runtime{cfg: cfg, words: c.words, store: st, driver: driver, close: st.Close}, nil
}

func (c *commands) lifecycle(opened *runtime, gitDriver *git.Driver) (*Lifecycle, error) {
	if c.backend != nil {
		lifecycle, err := c.backend.Lifecycle()
		if err != nil {
			return nil, err
		}
		if gitDriver != nil {
			lifecycle.runtime.Git = gitDriver
		}
		return lifecycle, nil
	}
	return NewLifecycle(Runtime{
		Config:   opened.cfg,
		Store:    opened.store,
		Driver:   opened.driver,
		Hooks:    hooks.NewManager(c.configDir),
		Git:      gitDriver,
		Snapshot: opened.store.SetSnapshot,
	})
}

// openStore makes the config directory first: a command can be the first
// thing to run here, before any manager made it.
func openStore(configDir string) (*store.Store, error) {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return nil, err
	}
	return store.Open(filepath.Join(configDir, "state.db"))
}

func (r *runtime) caller(sessionID string) (store.Session, error) {
	if err := validSession(sessionID); err != nil {
		return store.Session{}, err
	}
	sess, err := r.store.Get(sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Session{}, fmt.Errorf("calling session %s no longer exists", sessionID)
	}
	return sess, err
}

// optionalCaller serves commands that never act on the caller's own row, which
// a script outside Agent Manager may run with no session at all.
func (r *runtime) optionalCaller(sessionID string) (store.Session, error) {
	if sessionID == "" {
		return store.Session{}, nil
	}
	return r.caller(sessionID)
}
