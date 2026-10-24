package ui

import (
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
)

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
