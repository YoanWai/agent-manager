package ui

import (
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
)

// The wordmark's own measurements, timing and painting live in ui/startup;
// the root keeps the header height the layout and the mouse hit-tests are
// measured against.

// showBanner reports whether the terminal leaves the tracked-out wordmark
// room beside the fleet readings; a narrow one gets the name set plainly.
func (m *Model) showBanner() bool {
	return uistartup.Fits(m.layout.width, uiframe.RailGutter)
}

// headerRows is how many rows the header occupies, which the body height
// and every mouse hit-test are measured against.
func (m *Model) headerRows() int {
	if m.prefs.hideHeader {
		return 0
	}
	return uistartup.Rows
}

// viewBanner draws the wordmark, lit by a highlight that sweeps left to
// right during the intro and then rests just past the end of the word.
func (m *Model) viewBanner() []string {
	return uistartup.Line(m.startup.BannerPhase, uiframe.RailGutter, m.showBanner(), uitheme.Current())
}
