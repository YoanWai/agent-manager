package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/notify"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	uihelp "github.com/YoanWai/agent-manager/internal/ui/help"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	"github.com/YoanWai/agent-manager/internal/update"
	tea "github.com/charmbracelet/bubbletea"
)

type mode int

const (
	modeList mode = iota
	modeForm
	modeConfirmDelete
	modeHelp
	modeRename
	modeFork
	modeMove
	modeRepoPick
	modeGroupForm
	modeSettings
	modeDiff
	modeNotices
	// modeLaunchHint holds a refused spawn's fix in a dialog: the launch
	// stays blocked, and the command that unblocks it is what the user sees.
	modeLaunchHint
	// modeFocus routes the keyboard into the selected session's pane while
	// the list and live preview stay on screen.
	modeFocus
)

type Model struct {
	services      services
	workspace     workspace
	rail          uirail.Model
	displayedRail uirail.Frame
	focusPane     uifocus.Model
	focusRuntime  focusRuntimeState
	prefs         preferences
	ledger        launchLedger
	startup       startupState
	notices       noticesState
	poller        *poller
	mode          mode
	review        uireview.Model
	reviewFX      reviewAdapter
	reviewReturn  reviewReturn
	form          form
	groupForm     groupForm
	pathSugg      pathComplete
	confirm       confirmTarget
	launchFix     launchFix
	// install is the setup-dialog install still running in a shell tab,
	// nil when none is.
	install *pendingInstall
	// mouseReleased is true while the setup dialog has handed the mouse
	// back to the terminal, so a drag selects its text.
	mouseReleased  bool
	mouseHover     bool
	rename         renameTarget
	fork           forkState
	quick          quickState
	settings       settingsState
	help           uihelp.State
	helpReturnMode mode
	moveID         string
	movePath       string
	repoPick       repoPickState
	// editorReturnID is the session an editor request detached from, so the
	// attach it cost can be resumed once the editor is up.
	editorReturnID string
	frame          string
	width          int
	height         int
	errBar         errBar
	split          splitState
	update         updateInfo
}

func NewWithInboxOwner(cfg config.Config, st *store.Store, driver *tmux.Driver, engine *status.Engine, hookManager *hooks.Manager, version string, owner sessioncmd.InboxMaintenance) *Model {
	if owner == nil {
		panic("inbox owner is required for explicit composition")
	}
	model := New(cfg, st, driver, engine, hookManager, version)
	model.poller.dependencies.Inbox = owner
	model.poller.runner = execution.New(model.poller.dependencies, model.poller.options)
	return model
}

type Dependencies struct {
	Config     config.Config
	Store      *store.Store
	TMux       *tmux.Driver
	Engine     *status.Engine
	Hooks      *hooks.Manager
	Git        *git.Driver
	Lifecycle  *sessioncmd.Lifecycle
	Execution  *execution.Runner
	ProfileDir string
}

func New(cfg config.Config, st *store.Store, driver *tmux.Driver, engine *status.Engine, hookManager *hooks.Manager, version string) *Model {
	gitDriver, _ := git.New()
	dir, _ := config.Dir()
	deps := execution.Dependencies{Store: st, TMux: driver, Engine: engine, Hooks: hookManager, Git: gitDriver, Notify: postNotification,
		TakeFocus: func() (string, bool) { return notify.TakeFocus(dir) }}
	opts := execution.OptionsFromConfig(cfg)
	var model *Model
	lifecycle, err := sessioncmd.NewLifecycle(sessioncmd.Runtime{
		Config: cfg, Store: st, Driver: driver, Hooks: hookManager, Git: gitDriver,
		Snapshot: func(id, pane string) error { return model.services.setSnapshot(id, pane) },
	})
	if err != nil {
		panic(err)
	}
	model = newView(Dependencies{Config: cfg, Store: st, TMux: driver, Engine: engine, Hooks: hookManager, Git: gitDriver, Lifecycle: lifecycle, Execution: execution.New(deps, opts), ProfileDir: dir}, version)
	model.poller.dependencies, model.poller.options = deps, opts
	return model
}

func NewWithServices(deps Dependencies, version string) *Model {
	if deps.Execution == nil || deps.Lifecycle == nil {
		panic("execution and lifecycle services are required")
	}
	return newView(deps, version)
}

func newView(deps Dependencies, version string) *Model {
	cfg, st, driver, engine, hookManager, gitDriver := deps.Config, deps.Store, deps.TMux, deps.Engine, deps.Hooks, deps.Git
	applyTheme(themes[themeIndex(resolveStartupTheme(st))])
	driver.SetSessionKeys(cfg.SessionKeys)
	model := &Model{
		poller: &poller{runner: deps.Execution},
		split:  splitState{ratio: loadSplitRatio(st)},
		mode:   modeList,
		update: updateInfo{version: version},
		services: services{
			cfg:         cfg,
			lifecycle:   deps.Lifecycle,
			configDir:   deps.ProfileDir,
			store:       st,
			tmux:        driver,
			keys:        cfg.SessionKeys,
			listKeys:    cfg.ListKeys,
			hooks:       hookManager,
			gitDrv:      gitDriver,
			engine:      engine,
			setSnapshot: st.SetSnapshot,
		},
		rail: uirail.New(loadCollapsed(st)),
		focusRuntime: focusRuntimeState{
			imeCursor: &cursorAnchor{},
		},
		prefs: preferences{
			focusOnEnter:    storedFocusOnEnter(st),
			arrowStep:       storedArrowStep(st),
			comfortableRows: storedComfortableRows(st),
			fullLayout:      storedFullLayout(st),
			hideHeader:      storedHideHeader(st),
			hideStats:       storedHideStats(st),
			mouseDisabled:   storedMouseDisabled(st),
		},
		startup: startupState{
			booting: true,
		},
		notices: noticesState{
			dismissed:           loadDismissed(st),
			whatsNewVersion:     loadWhatsNewVersion(st),
			whatsNewFromVersion: loadWhatsNewFromVersion(st),
		},
	}
	if deps.ProfileDir != "" {
		cached := update.Cached(deps.ProfileDir, version)
		model.update.latest = cached.Latest
		model.update.url = cached.URL
		model.update.releases = cached.Releases
		model.update.checked = len(cached.Releases) > 0
	}
	model.openStartupNotice()
	model.indexReleaseRanges()
	model.review = uireview.New(model.defaultSplitLayout())
	model.prepareFrame()
	return model
}

func (m *Model) Init() tea.Cmd {
	m.syncPollInput()
	return tea.Batch(m.syncPaneTheme(), m.refreshExistingSessionUX, m.checkForUpdate, m.checkFeed, m.updateTick(), m.bannerTick(), m.previewTick(), m.startStartupTick(), m.sweepPastes, m.pasteSweepTick())
}
