package terminal

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

// copyToClipboard is the page's clipboard write, swapped by tests so a page
// test never reaches the user's clipboard.
var copyToClipboard = clipboard.WriteText

// LinkPageMsg asks for a link to be shown rather than opened, because a
// browser started on this host would open out of the user's sight.
type LinkPageMsg struct{ URL string }

func ShowLinkPage(url string) tea.Cmd {
	return tea.Exec(&LinkPage{URL: url}, func(err error) tea.Msg {
		if err != nil {
			return LinkOpenErrMsg{Err: fmt.Errorf("show link: %w", err)}
		}
		return nil
	})
}

// LinkPage prints a link on the plain terminal, with the manager's screen
// and mouse grab out of the way, so the user's own terminal can select or
// open it on the machine they sit at.
type LinkPage struct {
	URL    string
	stdin  io.Reader
	stdout io.Writer
}

func (p *LinkPage) SetStdin(r io.Reader)  { p.stdin = r }
func (p *LinkPage) SetStdout(w io.Writer) { p.stdout = w }
func (p *LinkPage) SetStderr(io.Writer)   {}

func (p *LinkPage) Run() error {
	clipboardNote := "It is on your clipboard too, if your terminal accepts clipboard writes."
	if err := copyToClipboard(p.URL); err != nil {
		clipboardNote = fmt.Sprintf("Copying it to your clipboard failed: %v", err)
	}
	if _, err := fmt.Fprintf(p.stdout, "\nOpen this link in a browser on your computer:\n\n%s\n\n%s\nPress Enter to go back.", p.URL, clipboardNote); err != nil {
		return err
	}
	_, err := bufio.NewReader(p.stdin).ReadString('\n')
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}
