package ui

import tea "github.com/charmbracelet/bubbletea"

func (d *confirmDialog) handleKey(h confirmHost, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		cmd := h.quit()
		return cmd
	case "n", "esc":
		d.confirmTarget = confirmTarget{}
		h.setMode(modeList)
		return nil
	case "y", "enter":
		target := d.confirmTarget
		target.sessions = append(target.sessions[:0:0], target.sessions...)
		d.confirmTarget = confirmTarget{}
		h.setMode(modeList)
		switch {
		case target.remote != nil:
			return h.runRemote(*target.remote)
		case target.connection != "":
			h.removeConnection(target.connection)
		default:
			h.queueLifecycle(target, false, "")
		}
		return h.nextEffectCmd()
	}
	return nil
}
