package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	tea "github.com/charmbracelet/bubbletea"
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
func (m *Model) recordLaunch(id string) { m.ledger.lifecycle.RecordLaunch(id, time.Now()) }

// forgetLaunch drops a row from the pending set, so a session the user has
// since sent away is not carried back onto the tree by a poll that listed the
// store before it was launched.
func (m *Model) forgetLaunch(id string) { m.ledger.lifecycle.ForgetLaunch(id) }

// reconcileListing reconciles a stale listing's group rows and archive flags
// against what this run has just done.
func (m *Model) reconcileListing(msg *refreshMsg) {
	listing := m.ledger.lifecycle.ReconcileListing(uilifecycle.Listing{
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

// applyAttachEffect hands the terminal to a session once its pane checked
// out. The fences are compared here, on the completion rather than the
// request, because the user may have moved the screen while it waited.
func (m *Model) applyAttachEffect(request uilifecycle.AttachRequest, result uilifecycle.AttachResult, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	if request.Reattach && (request.Generation != m.review.Generation() || m.review.Active()) {
		return nil
	}
	if m.effects.Quitting || request.ForegroundGen != m.gens.foreground || request.Screen != m.fence() {
		return nil
	}
	if result.Warning != nil {
		m.reportErr(result.Warning.Error())
	}
	return uiterminal.ExecProcess(m.services.tmux.AttachCommand(request.ID), func(err error) tea.Msg { return attachDoneMsg{sessID: request.ID, err: err} })
}

// launchLedger is what a manager run remembers about the sessions it starts
// and ends, outliving the cards that asked: each piece's state and runtime
// live in its package.
type launchLedger struct {
	// composerSeq numbers the prompt boxes this run has opened.
	composerSeq int
	lifecycle   uilifecycle.State
	spawner     uilaunch.State
	install     uiinstall.State
}
