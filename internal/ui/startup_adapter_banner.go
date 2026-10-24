package ui

import (
	"time"

	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"
	tea "github.com/charmbracelet/bubbletea"
)

// The wordmark's own measurements, timing and painting live in ui/startup;
// the root keeps the sweep's timer and the header height the layout and the
// mouse hit-tests are measured against.
const (
	bannerFrames       = uistartup.Frames
	bannerInterval     = uistartup.Interval
	bannerShimmerEvery = uistartup.ShimmerEvery
	bannerRows         = uistartup.Rows
)

type (
	bannerTickMsg    struct{}
	bannerShimmerMsg struct{}
)

// bannerTick drives a sweep frame by frame; a settled wordmark waits out
// the shimmer interval before the next one.
func (m *Model) bannerTick() tea.Cmd {
	if m.startup.bannerPhase >= bannerFrames {
		return tea.Tick(bannerShimmerEvery, func(time.Time) tea.Msg { return bannerShimmerMsg{} })
	}
	return tea.Tick(bannerInterval, func(time.Time) tea.Msg { return bannerTickMsg{} })
}

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
	return bannerRows
}

// viewBanner draws the wordmark, lit by a highlight that sweeps left to
// right during the intro and then rests just past the end of the word.
func (m *Model) viewBanner() []string {
	return uistartup.Line(m.startup.bannerPhase, uiframe.RailGutter, m.showBanner(), current())
}
