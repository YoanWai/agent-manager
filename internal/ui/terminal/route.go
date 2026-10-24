package terminal

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
