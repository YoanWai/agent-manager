package ui

import tea "github.com/charmbracelet/bubbletea"

func (h *helpState) close() {
	*h = helpState{}
}

func (h *helpState) scrollBy(delta int, ctx helpContext) {
	h.scroll = min(max(h.scroll+delta, 0), h.scrollLimit(ctx))
}

func (h helpState) page(ctx helpContext) int {
	return max(h.bodyRoom(ctx)-1, 1)
}

func (h *helpState) update(msg tea.KeyMsg, ctx helpContext) helpAction {
	if msg.String() == "ctrl+c" {
		return helpQuit
	}
	if h.searching {
		h.updateSearch(msg, ctx)
		return helpStay
	}
	switch msg.String() {
	case "esc":
		if h.query != "" {
			h.query = ""
			h.scroll = 0
			return helpStay
		}
		h.close()
		return helpClose
	case "q", "?", "enter":
		h.close()
		return helpClose
	case "/":
		h.searching = true
	case "up", "k":
		h.scrollBy(-1, ctx)
	case "down", "j":
		h.scrollBy(1, ctx)
	case "pgup", "ctrl+u":
		h.scrollBy(-h.page(ctx), ctx)
	case "pgdown", "ctrl+d":
		h.scrollBy(h.page(ctx), ctx)
	case "g", "home":
		h.scroll = 0
	case "G", "end":
		h.scroll = h.scrollLimit(ctx)
	}
	return helpStay
}

// updateSearch types into the search. Scrolling stays live while it is up,
// so a query with more hits than the card can show remains readable.
func (h *helpState) updateSearch(msg tea.KeyMsg, ctx helpContext) {
	switch msg.String() {
	case "enter":
		h.searching = false
	case "esc":
		h.searching = false
		h.query = ""
		h.scroll = 0
	case "backspace":
		if runes := []rune(h.query); len(runes) > 0 {
			h.query = string(runes[:len(runes)-1])
			h.scroll = 0
		}
	case "up":
		h.scrollBy(-1, ctx)
	case "down":
		h.scrollBy(1, ctx)
	case "pgup":
		h.scrollBy(-h.page(ctx), ctx)
	case "pgdown":
		h.scrollBy(h.page(ctx), ctx)
	default:
		switch msg.Type {
		case tea.KeyRunes:
			h.query += string(msg.Runes)
			h.scroll = 0
		case tea.KeySpace:
			h.query += " "
			h.scroll = 0
		}
	}
}

func (m *Model) openHelp() {
	m.helpReturnMode = m.mode
	scope := helpGlobal
	if m.mode == modeDiff {
		scope = helpReview
	}
	m.help = helpState{scope: scope}
	m.mode = modeHelp
}

func (m *Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.help.update(msg, m.helpContext()) {
	case helpQuit:
		return m, tea.Quit
	case helpClose:
		m.mode = m.helpReturnMode
		m.helpReturnMode = modeList
		return m, m.startStartupTick()
	default:
		return m, nil
	}
}
