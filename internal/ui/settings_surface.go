package ui

import (
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

// settingsSurface is the Settings card. It owns the dialog; the cache, the
// pending count and the generation stay on the root because the spawn form
// and the quick bar read the cache too. Closing it by esc, the save button
// or ctrl+c is the same pop, and Leave queues the save; Update starts it.
type settingsSurface struct {
	state settingsState
	root  settingsHost
}

// settingsHost is what Settings reads and does on the root beyond
// surfaceHost, whose cap leaves no room: the live key tables and update
// state a key reads, the work a key asks for, what closing saves and the
// card chrome.
type settingsHost interface {
	settingsContext() uisettings.Context
	// applySettingsResult runs what a key asked beyond the dialog's own
	// state and says whether the card closes.
	applySettingsResult(d *settingsState, result uisettings.Result) (cmd tea.Cmd, closes bool)
	// leaveSettings applies and queues the save of what the card chose.
	leaveSettings(d *settingsState)
	paintSettings(d *settingsState) string
}

var _ settingsHost = (*Model)(nil)

func newSettingsSurface(root settingsHost, state settingsState) *settingsSurface {
	return &settingsSurface{state: state, root: root}
}

func (s *settingsSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	return true, s.apply(h, s.state.Key(msg, s.root.settingsContext()))
}

// Click is given the painted line and not the column. Column 0 stands for
// every click, so the save button and the symbols row act on any press on
// their line, where they used to act only over their own text.
func (s *settingsSurface) Click(h surfaceHost, line int) tea.Cmd {
	return s.apply(h, s.state.Click(0, line, s.root.settingsContext()))
}

func (s *settingsSurface) Wheel(h surfaceHost, arrow tea.KeyMsg) tea.Cmd {
	step := 1
	if arrow.Type == tea.KeyUp {
		step = -1
	}
	return s.apply(h, s.state.Wheel(step, s.root.settingsContext()))
}

// apply runs a result, and closes the card when it asked to.
func (s *settingsSurface) apply(h surfaceHost, result uisettings.Result) tea.Cmd {
	cmd, closes := s.root.applySettingsResult(&s.state, result)
	if !closes {
		return cmd
	}
	h.pop()
	return nil
}

func (s *settingsSurface) View(surfaceHost, string) string { return s.root.paintSettings(&s.state) }

// Footer is empty: the card paints its own hints.
func (s *settingsSurface) Footer() [][2]string { return nil }

func (s *settingsSurface) Owner() inputOwner { return ownerManager }

// Dismissible: a command or a prompt being typed is a draft, so a stray
// click or ctrl+c keeps it.
func (s *settingsSurface) Dismissible() bool {
	return !s.state.Editor.Typing() && !s.state.Prompts.Editing()
}

func (s *settingsSurface) Leave(surfaceHost) { s.root.leaveSettings(&s.state) }
