package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"time"
)

// Captured panes use the whole column to preserve terminal layout.
func (m *Model) contentLines(width, height int) []contentLine {
	gutter := strings.Repeat(" ", contentGutter)
	inner := width - 2*contentGutter
	ours := func(lines []string) []contentLine {
		out := make([]contentLine, len(lines))
		for i, line := range lines {
			out[i] = contentLine{text: gutter + line}
		}
		return out
	}

	body := ours(splitLines(m.viewDetail(inner)))
	rest := height - len(body) - 1
	if rest >= 3 {
		if group, ok := m.selectedGroup(); ok {
			body = append(body, contentLine{rule: true})
			body = append(body, ours(splitLines(m.viewGroupAgents(group, inner, rest)))...)
		} else {
			separator := contentLine{rule: true}
			if m.mode == modeFocus {
				separator = contentLine{text: focusTopRule(width, m.services.keys), raw: true}
			}
			body = append(body, separator)
			body = append(body, m.previewLines(width, rest, gutter, m.listChromeRows()+len(body))...)
		}
	}
	for len(body) < height {
		body = append(body, contentLine{})
	}
	return body[:max(height, 0)]
}

// focusTopRule is the hairline that caps the focused pane in the split,
// where the detail head above it already names the session, so the rule
// spends its title on the keys instead.
func (m *Model) listHint(action, label string) string {
	glyph := m.listGlyph(action)
	if glyph == "" {
		return ""
	}
	return keyCap(glyph, label)
}

// previewLines is the captured pane, filling every row under the detail
// separator. Compact content sits at the bottom, where an agent's composer
// remains beside the manager footer instead of floating above a blank tail.
// The captured rows are marked raw and drawn without the column's gutters:
// painting our backdrop behind an agent's own CLI colors would replace the
// background it drew itself, and insetting its output would put a margin
// around a terminal that has its own.
func (m *Model) previewLines(width, height int, gutter string, frameOrigin ...int) []contentLine {
	var lines []contentLine
	loader := m.startupLoader(width, height)
	if m.mode == modeFocus {
		originY := m.listChromeRows()
		if len(frameOrigin) > 0 {
			originY = frameOrigin[0]
		}
		rendered := m.focus.pane.PrepareFrame(uifocus.FrameContext{
			Preview:        m.workspace.preview,
			Width:          width,
			Height:         height,
			OriginX:        m.focusPaneOriginX(),
			OriginY:        originY,
			CaretRow:       m.paneCaretRow(),
			CursorStyle:    lipgloss.NewStyle().Foreground(colorBg).Background(colorAccent),
			SelectionStyle: lipgloss.NewStyle().Foreground(colorBg).Background(colorAccent2),
		})
		if !rendered.HasPane {
			if loader != nil {
				for _, line := range loader {
					lines = append(lines, contentLine{text: previewLine(line, width), raw: true})
				}
				return lines
			}
			return append(lines, contentLine{text: gutter + mutedStyle.Render("(no output yet)")})
		}
		for len(lines) < rendered.TopPadding {
			lines = append(lines, contentLine{raw: true})
		}
		for i, line := range rendered.Rows {
			screenRow := rendered.TopPadding + i
			if screenRow < len(loader) && loader[screenRow] != "" {
				line = previewLine(loader[screenRow], width)
			}
			lines = append(lines, contentLine{text: line, raw: true})
		}
		for len(lines) < height {
			lines = append(lines, contentLine{raw: true})
		}
		return lines
	}
	pane := paneExact(m.workspace.preview, height, width, m.paneCaretRow())
	if len(pane) == 0 {
		if loader != nil {
			for _, line := range loader {
				lines = append(lines, contentLine{text: previewLine(line, width), raw: true})
			}
			return lines
		}
		return append(lines, contentLine{text: gutter + mutedStyle.Render("(no output yet)")})
	}
	topPadding := height - len(pane)
	for len(lines) < topPadding {
		lines = append(lines, contentLine{raw: true})
	}
	for i, line := range pane {
		screenRow := topPadding + i
		if screenRow < len(loader) && loader[screenRow] != "" {
			lines = append(lines, contentLine{text: previewLine(loader[screenRow], width), raw: true})
			continue
		}
		lines = append(lines, contentLine{text: previewLine(line, width), raw: true})
	}
	// Rows past the capture stay raw too: a painted tail under unpainted
	// output would read as a box drawn around the agent's last line.
	for len(lines) < height {
		lines = append(lines, contentLine{raw: true})
	}
	return lines
}

func (m *Model) focusPaneOriginX() int {
	if m.fullFocus() {
		return 0
	}
	leftWidth, _ := m.splitWidths()
	return leftWidth + 2
}

// detailLabelWidth is the column every fact label in the content head is
// padded to, so the values under it line up as one column.
const detailLabelWidth = 7

// factRow is one line of a detail head: a quiet label, its value, and a
// reading set against the right edge. The value is rendered against the
// columns it actually gets, and the reading is dropped rather than pushed
// off the edge when the column is too narrow to hold both.
func factRow(label string, value func(room int) string, right string, width int) string {
	room := width - detailLabelWidth - ansi.StringWidth(right) - 2
	if room < 12 {
		right, room = "", width-detailLabelWidth
	}
	return rowColumns(labelStyle.Render(padRight(label, detailLabelWidth))+value(max(room, 1)), right, width)
}

// plainValue is a factRow value that is already short enough to stand as it
// is, for facts whose text does not vary with the terminal.
func plainValue(value string) func(int) string {
	return func(int) string { return value }
}

// trimmedValue is a factRow value cut to the columns it gets, for readings
// that grow with the fleet rather than with the terminal.
func trimmedValue(value string) func(int) string {
	return func(room int) string { return ansi.Truncate(value, room, "…") }
}

// fitColumns lays the richest pair of readings that fits: rights are tried
// from richest to plainest, and for each, the lefts in turn. Nothing fitting
// means the plainest left is trimmed to what the column has.
func fitColumns(lefts, rights []string, width int) string {
	for _, right := range rights {
		for _, left := range lefts {
			if ansi.StringWidth(left)+ansi.StringWidth(right)+2 <= width {
				return rowColumns(left, right, width)
			}
		}
	}
	// Nothing fits whole, so the plainest left is trimmed to keep the richest
	// reading that still leaves it something readable.
	last := lefts[len(lefts)-1]
	for _, right := range rights {
		if room := width - ansi.StringWidth(right) - 2; room >= 8 {
			return rowColumns(ansi.Truncate(last, room, "…"), right, width)
		}
	}
	return rowColumns(ansi.Truncate(last, max(width, 1), "…"), "", width)
}

// viewDetail heads the content column: the selected session's name, its
// state, and the facts that place it (group, directory, age, usage).
func (m *Model) viewDetail(width int) string {
	sess, ok := m.selected()
	if !ok {
		if group, isGroup := m.selectedGroup(); isGroup {
			return m.viewGroupDetail(group, width)
		}
		return mutedStyle.Render("Select a session to inspect it.")
	}
	tool := sess.Tool
	if m.mode == modeRename && !m.rename.isGroup && m.rename.sessID == sess.ID {
		if picked := m.rename.tool(); picked != "" {
			tool = picked
		}
	}

	state := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).
		Render(statusGlyph(sess.Status)+" "+statusLabel(sess.Status)) +
		subtleStyle.Render(" · "+relSince(lastActivity(sess))+m.elsewhereNote(sess))
	if sess.AfterTurn != "" {
		state += subtleStyle.Render(" · ") + afterTurnBadge(sess.AfterTurn) + subtleStyle.Render(" "+afterTurnNote[sess.AfterTurn])
	}
	// The branch a worktree session lives on is the fact that tells it apart
	// from its siblings, so it rides beside the tool while the row has room,
	// and the tool chip goes before the name does.
	name := lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(m.displayName(sess))
	// Ahead of the name because fitColumns trims the head from its tail: the
	// rail still shows the name, while a trimmed badge leaves no trace that
	// anything is waiting.
	if queued := m.workspace.queuedMessages[sess.ID]; queued > 0 {
		name = inboxBadge(queued) + " " + name
	}
	withTool := name + "  " + chipStyle.Render(tool)
	heads := []string{withTool, name}
	if sess.WorktreeBranch != "" {
		heads = append([]string{withTool + " " + chipStyle.Render("⑂ "+sess.WorktreeBranch)}, heads...)
	}

	usage := ""
	if m.workspace.procFor == sess.ID && m.workspace.proc.OK {
		usage = labelStyle.Render("cpu ") + valueStyle.Render(fmt.Sprintf("%.1f%%", m.workspace.proc.CPUPercent)) +
			subtleStyle.Render(" · ") + labelStyle.Render("ram ") +
			valueStyle.Render(fmt.Sprintf("%.1f%%", m.workspace.proc.RamPercent)) +
			subtleStyle.Render(" · ") + valueStyle.Render(humanBytes(m.workspace.proc.RSS))
	}
	started := subtleStyle.Render("started " + relSince(sess.CreatedAt))
	group := lipgloss.NewStyle().Foreground(colorAccent2).Render(displayGroup(sess.Group))
	dir := func(room int) string { return mutedStyle.Render(truncateTail(m.sessionDir(sess), room)) }
	return fitColumns(heads, []string{state}, width) + "\n" +
		factRow("group", plainValue(group), started, width) + "\n" +
		factRow("dir", dir, usage, width)
}

// viewGroupDetail heads the content column for a group: its name, how many
// agents sit under it, where they start, and what they are all doing.
func (m *Model) viewGroupDetail(group string, width int) string {
	count := m.groupSessionCount(group)
	countLabel := fmt.Sprintf("%d agents", count)
	if count == 1 {
		countLabel = "1 agent"
	}
	title := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Render(displayGroup(group))
	head := fitColumns([]string{title + "  " + chipStyle.Render(countLabel), title}, []string{""}, width)

	if m.renamingGroup(group) {
		pathLabel := labelStyle
		if m.rename.focus == 1 {
			pathLabel = lipgloss.NewStyle().Foreground(colorAccent)
		}
		worktreeLabel := labelStyle
		if m.rename.focus == 2 {
			worktreeLabel = lipgloss.NewStyle().Foreground(colorAccent)
		}
		baseLabel := labelStyle
		if m.rename.focus == 3 {
			baseLabel = lipgloss.NewStyle().Foreground(colorAccent)
		}
		if fieldWidth := width - 12; fieldWidth >= 10 {
			m.rename.dir.Width = fieldWidth
		}
		out := head + "\n" + pathLabel.Width(10).Render("path") + textInputView(m.rename.dir)
		if m.rename.focus == 1 && m.pathSugg.active() {
			out += "\n" + m.viewPathSuggestions()
		}
		out += "\n" + worktreeLabel.Width(10).Render("worktree") +
			subtleStyle.Render("◂ ") + valueStyle.Render(groupWorktreeOptions[m.rename.worktreeIndex]) + subtleStyle.Render(" ▸")
		out += "\n" + baseLabel.Width(10).Render("base") + groupBaseChoice(m.rename.base, m.groupBase(parentGroup(group)))
		return out
	}

	path := m.workspace.groupPaths[group]
	source := ""
	if path == "" {
		path = m.capturedGroupDefaultDir(group)
		source = subtleStyle.Render("inherited")
	}
	dir := func(room int) string { return mutedStyle.Render(truncateTail(path, room)) }
	lines := []string{head, factRow("dir", dir, source, width)}
	if breakdown := m.groupStatusBreakdown(group); breakdown != "" {
		// The key that spawns into a group is the one people miss, since the
		// same key answers a session, so the group says what it does here.
		// The breakdown is the reading, so a column too tight for both keeps it.
		hint := m.listHint(keybind.Prompt, "new agent")
		if detailLabelWidth+ansi.StringWidth(breakdown)+ansi.StringWidth(hint)+2 > width {
			hint = ""
		}
		lines = append(lines, factRow("state", trimmedValue(breakdown), hint, width))
	}
	return strings.Join(lines, "\n")
}

// rosterToolColumn is the column a roster's tool names start at, so the
// roster reads as a table rather than as ragged pairs, and rosterNameMin
// the width under which a name column stops being worth reading.
const (
	rosterToolColumn = 14
	rosterNameMin    = 8
)

// viewGroupAgents lists a group's sessions where a session's pane preview
// would sit, so a group reads as a roster: one row per agent, its name, the
// CLI running it, and what it is doing.
func (m *Model) viewGroupAgents(group string, width, height int) string {
	total := m.groupSessionCount(group)
	if total == 0 {
		none := "(none yet)"
		if prompt := m.listGlyph(keybind.Prompt); prompt != "" {
			none = "(none yet, press " + prompt + " to spawn one)"
		}
		return subtleStyle.Render("agents") + "\n" + mutedStyle.Render(none)
	}

	type rosterRow struct{ name, tool, state string }
	var rows []rosterRow
	overflow := ""
	shown := 0
	for _, sess := range m.listedAgents() {
		if !inGroupSubtree(sess.Group, group) {
			continue
		}
		if shown >= height-2 && total > shown+1 {
			overflow = subtleStyle.Render(fmt.Sprintf("… %d more", total-shown))
			break
		}
		tint := lipgloss.NewStyle().Foreground(statusColor(sess.Status))
		rows = append(rows, rosterRow{
			name:  tint.Render(statusGlyph(sess.Status)) + " " + valueStyle.Render(m.displayName(sess)),
			tool:  subtleStyle.Render(sess.Tool),
			state: tint.Render(statusLabel(sess.Status)) + subtleStyle.Render(" · "+relSince(lastActivity(sess))),
		})
		shown++
	}

	// The name column is as wide as the longest name allows, bounded by what
	// the tool and state columns need, so tools land on one column and the
	// states share a right edge. A column too narrow for all three gives up
	// the tool first and the state second: a roster of names still answers
	// "who is in this group".
	nameWidth, toolWidth, stateWidth := rosterToolColumn, 0, 0
	for _, row := range rows {
		if w := ansi.StringWidth(row.name) + 2; w > nameWidth {
			nameWidth = w
		}
		if w := ansi.StringWidth(row.tool); w > toolWidth {
			toolWidth = w
		}
		if w := ansi.StringWidth(row.state); w > stateWidth {
			stateWidth = w
		}
	}
	showTool, showState := true, true
	if width-toolWidth-stateWidth-3 < rosterNameMin {
		showTool, toolWidth = false, 0
	}
	if width-stateWidth-2 < rosterNameMin {
		showState, stateWidth = false, 0
	}
	if room := width - toolWidth - stateWidth - 3; nameWidth > room {
		nameWidth = max(room, rosterNameMin)
	}

	head := padRight(subtleStyle.Render("agent"), nameWidth)
	if showTool {
		head += subtleStyle.Render("tool")
	}
	activity := ""
	if showState {
		activity = subtleStyle.Render("last activity")
	}
	lines := []string{rowColumns(head, activity, width)}
	for _, row := range rows {
		// A name trimmed to the column exactly would touch the tool beside
		// it, so the trim leaves the column's last cell as the gap.
		line := padRight(ansi.Truncate(row.name, max(nameWidth-1, 1), "…"), nameWidth)
		if showTool {
			line += row.tool
		}
		state := row.state
		if !showState {
			state = ""
		}
		lines = append(lines, rowColumns(line, state, width))
	}
	if overflow != "" {
		lines = append(lines, overflow)
	}
	return strings.Join(lines, "\n")
}

// lastActivity is when a session last changed state: the agent answering,
// finishing, erroring, or the moment a prompt set it working. It is what
// "how long since anything happened here" means to someone scanning the
// rail, where uptime says nothing about whether an agent is stuck.
func lastActivity(sess store.Session) time.Time {
	if sess.LastStatusAt.IsZero() {
		return sess.CreatedAt
	}
	return sess.LastStatusAt
}
