package app

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/notify"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

type Local struct {
	ProfileDir string
	Runtime    sessioncmd.Runtime
	Engine     *status.Engine
	Backend    *sessioncmd.Backend
	Lifecycle  *sessioncmd.Lifecycle
	Execution  *execution.Runner
}

func OpenLocal(profileDir string, driver *tmux.Driver) (*Local, error) {
	if driver == nil {
		return nil, errors.New("local application requires an explicit tmux driver")
	}
	cfg, err := config.Default()
	if err != nil {
		return nil, err
	}
	engine, err := status.NewEngine(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return nil, err
	}
	st, err := store.Open(filepath.Join(profileDir, "state.db"))
	if err != nil {
		return nil, err
	}
	keys, err := st.SessionKeys()
	if err != nil {
		st.Close()
		return nil, err
	}
	driver.SetSessionKeys(keys)
	optionalGitDriver, _ := git.New()
	runtime := sessioncmd.Runtime{
		Config: cfg, Store: st, Driver: driver, Hooks: hooks.NewManager(profileDir),
		Git: optionalGitDriver, Snapshot: st.SetSnapshot,
	}
	backend, err := sessioncmd.BorrowBackend(runtime)
	if err != nil {
		st.Close()
		return nil, err
	}
	lifecycle, err := backend.Lifecycle()
	if err != nil {
		st.Close()
		return nil, err
	}
	runner := execution.New(execution.Dependencies{
		Store: st, TMux: driver, Engine: engine, Hooks: runtime.Hooks, Git: optionalGitDriver,
		Inbox: sessioncmd.NewInboxOwner(st), Notify: notify.Notify,
		TakeFocus: func() (string, bool) { return notify.TakeFocus(profileDir) },
	}, execution.OptionsFromConfig(cfg))
	return &Local{ProfileDir: profileDir, Runtime: runtime, Engine: engine, Backend: backend, Lifecycle: lifecycle, Execution: runner}, nil
}

func (l *Local) Close() error {
	return l.Runtime.Store.Close()
}
