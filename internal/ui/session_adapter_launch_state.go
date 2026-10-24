package ui

// launchLedger is what the root still holds across a manager run: the prompt
// counter. Session, spawn and install state live in ui/session/lifecycle,
// ui/spawn/launch and ui/launchhint/install.
type launchLedger struct {
	// composerSeq numbers the prompt boxes this run has opened.
	composerSeq int
}
