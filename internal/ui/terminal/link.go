// Package terminal holds what takes over the terminal or the desktop: opening
// a link, serving the link page, running a child in the terminal, and the
// alternate-scroll reset the manager asks for.
package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Seams are the side effects a link reaches. The root passes its own, so a
// test keeps the desktop and the clipboard out of the run.
type Seams struct {
	// Open is the browser a link opens in.
	Open func(target string) error
	// Opener starts the platform's URL handler for a link the focused pane
	// reported.
	Opener func(url string) error
	// Copy is the clipboard the URL falls back to when nothing opens it.
	Copy func(target string) error
	// Remote reports a terminal at the far end of an SSH session, where a
	// browser started on this host would open out of the user's sight.
	Remote func() bool
}

// BrowserOpenMsg reports a link the browser opened, or the copy that stood in
// for it.
type BrowserOpenMsg struct {
	Target  string
	Err     error
	CopyErr error
}

// LinkOpenErrMsg carries a link the opener refused.
type LinkOpenErrMsg struct{ Err error }

// OpenLink opens target in the browser, leaving the URL on the clipboard when
// nothing opens it. Over SSH it shows the page instead.
func OpenLink(target string, seams Seams) tea.Cmd {
	return func() tea.Msg {
		if seams.Remote() {
			return LinkPageMsg{URL: target}
		}
		err := seams.Open(target)
		if err == nil {
			return BrowserOpenMsg{Target: target}
		}
		return BrowserOpenMsg{Target: target, Err: err, CopyErr: seams.Copy(target)}
	}
}

// OpenLinkCmd opens a URL the focused pane reported, and fails into the error
// bar rather than the clipboard: the pane's link is not what the user typed.
func OpenLinkCmd(target string, seams Seams) tea.Cmd {
	return func() tea.Msg {
		if seams.Remote() {
			return LinkPageMsg{URL: target}
		}
		if err := seams.Opener(target); err != nil {
			return LinkOpenErrMsg{Err: fmt.Errorf("open link: %w", err)}
		}
		return nil
	}
}

// DefaultOpenBrowser opens target in the desktop's browser, trying every
// candidate in $BROWSER before the platform's own opener.
func DefaultOpenBrowser(target string) error {
	return OpenBrowserWith(runtime.GOOS, os.Getenv("BROWSER"), target, func(cmd *exec.Cmd) error {
		return cmd.Run()
	})
}

// DefaultOpener starts the platform's URL handler and returns without
// waiting for it.
func DefaultOpener(url string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, url).Start()
}

func OpenBrowserWith(goos, browser, target string, run func(*exec.Cmd) error) error {
	commands := BrowserCommands(goos, browser, target)
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

func BrowserCommands(goos, browser, target string) []*exec.Cmd {
	if goos == "darwin" {
		return []*exec.Cmd{exec.Command("open", target)}
	}
	if goos == "windows" {
		return []*exec.Cmd{exec.Command("rundll32", "url.dll,FileProtocolHandler", target)}
	}

	var commands []*exec.Cmd
	// Keep candidates as argv so shell syntax in a URL remains inert text.
	for _, line := range strings.Split(browser, ":") {
		argv := splitArgs(line)
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
