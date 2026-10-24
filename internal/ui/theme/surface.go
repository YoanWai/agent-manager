package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The list view sits on the backdrop, with the sessions rail filling its
// pane flush — header rule to footer rule, window edge to seam — and the
// selected entry lifted once more. Views draw the backdrop as unpainted
// cells; FillBackdrop paints them, text color included, over the finished
// frame, and the terminal color sync keeps the window padding around the
// cell grid at the same tone. With the terminal background setting the
// cells stay unpainted, so a translucent window blends them exactly like
// its padding.

// BackdropHex is the backdrop's fill while a view draws: none. Paint treats
// it as "pad, but leave the terminal's background alone".
func BackdropHex() string { return "" }

// PanelHex is the rail's fill: one step above the backdrop.
func PanelHex() string { return Mix(Current().Bg, Current().Surface, 0.55) }

// BlockHex is a section block inside the content area, quieter than the
// rail so it groups without announcing itself.
func BlockHex() string { return Mix(Current().Bg, Current().Surface, 0.35) }

// SelectedHex is the band under the cursor's entry.
func SelectedHex() string { return Current().Surface }

func QuickModeHex() string { return PanelHex() }

// SearchFieldHex is the band under the open search field.
func SearchFieldHex() string { return Mix(PanelHex(), Current().Accent, 0.2) }

// LiftedHex is the band under a row lifted for reordering: the selection
// tone pulled toward the accent, so the row reads as picked up.
func LiftedHex() string { return Mix(Current().Surface, Current().Accent, 0.3) }

// DropHex is the band under the row a drag would land on: the selection
// tone pulled toward the second accent, apart from the lifted row's.
func DropHex() string { return Mix(Current().Surface, Current().Accent2, 0.3) }

// RestingMarkHex is a row handle's and menu button's tone away from the
// cursor: present, but quieter than the row's text.
func RestingMarkHex() string { return Mix(Current().Bg, Current().Text, 0.3) }

// RuleHex is the hairline tone: lifted just far enough off the backdrop to
// draw a seam without becoming a border.
func RuleHex() string { return Mix(Current().Bg, Current().Text, 0.22) }

// Hrule is a horizontal seam across a painted row.
func Hrule(width int) string {
	if width < 1 {
		return ""
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(RuleHex())).Render(strings.Repeat("─", width))
}

// Paint pads a possibly-styled line to an exact width and fills every cell
// with bg. Inner SGR resets emitted by per-segment renders would drop the
// fill partway across, so each reset re-applies it.
func Paint(s string, width int, bg string) string {
	if bg == "" {
		return Plain(s, width)
	}
	fill := BgSeq(bg)
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+fill)
	w := ansi.StringWidth(s)
	if w > width {
		s = ansi.Truncate(s, width, "…")
		w = ansi.StringWidth(s)
	}
	if w < width {
		s += strings.Repeat(" ", width-w)
	}
	return fill + s + "\x1b[0m"
}

// Plain pads a line to width without filling it, leaving it on the backdrop.
// Captured agent output is drawn this way so a session's CLI looks exactly
// as it does inside the session.
func Plain(s string, width int) string {
	w := ansi.StringWidth(s)
	if w > width {
		s = ansi.Truncate(s, width, "")
		w = ansi.StringWidth(s)
	}
	if w < width {
		s += strings.Repeat(" ", width-w)
	}
	return "\x1b[0m" + s + "\x1b[0m"
}

// FillBackdrop paints the theme's background and text color into every
// cell the frame left on the terminal's own and pads each row to width, so
// the frame keeps its colors on a terminal that ignored the OSC 10 and 11
// sync.
func FillBackdrop(frame string, width int, backgroundHex, foregroundHex string) string {
	fill, ink := BgSeq(backgroundHex), FgSeq(foregroundHex)
	parser := ansi.GetParser()
	defer ansi.PutParser(parser)
	lines := strings.Split(frame, "\n")
	for i, line := range lines {
		lines[i] = fillBackdropLine(line, width, fill, ink, parser)
	}
	return strings.Join(lines, "\n")
}

func fillBackdropLine(line string, width int, fill, ink string, parser *ansi.Parser) string {
	var out strings.Builder
	out.WriteString(fill + ink)
	cells := 0
	var state byte
	for len(line) > 0 {
		seq, cellWidth, n, next := ansi.DecodeSequence(line, state, parser)
		out.WriteString(seq)
		if ansi.HasCsiPrefix(seq) && parser.Command() == 'm' {
			background, foreground := clearedColors(parser.Params())
			if background {
				out.WriteString(fill)
			}
			if foreground {
				out.WriteString(ink)
			}
		}
		cells += cellWidth
		line, state = line[n:], next
	}
	if cells < width {
		out.WriteString("\x1b[0m" + fill + ink + strings.Repeat(" ", width-cells))
	}
	out.WriteString("\x1b[0m")
	return out.String()
}

// clearedColors reports which of the terminal's own colors an SGR leaves
// showing once all of its parameters apply.
func clearedColors(params ansi.Params) (background, foreground bool) {
	if len(params) == 0 {
		return true, true
	}
	for i := 0; i < len(params); i += 1 + parameterArguments(params, i) {
		switch param, _, _ := params.Param(i, 0); {
		case param == 0:
			background, foreground = true, true
		case param == 49:
			background = true
		case param == 39:
			foreground = true
		case param >= 40 && param <= 48, param >= 100 && param <= 107:
			background = false
		case param >= 30 && param <= 38, param >= 90 && param <= 97:
			foreground = false
		}
	}
	return background, foreground
}

// parameterArguments counts the parameters that belong to the one at index
// at: its colon subparameters (4:3, 48:2::r:g:b), or the semicolon arguments
// of an extended color (38;5;n, 48;2;r;g;b, 58;5;n).
func parameterArguments(params ansi.Params, at int) int {
	count := 0
	for {
		_, subparameter, _ := params.Param(at+count, 0)
		if !subparameter {
			break
		}
		count++
	}
	if count > 0 {
		return count
	}
	switch param, _, _ := params.Param(at, 0); param {
	case 38, 48, 58:
		switch kind, _, _ := params.Param(at+1, 0); kind {
		case 5:
			return 2
		case 2:
			return 4
		}
	}
	return 0
}

// PaintRows paints each line of a block, padding the block itself out to
// height so a short column still fills its side of the frame.
func PaintRows(lines []string, width, height int, bg string) []string {
	out := make([]string, height)
	for i := 0; i < height; i++ {
		content := ""
		if i < len(lines) {
			content = lines[i]
		}
		out[i] = Paint(content, width, bg)
	}
	return out
}

// JoinColumns stitches painted columns row by row.
func JoinColumns(columns ...[]string) []string {
	height := 0
	for _, col := range columns {
		if len(col) > height {
			height = len(col)
		}
	}
	rows := make([]string, height)
	for i := 0; i < height; i++ {
		var b strings.Builder
		for _, col := range columns {
			if i < len(col) {
				b.WriteString(col[i])
			}
		}
		rows[i] = b.String()
	}
	return rows
}

// IndentLines insets a block by n columns.
func IndentLines(lines []string, n int) []string {
	pad := strings.Repeat(" ", n)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = pad + line
	}
	return out
}

// SplitLines splits a rendered block into lines, treating the empty string
// as no lines rather than one blank line.
func SplitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
