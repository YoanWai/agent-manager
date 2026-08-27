package ui

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// paneLinkRe finds web addresses in a captured row. Only the web schemes:
// a screen full of agent output must never click into file:, mailto: or
// anything a browser would hand to another program.
var paneLinkRe = regexp.MustCompile(`https?://\S+`)

// linkAt is the URL whose painted span covers the clicked cell, read from
// the same rows the renderer painted so columns line up with the screen.
// A URL the pane wrapped is joined across rows while each full row runs to
// the pane's edge.
func (m *Model) linkAt(row, col int) string {
	lines := m.paneTextLines()
	if row < 0 || row >= len(lines) {
		return ""
	}
	line := lines[row]
	clicked, _ := graphemeRangeAtColumns(line, col, col+1)
	for _, span := range paneLinkRe.FindAllStringIndex(line, -1) {
		if clicked < span[0] || clicked >= span[1] {
			continue
		}
		url := line[span[0]:span[1]]
		// A URL cut by the pane's edge continues at the head of the next
		// row; every full row of it runs edge to edge, so a row that ends
		// short of the edge, indents, or carries more words ends the join.
		current, end := line, span[1]
		for next := row + 1; end == len(current) &&
			ansi.StringWidth(current) >= m.pane.box.width && next < len(lines); next++ {
			rest := lines[next]
			if rest == "" || strings.HasPrefix(rest, " ") {
				break
			}
			fields := strings.Fields(rest)
			url += fields[0]
			if len(fields) > 1 || ansi.StringWidth(rest) < m.pane.box.width {
				break
			}
			current, end = rest, len(rest)
		}
		return trimLinkPunct(url)
	}
	return ""
}

// trimLinkPunct peels the prose the URL was quoted inside: the sentence's
// closing punctuation, and a bracket only when the URL never opened one.
func trimLinkPunct(url string) string {
	url = strings.TrimRight(url, `.,;:!?'"`)
	for _, pair := range [][2]string{{"(", ")"}, {"[", "]"}, {"<", ">"}} {
		for strings.HasSuffix(url, pair[1]) &&
			strings.Count(url, pair[1]) > strings.Count(url, pair[0]) {
			url = strings.TrimSuffix(url, pair[1])
			url = strings.TrimRight(url, `.,;:!?'"`)
		}
	}
	return url
}

// openURL hands a clicked link to the system opener; tests point it
// elsewhere.
var openURL = func(url string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, url).Start()
}

type linkOpenErrMsg struct{ err error }

// openLinkCmd opens the URL off the update path, surfacing a failure on
// the error bar rather than swallowing it.
func openLinkCmd(url string) tea.Cmd {
	return func() tea.Msg {
		if err := openURL(url); err != nil {
			return linkOpenErrMsg{err: fmt.Errorf("open link: %w", err)}
		}
		return nil
	}
}
