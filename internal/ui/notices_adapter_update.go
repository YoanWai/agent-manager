package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// releaseRoot is the root as a routed release, feed or self-update result
// reaches it.
type releaseRoot struct{ *Model }

func (r releaseRoot) ApplyNotices(apply func()) { r.applyNotices(apply) }

func (r releaseRoot) KeepNoticeSelection(apply func()) { r.keepNoticeSelection(apply) }

func (r releaseRoot) ReportDone(text string) { r.reportDone(text) }

func (r releaseRoot) Quit() tea.Cmd {
	_, cmd := r.requestQuit()
	return cmd
}

func (m *Model) routeNoticesMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if msg, ok := msg.(browserOpenMsg); ok {
		m.handleBrowserOpen(msg)
		return routed(m, nil)
	}
	if cmd, ok := m.release.Route(releaseRoot{m}, msg); ok {
		return routed(m, cmd)
	}
	return nil, nil, false
}

// RestartPath is the binary main execs into after the program exits;
// empty when no self-update happened this run.
func (m *Model) RestartPath() string { return m.release.RestartPath }
