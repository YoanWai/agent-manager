package sessioncmd

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// commands is the shared plumbing of every managed-pane command: the
// manager's config directory, the words the calling front speaks, and the
// tmux driver behind its socket.
type commands struct {
	configDir string
	words     Vocabulary
	newDriver func() (*tmux.Driver, error)
	// loadConfig is config.LoadDir outside the tests, which inject fake CLIs.
	loadConfig func(string) (config.Config, error)
}

type runtime struct {
	cfg    config.Config
	words  Vocabulary
	store  *store.Store
	driver *tmux.Driver
}

// createPane opens a session's pane at the box the running manager pins
// its panes to. Nothing here can measure the preview, and tmux hands an
// unsized detached session 80x24, which is narrower than any manager
// layout and holds until something resizes it.
func (r *runtime) createPane(id, cwd, command string, env map[string]string) error {
	width, height, err := r.store.PaneSize()
	if err != nil {
		return err
	}
	return r.driver.Create(id, cwd, command, env, width, height)
}

func (c *commands) open() (*runtime, error) {
	cfg, err := c.loadConfig(c.configDir)
	if err != nil {
		return nil, err
	}
	driver, err := c.newDriver()
	if err != nil {
		return nil, err
	}
	driver.SetSessionKeys(cfg.SessionKeys)
	st, err := store.Open(filepath.Join(c.configDir, "state.db"))
	if err != nil {
		return nil, err
	}
	return &runtime{cfg: cfg, words: c.words, store: st, driver: driver}, nil
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
