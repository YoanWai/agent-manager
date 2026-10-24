package ui

import (
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
)

// noticesRoot is the root as the messages modal reads it.
type noticesRoot struct{ *Model }

func (r noticesRoot) ActiveNotices() []uinotices.Notice { return r.activeNotices() }

func (r noticesRoot) NoticesFrame() uinotices.Frame { return r.noticesFrame() }

func (r noticesRoot) RefreshNotices() tea.Cmd { return r.refreshNotices() }

func (r noticesRoot) StartUpdate() tea.Cmd { return r.startUpdate() }

func (r noticesRoot) DismissNotice(id string) { r.dismissNotice(id) }

func (r noticesRoot) PaintNoticesCard(card []string, foot []presentation.PaintedKey) string {
	return r.paintNoticesCard(card, foot)
}

func (r noticesRoot) TakeForeground() { r.takeForeground() }

func (r noticesRoot) OpenLink(url string) tea.Cmd { return openLink(url) }
