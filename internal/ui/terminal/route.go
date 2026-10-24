package terminal

import tea "github.com/charmbracelet/bubbletea"

// Notice is the line a refused link reports: a browser that would not open
// leaves the URL on the clipboard, and a clipboard that failed too says so.
func (m BrowserOpenMsg) Notice() string {
	if m.Err == nil {
		return ""
	}
	if m.CopyErr == nil {
		return "could not open link; URL copied to clipboard: " + m.Err.Error()
	}
	return "could not open " + m.Target + ": " + m.Err.Error() + "; copying URL: " + m.CopyErr.Error()
}

// RouteLink is what the root does with a queued link's outcome: the line to
// report, the page the terminal shows instead, or neither.
func RouteLink(msg tea.Msg) (notice string, follow tea.Cmd, ok bool) {
	switch msg := msg.(type) {
	case BrowserOpenMsg:
		return msg.Notice(), nil, true
	case LinkOpenErrMsg:
		return msg.Err.Error(), nil, true
	case LinkPageMsg:
		return "", ShowLinkPage(msg.URL), true
	}
	return "", nil, false
}
