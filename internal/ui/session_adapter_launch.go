package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
	"time"
)

type launchOptions struct {
	rollbackWorktree bool
}

func (m *Model) launchNewSession(sess store.Session, tool config.Tool, baseCommand string, opts launchOptions) error {
	paneWidth, paneHeight := m.paneTargetSize()
	launched, err := m.services.lifecycle.Launch(sessioncmd.LaunchRequest{
		Session:          sess,
		Tool:             tool,
		BaseCommand:      baseCommand,
		Pane:             sessioncmd.PaneSize{Width: paneWidth, Height: paneHeight},
		RollbackWorktree: opts.rollbackWorktree,
	})
	if err != nil {
		return err
	}
	m.markFreshPane(sess.ID)
	sess = launched.Session
	m.recordLaunch(sess.ID)
	m.workspace.sessions = append(m.workspace.sessions, sess)
	m.rebuildRows()
	return launched.LabelError
}

// recordLaunch marks a row this run launched, so a poll that listed the store
// before it cannot blink the row off screen.
func (m *Model) recordLaunch(id string) { m.lifecycle.RecordLaunch(id, time.Now()) }

// forgetLaunch drops a row from the pending set, so a session the user has
// since sent away is not carried back onto the tree by a poll that listed the
// store before it was launched.
func (m *Model) forgetLaunch(id string) { m.lifecycle.ForgetLaunch(id) }

// keepPendingLaunches carries this run's spawns over a listing captured
// before them, so the new agents do not blink off screen.
func (m *Model) keepPendingLaunches(polled []store.Session, listedAt time.Time) []store.Session {
	return m.lifecycle.KeepPendingLaunches(polled, m.workspace.sessions, listedAt)
}

// markSession records the loaded-list state this run just gave a session, so
// stale polls predating the change are reconciled on arrival.
func (m *Model) markSession(id string, mark uilifecycle.GoneMark) {
	m.lifecycle.MarkSession(id, mark)
}

// dropRecentlyRemoved reconciles a poll's rows against what this run has just
// done to them.
func (m *Model) dropRecentlyRemoved(polled []store.Session, listedAt time.Time) []store.Session {
	return m.lifecycle.DropRecentlyRemoved(polled, listedAt)
}

// reconcileListing reconciles a stale listing's group rows and archive flags
// against what this run has just done.
func (m *Model) reconcileListing(msg *refreshMsg) {
	listing := m.lifecycle.ReconcileListing(uilifecycle.Listing{
		ListedAt:       msg.listedAt,
		Groups:         msg.groups,
		GroupPaths:     msg.groupPaths,
		GroupWorktrees: msg.groupWorktrees,
		GroupBases:     msg.groupBases,
		ArchivedGroups: msg.archivedGroups,
	})
	msg.groups = listing.Groups
	msg.groupPaths = listing.GroupPaths
	msg.groupWorktrees = listing.GroupWorktrees
	msg.groupBases = listing.GroupBases
	msg.archivedGroups = listing.ArchivedGroups
}

type attachRequest struct {
	id            string
	reattach      bool
	generation    int
	foregroundGen uint64
	screen        screenID
}

func (attachRequest) EffectRequest() {}

func (request attachRequest) copied() effectRequest { return request }

func (attachRequest) effectName() string { return "focus or attach" }

func (request attachRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runAttach(request)
}

type attachEffectResult struct{ uilifecycle.AttachResult }

func (attachEffectResult) EffectResult() {}

func (result attachEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applyAttachEffect(job.Request.(attachRequest), result, err)
}

func (s effectServices) runAttach(request attachRequest) (uieffects.Result, error) {
	result, err := uilifecycle.RunAttach(s.lifecycleServices(), uilifecycle.AttachRequest{ID: request.id, Reattach: request.reattach})
	return attachEffectResult{result}, err
}

func (m *Model) applyAttachEffect(request attachRequest, result attachEffectResult, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	if request.reattach && (request.generation != m.review.Generation() || m.review.Active()) {
		return nil
	}
	if m.effects.Quitting || request.foregroundGen != m.gens.foreground || request.screen != m.screenID() {
		return nil
	}
	if result.Warning != nil {
		m.reportErr(result.Warning.Error())
	}
	return execTerminalProcess(m.services.tmux.AttachCommand(request.id), func(err error) tea.Msg { return attachDoneMsg{sessID: request.id, err: err} })
}
