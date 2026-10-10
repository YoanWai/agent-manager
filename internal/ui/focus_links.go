package ui

import (
	"fmt"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
)

var openURL = func(url string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, url).Start()
}

type linkOpenErrMsg struct{ err error }

func openLinkCmd(url string) tea.Cmd {
	return func() tea.Msg {
		if remoteTerminal() {
			return linkPageMsg{url: url}
		}
		if err := openURL(url); err != nil {
			return linkOpenErrMsg{err: fmt.Errorf("open link: %w", err)}
		}
		return nil
	}
}
