package ui

type startupState struct {
	// sessionsSized flips after the first refresh shrinks sessions left
	// over from a previous manager run to the preview panel's width.
	sessionsSized bool
	// bannerPhase advances the wordmark's current sweep and then rests, so
	// the frame is not repainted forever.
	bannerPhase      int
	startupPhase     int
	startupAnimating bool
	booting          bool
	pendingTyped     *typedPromptCandidate
}
