package ui

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/YoanWai/agent-manager/internal/termseq"
)

// remoteTerminal is swapped by tests to pin the session to one side of SSH.
var remoteTerminal = termseq.Remote

// linkPageMsg asks for a link to be shown rather than opened, because a
// browser started on this host would open out of the user's sight.
type linkPageMsg struct{ url string }

func showLinkPage(url string) tea.Cmd {
	return tea.Exec(&linkPage{url: url}, func(err error) tea.Msg {
		if err != nil {
			return linkOpenErrMsg{err: fmt.Errorf("show link: %w", err)}
		}
		return nil
	})
}

// linkPage prints a link on the plain terminal, with the manager's screen
// and mouse grab out of the way, so the user's own terminal can select or
// open it on the machine they sit at.
type linkPage struct {
	url    string
	stdin  io.Reader
	stdout io.Writer
}

func (p *linkPage) SetStdin(r io.Reader)  { p.stdin = r }
func (p *linkPage) SetStdout(w io.Writer) { p.stdout = w }
func (p *linkPage) SetStderr(io.Writer)   {}

func (p *linkPage) Run() error {
	clipboardNote := "It is on your clipboard too, if your terminal accepts clipboard writes."
	if err := copyBrowserURL(p.url); err != nil {
		clipboardNote = fmt.Sprintf("Copying it to your clipboard failed: %v", err)
	}
	if _, err := fmt.Fprintf(p.stdout, "\nOpen this link in a browser on your computer:\n\n%s\n\n%s\nPress Enter to go back.", p.url, clipboardNote); err != nil {
		return err
	}
	_, err := bufio.NewReader(p.stdin).ReadString('\n')
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
