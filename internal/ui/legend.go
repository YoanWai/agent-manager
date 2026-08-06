package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// A legend is the app's key map made visible: tiers of bindings, each tier
// named for what its keys act on, so the footer answers "what can I do to
// the thing under the cursor" before it answers "what keys exist".
const (
	// legendTitleColumn keeps every tier's first binding on one column, so
	// stacked tiers read as a table rather than as ragged prose.
	legendTitleColumn = 10
	legendGap         = 2
	// legendMaxRows is the footer's height budget. Past it the tail is cut
	// and marked; the full map is one ? away.
	legendMaxRows = 3
)

// legendSection is one tier: its title, its bindings, and whether it is a
// secondary tier that recedes behind the tier above it.
type legendSection struct {
	title string
	pairs [][2]string
	quiet bool
}

// legendBar renders a legend as the app's footer, one tier per line where
// the terminal allows it and the tail marked when it does not.
func legendBar(sections []legendSection, width int) string {
	indent := strings.Repeat(" ", railGutter)
	cont := indent + strings.Repeat(" ", legendTitleColumn)
	sep := subtleStyle.Render(" · ")
	more := subtleStyle.Render("…")

	var out []string
	for _, section := range sections {
		if len(section.pairs) == 0 || len(out) >= legendMaxRows {
			continue
		}
		head := indent + padRight(legendTitleStyle.Render(section.title), legendTitleColumn)
		line, lineWidth, started := head, ansi.StringWidth(head), false
		cut := false
		for _, pair := range section.pairs {
			part, gap := keyCap(pair[0], pair[1]), strings.Repeat(" ", legendGap)
			if section.quiet {
				part, gap = keyCapQuiet(pair[0], pair[1]), sep
			}
			partWidth := ansi.StringWidth(part) + ansi.StringWidth(gap)
			switch {
			case !started:
				line, lineWidth, started = line+part, lineWidth+ansi.StringWidth(part), true
			case lineWidth+partWidth <= width:
				line += gap + part
				lineWidth += partWidth
			case len(out) < legendMaxRows-1:
				out = append(out, line)
				line, lineWidth = cont+part, ansi.StringWidth(cont)+ansi.StringWidth(part)
			default:
				cut = true
			}
			if cut {
				break
			}
		}
		if cut {
			line += " " + more
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// legendInline renders one tier as a single untitled run, for the foot of a
// modal card where the tier's subject is the card's own title.
func legendInline(pairs [][2]string, width int) string {
	gap := strings.Repeat(" ", legendGap)
	var lines []string
	var parts []string
	lineWidth := 0
	for _, pair := range pairs {
		part := keyCap(pair[0], pair[1])
		partWidth := ansi.StringWidth(part)
		if len(parts) > 0 && lineWidth+legendGap+partWidth > width {
			lines = append(lines, strings.Join(parts, gap))
			parts, lineWidth = nil, 0
		}
		if len(parts) > 0 {
			lineWidth += legendGap
		}
		parts = append(parts, part)
		lineWidth += partWidth
	}
	lines = append(lines, strings.Join(parts, gap))
	return strings.Join(lines, "\n")
}
