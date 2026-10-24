package ui

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
)

// startingLoader is the ring a session still booting shows in place of its
// pane: the name of what is happening, until the pane paints something.
func startingLoader(g presentation.GlyphSet, starting bool, preview string, width, height, phase int) []string {
	if !starting || paneBooted(preview) {
		return nil
	}
	return ringLoader(g, width, height, "starting up", phase)
}

func ringLoader(g presentation.GlyphSet, width, height int, label string, phase int) []string {
	return uistartup.Ring(g, width, height, label, phase, uitheme.Current())
}
