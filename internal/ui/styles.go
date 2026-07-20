package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	colorText    = lipgloss.Color("252")
	colorDim     = lipgloss.Color("245")
	colorSubtle  = lipgloss.Color("240")
	colorBorder  = lipgloss.Color("238")
	colorBg      = lipgloss.Color("236")
	colorSelBg   = lipgloss.Color("239")
	colorBright  = lipgloss.Color("231")
	colorAccent  = lipgloss.Color("111")
	colorAccent2 = lipgloss.Color("79")

	colorWorking  = lipgloss.Color("214")
	colorWaiting  = lipgloss.Color("213")
	colorFinished = lipgloss.Color("82")
	colorErrored  = lipgloss.Color("203")
	colorIdle     = lipgloss.Color("244")

	badgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBg).
			Background(colorAccent).
			Padding(0, 1)

	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	selectedRowStyle = lipgloss.NewStyle().Background(colorSelBg).Foreground(colorBright)

	// selectedRowReapply is the SGR sequence re-applied after every inner
	// SGR reset inside a selected row. lipgloss's .Render emits one reset
	// per call, so wrapping pre-styled content with selectedRowStyle leaves
	// the background set only on the first segment. renderSelectedRow
	// substitutes each inner reset with reset+reapply so the selected bg
	// holds across the whole row.
	selectedRowReapply = "\x1b[0m\x1b[48;5;" + string(colorSelBg) + "m\x1b[38;5;" + string(colorBright) + "m"

	mutedStyle  = lipgloss.NewStyle().Foreground(colorDim)
	subtleStyle = lipgloss.NewStyle().Foreground(colorSubtle)
	valueStyle  = lipgloss.NewStyle().Foreground(colorText)
	labelStyle  = lipgloss.NewStyle().Foreground(colorDim)
	errStyle    = lipgloss.NewStyle().Foreground(colorErrored).Bold(true)
	keyStyle    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
)

// renderSelectedRow wraps a pre-styled line with the selected row's
// background and foreground. Internal SGR resets emitted by per-segment
// lipgloss.Render calls (and padRight) would otherwise clear the outer
// background after the first segment, leaving only the bar glyph tinted.
// Re-applying the selected bg+fg after every reset keeps the row tinted
// end-to-end.
func renderSelectedRow(s string) string {
	return selectedRowStyle.Render(strings.ReplaceAll(s, "\x1b[0m", selectedRowReapply))
}

func statusColor(s string) lipgloss.Color {
	switch s {
	case status.Working:
		return colorWorking
	case status.Waiting:
		return colorWaiting
	case status.Finished:
		return colorFinished
	case status.Errored, status.Dead:
		return colorErrored
	default:
		return colorIdle
	}
}

func statusGlyph(s string) string {
	switch s {
	case status.Working:
		return "◐"
	case status.Waiting:
		return "?"
	case status.Finished:
		return "✔"
	case status.Errored, status.Dead:
		return "✖"
	default:
		return "○"
	}
}

// titledPanel draws a rounded box with the title embedded in the top
// border, its body clipped and padded to fill the given outer size.
func titledPanel(title, body string, width, height int) string {
	bs := lipgloss.NewStyle().Foreground(colorBorder)
	inner := width - 4
	if inner < 1 {
		inner = 1
	}
	bodyRows := height - 2
	if bodyRows < 1 {
		bodyRows = 1
	}

	titleText := sectionStyle.Render(title)
	dashCount := inner - ansi.StringWidth(title) - 1
	if dashCount < 0 {
		dashCount = 0
	}
	top := bs.Render("╭─ ") + titleText + " " + bs.Render(strings.Repeat("─", dashCount)+"╮")
	bottom := bs.Render("╰" + strings.Repeat("─", width-2) + "╯")

	side := bs.Render("│")
	lines := strings.Split(body, "\n")
	var b strings.Builder
	b.WriteString(top + "\n")
	for i := 0; i < bodyRows; i++ {
		content := ""
		if i < len(lines) {
			content = lines[i]
		}
		b.WriteString(side + " " + padRight(content, inner) + " " + side + "\n")
	}
	b.WriteString(bottom)
	return b.String()
}

// padRight pads or clips a possibly-styled string to an exact display width.
func padRight(s string, width int) string {
	w := ansi.StringWidth(s)
	if w > width {
		s = ansi.Truncate(s, width, "…")
		w = ansi.StringWidth(s)
	}
	if w < width {
		s += strings.Repeat(" ", width-w)
	}
	if strings.ContainsRune(s, 0x1b) {
		s += "\x1b[0m"
	}
	return s
}

// gauge renders a colored bar meter for a 0-100 percentage.
func gauge(percent float64, width int) string {
	if width < 1 {
		width = 1
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(percent/100*float64(width) + 0.5)
	color := colorFinished
	switch {
	case percent >= 85:
		color = colorErrored
	case percent >= 60:
		color = colorWorking
	}
	on := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("▰", filled))
	off := subtleStyle.Render(strings.Repeat("▱", width-filled))
	return on + off
}

// pill renders a small rounded label chip.
func pill(text string, fg lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(fg).Render("▏" + text)
}
