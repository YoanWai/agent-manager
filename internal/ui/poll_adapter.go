package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/notify"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var postNotification = notify.Notify

type poller struct {
	runner       *execution.Runner
	dependencies execution.Dependencies
	options      execution.Options
}

func newPoller(st *store.Store, driver *tmux.Driver, engine *status.Engine, hookManager *hooks.Manager, gitDriver *git.Driver, statusSources, sessionStores, mcpStyles map[string]string, shellTools map[string]bool, binaries execution.ToolBinaries, interval time.Duration) *poller {
	deps := execution.Dependencies{Store: st, TMux: driver, Engine: engine, Hooks: hookManager, Git: gitDriver, Notify: postNotification, TakeFocus: takeNotifyFocus}
	opts := execution.Options{StatusSources: statusSources, SessionStores: sessionStores, MCPStyles: mcpStyles, ShellTools: shellTools, Binaries: binaries, Interval: interval}
	return &poller{runner: execution.New(deps, opts), dependencies: deps, options: opts}
}

func pollMessage(result execution.Result) tea.Msg {
	if result.Err != nil {
		return errMsg{result.Err}
	}
	s := result.Snapshot
	return refreshMsg{sessions: s.Sessions, listedAt: s.ListedAt, groups: s.Groups, groupPaths: s.GroupPaths,
		groupWorktrees: s.GroupWorktrees, archivedGroups: s.ArchivedGroups, snap: s.Snap, snapOK: s.SnapOK,
		proc: s.Proc, procFor: s.ProcFor, preview: s.Preview, agents: agentStats{count: s.Agents.Count, cpu: s.Agents.CPU, ram: s.Agents.RAM, rss: s.Agents.RSS},
		queuedMessages: s.QueuedMessages, paneLines: s.PaneLines, panePrompts: s.PanePrompts, panes: s.Panes,
		tmuxSocket: s.TmuxSocket, leadingManager: s.LeadingManager, focusID: s.FocusID}
}

func (p *poller) refreshOnce() tea.Msg                       { return pollMessage(p.runner.Step()) }
func (p *poller) setInput(archived bool, selected string)    { p.runner.SetInput(archived, selected) }
func (p *poller) requestRefresh()                            { p.runner.RequestRefresh() }
func (p *poller) reflowSessions(ids []string, reflow func()) { p.runner.ReflowSessions(ids, reflow) }
func (p *poller) typeForkKeys(session store.Session, keys string) error {
	return p.runner.TypeForkKeys(session, keys)
}
func (p *poller) results(ctx context.Context) <-chan execution.Result { return p.runner.Run(ctx) }

func takeNotifyFocus() (string, bool) {
	dir, err := config.Dir()
	if err != nil {
		return "", false
	}
	return notify.TakeFocus(dir)
}
func newToolBinaries(cfg config.Config) execution.ToolBinaries { return execution.NewToolBinaries(cfg) }
func storedPreview(st *store.Store, driver *tmux.Driver, id string) (string, error) {
	return execution.StoredPreview(st, driver, id)
}

const quoteHistoryLines = 300

func oneLine(name string) string  { return strings.Join(strings.Fields(name), " ") }
func paneBooted(pane string) bool { return strings.TrimSpace(ansi.Strip(pane)) != "" }
func ignoreDeletedSession(err error) error {
	if errors.Is(err, store.ErrSessionGone) {
		return nil
	}
	return err
}
