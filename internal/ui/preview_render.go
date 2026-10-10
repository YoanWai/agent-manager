package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"regexp"
	"strings"
)

// previewDangerSeqs strips capture sequences that would scroll or clear the
// outer manager terminal when embedded in View output: erase (K/J), scroll
// (S/T), insert/delete lines (L/M), set scroll region (r), and the 7-bit
// index / reverse-index / next-line controls (D/M/E).
var previewDangerSeqs = regexp.MustCompile(
	`\x1b\[[0-9;]*[KJLMSTr]|\x1b[DEM]`,
)

func previewLine(line string, width int) string {
	line = strings.ReplaceAll(line, cursorAnchorMarker, "")
	line = previewDangerSeqs.ReplaceAllString(line, "")
	line = strings.Map(func(r rune) rune {
		if r < 0x20 && r != 0x1b && r != '\t' {
			return -1
		}
		return r
	}, line)
	w := ansi.StringWidth(line)
	if w > width {
		line = ansi.Truncate(line, width, "")
		w = ansi.StringWidth(line)
	}
	// Reset before padding so an open background from the agent does not
	// paint the rest of the column.
	if strings.ContainsRune(line, 0x1b) {
		line += "\x1b[0m"
	}
	if w < width {
		line += strings.Repeat(" ", width-w)
	}
	return line
}

// expandPaneTabs writes a captured row's tabs out as the spaces the pane
// already painted. tmux serializes a tab as the single byte even though it
// spans the cells up to the next eight-column stop, so a row that keeps one
// measures narrower than it paints and overflows its column. The stops
// count from the row's start, which is pane column zero; escape sequences
// hold no column. A tab that would reach past the row's last cell stops on
// it, which is where tmux leaves the cursor.
func expandPaneTabs(line string, width int) string {
	if !strings.ContainsRune(line, '\t') {
		return line
	}
	const tabStop = 8
	var out strings.Builder
	column := 0
	for i, segment := range strings.Split(line, "\t") {
		if i > 0 {
			pad := max(min(tabStop-column%tabStop, width-1-column), 0)
			out.WriteString(strings.Repeat(" ", pad))
			column += pad
		}
		out.WriteString(segment)
		column += ansi.StringWidth(segment)
	}
	return out.String()
}

// paneWindow picks the half-open row range of a capture the panel shows.
// A pane is left taller than the panel on purpose, since shrinking it
// costs agents like Codex their whole scrollback (#369), so the window
// drops a blank tail instead. A completely blank, not-yet-painted pane
// keeps its rows hit-testable, and a live caret keeps its row in view.
func paneWindow(pane string, n, caretRow int) (lines []string, start int) {
	if n <= 0 || pane == "" {
		return nil, 0
	}
	// capture-pane often ends with a trailing newline; drop only that.
	lines = strings.Split(strings.TrimSuffix(pane, "\n"), "\n")
	end := len(lines)
	for end > 0 && blankPaneRow(lines[end-1]) {
		end--
	}
	if end == 0 {
		end = min(n, len(lines))
	}
	if caretRow+1 > end {
		end = caretRow + 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	start = end - n
	if start < 0 {
		start = 0
	}
	return lines[start:end], start
}

func blankPaneRow(line string) bool {
	return strings.TrimSpace(ansi.Strip(line)) == ""
}

// paneExact returns up to n lines of pane text as the pane painted them,
// preserving blank rows so a full-screen agent TUI looks the same in the
// preview. caretRow is the capture row the live caret sits on, or -1 when
// none is in play. Rows arrive here before anything measures them, so this
// is where a tab becomes the cells it covers and the frame, the caret and
// the selection all count one set of columns.
func paneExact(pane string, n, width, caretRow int) []string {
	lines, _ := paneWindow(pane, n, caretRow)
	for i, line := range lines {
		lines[i] = expandPaneTabs(line, width)
	}
	return lines
}

func padToHeight(s string, height int) string {
	missing := height - lipgloss.Height(s)
	if missing > 0 {
		s += strings.Repeat("\n", missing)
	}
	return s
}
