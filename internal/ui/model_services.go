package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

type services struct {
	saveKeys  func(keybind.Table) error
	lifecycle *sessioncmd.Lifecycle
	cfg       config.Config
	store     *store.Store
	tmux      *tmux.Driver
	hooks     *hooks.Manager
	gitDrv    *git.Driver
	engine    *status.Engine
	keys      keybind.Table
	listKeys  keybind.Table
	// editor is the command Settings picked for the editor key; empty
	// leaves the choice to the environment and what is on PATH.
	editor    string
	configDir string
	// setSnapshot writes a session's pane capture before archive or kill
	// takes the window; a seam so snapshot failures can be exercised
	// without a broken store.
	setSnapshot func(id, snapshot string) error
	// quick-send seams are captured with an accepted effect. Tests use them
	// to hold transport without blocking Bubble Tea's Update loop.
	quickSessionExists func(*tmux.Driver, string) (bool, error)
	quickSendText      func(*tmux.Driver, string, string) (tmux.SendResult, error)
}
