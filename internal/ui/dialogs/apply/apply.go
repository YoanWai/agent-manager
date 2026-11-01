// Package apply is the runtime behind the fork, rename and move dialogs: the
// workers that branch a conversation, rename a group or a session, and the
// store and git writes they make. The cards in package dialogs paint the
// forms; the root keeps the handlers that apply an outcome to the loaded
// workspace, the Rail and the surface stack.
package apply

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
)

// Sources is what a worker reaches off the update path: the store it writes,
// the repository the worktree branch lives in, the loaded configuration, the
// panes it relabels, the launch runway a fork needs, and the check a row
// retyped into the browser must pass.
type Sources struct {
	Store        *store.Store
	Git          *git.Driver
	Config       config.Config
	Panes        Labels
	Lifecycle    Launcher
	Runner       KeyTyper
	BrowserReady func() error
}

// Preflight is the read-only filesystem probe the workers run: a rename
// resolves its default path through it and a fork refuses a source whose
// directory died. The root answers it with the spawn effect's probe.
type Preflight interface {
	Resolve(raw string, fallbacks []string) (string, bool)
	ResolveDir(path string) bool
}

type Labels interface {
	Exists(id string) bool
	SetLabel(id, label string) error
}

type Launcher interface {
	Launch(request sessioncmd.LaunchRequest) (sessioncmd.LaunchResult, error)
	ValidatePlacement(sess store.Session) error
}

type KeyTyper interface {
	TypeForkKeys(sess store.Session, keys string) error
}

func newID() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405")))
	}
	return hex.EncodeToString(buf)
}
