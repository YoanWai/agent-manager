package dialogs

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// LaunchHint is a refused launch's fix as the setup-step dialog shows it:
// why the launch stopped and the command that unblocks it. Command is empty
// when the manager knows no recipe, which leaves the dialog read-only. The
// root holds the launch to retry, the images it names and the install.
type LaunchHint struct {
	Text    string
	Command string
}

// LaunchHintAction is what a key asks the root to do.
type LaunchHintAction uint8

const (
	LaunchHintStay LaunchHintAction = iota
	LaunchHintQuit
	LaunchHintCopy
	LaunchHintInstall
	LaunchHintClose
)

// Click is a press on line of the card's body: the paragraph that holds the
// command copies it, the same as c.
func (d LaunchHint) Click(line, inner int) LaunchHintAction {
	if d.Command == "" {
		return LaunchHintStay
	}
	// Wrapping breaks at spaces and after hyphens, so the paragraph is
	// matched with its spaces dropped.
	command := strings.ReplaceAll(d.Command, " ", "")
	top := 0
	for _, lines := range d.paragraphs(inner) {
		held := strings.Contains(strings.ReplaceAll(strings.Join(lines, ""), " ", ""), command)
		if held && line >= top && line < top+len(lines) {
			return LaunchHintCopy
		}
		top += len(lines) + 1
	}
	return LaunchHintStay
}

// paragraphs are the text's paragraphs wrapped to inner columns, which the
// card sets apart by a blank line.
func (d LaunchHint) paragraphs(inner int) [][]string {
	var out [][]string
	for _, paragraph := range strings.Split(d.Text, "\n\n") {
		out = append(out, strings.Split(ansi.Wordwrap(paragraph, inner, "-"), "\n"))
	}
	return out
}

func (d LaunchHint) Key(msg tea.KeyMsg) LaunchHintAction {
	switch msg.String() {
	case "ctrl+c":
		return LaunchHintQuit
	case "c":
		if d.Command == "" {
			return LaunchHintStay
		}
		return LaunchHintCopy
	case "i":
		if d.Command == "" {
			return LaunchHintStay
		}
		return LaunchHintInstall
	case "esc", "q", "enter":
		return LaunchHintClose
	}
	return LaunchHintStay
}

// View sets the first paragraph in tone and the rest muted, wrapped to the
// card's inner width.
func (d LaunchHint) View(inner int, tone, muted lipgloss.Style, glyphs presentation.GlyphSet) Card {
	var body strings.Builder
	for i, lines := range d.paragraphs(inner) {
		style := muted
		if i == 0 {
			style = tone
		}
		if i > 0 {
			body.WriteString("\n")
		}
		for _, line := range lines {
			body.WriteString(style.Render(line) + "\n")
		}
	}
	hint := [][2]string{{"esc", "close"}}
	if d.Command != "" {
		hint = [][2]string{{"i", "install"}, {"c", "copy"}, {"esc", "close"}}
	}
	return Card{Title: glyphs.SetupGlyph + " Session needs a setup step", Body: strings.TrimRight(body.String(), "\n"), Hints: hint}
}
