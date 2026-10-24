package ui

import (
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// startupState is the loader, the wordmark sweep and the boot ring: the
// package owns the timing and the frame counter, the root the fields the
// rail, preview and layout read every frame.
type startupState = uistartup.State

func (m *Model) routeStartupMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	cmd, ok := m.startup.Update(startupRoot{m}, msg)
	if !ok {
		return nil, nil, false
	}
	return routed(m, cmd)
}

// startupRoot is the root as the startup animation reads it.
type startupRoot struct{ *Model }

func (h startupRoot) LoaderNeeded() bool { return h.needsLoaderTick() }

func (h startupRoot) ReportWarn(text string) { h.reportWarn(text) }

func (m *Model) startStartupTick() tea.Cmd { return m.startup.StartTick(startupRoot{m}) }

func (m *Model) bannerTick() tea.Cmd { return m.startup.BannerTick() }

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

// startingLoader is the ring a session still booting shows in place of its
// pane: the name of what is happening, until the pane paints something.
func startingLoader(g presentation.GlyphSet, starting bool, preview string, width, height, phase int) []string {
	if !starting || paneBooted(preview) {
		return nil
	}
	return uistartup.Ring(g, width, height, "starting up", phase, uitheme.Current())
}
