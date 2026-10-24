package ui

import (
	"errors"
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
	uiquicksend "github.com/YoanWai/agent-manager/internal/ui/quick/send"
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
	binders := []func(uieffects.Request) (func() (uieffects.Result, error), bool){
		uieffects.Binder(func() focusruntime.Services { return capture().focusRuntime() }),
		uieffects.Binder(func() uigeometrypanes.Services { return capture().geometry() }),
		uieffects.Binder(func() uiinstall.Services { return capture().install() }),
		uieffects.Binder(func() uilink.Services { return capture().link() }),
		uieffects.Binder(func() uiapply.Services { return capture().dialogApply() }),
		uieffects.Binder(func() uirailpersist.Services { return capture().railServices() }),
		uieffects.Binder(func() uireviewsource.Services {
			return uireviewsource.Services{Store: capture().store, Writer: uireviewsource.DriverWriter(capture().driver)}
		}),
		uieffects.Binder(func() uipersist.Services {
			services := capture()
			return uipersist.Services{Writer: uipersist.StoreWriter{Store: services.store}, Keys: services.keySaver(), Presets: services.store}
		}),
		uieffects.Binder[uinotices.Services](m.noticesServices),
		uieffects.Binder[uitheme.Services](themeHost{m}.Services),
		uieffects.Binder(func() uilifecycle.Services { return capture().lifecycleServices() }),
		uieffects.Binder(func() uilaunch.Services { return capture().launchServices() }),
		uieffects.Binder(func() uiquicksend.Services { return capture().quickServices() }),
	}
	for _, bind := range binders {
		if work, ok := bind(request); ok {
			return work
		}
	}
	return func() (uieffects.Result, error) {
		return nil, fmt.Errorf("effect %s binds no services", request.EffectName())
	}
}

// as is a job's result as the type its request's Run returns; the zero value
// for a request that carries nothing back.
func as[T any](result uieffects.Result) T {
	value, _ := result.(T)
	return value
}

// applyEffect applies a finished job's result on Update, by the request
// that produced it: a package result through its feature's host, a root
// result through its own apply.
func (m *Model) applyEffect(job *uieffects.Job, result uieffects.Result, err error) tea.Cmd {
	if errors.Is(err, uieffects.ErrStopped) {
		m.reportErr(err.Error())
		return nil
	}
	switch request := job.Request.(type) {
	case uiinstall.StartRequest:
		return m.ledger.install.Started(installRoot{m}, request, as[uiinstall.StartResult](result), err)
	case uiinstall.SettleRequest:
		return m.ledger.install.Settled(installRoot{m}, request, as[uiinstall.SettleResult](result), err)
	case uirailpersist.Request:
		return m.applyRailEffect(as[uirailpersist.Result](result), request, job.Chain, err)
	case focusruntime.ProbeRequest:
		return m.applyFocusEffect(request, err)
	case focusruntime.AckRequest:
		m.applyAckEffect(err)
		return nil
	case focusruntime.DetachRequest:
		return m.applyDetachEffect(request, as[focusruntime.Detach](result), err)
	case focusruntime.InputRequest:
		m.applyInputEffect(as[focusruntime.Result](result), err)
		return nil
	case uigeometrypanes.ResizeRequest:
		m.applyGeometryEffect(as[uigeometrypanes.Result](result), err)
		return nil
	case uigeometrypanes.SplitSaveRequest:
		m.layout.panes.ApplySplit(paneSizing{m}, job.ID, err)
		return nil
	case uilink.SaveRequest:
		return m.applyConnectionSave(request, as[[]store.Connection](result), err)
	case uilink.Call:
		return m.applyRemoteCall(request, as[uilink.Result](result), err)
	case uiapply.ForkEffect:
		return m.applyFork(request, as[uiapply.ForkOutcome](result), err)
	case uiapply.RenameEffect:
		m.applyRenameOutcome(job, as[uiapply.Outcome](result), err)
		return nil
	case uiapply.MoveClose:
		m.applyMoveClose(request)
		return nil
	case uireviewsource.Change:
		return m.applyReviewChange(as[uireviewsource.Result](result), err)
	case uipersist.SaveRequest:
		return m.settings.ApplySave(settingsSaveHost{m}, request, as[uipersist.SaveResult](result), err)
	case uipersist.KeysRequest:
		return uipersist.ApplyKeys(settingsSaveHost{m}, request, as[uipersist.KeysResult](result), err)
	case uipersist.PromptRequest:
		m.applyPromptsResult(request, as[uipersist.PromptResult](result), err)
		return nil
	case uinotices.DismissRequest:
		m.applyNoticeDismiss(job, err)
		return nil
	case uitheme.PaneRequest:
		return uitheme.ApplyPane(themeHost{m}, err)
	case uilifecycle.Request:
		return m.applyLifecycleEffect(request, as[uilifecycle.Result](result), err)
	case uilifecycle.AfterTurnRequest:
		return m.applyAfterTurn(request, as[uilifecycle.AfterTurnResult](result), err)
	case uilifecycle.AttachRequest:
		return m.applyAttachEffect(request, as[uilifecycle.AttachResult](result), err)
	case uilaunch.Request:
		return m.applySpawn(request, as[uilaunch.Result](result), err)
	case uilaunch.GroupRequest:
		return m.applyGroupEffect(request, as[uilaunch.GroupResult](result), err)
	case uiquicksend.SendRequest:
		return uiquicksend.Apply(quickSendRoot{m}, request, as[uiquicksend.SendResult](result), err)
	}
	// A kept model choice carries nothing back: the cache already holds it.
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
