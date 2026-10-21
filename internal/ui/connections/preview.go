package connections

import (
	"regexp"
	"strings"
	"unicode/utf8"

	remote "github.com/YoanWai/agent-manager/internal/remote/value"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Preview is the selected remote row's screen as its last read returned
// it. gen fences reads against the selection they were made for.
type Preview struct {
	ref      remote.Ref
	terminal bool
	text     string
	err      string
	gen      uint64
	reading  bool
}

// Read is one read of a remote screen the root runs off the update path.
type Read struct {
	Ref      remote.Ref
	Terminal bool
	Gen      uint64
}

// Follow tracks the selected row, and reports whether it is an agent or
// terminal not read yet. Anything else clears the preview.
func (p *Preview) Follow(row Row, ok bool) bool {
	if !ok || row.Kind != SessionRow {
		*p = Preview{gen: p.gen + 1}
		return false
	}
	if p.ref == row.Ref {
		return false
	}
	*p = Preview{ref: row.Ref, terminal: row.Terminal, gen: p.gen + 1}
	return true
}

// StartRead claims the next read of the tracked row, unless there is none,
// one is already out, or its host is offline.
func (c *Connections) StartRead() (Read, bool) {
	p := &c.Preview
	if p.ref.ID == "" || p.reading {
		return Read{}, false
	}
	if _, offline := c.Offline(p.ref.Host); offline {
		return Read{}, false
	}
	p.reading = true
	return Read{Ref: p.ref, Terminal: p.terminal, Gen: p.gen}, true
}

// Apply lands a read, unless the selection moved since it started.
func (p *Preview) Apply(read Read, text string, err error) {
	if read.Gen != p.gen || read.Ref != p.ref {
		return
	}
	p.reading = false
	if err != nil {
		p.err = err.Error()
		return
	}
	p.text, p.err = text, ""
}

// ScreenView is the root's styles and its tab expansion for pane rows.
type ScreenView struct {
	Err, Muted lipgloss.Style
	ExpandTabs func(line string, width int) string
}

// Screen is the last read as plain text, its bottom rows kept the way a
// local preview keeps them.
func (p *Preview) Screen(width, height int, view ScreenView) []string {
	if height <= 0 {
		return []string{}
	}
	if p.err != "" {
		return []string{view.Err.Render(ansi.Truncate(presentation.EscapeControlsInline(p.err), width, "…"))}
	}
	text := strings.TrimRight(p.text, "\n ")
	if text == "" {
		if p.reading {
			return []string{view.Muted.Render("reading the screen over SSH…")}
		}
		return []string{view.Muted.Render("(no output yet)")}
	}
	lines := strings.Split(ScreenText(text), "\n")
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(view.ExpandTabs(line, width), width, "")
		// A color the host left set ends with its row, as it does there.
		if strings.Contains(lines[i], "\x1b[") {
			lines[i] += "\x1b[m"
		}
	}
	return lines
}

// sgr is one Select Graphic Rendition sequence: colors and text attributes,
// which move no cursor and reach no other terminal feature.
var sgr = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// ScreenText is a remote screen as the preview paints it: its colors kept,
// every other escape sequence stripped, and every control but a newline or
// tab dropped, since a bare \r or \b moves the cursor of the terminal the
// frame paints into.
func ScreenText(text string) string {
	var out strings.Builder
	last := 0
	for _, at := range sgr.FindAllStringIndex(text, -1) {
		out.WriteString(plainText(text[last:at[0]]))
		out.WriteString(text[at[0]:at[1]])
		last = at[1]
	}
	out.WriteString(plainText(text[last:]))
	return out.String()
}

func plainText(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if (r < utf8.RuneSelf && presentation.IsControlByte(byte(r))) || presentation.IsEscapedRune(r) {
			return -1
		}
		return r
	}, ansi.Strip(text))
}
