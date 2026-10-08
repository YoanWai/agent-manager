package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) quickFooter() string {
	edge := keyStyle.Render(quickEdge)
	gutter := strings.Repeat(" ", quickGutter-1)
	keys := splitLines(legendBar([]legendSection{{title: quickModeTitle, pairs: m.quickLegend()}}, max(m.layout.width-1, 1)))
	rows := quickBarMaxRows + 1
	if m.quick.picking != pickNone {
		rows = max(m.layout.height/2, rows)
	}
	const minBody = 3
	room := m.layout.height - m.listChromeRows() - 1 - minBody - len(keys)
	rows = max(min(rows, room), 2)
	var lines []string
	for i, line := range splitLines(m.viewQuickBar(max(m.layout.width-2*quickGutter, 1), rows)) {
		tone := quickModeHex()
		if i > 0 && m.quick.picking == pickNone {
			tone = blockHex()
		}
		lines = append(lines, paint(edge+gutter+line, m.layout.width, tone))
	}
	for _, line := range keys {
		lines = append(lines, paint(edge+line, m.layout.width, quickModeHex()))
	}
	m.quick.originX = quickGutter
	return strings.Join(lines, "\n")
}

const (
	quickEdge   = "▌"
	quickGutter = 3
)

// viewQuickBar is the docked prompt: enter answers the selected session, or
// spawns a fresh agent when a group is selected.
func (m *Model) viewQuickBar(width, maxRows int) string {
	label := func(text string) string {
		return labelStyle.Render(padRight(text, detailLabelWidth)) + subtleStyle.Render("│ ")
	}
	target := rowColumns(label("target")+mutedStyle.Render("no selection"), "", width)
	m.quick.hits = m.quick.hits[:0]
	if entry, ok := m.selectedRow(); ok {
		if entry.isGroup {
			group := lipgloss.NewStyle().Foreground(colorAccent2).Render(displayGroup(entry.group))
			if m.quick.picking != pickNone {
				return m.viewQuickSheet(width, maxRows, group)
			}
			// Spawning: what the new agent launches with sits beside the
			// target, where the eye lands before typing.
			target = m.quickStatusRow(label("new")+group, width, 0)
		} else {
			sess := entry.sess
			state := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).
				Render(statusGlyph(sess.Status) + " " + statusLabel(sess.Status))
			target = fitColumns(
				[]string{label("answer") + lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(m.displayName(sess))},
				[]string{state + " " + chipStyle.Render(sess.Tool), state, ""}, width)
		}
	}
	// Count wrapped rows only after setting the width.
	m.quick.input.SetWidth(width)
	m.quick.maxRows = max(min(maxRows-1, quickBarMaxRows), 1)
	return target + "\n" + m.quick.view()
}

const quickBarMaxRows = 5

type quickHit struct {
	line, x0, x1 int
	action       int
	entry        int
}

const (
	quickClickTool = iota
	quickClickModel
	quickClickEffort
	quickClickProfile
	quickClickWorktree
	quickClickEntry
)

// quickStatusRow puts what a spawn launches with against the right edge of
// the row: the CLI, model, effort, profile and worktree, each a stretch a
// click steps the way its key does.
func (m *Model) quickStatusRow(left string, width, line int) string {
	toolName, ch := m.quickTool(), &m.quick.choice
	type segment struct {
		text   string
		action int
	}
	orDefault := func(value, fallback string) string {
		if value == "" {
			return subtleStyle.Render(fallback)
		}
		return valueStyle.Render(value)
	}
	segments := []segment{{valueStyle.Render(toolName), quickClickTool}}
	tool := m.services.cfg.Tools[toolName]
	switch note, listed := m.modelRowNote(toolName); {
	case listed:
		segments = append(segments, segment{orDefault(ch.model, "default model"), quickClickModel})
		if _, _, active := m.effortRow(toolName, ch); active {
			segments = append(segments, segment{orDefault(m.choiceEffort(toolName, ch), "default effort"), quickClickEffort})
		}
	case tool.Catalog != "" && tool.ModelArgs != "":
		// Still reading, or the CLI failed to answer: say so. A CLI with
		// nothing to pick shows nothing.
		segments = append(segments, segment{note, quickClickModel})
	}
	if _, shown := m.profileRow(toolName, ch); shown {
		segments = append(segments, segment{orDefault(m.choiceProfileName(toolName, ch), "default profile"), quickClickProfile})
	}
	worktree := subtleStyle.Render("⎇ off")
	capable, known := m.cachedWorktreeCapability(m.quickTargetDir())
	switch {
	case !known || !capable:
		worktree = subtleStyle.Render("⎇ no repo")
	case m.quickWorktreeOn():
		worktree = lipgloss.NewStyle().Foreground(colorAccent2).Render("⎇ on")
	}
	segments = append(segments, segment{worktree, quickClickWorktree})

	var status strings.Builder
	offsets := make([]int, len(segments))
	for i, seg := range segments {
		if i > 0 {
			status.WriteString(subtleStyle.Render(" · "))
		}
		offsets[i] = ansi.StringWidth(status.String())
		status.WriteString(seg.text)
	}
	leftWidth := ansi.StringWidth(left)
	statusWidth := min(ansi.StringWidth(status.String()), max(width-leftWidth-2, 0))
	start := width - statusWidth
	for i, seg := range segments {
		if offsets[i] >= statusWidth {
			break
		}
		x1 := min(offsets[i]+ansi.StringWidth(seg.text), statusWidth)
		m.quick.hits = append(m.quick.hits, quickHit{line: line, x0: start + offsets[i], x1: start + x1, action: seg.action, entry: -1})
	}
	return left + strings.Repeat(" ", max(start-leftWidth, 0)) + ansi.Truncate(status.String(), statusWidth, "…")
}

// viewQuickSheet stands in for the whole bar while a choice is picked, so
// the list has the rows the prompt and its target would take.
func (m *Model) viewQuickSheet(width, maxRows int, group string) string {
	toolName, ch := m.quickTool(), &m.quick.choice
	var lines []string
	if m.quick.picking == pickEffort {
		lines = []string{
			subtleStyle.Render("effort for ") + group,
			textInputView(ch.typedEffort) + "  " + subtleStyle.Render("typed · "+toolName+" lists no levels"),
		}
	} else {
		lines = []string{subtleStyle.Render("model for ") + group, textInputView(ch.filter)}
		list, entries := m.viewModelSuggestions(toolName, ch, ch.query(), 0, width, max(maxRows-len(lines)-1, 1))
		for i, line := range list {
			if entries[i] >= 0 {
				m.quick.hits = append(m.quick.hits, quickHit{line: len(lines), x0: 0, x1: width, action: quickClickEntry, entry: entries[i]})
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(append(lines, m.quickStatusRow("", width, len(lines))), "\n")
}
