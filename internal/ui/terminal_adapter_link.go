package ui

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/termseq"
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	tea "github.com/charmbracelet/bubbletea"
)

// Seams keep link tests from mutating the desktop and clipboard.
var (
	openBrowser    = uiterminal.DefaultOpenBrowser
	copyBrowserURL = clipboard.WriteText
	openURL        = uiterminal.DefaultOpener
)

// remoteTerminal is swapped by tests to pin the session to one side of SSH.
var remoteTerminal = termseq.Remote

// The routing and the error bar read these names, so the package's messages
// arrive in the root's own shapes.
type (
	browserOpenMsg struct {
		target  string
		err     error
		copyErr error
	}
	linkPageMsg    struct{ url string }
	linkOpenErrMsg struct{ err error }
)

func linkSeams() uiterminal.Seams {
	return uiterminal.Seams{
		Open:   func(target string) error { return openBrowser(target) },
		Opener: func(url string) error { return openURL(url) },
		Copy:   func(target string) error { return copyBrowserURL(target) },
		Remote: func() bool { return remoteTerminal() },
	}
}

func openLink(target string) tea.Cmd { return terminalMsgCmd(uiterminal.OpenLink(target, linkSeams())) }

func openLinkCmd(url string) tea.Cmd { return terminalMsgCmd(uiterminal.OpenLinkCmd(url, linkSeams())) }

// terminalMsgCmd turns a link's outcome into the message the root routes.
func terminalMsgCmd(cmd tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		switch msg := cmd().(type) {
		case uiterminal.BrowserOpenMsg:
			return browserOpenMsg{target: msg.Target, err: msg.Err, copyErr: msg.CopyErr}
		case uiterminal.LinkPageMsg:
			return linkPageMsg{url: msg.URL}
		case uiterminal.LinkOpenErrMsg:
			return linkOpenErrMsg{err: msg.Err}
		default:
			return msg
		}
	}
}

func (m *Model) handleBrowserOpen(msg browserOpenMsg) {
	if msg.err == nil {
		return
	}
	if msg.copyErr == nil {
		m.reportErr(fmt.Sprintf("could not open link; URL copied to clipboard: %v", msg.err))
		return
	}
	m.reportErr(fmt.Sprintf("could not open %s: %v; copying URL: %v", msg.target, msg.err, msg.copyErr))
}
