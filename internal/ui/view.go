package ui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) View() string {
	if m.width == 0 {
		return "loading..."
	}
	var frame string
	switch m.mode {
	case modeForm:
		frame = m.viewForm()
	case modeHelp:
		frame = m.viewHelp()
	case modeSettings:
		frame = m.viewSettings()
	case modeMove:
		frame = m.viewMove()
	case modeRepoPick:
		frame = m.viewRepoPick()
	case modeGroupForm:
		frame = m.viewGroupForm()
	case modeDiff:
		frame = m.viewDiffFull()
	default:
		frame = m.viewListFrame()
	}
	return clampFrame(frame, m.height)
}

// clampFrame pins a rendered frame to exactly height rows so the outer
// terminal cannot scroll the TUI away when a layout overshoots.
func clampFrame(frame string, height int) string {
	if height <= 0 {
		return frame
	}
	lines := strings.Split(frame, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// splitWidths is the body's horizontal split: the sessions panel takes
// splitRatio of the terminal (default 34%), floored so both sides stay
// usable when the window is wide enough.
func (m *Model) splitWidths() (int, int) {
	if m.width <= 0 {
		return 0, 0
	}
	ratio := m.splitRatio
	if ratio <= 0 || ratio >= 1 {
		ratio = defaultSplitRatio
	}
	leftWidth := int(float64(m.width)*ratio + 0.5)
	leftWidth = clampSplitLeft(leftWidth, m.width)
	return leftWidth, m.width - leftWidth
}

// previewPaneWidth is the sidebar's inner content width: the columns the
// pane preview can actually show. Sessions size to it so captured lines
// fit the panel instead of getting clipped on the right.
func (m *Model) previewPaneWidth() int {
	_, rightWidth := m.splitWidths()
	if m.resizeMode && rightWidth > 1 {
		// Grip steals one column from the right panel while resize is on.
		rightWidth--
	}
	w := rightWidth - 2*contentGutter
	if w < 1 {
		return 1
	}
	return w
}

// previewPaneHeight is the rows of session pane content the Preview
// section can show. Mirrors viewSidebar + viewPreview so tmux is pinned
// to the same box the UI paints into.
func (m *Model) previewPaneHeight() int {
	width := m.previewPaneWidth()
	if m.height < 1 {
		return 1
	}
	avail := m.listBodyHeight()
	if avail < 1 {
		return 1
	}
	if m.quick.active {
		avail -= lipgloss.Height(m.viewQuickBar(width)) + 1
	}
	// Mirrors contentLines: the detail head, a blank, then the preview
	// under its label.
	rest := avail - lipgloss.Height(m.viewDetail(width)) - 1
	if rest < 3 {
		// Preview section is hidden; keep a tiny pane for create/attach paths.
		return 3
	}
	h := rest - 1
	if h < 1 {
		return 1
	}
	return h
}

// viewStatus is the transient message line: prompts, search, and
// self-dismissing errors. Keeps the footer free for key hints.
func (m *Model) viewStatus() string {
	switch {
	case m.mode == modeConfirmDelete:
		return "  " + errStyle.Render("⚠ "+m.confirm.label) + subtleStyle.Render("  y/n")
	case m.resizeMode:
		hint := "←→ resize · drag divider · | set · esc cancel"
		if m.splitDragging {
			hint = "release to set · esc cancels"
		}
		return "  " + keyStyle.Render("resize ") + subtleStyle.Render(hint)
	case m.searching:
		cursor := lipgloss.NewStyle().Foreground(colorAccent).Render("▏")
		return "  " + keyStyle.Render("search ") + valueStyle.Render(m.search) + cursor +
			subtleStyle.Render("  enter/esc to close")
	case m.err != "":
		return "  " + errStyle.Render("✕ "+m.err)
	case m.diff.notice != "":
		return "  " + lipgloss.NewStyle().Foreground(colorFinished).Render("● "+m.diff.notice)
	default:
		return ""
	}
}

// inGroupSubtree reports whether a session's group sits at or below the
// given group in the tree.
func inGroupSubtree(sessGroup, group string) bool {
	return sessGroup == group || strings.HasPrefix(sessGroup, group+"/")
}

func (m *Model) groupSessionCount(path string) int {
	count := 0
	for _, sess := range m.visibleSessions() {
		if inGroupSubtree(sess.Group, path) {
			count++
		}
	}
	return count
}

// rowColumns lays a list row out as a name column and a right-aligned meta
// column, so status, tool and age line up down the list instead of ragging
// off the end of each name. Rows too narrow to split keep meta inline and
// let the caller's truncation decide what survives.
func rowColumns(lead, meta string, width int) string {
	if meta == "" {
		return lead
	}
	const gap = 2
	leadWidth := ansi.StringWidth(lead)
	metaWidth := ansi.StringWidth(meta)
	if width < 1 || leadWidth+gap+metaWidth > width {
		return lead + strings.Repeat(" ", gap) + meta
	}
	return lead + strings.Repeat(" ", width-leadWidth-metaWidth) + meta
}

func (m *Model) renamingGroup(group string) bool {
	return m.mode == modeRename && m.rename.isGroup && m.rename.path == group
}

func (m *Model) renamingRow(entry treeRow) bool {
	if entry.isGroup {
		return m.renamingGroup(entry.group)
	}
	return m.mode == modeRename && !m.rename.isGroup && entry.sess.ID == m.rename.sessID
}

// renameRowInput renders the inline name editor in place of the row's
// label, keeping the row's glyph so the edit reads in context.
func (m *Model) renameRowInput(entry treeRow, width int) string {
	lead := subtleStyle.Render("▾")
	if !entry.isGroup {
		lead = lipgloss.NewStyle().Foreground(statusColor(entry.sess.Status)).
			Render(statusGlyph(entry.sess.Status))
	}
	if fieldWidth := width - 4; fieldWidth >= 5 {
		m.rename.input.Width = fieldWidth
	}
	return lead + " " + m.rename.input.View()
}

// divider renders a labeled section rule that fills the given width: an
// accent tick, the label, then a hairline out to the edge.
func divider(label string, width int) string {
	head := sectionStyle.Render("▍"+label) + " "
	dashes := width - ansi.StringWidth(label) - 2
	if dashes < 0 {
		dashes = 0
	}
	return head + lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", dashes))
}

// syncQuickInlineChips rebuilds the line-0 prompt as "> [chip] [chip] " so
// pasted images render inline with the caret. Paths stay out of the value;
// backspace at the text start peels chips off (see handleQuickKey).
func (m *Model) syncQuickInlineChips() {
	prefix := m.quickInlineChipPrompt()
	// lipgloss.Width ignores SGR so the reserved prompt slot matches what
	// the terminal paints; uniseg alone would mis-count styled chips.
	promptWidth := max(lipgloss.Width(prefix), 2)
	m.quick.input.SetPromptFunc(promptWidth, func(lineIndex int) string {
		if lineIndex == 0 {
			return prefix
		}
		return strings.Repeat(" ", promptWidth)
	})
}

// quickInlineChipPrompt is the first-line prompt: a styled caret plus one
// soft chip per attachment (and a transient pasting chip while the
// clipboard read runs). Labels are short "Image N" tokens so the bar stays
// readable; full paths still go out on submit via attachments.
func (m *Model) quickInlineChipPrompt() string {
	var b strings.Builder
	b.WriteString(keyStyle.Render("❯ "))
	for i := range m.quick.attachments {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(imageChip(fmt.Sprintf("Image %d", i+1)))
	}
	if m.quick.pasting {
		if len(m.quick.attachments) > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(imageChipPasting())
	}
	if len(m.quick.attachments) > 0 || m.quick.pasting {
		b.WriteByte(' ')
	}
	return b.String()
}

const quickBarMaxRows = 5

// quickBarRows is the rows the typed text needs at the current width,
// capped so the bar never swallows the sidebar. Single-line values (the
// normal case) count exact soft-wrap rows; pasted multi-line values are
// estimated, with the textarea scrolling to keep the cursor visible.
func (m *Model) quickBarRows(textWidth int) int {
	rows := 0
	if m.quick.input.LineCount() == 1 {
		rows = m.quick.input.LineInfo().Height
	} else {
		if textWidth < 1 {
			textWidth = 1
		}
		for _, line := range strings.Split(m.quick.input.Value(), "\n") {
			rows += 1 + (max(lipgloss.Width(line), 1)-1)/textWidth
		}
	}
	if rows > quickBarMaxRows {
		rows = quickBarMaxRows
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m *Model) selectedGroup() (string, bool) {
	if entry, ok := m.selectedRow(); ok && entry.isGroup {
		return entry.group, true
	}
	return "", false
}

func parentGroup(group string) string {
	if idx := strings.LastIndex(group, "/"); idx >= 0 {
		return group[:idx]
	}
	return ""
}

func (m *Model) directSubgroupCount(group string) int {
	count := 0
	for path := range groupClosure(m.groups, m.sessions) {
		if parentGroup(path) == group && path != group {
			count++
		}
	}
	return count
}

// groupStatusBreakdown renders "2 working · 1 waiting" for the subtree,
// each count tinted in its status color, skipping zero statuses.
func (m *Model) groupStatusBreakdown(group string) string {
	counts := m.groupStatusCounts(group)
	var parts []string
	for _, st := range []string{status.Working, status.Waiting, status.Finished, status.Errored, status.Idle, status.Dead} {
		if counts[st] > 0 {
			parts = append(parts, lipgloss.NewStyle().Foreground(statusColor(st)).
				Render(fmt.Sprintf("%d %s", counts[st], st)))
		}
	}
	return strings.Join(parts, subtleStyle.Render(" · "))
}

func (m *Model) groupStatusCounts(group string) map[string]int {
	counts := map[string]int{}
	for _, sess := range m.visibleSessions() {
		if inGroupSubtree(sess.Group, group) {
			counts[sess.Status]++
		}
	}
	return counts
}

// groupStatusGlyphs is the compact per-row rollup of a group subtree's
// live statuses (" ◐2 ?1"), idle omitted so quiet groups stay clean.
func (m *Model) groupStatusGlyphs(group string) string {
	counts := m.groupStatusCounts(group)
	var b strings.Builder
	for _, st := range []string{status.Working, status.Waiting, status.Finished, status.Errored, status.Dead} {
		if counts[st] > 0 {
			b.WriteString(" " + lipgloss.NewStyle().Foreground(statusColor(st)).
				Render(fmt.Sprintf("%s%d", statusGlyph(st), counts[st])))
		}
	}
	return b.String()
}

// previewDangerSeqs strips capture sequences that would scroll or clear the
// outer manager terminal when embedded in View output: erase (K/J), scroll
// (S/T), insert/delete lines (L/M), set scroll region (r), and the 7-bit
// index / reverse-index / next-line controls (D/M/E).
var previewDangerSeqs = regexp.MustCompile(
	`\x1b\[[0-9;]*[KJLMSTr]|\x1b[DEM]`,
)

func previewLine(line string, width int) string {
	line = previewDangerSeqs.ReplaceAllString(line, "")
	line = strings.Map(func(r rune) rune {
		if r < 0x20 && r != 0x1b && r != '\t' {
			return -1
		}
		return r
	}, line)
	if ansi.StringWidth(line) > width {
		line = ansi.Truncate(line, width, "")
	}
	if strings.ContainsRune(line, 0x1b) {
		line += "\x1b[0m"
	}
	return line
}

// paneExact returns up to n lines of pane text as captured, preserving
// blank rows so a full-screen agent TUI looks the same in the preview.
// When the capture is taller than the panel (stale size), the bottom n
// lines are kept — the visible end of the pane.
func paneExact(pane string, n int) []string {
	if n <= 0 || pane == "" {
		return nil
	}
	// capture-pane often ends with a trailing newline; drop only that.
	pane = strings.TrimSuffix(pane, "\n")
	lines := strings.Split(pane, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// paneTail returns the last n content-bearing lines of pane text with
// blank runs collapsed. Used by tests and any caller that wants a dense
// log-style excerpt rather than a 1:1 TUI frame.
func paneTail(pane string, n int) []string {
	if n <= 0 || pane == "" {
		return nil
	}
	blank := func(line string) bool {
		return strings.TrimSpace(ansi.Strip(line)) == ""
	}
	var lines []string
	for _, line := range strings.Split(pane, "\n") {
		if blank(line) && len(lines) > 0 && blank(lines[len(lines)-1]) {
			continue
		}
		lines = append(lines, line)
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
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

// viewFooter lists every shortcut, wrapping onto extra lines when the
// terminal is too narrow for one.
func (m *Model) viewFooter() string {
	// The footer carries the handful of keys a session is actually driven
	// with; ? opens the full map, so the rest stay out of the frame.
	pairs := [][2]string{
		{"↑↓", "navigate"}, {"↵", "attach"}, {"n", "new"}, {"g", "group"},
		{"space", "prompt"}, {"ctrl+r", "review"}, {"/", "search"},
		{"s", "settings"}, {"?", "keys"}, {"q", "quit"},
	}
	if m.quick.active {
		pairs = [][2]string{
			{"↵", "send"}, {"↑↓", "switch target"}, {"⇥", "tool: " + m.quickTool()},
			{"esc", "close"},
		}
	}
	if m.resizeMode {
		pairs = [][2]string{
			{"←→", "nudge"}, {"drag", "divider"}, {"| / release", "commit"}, {"esc", "cancel"},
		}
	}
	if m.mode == modeRename {
		pairs = [][2]string{{"↵", "save"}, {"esc", "cancel"}}
		if m.rename.isGroup {
			pairs = [][2]string{{"⇥", "name / path"}, {"↵", "save"}, {"esc", "cancel"}}
		} else if tool := m.renameTool(); tool != "" {
			pairs = [][2]string{{"⇥", "tool: " + tool}, {"↵", "save"}, {"esc", "cancel"}}
		}
	}
	return footerLine(pairs, m.width)
}

// footerLine wraps key hint pairs onto extra lines when the terminal is
// too narrow for one.
func footerLine(pairs [][2]string, width int) string {
	sep := subtleStyle.Render(" · ")
	sepWidth := ansi.StringWidth(sep)
	var lines []string
	line, lineWidth := "", 0
	for _, p := range pairs {
		part := keyCap(p[0], p[1])
		partWidth := ansi.StringWidth(part)
		switch {
		case line == "":
			line, lineWidth = strings.Repeat(" ", railGutter)+part, railGutter+partWidth
		case lineWidth+sepWidth+partWidth <= width:
			line += sep + part
			lineWidth += sepWidth + partWidth
		default:
			lines = append(lines, line)
			line, lineWidth = strings.Repeat(" ", railGutter)+part, railGutter+partWidth
		}
	}
	return strings.Join(append(lines, line), "\n")
}

func displayGroup(path string) string {
	if path == "" {
		return "root"
	}
	return path
}

func kv(key, value string) string {
	return labelStyle.Width(6).Render(key) + valueStyle.Render(value) + "\n"
}

func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// truncateTail keeps the end of the string (best for paths).
func truncateTail(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max || max <= 1 {
		return s
	}
	return "…" + string(runes[len(runes)-max+1:])
}

// scrollWindow keeps the cursor visible inside a height-limited window of
// single-line rows, reserving one line for each overflow indicator.
func scrollWindow(total, cursor, height int) (int, int) {
	if total <= height {
		return 0, total
	}
	visible := height - 2
	if visible < 1 {
		visible = 1
	}
	start := cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}
