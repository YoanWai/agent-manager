package ui

import (
	"maps"
	"slices"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/notify"
	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	uirelease "github.com/YoanWai/agent-manager/internal/ui/notices/release"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	tea "github.com/charmbracelet/bubbletea"
)

type Model struct {
	gens      generations
	effects   effectState
	services  services
	workspace workspace
	rail      uirail.Model
	focus     focusState
	prefs     preferences
	ledger    launchLedger
	startup   startupState
	notices   noticesPanel
	poller    *poller
	screen
	review    uireview.Model
	reviewNav reviewNavState
	mouse     mouseCapture
	settings  uipersist.State
	env       environment
	ssh       uilink.State
	layout    layoutState
	errBar    errBar
	release   uirelease.State
	install   uiinstall.State
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
	// card places the last card painted and its body, for clicks.
	card cardRect
	// keyHits are the key pairs this frame painted, for clicks.
	keyHits keyHits
}

type focusState struct {
	pane    uifocus.Model
	runtime focusRuntimeState
}

type reviewNavState struct {
	ret reviewReturn
}

type mouseCapture struct {
	// released is true while the mouse-mode setting has handed the mouse
	// back to the terminal, so a drag selects its text.
	released bool
	hover    bool
	// pressed is what the last left press landed on, which its release
	// clicks when it comes back up over the same one.
	pressed clickTarget
}

type environment struct {
	workDir string
	homeDir string
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
	deps := execution.Dependencies{Store: st, TMux: driver, Engine: engine, Hooks: hookManager, Git: gitDriver, Notify: postNotification, ShowCommand: showCommand,
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
	connections, err := st.Connections()
	if err != nil {
		return nil, err
	}
	applyTheme(themes[themeIndex(resolveStartupTheme(st))])
	driver.SetSessionKeys(sessionKeys)
	model := &Model{
		effects: effectState{Queue: uieffects.Queue{Lifetime: &uieffects.Lifetime{}}},
		poller:  &poller{runner: deps.Execution},
		layout:  layoutState{split: splitState{ratio: loadSplitRatio(st)}},
		env: environment{
			workDir: initialWorkingDir(),
			homeDir: initialHomeDir(),
		},
		release: uirelease.New(st, version),
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
		rail:     loadRail(st),
		ssh:      uilink.New(remote.New(deps.ProfileDir), connections),
		settings: uipersist.New(uipersist.StoreWriter{Store: st}, choiceSettingKeys(slices.Sorted(maps.Keys(cfg.Tools)))),
		focus: focusState{runtime: focusRuntimeState{
			imeCursor: &cursorAnchor{},
		}},
		startup: startupState{
			booting: true,
		},
		notices: noticesPanel{
			Panel:             uinotices.NewPanel(uirelease.Dismissed(st)),
			configImportError: configImportError,
		},
	}
	if deps.ProfileDir != "" {
		model.release.UseCached(deps.ProfileDir)
	}
	model.applyCachedSettingsPrefs()
	model.prefs.terminalBackground = model.storedTerminalBackground()
	glyph := uipersist.ReadKeys(uipersist.StoreWriter{Store: st}, []string{uisettings.GlyphModeSetting})[0]
	if glyph.Err != nil {
		model.reportErr("reading symbols setting: " + glyph.Err.Error())
	} else {
		model.prefs.glyphMode = presentation.GlyphMode(glyph.Value)
	}
	model.openStartupNotice()
	model.release.IndexRanges()
	model.review = uireview.New(model.defaultSplitLayout())
	model.prepareFrame()
	return model, nil
}

func (m *Model) Init() tea.Cmd {
	m.syncPollInput()
	return tea.Batch(m.syncPaneTheme(), m.refreshExistingSessionUX, m.release.Check(), m.release.CheckFeed(), uirelease.Tick(), m.bannerTick(), m.previewTick(), m.startStartupTick(), m.sweepPastes, m.pasteSweepTick(), checkPaneShell)
}
