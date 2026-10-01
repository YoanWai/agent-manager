package ui

import (
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

// quickBarChrome is what the docked bar spends around the prompt: the rule
// that parts it from the list, and the target line.
const quickBarChrome = 2

// fullQuickLines docks the open quick bar at the full screen frame's foot,
// the prompt capped to the rows left over so the textarea scrolls the caret
// into view rather than the frame cutting the row it sits on. Empty while
// the bar is closed.
func (m *Model) fullQuickLines(width, height int) []contentLine {
	if !m.quick.active {
		return nil
	}
	gutter := strings.Repeat(" ", contentGutter)
	inner := width - 2*contentGutter
	if inner < 1 {
		inner = 1
	}
	inset := func(block []string) []contentLine {
		out := make([]contentLine, len(block))
		for i, line := range block {
			out[i] = contentLine{text: gutter + line}
		}
		return out
	}
	lines := append([]contentLine{{rule: true}}, inset(splitLines(m.viewQuickBar(inner, height-quickBarChrome)))...)
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	return lines
}

// viewQuickBar is the docked prompt: enter answers the selected session, or
// spawns a fresh agent when a group is selected.
func (m *Model) viewQuickBar(width, maxRows int) string {
	label := func(text string) string { return labelStyle.Render(padRight(text, detailLabelWidth)) }
	target := rowColumns(label("target")+mutedStyle.Render("no selection"), "", width)
	if entry, ok := m.selectedRow(); ok {
		if entry.isGroup {
			// Spawning: the tool and the worktree choice decide what gets
			// created, so they sit where the eye lands before typing.
			worktree := subtleStyle.Render("worktree off")
			capable, known := m.cachedWorktreeCapability(m.quickTargetDir())
			switch {
			case !known || !capable:
				worktree = subtleStyle.Render("worktree " + worktreeUnavailable)
			case m.quickWorktreeOn():
				worktree = lipgloss.NewStyle().Foreground(colorAccent2).Render("worktree on")
			}
			tool := chipStyle.Render(m.quickTool())
			target = fitColumns(
				[]string{label("new") + lipgloss.NewStyle().Foreground(colorAccent2).Render(displayGroup(entry.group))},
				[]string{tool + " " + worktree, tool, ""}, width)
		} else {
			sess := entry.sess
			state := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).
				Render(statusGlyph(sess.Status) + " " + statusLabel(sess.Status))
			target = fitColumns(
				[]string{label("answer") + lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(m.displayName(sess))},
				[]string{state + " " + chipStyle.Render(sess.Tool), state, ""}, width)
		}
	}
	// The rows the frame can spare become the box's own cap, so a keystroke
	// repositions the viewport inside the rows that are actually on screen.
	// LineInfo counts wraps at the width already stored on the box.
	m.quick.input.SetWidth(width)
	m.quick.maxRows = m.quickBarRows(width-2, maxRows)
	m.quick.input.SetHeight(m.quick.maxRows)
	// Chips are tokens inside the typed text, so they wrap and reflow with
	// the words around them; painting happens on the rendered prompt.
	return target + "\n" + m.quick.renderChips(textAreaView(m.quick.input))
}

const quickBarMaxRows = 5

// quickBarRows is the rows the typed text needs at the current width,
// capped so the bar never swallows the sidebar. Single-line values (the
// normal case) count exact soft-wrap rows; pasted multi-line values are
// estimated, with the textarea scrolling to keep the cursor visible.
func (m *Model) quickBarRows(textWidth, maxRows int) int {
	return textareaRows(m.quick.input, textWidth, min(maxRows, quickBarMaxRows))
}

func textareaRows(input textarea.Model, textWidth, maxRows int) int {
	rows := 0
	if input.LineCount() == 1 {
		rows = input.LineInfo().Height
	} else {
		if textWidth < 1 {
			textWidth = 1
		}
		// A line filling its last row exactly wraps onto one more empty row.
		for _, line := range strings.Split(input.Value(), "\n") {
			rows += 1 + max(lipgloss.Width(line), 1)/textWidth
		}
	}
	if rows > maxRows {
		rows = maxRows
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}
