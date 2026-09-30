package ui

import (
	"strings"
)

func (m *Model) View() string {
	if m.frame == "" {
		return "loading..."
	}
	return m.frame
}

func (m *Model) prepareFrame() {
	m.prepareReviewLayout()
	m.frame = m.renderFrame()
}

func (m *Model) renderFrame() string {
	if m.width == 0 {
		return m.syncCursorAnchor("loading...")
	}
	if m.startup.booting {
		var lines []string
		for _, line := range ringLoader(m.width, m.height, "loading", m.startup.startupPhase) {
			lines = append(lines, paint(line, m.width, backdropHex()))
		}
		frame := m.overlayTopRight(strings.Join(lines, "\n"), m.statusToast(), 0)
		return m.syncCursorAnchor(clampFrame(frame, m.height))
	}
	var frame string
	switch m.mode {
	case modeForm:
		frame = m.viewForm()
	case modeHelp:
		frame = m.viewHelp()
	case modeConfirmDelete:
		frame = m.viewConfirm()
	case modeLaunchHint:
		frame = m.viewLaunchHint()
	case modeSettings:
		frame = m.viewSettings()
	case modeFork:
		frame = m.viewFork()
	case modeMove:
		frame = m.viewMove()
	case modeRepoPick:
		frame = m.viewRepoPick()
	case modeGroupForm:
		frame = m.viewGroupForm()
	case modeDiff:
		frame = m.viewDiffFull()
	case modeNotices:
		frame = m.viewNotices()
	default:
		frame = m.overlayRowMenu(m.viewListFrame())
	}
	return m.syncCursorAnchor(clampFrame(frame, m.height))
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
