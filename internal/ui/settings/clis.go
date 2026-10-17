package settings

import tea "github.com/charmbracelet/bubbletea"

func (s *State) openCLIPicker(ctx Context) {
	hidden := s.CLIHidden
	if hidden == nil {
		hidden = ctx.Tools.KnownHidden(ctx.Hidden)
	}
	s.CLIPicker = true
	s.CLINames = ctx.Tools.Agents
	s.CLIHidden = hidden
	s.CLICursor = 0
}

func (s *State) cliPickerKey(msg tea.KeyMsg, ctx Context) Result {
	// cursor 0..len(names)-1 = tools; len(names) = request-support action.
	count := len(s.CLINames) + 1
	if count < 1 {
		count = 1
	}
	switch msg.String() {
	case "up", "k":
		s.CLICursor = (s.CLICursor + count - 1) % count
	case "down", "j":
		s.CLICursor = (s.CLICursor + 1) % count
	case " ", "space":
		if s.CLICursor < len(s.CLINames) {
			return Result{Notice: s.toggleCLIHidden(s.CLINames[s.CLICursor])}
		}
	case "enter":
		if s.CLICursor >= len(s.CLINames) {
			return Result{Exit: RequestCLISupport}
		}
		return Result{Notice: s.toggleCLIHidden(s.CLINames[s.CLICursor])}
	case "esc":
		s.CLIPicker = false
		// Refresh the quick-spawn tool list so it matches the new filter.
		preferred := ""
		if len(s.ToolNames) > 0 && s.ToolIndex < len(s.ToolNames) {
			preferred = s.ToolNames[s.ToolIndex]
		}
		if preferred == "" {
			preferred = ctx.DefaultTool
		}
		s.ToolNames = ctx.Tools.Enabled(s.CLIHidden)
		s.ToolIndex = ToolIndex(s.ToolNames, preferred)
		return Result{SaveHidden: true}
	}
	return Result{}
}

// toggleCLIHidden flips visibility for one tool. At least one CLI must stay
// enabled so new sessions still have something to launch.
func (s *State) toggleCLIHidden(name string) Notice {
	if s.CLIHidden == nil {
		s.CLIHidden = map[string]bool{}
	}
	if s.CLIHidden[name] {
		delete(s.CLIHidden, name)
		s.Dirty = true
		return clearNotice()
	}
	enabled := 0
	for _, toolName := range s.CLINames {
		if !s.CLIHidden[toolName] {
			enabled++
		}
	}
	if enabled <= 1 {
		return errorNotice("keep at least one CLI enabled")
	}
	s.CLIHidden[name] = true
	s.Dirty = true
	return clearNotice()
}
