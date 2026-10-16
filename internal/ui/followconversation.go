package ui

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
)

// followConversation moves a session onto the conversation its CLI's own
// state says the agent is on, for the CLIs that report no switch
// themselves. State that outlives the agent, such as grok's log, is read
// for an agent that has quit too, since it may have switched while no
// manager was watching. Once the agent has quit, the CLI that names its
// conversation only as it closes is read as well. paneGone says the
// session's pane no longer exists.
func (p *poller) followConversation(sess *store.Session, agentAlive, paneGone bool) error {
	style := p.reportStyles[sess.Tool]
	followed, err := sessioncmd.FollowConversation(p.store, p.hooks, &p.conversations, *sess, style)
	if err == nil && !agentAlive {
		followed, err = sessioncmd.SettleConversation(p.store, p.hooks, followed, style, paneGone, 0)
	}
	if err != nil {
		return ignoreDeletedSession(err)
	}
	sess.AgentSessionID = followed.AgentSessionID
	return nil
}

// newFollowFailure names a session whose conversation this pass could not
// follow for a reason not reported yet. The pass carries on for every other
// row, and a failure that lasts is reported once.
func (p *poller) newFollowFailure(sessions []store.Session, failures map[string]error) string {
	failed := make(map[string]string, len(failures))
	warning := ""
	for _, sess := range sessions {
		err := failures[sess.ID]
		if err == nil {
			continue
		}
		failed[sess.ID] = err.Error()
		if warning == "" && p.followFailed[sess.ID] != err.Error() {
			warning = fmt.Sprintf("%s: could not follow its conversation: %v", sess.Name, err)
		}
	}
	p.followFailed = failed
	return warning
}
