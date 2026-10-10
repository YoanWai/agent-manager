package ui

import (
	"net/url"

	tea "github.com/charmbracelet/bubbletea"
)

func (s *settingsFeature) openCLIPicker(h settingsHost) {
	cfg := h.toolConfig()
	names := cfg.AgentToolNames()
	hidden := s.dialog.cliHidden
	if hidden == nil {
		hidden = s.cachedHiddenTools(cfg)
	}
	s.dialog.cliPicker = true
	s.dialog.cliNames = names
	s.dialog.cliHidden = hidden
	s.dialog.cliCursor = 0
}

func (s *settingsFeature) handleCLIPickerKey(h settingsHost, msg tea.KeyMsg) tea.Cmd {
	// cursor 0..len(names)-1 = tools; len(names) = request-support action.
	count := len(s.dialog.cliNames) + 1
	if count < 1 {
		count = 1
	}
	switch msg.String() {
	case "up", "k":
		s.dialog.cliCursor = (s.dialog.cliCursor + count - 1) % count
	case "down", "j":
		s.dialog.cliCursor = (s.dialog.cliCursor + 1) % count
	case " ", "space":
		if s.dialog.cliCursor < len(s.dialog.cliNames) {
			s.toggleCLIHidden(h, s.dialog.cliNames[s.dialog.cliCursor])
		}
	case "enter":
		if s.dialog.cliCursor >= len(s.dialog.cliNames) {
			return openLink(requestCLISupportURL())
		}
		s.toggleCLIHidden(h, s.dialog.cliNames[s.dialog.cliCursor])
	case "esc":
		s.dialog.cliPicker = false
		// Refresh the quick-spawn tool list so it matches the new filter.
		preferred := ""
		if len(s.dialog.toolNames) > 0 && s.dialog.toolIndex < len(s.dialog.toolNames) {
			preferred = s.dialog.toolNames[s.dialog.toolIndex]
		}
		names, index := s.cachedToolSelection(h.toolConfig(), s.dialog.cliHidden, preferred)
		s.dialog.toolNames = names
		s.dialog.toolIndex = index
		return s.captureHiddenSave(h)
	}
	return nil
}

// toggleCLIHidden flips visibility for one tool. At least one CLI must stay
// enabled so new sessions still have something to launch.
func (s *settingsFeature) toggleCLIHidden(h settingsHost, name string) {
	if s.dialog.cliHidden == nil {
		s.dialog.cliHidden = map[string]bool{}
	}
	if s.dialog.cliHidden[name] {
		delete(s.dialog.cliHidden, name)
		s.dialog.dirty = true
		h.clearErr()
		return
	}
	enabled := 0
	for _, toolName := range s.dialog.cliNames {
		if !s.dialog.cliHidden[toolName] {
			enabled++
		}
	}
	if enabled <= 1 {
		h.reportErr("keep at least one CLI enabled")
		return
	}
	s.dialog.cliHidden[name] = true
	s.dialog.dirty = true
	h.clearErr()
}

// requestCLISupportURL opens a prefilled feature request for another CLI.
func requestCLISupportURL() string {
	body := "**What are you trying to do**\n\n" +
		"I want agent-manager to support another coding CLI.\n\n" +
		"**What you have in mind**\n\n" +
		"CLI name:\nHow to launch it:\nResume / session flags (if any):\n\n" +
		"**Area**\nConfig and tool support\n"
	return repoURL + "/issues/new?labels=enhancement&body=" + url.QueryEscape(body)
}
