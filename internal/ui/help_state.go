package ui

import "github.com/YoanWai/agent-manager/internal/keybind"

type helpState struct {
	scroll    int
	query     string
	searching bool
	scope     helpScope
}

type helpScope uint8

const (
	helpGlobal helpScope = iota
	helpReview
)

type helpContext struct {
	sessionKeys keybind.Table
	listKeys    keybind.Table
	arrowStep   bool
	width       int
	height      int
	status      string
}

type helpAction uint8

const (
	helpStay helpAction = iota
	helpClose
	helpQuit
)

func (m *Model) helpContext() helpContext {
	status := ""
	if m.errBar.text != "" {
		status = m.statusMessage("⚠", "●", "▲")
	}
	return helpContext{
		sessionKeys: m.services.keys,
		listKeys:    m.services.listKeys,
		arrowStep:   m.prefs.arrowStep,
		width:       m.width,
		height:      m.height,
		status:      status,
	}
}
