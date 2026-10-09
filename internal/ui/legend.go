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
	legendTitleColumn = 11
	legendGap         = 2
	// legendMaxRows is the footer's height budget. Past it the tail is cut
	// and marked; the full map is one ? away.
	legendMaxRows = 3
)

// legendSection is one tier: its title, its bindings, and whether it is a
// secondary tier that recedes behind the tier above it.
type legendSection struct {
	title  string
	leads  [][2]string
	alerts [][2]string
	hint   string
	pairs  [][2]string
	quiet  bool
}

func (s legendSection) parts() []string {
	var parts []string
	for _, lead := range s.leads {
		if lead[0] != "" {
			parts = append(parts, keyCapLead(lead[0], lead[1]))
		}
	}
	for _, alert := range s.alerts {
		if alert[0] != "" {
			parts = append(parts, keyCapAlert(alert[0], alert[1]))
		}
	}
	if s.hint != "" {
		parts = append(parts, mutedStyle.Render(s.hint))
	}
	for _, pair := range s.pairs {
		if s.quiet {
			parts = append(parts, keyCapQuiet(pair[0], pair[1]))
		} else {
			parts = append(parts, keyCap(pair[0], pair[1]))
		}
	}
	return parts
}

// legendBar renders a legend as the app's footer, one tier per line where
// the terminal allows it and the tail marked when it does not.
func legendBar(sections []legendSection, width int) string {
	indent := strings.Repeat(" ", railGutter)
	sep := subtleStyle.Render(" · ")
	more := subtleStyle.Render("…")

	var out []string
	for i, section := range sections {
		parts := section.parts()
		if len(parts) == 0 || len(out) >= legendMaxRows {
			continue
		}
		maxRows := legendMaxRows
		for _, next := range sections[i+1:] {
			if len(next.parts()) > 0 {
				maxRows--
			}
		}
		title := legendBadgeStyle.Render(section.title)
		if section.quiet {
			title = legendTitleStyle.Render(section.title)
		}
		column := max(legendTitleColumn, ansi.StringWidth(title)+legendGap)
		head := indent + padRight(title, column)
		cont := indent + strings.Repeat(" ", column)
		line, lineWidth, started := head, ansi.StringWidth(head), false
		cut := false
		gap := strings.Repeat(" ", legendGap)
		if section.quiet {
			gap = sep
		}
		for _, part := range parts {
			partWidth := ansi.StringWidth(part) + ansi.StringWidth(gap)
			// The row that cannot wrap further keeps room for the cut
			// marker, so the marker never lands past the terminal edge.
			avail := width
			if len(out) >= maxRows-1 {
				avail = width - 1 - ansi.StringWidth(more)
			}
			switch {
			case !started:
				line, lineWidth, started = line+part, lineWidth+ansi.StringWidth(part), true
			case lineWidth+partWidth <= avail:
				line += gap + part
				lineWidth += partWidth
			case len(out) < maxRows-1:
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
