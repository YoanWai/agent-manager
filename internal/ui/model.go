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
	"maps"
	"slices"
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
	gens       generations
	effects    effectState
	services   services
	workspace  workspace
	rail       uirail.Model
	focus      focusState
	prefs      preferences
	ledger     launchLedger
	startup    startupState
	notices    noticesPanel
	poller     *poller
	mode       mode
	review     uireview.Model
	reviewNav  reviewNavState
	form       form
	groupForm  groupForm
	pathSugg   pathComplete
	confirm    confirmTarget
	launchHint launchHintState
	mouse      mouseCapture
	rename     renameTarget
	fork       forkState
	quick      quickState
	settings   settingsFeature
	env        environment
	help       helpFeature
	move       moveDialog
	layout     layoutState
	errBar     errBar
	update     updateInfo
}

// generations fence asynchronous completions against the state that
// requested them.
type generations struct {
	foreground    uint64
	dialog        uint64
	worktreeProbe uint64
}

type layoutState struct {
	frame         string
	width         int
	height        int
	split         splitState
	displayedRail uirail.Frame
	// cardTop, cardLeft and cardRight place the last card painted, for clicks.
	cardTop, cardLeft, cardRight int
}

type focusState struct {
	pane    uifocus.Model
	runtime focusRuntimeState
}

type reviewNavState struct {
	ret    reviewReturn
	picker repoPicker
}

type launchHintState struct {
	fix launchFix
	// install is the setup-dialog install still running in a shell tab,
	// nil when none is.
	install *pendingInstall
}

type mouseCapture struct {
	// released is true while the setup dialog has handed the mouse
	// back to the terminal, so a drag selects its text.
	released bool
	hover    bool
}

type settingsFeature struct {
	dialog  settingsState
	cache   settingsCache
	pending int
	gen     uint64
}

type environment struct {
	workDir string
	homeDir string
}

type helpFeature struct {
	state      uihelp.State
	returnMode mode
}

type moveDialog struct {
	id   string
	path string
}

func NewWithInboxOwner(cfg config.Config, st *store.Store, driver *tmux.Driver, engine *status.Engine, hookManager *hooks.Manager, version string, owner sessioncmd.InboxMaintenance) (*Model, error) {
	if owner == nil {
		panic("inbox owner is required for explicit composition")
	}
	model, err := New(cfg, st, driver, engine, hookManager, version)
	if err != nil {
		return nil, err
	}
	model.poller.dependencies.Inbox = owner
	model.poller.runner = execution.New(model.poller.dependencies, model.poller.options)
	return model, nil
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

func New(cfg config.Config, st *store.Store, driver *tmux.Driver, engine *status.Engine, hookManager *hooks.Manager, version string) (*Model, error) {
	gitDriver, _ := git.New()
	dir, _ := config.Dir()
	deps := execution.Dependencies{Store: st, TMux: driver, Engine: engine, Hooks: hookManager, Git: gitDriver, Notify: postNotification,
		TakeFocus: func() (string, bool) { return notify.TakeFocus(dir) }}
	opts := execution.OptionsFromConfig(cfg)
	var model *Model
	lifecycle, err := sessioncmd.NewLifecycle(sessioncmd.Runtime{
		Config: cfg, Store: st, Driver: driver, Hooks: hookManager, Git: gitDriver,
		Snapshot: st.SetSnapshot,
	})
	if err != nil {
		return nil, err
	}
	model, err = newView(Dependencies{Config: cfg, Store: st, TMux: driver, Engine: engine, Hooks: hookManager, Git: gitDriver, Lifecycle: lifecycle, Execution: execution.New(deps, opts), ProfileDir: dir}, version)
	if err != nil {
		return nil, err
	}
	model.poller.dependencies, model.poller.options = deps, opts
	return model, nil
}

func NewWithServices(deps Dependencies, version string) (*Model, error) {
	if deps.Execution == nil || deps.Lifecycle == nil {
		panic("execution and lifecycle services are required")
	}
	return newView(deps, version)
}

func newView(deps Dependencies, version string) (*Model, error) {
	cfg, st, driver, engine, hookManager, gitDriver := deps.Config, deps.Store, deps.TMux, deps.Engine, deps.Hooks, deps.Git
	sessionKeys, err := st.SessionKeys()
	if err != nil {
		return nil, err
	}
	listKeys, err := st.ListKeys()
	if err != nil {
		return nil, err
	}
	editor, err := st.Editor()
	if err != nil {
		return nil, err
	}
	configImportError, err := st.ConfigImportError()
	if err != nil {
		return nil, err
	}
	applyTheme(themes[themeIndex(resolveStartupTheme(st))])
	driver.SetSessionKeys(sessionKeys)
	model := &Model{
		effects: effectState{lifetime: &effectLifetime{}},
		poller:  &poller{runner: deps.Execution},
		layout:  layoutState{split: splitState{ratio: loadSplitRatio(st)}},
		mode:    modeList,
		env: environment{
			workDir: initialWorkingDir(),
			homeDir: initialHomeDir(),
		},
		update: updateInfo{version: version},
		services: services{
			cfg:         cfg,
			lifecycle:   deps.Lifecycle,
			configDir:   deps.ProfileDir,
			store:       st,
			tmux:        driver,
			keys:        sessionKeys,
			listKeys:    listKeys,
			editor:      editor,
			hooks:       hookManager,
			gitDrv:      gitDriver,
			engine:      engine,
			setSnapshot: st.SetSnapshot,
		},
		rail:     uirail.New(loadCollapsed(st)),
		settings: settingsFeature{cache: loadSettingsCache(st, choiceSettingKeys(slices.Sorted(maps.Keys(cfg.Tools))))},
		focus: focusState{runtime: focusRuntimeState{
			imeCursor: &cursorAnchor{},
		}},
		prefs: preferences{
			focusOnEnter:    storedFocusOnEnter(st),
			arrowStep:       storedArrowStep(st),
			comfortableRows: storedComfortableRows(st),
			fullLayout:      storedFullLayout(st),
			hideHeader:      storedHideHeader(st),
			hideStats:       storedHideStats(st),
			mouseDisabled:   storedMouseDisabled(st),
			baseFetchOff:    storedBaseFetchOff(st),
		},
		startup: startupState{
			booting: true,
		},
		notices: noticesPanel{noticesState{
			dismissed:           loadDismissed(st),
			whatsNewVersion:     loadWhatsNewVersion(st),
			whatsNewFromVersion: loadWhatsNewFromVersion(st),
			configImportError:   configImportError,
		}},
	}
	if deps.ProfileDir != "" {
		cached := update.Cached(deps.ProfileDir, version)
		model.update.latest = cached.Latest
		model.update.url = cached.URL
		model.update.releases = cached.Releases
		model.update.checked = len(cached.Releases) > 0
	}
	model.prefs.terminalBackground = model.storedTerminalBackground()
	model.openStartupNotice()
	model.indexReleaseRanges()
	model.review = uireview.New(model.defaultSplitLayout())
	model.prepareFrame()
	return model, nil
}

func (m *Model) Init() tea.Cmd {
	m.syncPollInput()
	return tea.Batch(m.syncPaneTheme(), m.refreshExistingSessionUX, m.checkForUpdate, m.checkFeed, m.updateTick(), m.bannerTick(), m.previewTick(), m.startStartupTick(), m.sweepPastes, m.pasteSweepTick())
}
