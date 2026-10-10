package ui

import (
	"strings"

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
		frame = fillBackdrop(frame, m.layout.width, current.Bg, current.Text)
	}
	m.layout.frame = frame
}

func (m *Model) renderFrame() string {
	m.notices.noticeHit = noticeHit{}
	if m.layout.width == 0 {
		return m.syncCursorAnchor("loading...")
	}
	if m.startup.booting {
		var lines []string
		for _, line := range ringLoader(m.layout.width, m.layout.height, "loading", m.startup.startupPhase) {
			lines = append(lines, paint(line, m.layout.width, backdropHex()))
		}
		frame := m.overlayTopRight(strings.Join(lines, "\n"), m.statusToast(), 0)
		return m.syncCursorAnchor(clampFrame(frame, m.layout.height))
	}
	var frame string
	switch m.mode {
	case modeForm:
		frame = m.viewForm()
	case modeHelp:
		frame = m.viewHelp()
	case modeConfirmDelete:
		frame = m.confirm.view(m)
	case modeLaunchHint:
		frame = m.launchHint.view(m)
	case modeSettings:
		frame = m.viewSettings()
	case modeFork:
		frame = m.fork.view(m)
	case modeMove:
		frame = m.move.view(m)
	case modeRepoPick:
		frame = m.reviewNav.picker.view(m)
	case modeGroupForm:
		frame = m.viewGroupForm()
	case modeDiff:
		frame = m.viewDiffFull()
	case modeNotices:
		frame = m.centerOnBackdrop(m.notices.view(m))
	default:
		frame = m.overlayRowMenu(m.titleTopRowWithUpdate(m.viewListFrame()))
	}
	return m.syncCursorAnchor(clampFrame(frame, m.layout.height))
}

// clampFrame pins a rendered frame to exactly height rows so the outer
// terminal cannot scroll the TUI away when a layout overshoots.
func clampFrame(frame string, height int) string {
	if height <= 0 {
		return frame
	}
	lines := strings.Split(frame, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		// Padding rows carry the backdrop too; a bare row would show the
		// terminal's own background through the bottom of a short frame.
		lines = append(lines, paint("", 1, backdropHex()))
	}
	return strings.Join(lines, "\n")
}
