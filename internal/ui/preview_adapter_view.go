package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	uipreview "github.com/YoanWai/agent-manager/internal/ui/preview"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Captured panes use the whole column to preserve terminal layout.
func (m *Model) contentLines(width, height int) []contentLine {
	if lines, ok := m.remoteContentLines(width, height); ok {
		return lines
	}
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
			if m.focused() {
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

func (m *Model) listHint(action, label string) string {
	glyph := m.listGlyph(action)
	if glyph == "" {
		return ""
	}
	return keyCap(glyph, label)
}

// startupLoader is the ring a session still booting shows in place of its
// pane, once selected and not focused: a focused pane is the screen the user
// types on, so it keeps its own frame.
func (m *Model) startupLoader(width, height int) []string {
	sess, ok := m.selected()
	if !ok {
		return nil
	}
	starting := sess.Status == status.Starting && !m.focused()
	return startingLoader(m.glyphs(), starting, m.workspace.preview, width, height, m.startup.startupPhase)
}

// previewLines is the captured pane, filling every row under the detail
// separator. Compact content sits at the bottom, where an agent's composer
// remains beside the manager footer instead of floating above a blank tail.
// The captured rows are marked raw and drawn without the column's gutters:
// painting our backdrop behind an agent's own CLI colors would replace the
// background it drew itself, and insetting its output would put a margin
// around a terminal that has its own.
func (m *Model) previewLines(width, height int, gutter string, frameOrigin ...int) []contentLine {
	loader := m.startupLoader(width, height)
	loaderRow := func(row int) string {
		if row < len(loader) {
			return loader[row]
		}
		return ""
	}
	rows := []string(nil)
	if m.focused() {
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
			CursorStyle:    lipgloss.NewStyle().Foreground(colorBg()).Background(colorAccent()),
			SelectionStyle: lipgloss.NewStyle().Foreground(colorBg()).Background(colorAccent2()),
		})
		if !rendered.HasPane {
			return m.emptyPreview(width, gutter, loader)
		}
		rows = make([]string, 0, height)
		for range rendered.TopPadding {
			rows = append(rows, "")
		}
		for i, line := range rendered.Rows {
			if replaced := loaderRow(rendered.TopPadding + i); replaced != "" {
				line = replaced
			}
			rows = append(rows, uipreview.Line(line, width))
		}
	} else {
		rows = uipreview.Rows(m.workspace.preview, height, width, m.paneCaretRow(), loaderRow)
	}
	if len(rows) == 0 {
		return m.emptyPreview(width, gutter, loader)
	}
	return previewContent(rows, height)
}

// emptyPreview is the column a pane with no capture leaves: the startup
// ring while the session boots, otherwise a quiet placeholder.
func (m *Model) emptyPreview(width int, gutter string, loader []string) []contentLine {
	if loader == nil {
		return []contentLine{{text: gutter + mutedStyle().Render("(no output yet)")}}
	}
	rows := make([]string, len(loader))
	for i, line := range loader {
		rows[i] = uipreview.Line(line, width)
	}
	return previewContent(rows, len(loader))
}

// previewContent marks a column of pane rows as captured output and pads it
// to height. Rows past the capture stay raw too: a painted tail under
// unpainted output would read as a box drawn around the agent's last line.
func previewContent(rows []string, height int) []contentLine {
	lines := make([]contentLine, 0, max(height, len(rows)))
	for _, row := range rows {
		lines = append(lines, contentLine{text: row, raw: true})
	}
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

// factRow is one line of a detail head, with its label painted in the
// manager's quiet label tone inside the head's label column.
func factRow(label string, value func(room int) string, right string, width int) string {
	return uipreview.FactRow(labelStyle().Render(uipreview.FactLabel(label)), value, right, width)
}

// viewDetail heads the content column: the selected session's name, its
// state, and the facts that place it (group, directory, age, usage).
func (m *Model) viewDetail(width int) string {
	sess, ok := m.selected()
	if !ok {
		if group, isGroup := m.selectedGroup(); isGroup {
			return m.viewGroupDetail(group, width)
		}
		return mutedStyle().Render("Select a session to inspect it.")
	}
	tool := sess.Tool
	if s, ok := m.renaming(); ok && !s.IsGroup() && s.SessionID() == sess.ID {
		if picked := s.Tool(); picked != "" {
			tool = picked
		}
	}

	state := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).
		Render(m.glyphs().StatusGlyph(sess.Status)+" "+statusLabel(sess.Status)) +
		subtleStyle().Render(" · "+relSince(uipreview.LastActivity(sess.CreatedAt, sess.LastStatusAt))+m.elsewhereNote(sess))
	if sess.AfterTurn != "" {
		state += subtleStyle().Render(" · ") + m.afterTurnBadge(sess.AfterTurn) + subtleStyle().Render(" "+afterTurnNote[sess.AfterTurn])
	}
	// The branch a worktree session lives on is the fact that tells it apart
	// from its siblings, so it rides beside the tool while the row has room,
	// and the tool chip goes before the name does.
	name := lipgloss.NewStyle().Foreground(colorBright()).Bold(true).Render(m.displayName(sess))
	// Ahead of the name because FitColumns trims the head from its tail: the
	// rail still shows the name, while a trimmed badge leaves no trace that
	// anything is waiting.
	if queued := m.workspace.queuedMessages[sess.ID]; queued > 0 {
		name = inboxBadge(m.glyphs(), queued) + " " + name
	}
	withTool := name + "  " + chipStyle().Render(tool)
	heads := []string{withTool, name}
	if sess.WorktreeBranch != "" {
		heads = append([]string{withTool + " " + chipStyle().Render(m.glyphs().BranchGlyph+" "+sess.WorktreeBranch)}, heads...)
	}

	usage := ""
	if m.workspace.procFor == sess.ID && m.workspace.proc.OK {
		usage = labelStyle().Render("cpu ") + valueStyle().Render(fmt.Sprintf("%.1f%%", m.workspace.proc.CPUPercent)) +
			subtleStyle().Render(" · ") + labelStyle().Render("ram ") +
			valueStyle().Render(fmt.Sprintf("%.1f%%", m.workspace.proc.RamPercent)) +
			subtleStyle().Render(" · ") + valueStyle().Render(humanBytes(m.workspace.proc.RSS))
	}
	started := subtleStyle().Render("started " + relSince(sess.CreatedAt))
	group := lipgloss.NewStyle().Foreground(colorAccent2()).Render(displayGroup(sess.Group))
	dir := func(room int) string { return mutedStyle().Render(truncateTail(m.sessionDir(sess), room)) }
	return uipreview.FitColumns(heads, []string{state}, width) + "\n" +
		factRow("group", uipreview.PlainValue(group), started, width) + "\n" +
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
	title := lipgloss.NewStyle().Foreground(colorAccent2()).Bold(true).Render(displayGroup(group))
	head := uipreview.FitColumns([]string{title + "  " + chipStyle().Render(countLabel), title}, []string{""}, width)

	if s, ok := m.renaming(); ok && s.IsGroup() && s.Path() == group {
		return head + "\n" + m.viewRenameGroupFields(s, group, width)
	}

	path := m.workspace.groupPaths[group]
	source := ""
	if path == "" {
		path = m.capturedGroupDefaultDir(group)
		source = subtleStyle().Render("inherited")
	}
	dir := func(room int) string { return mutedStyle().Render(truncateTail(path, room)) }
	lines := []string{head, factRow("dir", dir, source, width)}
	if breakdown := m.groupStatusBreakdown(group); breakdown != "" {
		// The key that spawns into a group is the one people miss, since the
		// same key answers a session, so the group says what it does here.
		// The breakdown is the reading, so a column too tight for both keeps it.
		hint := m.listHint(keybind.Prompt, "new agent")
		if uipreview.DetailLabelWidth+ansi.StringWidth(breakdown)+ansi.StringWidth(hint)+2 > width {
			hint = ""
		}
		lines = append(lines, factRow("state", uipreview.TrimmedValue(breakdown), hint, width))
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
		return subtleStyle().Render("agents") + "\n" + mutedStyle().Render(none)
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
			overflow = subtleStyle().Render(fmt.Sprintf("… %d more", total-shown))
			break
		}
		tint := lipgloss.NewStyle().Foreground(statusColor(sess.Status))
		rows = append(rows, rosterRow{
			name:  tint.Render(m.glyphs().StatusGlyph(sess.Status)) + " " + valueStyle().Render(m.displayName(sess)),
			tool:  subtleStyle().Render(sess.Tool),
			state: tint.Render(statusLabel(sess.Status)) + subtleStyle().Render(" · "+relSince(uipreview.LastActivity(sess.CreatedAt, sess.LastStatusAt))),
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

	head := padRight(subtleStyle().Render("agent"), nameWidth)
	if showTool {
		head += subtleStyle().Render("tool")
	}
	activity := ""
	if showState {
		activity = subtleStyle().Render("last activity")
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
