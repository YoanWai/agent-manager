package sessioncmd

import (
	"errors"
	"sync"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// Runtime is the already-open local workspace used by a long-lived owner.
// The caller retains ownership of borrowed dependencies.
type Runtime struct {
	Config   config.Config
	Store    *store.Store
	Driver   *tmux.Driver
	Hooks    *hooks.Manager
	Git      *git.Driver
	Snapshot func(string, string) error
}

// Backend binds command services to exactly one local runtime. OpenBackend
// owns a lazily opened store; BorrowBackend uses the caller's runtime and
// never closes it.
type Backend struct {
	mu        sync.Mutex
	configDir string
	runtime   Runtime
	opened    bool
	owned     bool
	closed    bool
}

func OpenBackend(configDir string) *Backend {
	return &Backend{configDir: configDir, owned: true}
}

func BorrowBackend(runtime Runtime) (*Backend, error) {
	if err := validateRuntime(runtime); err != nil {
		return nil, err
	}
	if runtime.Hooks == nil {
		return nil, errors.New("session command runtime requires a hook manager")
	}
	return &Backend{runtime: runtime, opened: true}, nil
}

func validateRuntime(runtime Runtime) error {
	switch {
	case runtime.Store == nil:
		return errors.New("session command runtime requires a store")
	case runtime.Driver == nil:
		return errors.New("session command runtime requires a tmux driver")
	case runtime.Snapshot == nil:
		return errors.New("session command runtime requires a snapshot writer")
	default:
		return nil
	}
}

func (b *Backend) resolve() (Runtime, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return Runtime{}, errors.New("session command backend is closed")
	}
	if !b.opened {
		runtime, err := openRuntime(b.configDir)
		if err != nil {
			return Runtime{}, err
		}
		b.runtime = runtime
		b.opened = true
	} else if b.owned {
		// Settings can change between commands of a long-lived owner, so the
		// session keys are reread from the store each time.
		keys, err := b.runtime.Store.SessionKeys()
		if err != nil {
			return Runtime{}, err
		}
		b.runtime.Driver.SetSessionKeys(keys)
	}
	return b.runtime, nil
}

func openRuntime(configDir string) (Runtime, error) {
	cfg, err := config.Default()
	if err != nil {
		return Runtime{}, err
	}
	driver, err := tmux.New()
	if err != nil {
		return Runtime{}, err
	}
	st, err := openStore(configDir)
	if err != nil {
		return Runtime{}, err
	}
	keys, err := st.SessionKeys()
	if err != nil {
		st.Close()
		return Runtime{}, err
	}
	driver.SetSessionKeys(keys)
	gitDriver, _ := git.New()
	return Runtime{
		Config:   cfg,
		Store:    st,
		Driver:   driver,
		Hooks:    hooks.NewManager(configDir),
		Git:      gitDriver,
		Snapshot: st.SetSnapshot,
	}, nil
}

// Close releases only a runtime opened by this backend. It is safe to call
// more than once and is a no-op for borrowed runtimes.
func (b *Backend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	if !b.owned || !b.opened || b.runtime.Store == nil {
		return nil
	}
	return b.runtime.Store.Close()
}

func (b *Backend) Lifecycle() (*Lifecycle, error) {
	runtime, err := b.resolve()
	if err != nil {
		return nil, err
	}
	return NewLifecycle(runtime)
}

// ProactiveCoordination reads the coordination mode from this backend's
// already-open store. Long-lived MCP owners must not open a second store to
// decide which instructions they serve.
func (b *Backend) ProactiveCoordination() (bool, error) {
	runtime, err := b.resolve()
	if err != nil {
		return false, err
	}
	return runtime.Store.ProactiveCoordination()
}

func (b *Backend) commands(words Vocabulary) (*runtime, error) {
	bound, err := b.resolve()
	if err != nil {
		return nil, err
	}
	return &runtime{
		cfg:    bound.Config,
		words:  words,
		store:  bound.Store,
		driver: bound.Driver,
	}, nil
}

func (b *Backend) gitDriver() (*git.Driver, error) {
	bound, err := b.resolve()
	if err != nil {
		return nil, err
	}
	if bound.Git != nil {
		return bound.Git, nil
	}
	return git.New()
}
