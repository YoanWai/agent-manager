package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

var (
	// Seams keep link tests from mutating the desktop and clipboard.
	openBrowser    = defaultOpenBrowser
	copyBrowserURL = clipboard.WriteText
)

func defaultOpenBrowser(target string) error {
	return openBrowserWith(runtime.GOOS, os.Getenv("BROWSER"), target, func(cmd *exec.Cmd) error {
		return cmd.Run()
	})
}

func openBrowserWith(goos, browser, target string, run func(*exec.Cmd) error) error {
	commands := browserCommands(goos, browser, target)
	var failures []error
	for _, cmd := range commands {
		err := run(cmd)
		if err == nil {
			return nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", cmd.Args[0], err))
	}
	return errors.Join(failures...)
}

func browserCommands(goos, browser, target string) []*exec.Cmd {
	if goos == "darwin" {
		return []*exec.Cmd{exec.Command("open", target)}
	}
	if goos == "windows" {
		return []*exec.Cmd{exec.Command("rundll32", "url.dll,FileProtocolHandler", target)}
	}

	var commands []*exec.Cmd
	// Keep candidates as argv so shell syntax in a URL remains inert text.
	for _, line := range strings.Split(browser, ":") {
		argv := splitEditorLine(line)
		if len(argv) == 0 {
			continue
		}
		placed := false
		for i := range argv {
			if strings.Contains(argv[i], "%s") {
				argv[i] = strings.ReplaceAll(argv[i], "%s", target)
				placed = true
			}
		}
		if !placed {
			argv = append(argv, target)
		}
		commands = append(commands, exec.Command(argv[0], argv[1:]...))
	}
	return append(commands, exec.Command("xdg-open", target))
}

type browserOpenMsg struct {
	target  string
	err     error
	copyErr error
}

func openLink(target string) tea.Cmd {
	return func() tea.Msg {
		if remoteTerminal() {
			return linkPageMsg{url: target}
		}
		err := openBrowser(target)
		if err == nil {
			return browserOpenMsg{target: target}
		}
		return browserOpenMsg{target: target, err: err, copyErr: copyBrowserURL(target)}
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

var openURL = func(url string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
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
