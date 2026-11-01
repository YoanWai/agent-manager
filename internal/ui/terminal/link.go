// Package terminal holds what takes over the terminal or the desktop: opening
// a link, serving the link page, running a child in the terminal, and the
// alternate-scroll reset the manager asks for.
package terminal

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"runtime"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// Seams are the side effects a link reaches. The root passes its own, so a
// test keeps the desktop and the clipboard out of the run.
type Seams struct {
	Open   func(target string) error
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
func DefaultOpener(target string) error {
	cmd, err := OpenerCommand(runtime.GOOS, target)
	if err != nil {
		return err
	}
	return StartReaped(cmd)
}

var windowsDrivePath = regexp.MustCompile(`^/[A-Za-z]:/`)

// OpenerCommand is what DefaultOpener runs on goos. A file link is shown in
// the file manager rather than opened, so a click never runs what an agent
// linked to.
func OpenerCommand(goos, target string) (*exec.Cmd, error) {
	link, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(link.Scheme, "file") {
		file, ok := localPath(goos, link)
		if !ok {
			return nil, errors.New("the file link does not name a file on this machine")
		}
		return revealCommand(goos, file)
	}
	switch goos {
	case "darwin":
		return exec.Command("open", target), nil
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target), nil
	default:
		return exec.Command("xdg-open", target), nil
	}
}

// localPath is the path a file link names on this machine. A path naming
// another host, as a UNC share does, is refused, since Windows would contact
// that host to look it up.
func localPath(goos string, link *url.URL) (string, bool) {
	if (link.Host != "" && link.Host != "localhost") || strings.HasPrefix(link.Path, "//") ||
		strings.IndexFunc(link.Path, unicode.IsControl) >= 0 {
		return "", false
	}
	if goos == "windows" {
		if !windowsDrivePath.MatchString(link.Path) || strings.ContainsAny(link.Path, `",;`) {
			return "", false
		}
		return strings.ReplaceAll(strings.TrimPrefix(link.Path, "/"), "/", `\`), true
	}
	if !strings.HasPrefix(link.Path, "/") {
		return "", false
	}
	return path.Clean(link.Path), true
}

// revealCommand shows file in the file manager. explorer reads its switches
// from commas in its one argument, which localPath keeps out of file. xdg-open
// has no reveal, so it opens the folder, and only one that is a folder here,
// since xdg-open runs whatever else it is handed.
func revealCommand(goos, file string) (*exec.Cmd, error) {
	switch goos {
	case "darwin":
		return exec.Command("open", "-R", file), nil
	case "windows":
		return exec.Command("explorer", "/select,"+file), nil
	default:
		folder := path.Dir(file)
		if info, err := os.Stat(folder); err != nil || !info.IsDir() {
			return nil, errors.New("the file link does not sit in a folder on this machine")
		}
		return exec.Command("xdg-open", folder), nil
	}
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
