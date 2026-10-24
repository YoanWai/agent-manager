package ui

import (
	"strings"

	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func (m *Model) View() string {
	if m.layout.frame == "" {
		return "loading..."
	}
	return m.layout.frame
}

func (m *Model) prepareFrame() {
	m.prepareReviewLayout()
	frame := m.renderFrame()
	// A colorless profile renders the backdrop as a bare reset, which would
	// strip the bold and reverse cells it lands beside.
	if !m.prefs.terminalBackground && lipgloss.ColorProfile() != termenv.Ascii {
		frame = fillBackdrop(frame, m.layout.width, current().Bg, current().Text)
	}
	m.layout.frame = frame
}

func (m *Model) renderFrame() string {
	m.layout.keyHits, m.layout.card = nil, cardRect{}
	if m.layout.width == 0 {
		return m.syncCursorAnchor("loading...")
	}
	if m.startup.booting {
		var lines []string
		for _, line := range ringLoader(m.glyphs(), m.layout.width, m.layout.height, "loading", m.startup.startupPhase) {
			lines = append(lines, paint(line, m.layout.width, backdropHex()))
		}
		frame := m.overlayTopRight(strings.Join(lines, "\n"), m.statusToast(), 0)
		// Pinned to exactly height rows so the outer terminal cannot scroll
		// the TUI away when a layout overshoots.
		return m.syncCursorAnchor(uiframe.Clamp(frame, m.layout.height, func() string { return paint("", 1, backdropHex()) }))
	}
	var frame string
	if m.base == baseReview {
		frame = m.viewDiffFull()
	} else {
		frame = m.overlayRowMenu(m.titleTopRowWithUpdate(m.viewListFrame()))
	}
	frame = m.surfaceView(frame)
	m.layout.keyHits = m.layout.keyHits.above(m.layout.height)
	return m.syncCursorAnchor(uiframe.Clamp(frame, m.layout.height, func() string { return paint("", 1, backdropHex()) }))
}
