package ui

import (
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	tea "github.com/charmbracelet/bubbletea"
)

func showLinkPage(url string) tea.Cmd { return terminalMsgCmd(uiterminal.ShowLinkPage(url)) }
