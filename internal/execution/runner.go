package execution

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/notify"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

type Dependencies struct {
	Store     *store.Store
	TMux      *tmux.Driver
	Engine    *status.Engine
	Hooks     *hooks.Manager
	Git       *git.Driver
	Inbox     sessioncmd.InboxMaintenance
	Notify    func(notify.Event)
	TakeFocus func() (string, bool)
}

type Options struct {
	StatusSources map[string]string
	SessionStores map[string]string
	MCPStyles     map[string]string
	ShellTools    map[string]bool
	Binaries      ToolBinaries
	Interval      time.Duration
}

type AgentStats struct {
	Count    int
	CPU, RAM float64
	RSS      uint64
}

type Snapshot struct {
	Sessions                   []store.Session
	ListedAt                   time.Time
	Groups                     []string
	GroupPaths, GroupWorktrees map[string]string
	ArchivedGroups             map[string]bool
	Snap                       sysstat.Snapshot
	SnapOK                     bool
	Proc                       sysstat.ProcStat
	ProcFor, Preview           string
	Agents                     AgentStats
	QueuedMessages             map[string]int
	PaneLines, PanePrompts     map[string]string
	Panes                      map[string]tmux.Pane
	TmuxSocket                 string
	LeadingManager             bool
	FocusID                    string
}

type Result struct {
	Snapshot Snapshot
	Err      error
}

func New(deps Dependencies, opts Options) *Runner {
	interval := opts.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	inbox := deps.Inbox
	if inbox == nil && deps.Store != nil {
		inbox = sessioncmd.NewInboxOwner(deps.Store)
	}
	notifyFn := deps.Notify
	if notifyFn == nil {
		notifyFn = func(notify.Event) {}
	}
	return &Runner{
		store: deps.Store, tmux: deps.TMux, engine: deps.Engine, hooks: deps.Hooks,
		gitDrv: deps.Git, inboxOwner: inbox, notifyFn: notifyFn, takeFocus: deps.TakeFocus,
		statusSources: opts.StatusSources, sessionStores: opts.SessionStores,
		mcpStyles: opts.MCPStyles, shellTools: opts.ShellTools, binaries: opts.Binaries,
		interval: interval, poke: make(chan struct{}, 1), paneHashes: map[string]uint64{},
		claudeTails: map[string]claudeTailCache{}, quietSince: map[string]quietTimer{},
		recaptureSeen: map[string]recaptureSighting{},
	}
}

func OptionsFromConfig(cfg config.Config) Options {
	opts := Options{Interval: cfg.PollInterval.Duration, StatusSources: map[string]string{}, SessionStores: map[string]string{}, MCPStyles: map[string]string{}, ShellTools: map[string]bool{}, Binaries: NewToolBinaries(cfg)}
	for name, tool := range cfg.Tools {
		opts.StatusSources[name] = tool.StatusSource
		opts.SessionStores[name] = tool.SessionStore
		opts.MCPStyles[name] = mcpreg.Style(name, tool.MCP)
		opts.ShellTools[name] = tool.Shell
	}
	return opts
}
