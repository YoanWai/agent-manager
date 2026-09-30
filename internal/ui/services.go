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
	lifecycle *sessioncmd.Lifecycle
	cfg       config.Config
	store     *store.Store
	tmux      *tmux.Driver
	hooks     *hooks.Manager
	gitDrv    *git.Driver
	engine    *status.Engine
	keys      keybind.Table
	listKeys  keybind.Table
	// configDir is resolved once, at New, so the settings screen writes
	// keys back to the config.toml the manager loaded.
	configDir string
	// setSnapshot writes a session's pane capture before archive or kill
	// takes the window; a seam so snapshot failures can be exercised
	// without a broken store.
	setSnapshot func(id, snapshot string) error
}
