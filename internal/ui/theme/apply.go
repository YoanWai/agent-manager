package theme

import tea "github.com/charmbracelet/bubbletea"

// Root is what a finished pane-theme push asks of the manager.
type Root interface {
	ReportErr(text string)
}

// ApplyPane reports a push that failed; the panes already up keep the
// palette they resolved at startup either way.
func ApplyPane(root Root, err error) tea.Cmd {
	if err != nil {
		root.ReportErr(err.Error())
	}
	return nil
}
