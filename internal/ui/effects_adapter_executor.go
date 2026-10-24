package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uiapply "github.com/YoanWai/agent-manager/internal/ui/dialogs/apply"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	focuswatch "github.com/YoanWai/agent-manager/internal/ui/focus/watch"
	uigeometrypanes "github.com/YoanWai/agent-manager/internal/ui/geometry/panes"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uirailpersist "github.com/YoanWai/agent-manager/internal/ui/rail/persist"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

type effectServices struct {
	engine             *status.Engine
	hooks              *hooks.Manager
	pasteInput         func(*tmux.Driver, string, string) error
	saveKeys           func(keybind.Table) error
	lifecycle          *sessioncmd.Lifecycle
	cfg                config.Config
	store              *store.Store
	driver             *tmux.Driver
	runner             *execution.Runner
	watch              *focuswatch.Watcher
	gitDrv             *git.Driver
	quickSessionExists func(*tmux.Driver, string) (bool, error)
	quickSendText      func(*tmux.Driver, string, string) (tmux.SendResult, error)
	remote             *remote.Client
}

// bindEffect captures the services request runs against: those of the
// feature package that declares it.
func (m *Model) bindEffect(request uieffects.Request) func() (uieffects.Result, error) {
	capture := m.captureServices
	if work, ok := uieffects.Bind(request, func() focusruntime.Services { return capture().focusRuntime() }); ok {
		return work
	}
	if work, ok := uieffects.Bind(request, func() uigeometrypanes.Services { return capture().geometry() }); ok {
		return work
	}
	if work, ok := uieffects.Bind(request, func() uiinstall.Services { return capture().install() }); ok {
		return work
	}
	if work, ok := uieffects.Bind(request, func() uilink.Services { return capture().link() }); ok {
		return work
	}
	if work, ok := uieffects.Bind(request, func() uiapply.Services { return capture().dialogApply() }); ok {
		return work
	}
	if work, ok := uieffects.Bind(request, func() uirailpersist.Services { return capture().railServices() }); ok {
		return work
	}
	if work, ok := uieffects.Bind(request, func() uireviewsource.Services {
		return uireviewsource.Services{Store: capture().store, Writer: uireviewsource.DriverWriter(capture().driver)}
	}); ok {
		return work
	}
	if work, ok := uieffects.Bind[uipersist.Services](request, func() uipersist.Services {
		services := capture()
		return uipersist.Services{Writer: uipersist.StoreWriter{Store: services.store}, Keys: services.keySaver(), Presets: services.store}
	}); ok {
		return work
	}
	if work, ok := uieffects.Bind[uinotices.Services](request, m.noticesServices); ok {
		return work
	}
	if work, ok := uieffects.Bind[uitheme.Services](request, m.themeServices); ok {
		return work
	}
	if work, ok := uieffects.Bind(request, func() uilifecycle.Services { return capture().lifecycleServices() }); ok {
		return work
	}
	if work, ok := uieffects.Bind(request, func() uilaunch.Services { return capture().launchServices() }); ok {
		return work
	}
	if work, ok := uieffects.Bind(request, func() uiquick.Services { return capture().quickServices() }); ok {
		return work
	}
	return func() (uieffects.Result, error) {
		return nil, fmt.Errorf("effect %s binds no services", request.EffectName())
	}
}

// applyEffect applies a finished job's result on Update: a package result
// through its feature's host, a root result through its own apply.
func (m *Model) applyEffect(job *uieffects.Job, result uieffects.Result, err error) tea.Cmd {
	switch result := result.(type) {
	case uiinstall.StartResult:
		return m.ledger.install.Started(installRoot{m}, job.Request.(uiinstall.StartRequest), result, err)
	case uiinstall.SettleResult:
		return m.ledger.install.Settled(installRoot{m}, job.Request.(uiinstall.SettleRequest), result, err)
	case uirailpersist.ChangeResult:
		return m.applyRailEffect(result, job.Request.(uirailpersist.Request), job.Chain, err)
	case focusruntime.ProbeResult:
		return m.applyFocusEffect(job.Request.(focusruntime.ProbeRequest), err)
	case focusruntime.AckResult:
		m.applyAckEffect(err)
		return nil
	case focusruntime.DetachResult:
		return m.applyDetachEffect(job.Request.(focusruntime.DetachRequest), result, err)
	case focusruntime.InputResult:
		m.applyInputEffect(result, err)
		return nil
	case uigeometrypanes.ResizeResult:
		m.applyGeometryEffect(result.Result, err)
		return nil
	case uigeometrypanes.SplitSaveResult:
		m.layout.panes.ApplySplit(paneSizing{m}, job.ID, err)
		return nil
	case uilink.SaveResult:
		return m.applyConnectionSave(job.Request.(uilink.SaveRequest), result, err)
	case uilink.Result:
		return m.applyRemoteCall(job.Request.(uilink.Call), result, err)
	case uiapply.ForkEffectResult:
		return m.applyFork(job.Request.(uiapply.ForkEffect), result, err)
	case uiapply.RenameEffectResult:
		m.applyRenameOutcome(job, result, err)
		return nil
	case uiapply.MoveCloseResult:
		m.applyMoveClose(job.Request.(uiapply.MoveClose))
		return nil
	case uireviewsource.ChangeResult:
		return m.applyReviewChange(result, err)
	case uipersist.SaveResult:
		return m.settings.ApplySave(settingsSaveRoot{m}, job.Request.(uipersist.SaveRequest), result, err)
	case uipersist.KeysResult:
		return uipersist.ApplyKeys(settingsSaveRoot{m}, job.Request.(uipersist.KeysRequest), result, err)
	case uipersist.PromptResult:
		m.applyPromptsResult(job.Request.(uipersist.PromptRequest), result, err)
		return nil
	case uinotices.DismissResult:
		m.applyNoticeDismiss(job, err)
		return nil
	case uitheme.PaneResult:
		return m.applyPaneTheme(err)
	case uilifecycle.Result:
		return m.applyLifecycleEffect(job.Request.(uilifecycle.Request), result, err)
	case uilifecycle.AfterTurnResult:
		return m.applyAfterTurn(job.Request.(uilifecycle.AfterTurnRequest), result, err)
	case uilifecycle.AttachResult:
		return m.applyAttachEffect(job.Request.(uilifecycle.AttachRequest), result, err)
	case uilaunch.Result:
		return m.applySpawn(job.Request.(uilaunch.Request), result, err)
	case uilaunch.GroupResult:
		return m.applyGroupEffect(job.Request.(uilaunch.GroupRequest), result, err)
	case uiquick.SendResult:
		return uiquick.Apply(quickSendRoot{m}, job.Request.(uiquick.SendRequest), result, err)
	case uilaunch.ChoiceSaveResult:
		// A kept choice carries nothing back: the cache already holds it.
		if err != nil {
			m.reportErr(err.Error())
		}
		return nil
	}
	if err != nil {
		m.reportErr(err.Error())
	}
	return nil
}

func (m *Model) captureServices() effectServices {
	cfg := m.services.cfg
	cfg.Tools = make(map[string]config.Tool, len(m.services.cfg.Tools))
	for name, tool := range m.services.cfg.Tools {
		tool.Rules = append([]config.Rule(nil), tool.Rules...)
		cfg.Tools[name] = tool
	}
	services := effectServices{cfg: cfg, store: m.services.store, driver: m.services.tmux, watch: m.focus.runtime.Watch, gitDrv: m.services.gitDrv, remote: m.ssh.Client}
	services.engine = m.services.engine
	services.hooks = m.services.hooks
	services.pasteInput = pasteFocused
	services.quickSessionExists = m.services.quickSessionExists
	if services.quickSessionExists == nil {
		services.quickSessionExists = func(driver *tmux.Driver, id string) (bool, error) { return driver.SessionExists(id) }
	}
	services.quickSendText = m.services.quickSendText
	if services.quickSendText == nil {
		services.quickSendText = func(driver *tmux.Driver, id, text string) (tmux.SendResult, error) {
			return driver.SendTextResult(id, text)
		}
	}
	services.saveKeys = m.services.saveKeys
	if services.saveKeys == nil {
		services.saveKeys = m.services.store.SetKeys
	}
	if m.poller != nil {
		services.runner = m.poller.runner
	}
	if m.services.lifecycle != nil {
		services.lifecycle = m.services.lifecycle.Capture(cfg, m.services.setSnapshot)
	}
	return services
}

func (s effectServices) reflow(ids []string, work func()) error {
	if s.runner == nil {
		work()
		return nil
	}
	return s.runner.ReflowSessions(ids, work)
}

// focusRuntime is the store, tmux server, watcher, engine and paste transport
// the focus effects run against.
func (s effectServices) focusRuntime() focusruntime.Services {
	return focusruntime.Services{
		Store:  s.store,
		Driver: s.driver,
		Engine: s.engine,
		Watch:  s.watch,
		Paste:  s.pasteInput,
	}
}

// geometry is the store, tmux server and poller reflower the pane resize runs
// against.
func (s effectServices) geometry() uigeometrypanes.Services {
	var reflower uigeometrypanes.Reflower
	if s.runner != nil {
		reflower = s.runner
	}
	return uigeometrypanes.Services{Store: s.store, Driver: s.driver, Reflower: reflower}
}
