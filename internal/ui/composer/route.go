package composer

import tea "github.com/charmbracelet/bubbletea"

// Root is what a paste or a sweep reaches on the manager: the prompt box a
// target names, whether that box is still on screen, and the status bar an
// error is reported on.
type Root interface {
	ComposerFor(target ID) *Composer
	ComposerOpen(target ID) bool
	ReportErr(text string)
	ClearErr()
}

// Update applies a message the prompt boxes own, reporting whether msg was
// one of theirs.
func Update(root Root, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case SweepMsg:
		if msg.Err != nil {
			root.ReportErr("clearing old pasted images: " + msg.Err.Error())
		}
		return nil, true

	case SweepTickMsg:
		return tea.Batch(SweepCmd, SweepTick), true

	case PasteImageMsg:
		return root.ComposerFor(msg.Target).AcceptImage(root, msg, root.ComposerOpen(msg.Target)), true

	case PasteTextMsg:
		return root.ComposerFor(msg.Target).AcceptText(msg, root.ComposerOpen(msg.Target)), true
	}
	return nil, false
}
