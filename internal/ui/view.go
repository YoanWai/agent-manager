package ui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/unicode/bidi"
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
	case modeConfirmDelete:
		frame = m.viewConfirm()
	case modeSettings:
		frame = m.viewSettings()
	case modeFork:
		frame = m.viewFork()
	case modeMove:
		frame = m.viewMove()
	case modeRepoPick:
		frame = m.viewRepoPick()
	case modeGroupForm:
		frame = m.viewGroupForm()
	case modeDiff:
		frame = m.viewDiffFull()
	case modeNotices:
		frame = m.viewNotices()
	default:
		frame = m.viewListFrame()
	}
	return clampFrame(pinRowsLTR(frame), m.height)
}

// pinRowsLTR prepends an LRM to any frame row whose first strong character
// is RTL (a Hebrew session name in the rail, say). UAX#9 takes paragraph
// direction from that first strong character, so without the mark a bidi
// host right-justifies the whole row and pulls it out of column. The mark
// is zero width and applied after layout, so geometry is untouched.
func pinRowsLTR(frame string) string {
	lines := strings.Split(frame, "\n")
	changed := false
	for i, line := range lines {
	scan:
		for _, r := range line {
			props, _ := bidi.LookupRune(r)
			switch props.Class() {
			case bidi.L:
				break scan
			case bidi.R, bidi.AL:
				lines[i] = "\u200e" + line
				changed = true
				break scan
			}
		}
	}
	if !changed {
		return frame
	}
	return strings.Join(lines, "\n")
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
		// Padding rows carry the backdrop too; a bare row would show the
		// terminal's own background through the bottom of a short frame.
		lines = append(lines, paint("", 1, backdropHex()))
	}
	return strings.Join(lines, "\n")
}

// splitWidths is the body's horizontal split: the sessions panel takes
// splitRatio of the terminal (default 30%), floored so both sides stay
// usable when the window is wide enough.
func (m *Model) splitWidths() (int, int) {
	if m.width <= 0 {
		return 0, 0
	}
	ratio := m.split.ratio
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
	// The seam and bleed columns between the rail and the content are not
	// the pane's. The rest is: captured output spans the column, so a
	// session is sized to the full width its preview paints into.
	w := rightWidth - 2
	if w < 1 {
		return 1
	}
	return w
}

// previewPaneHeight is the rows of session pane content the Preview
// section can show. Mirrors viewSidebar + viewPreview so tmux is pinned
// to the same box the UI paints into.
func (m *Model) previewPaneHeight() int {
	// Our own blocks wrap inside the column's gutters, so their heights
	// are measured at that width rather than at the preview's full span.
	inner := m.previewPaneWidth() - 2*contentGutter
	if inner < 1 {
		inner = 1
	}
	if m.height < 1 {
		return 1
	}
	avail := m.listBodyHeight()
	if avail < 1 {
		return 1
	}
	if m.quick.active {
		avail -= lipgloss.Height(m.viewQuickBar(inner)) + 1
	}
	// Mirrors contentLines: the detail head, the seam, then the pane
	// filling everything below.
	rest := avail - lipgloss.Height(m.viewDetail(inner)) - 1
	if rest < 3 {
		// Preview section is hidden; keep a tiny pane for create/attach paths.
		return 3
	}
	return rest
}

// statusLine is the transient message: prompts, search, and self-dismissing
// errors. It floats in a card over the frame rather than taking a row, so the
// body keeps its height whether or not a notice is up.
func (m *Model) statusLine() string {
	switch {
	// Errors outrank the focus notices: a scrolled or focused pane must
	// not hide a failure report.
	case m.mode == modeFocus && m.errBar.text != "":
		return errStyle.Render("✕ " + m.errBar.text)
	case m.scrolledBack():
		return keyStyle.Render("scrolled ") +
			subtleStyle.Render(fmt.Sprintf("%d lines back · wheel down or type to catch up", m.focusScroll))
	case m.mode == modeFocus && m.copied > 0:
		return keyStyle.Render("copied ") +
			subtleStyle.Render(fmt.Sprintf("%d chars to clipboard", m.copied))
	case m.split.resizeMode:
		hint := "←→ resize · drag divider · enter set · esc cancel"
		if m.split.dragging {
			hint = "release to set · esc cancels"
		}
		return keyStyle.Render("resize ") + subtleStyle.Render(hint)
	case m.errBar.text != "":
		return errStyle.Render("✕ " + m.errBar.text)
	case m.diff.notice != "":
		return lipgloss.NewStyle().Foreground(colorFinished).Render("● " + m.diff.notice)
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
	for _, sess := range m.listedAgents() {
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
		lead = m.sessionGlyph(entry.sess)
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

const quickBarMaxRows = 5

// quickBarRows is the rows the typed text needs at the current width,
// capped so the bar never swallows the sidebar. Single-line values (the
// normal case) count exact soft-wrap rows; pasted multi-line values are
// estimated, with the textarea scrolling to keep the cursor visible.
func (m *Model) quickBarRows(textWidth int) int {
	return textareaRows(m.quick.input, textWidth, quickBarMaxRows)
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

// groupStatusBreakdown renders "2 working · 1 waiting" for the subtree,
// each count tinted in its status color, skipping zero statuses.
func (m *Model) groupStatusBreakdown(group string) string {
	counts := m.groupStatusCounts(group)
	var parts []string
	for _, st := range []string{status.Working, status.Waiting, status.Finished, status.Errored, status.Idle, status.Dead} {
		if counts[st] > 0 {
			// The count carries the state's color, the word stays quiet: a
			// rollup line should read as one texture, not as six labels
			// competing with the session names above it.
			parts = append(parts, lipgloss.NewStyle().Foreground(statusColor(st)).
				Render(fmt.Sprintf("%d", counts[st]))+subtleStyle.Render(" "+st))
		}
	}
	return strings.Join(parts, subtleStyle.Render(" · "))
}

func (m *Model) groupStatusCounts(group string) map[string]int {
	counts := map[string]int{}
	for _, sess := range m.listedAgents() {
		if inGroupSubtree(sess.Group, group) {
			counts[sess.Status]++
		}
	}
	return counts
}

// groupStatusGlyphs is the subtree's rollup written in dots: each state
// present as its own glyph and count, tinted its own color. A one-line
// group row has no width to spell the states out, and the glyphs are the
// same ones the sessions under it wear.
func (m *Model) groupStatusGlyphs(group string) string {
	counts := m.groupStatusCounts(group)
	var parts []string
	for _, st := range []string{status.Working, status.Waiting, status.Finished, status.Errored, status.Idle, status.Dead} {
		if counts[st] == 0 {
			continue
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(statusColor(st)).
			Render(fmt.Sprintf("%s %d", statusGlyph(st), counts[st])))
	}
	return strings.Join(parts, subtleStyle.Render("  "))
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
	if captureOnDark {
		line = captureOpen + reassertCaptureColors(line)
	}
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
		if captureOnDark {
			line += captureBgSeq + strings.Repeat(" ", width-w) + "\x1b[0m"
		} else {
			line += strings.Repeat(" ", width-w)
		}
	}
	if !containsRTL(line) {
		// Terminals that give format characters a cell (Windows Terminal
		// under conpty) would overflow the row and scroll the whole frame.
		return line
	}
	// LRM is strong LTR (zero width). On rows where the rail is only neutrals,
	// a leading Hebrew run is the line's first strong character; hosts that
	// right-justify RTL paragraphs then pull the pane into the rail. Isolate
	// plus a full-width pad keep the run inside the content column.
	return "\u200e\u2066" + line + "\u2069"
}

func containsRTL(line string) bool {
	for _, r := range line {
		if r < 0x0590 {
			continue
		}
		props, _ := bidi.LookupRune(r)
		switch props.Class() {
		case bidi.R, bidi.AL:
			return true
		}
	}
	return false
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

func padToHeight(s string, height int) string {
	missing := height - lipgloss.Height(s)
	if missing > 0 {
		s += strings.Repeat("\n", missing)
	}
	return s
}

// viewFooter is the app's legend: a tier of keys for whatever the cursor is
// on, then a quieter tier for the keys that always apply. A transient mode
// (quick prompt, rename, resize) owns the legend alone while it is up.
func (m *Model) viewFooter() string {
	if m.quick.active {
		worktreeHint := "off"
		switch {
		case !m.worktreeCapable(m.quickTargetDir()):
			worktreeHint = worktreeUnavailable
		case m.quickWorktreeOn():
			worktreeHint = "on"
		}
		return legendBar([]legendSection{{title: "Prompt", pairs: [][2]string{
			{"↵", "send"}, {"↑↓", "switch target"}, {"tab", "tool: " + m.quickTool()},
			{"shift+tab", "worktree: " + worktreeHint}, {"esc", "close"},
		}}}, m.width)
	}
	if m.split.resizeMode {
		return legendBar([]legendSection{{title: "Resize", pairs: [][2]string{
			{"←→", "nudge"}, {"drag", "divider"}, {"| / release", "commit"}, {"esc", "cancel"},
		}}}, m.width)
	}
	if m.mode == modeRename {
		pairs := [][2]string{{"↵", "save"}, {"esc", "cancel"}}
		if m.rename.isGroup {
			pairs = [][2]string{{"tab", "name / path"}, {"↵", "save"}, {"esc", "cancel"}}
		} else if tool := m.renameTool(); tool != "" {
			pairs = [][2]string{{"tab", "tool: " + tool}, {"↵", "save"}, {"esc", "cancel"}}
		}
		return legendBar([]legendSection{{title: "Rename", pairs: pairs}}, m.width)
	}
	// Focused, the keyboard belongs to the agent: the tier says so in its
	// title, carries the few keys the manager keeps, and drops the app-wide
	// tier, which would name keys the agent receives.
	if m.mode == modeFocus {
		return legendBar([]legendSection{{title: "Focused", pairs: [][2]string{
			{"typing", "goes to the agent"},
			{"ctrl+q / ctrl+\\", "back to manager"},
			{"drag / double / triple click", "copy"},
		}}}, m.width)
	}
	return legendBar([]legendSection{m.rowLegend(), m.viewLegend()}, m.width)
}

// rowLegend is the tier for the entry under the cursor: what this session or
// this group can be told to do.
func (m *Model) rowLegend() legendSection {
	enterHint, attachHint := "focus / fold", "attach"
	if !m.enterFocuses() {
		enterHint, attachHint = "attach / fold", "focus"
	}
	row, ok := m.selectedRow()
	if !ok {
		return legendSection{}
	}
	if row.isGroup {
		foldAction := "fold"
		if m.collapsed[row.group] {
			foldAction = "unfold"
		}
		return legendSection{title: "Group", pairs: [][2]string{
			{"↵", foldAction}, {"r", "rename"},
			{"x/X", "kill / all"}, {"v/V", "revive / all"},
			{"a/u", "archive / restore"}, {"d", "delete"},
		}}
	}
	title := "Session"
	conversation := [][2]string{{"space", "prompt"}, {"ctrl+r", "review"}, {"f", "fork"}}
	if m.isShell(row.sess.Tool) {
		// A shell has no conversation, so the keys that would prompt,
		// review or fork one are left off rather than offered and refused.
		title, conversation = "Shell", nil
	}
	pairs := [][2]string{{"↵", enterHint}, {"A", attachHint}}
	if row.sess.Status == status.Finished && !row.sess.Archived {
		pairs = append(pairs, [2]string{".", "mark idle"})
	}
	pairs = append(pairs, conversation...)
	pairs = append(pairs, [][2]string{
		{"r", "rename"}, {"m", "move"},
		{"x/X", "kill / all"}, {"v/V", "revive / all"}, {"R", "restart"},
		{"a/u", "archive / restore"}, {"d", "delete"},
	}...)
	return legendSection{title: title, pairs: pairs}
}

// viewLegend is the tier that never changes with the cursor: moving around
// the list, filtering it, and leaving.
func (m *Model) viewLegend() legendSection {
	emptyGroupsAction := "hide empty"
	if m.hideEmptyGroups {
		emptyGroupsAction = "show empty"
	}
	statusFilterAction := "attention"
	if m.statusFilter.active() {
		statusFilterAction = "show all"
	}
	archivedAction := "archived"
	if m.showArchived {
		archivedAction = "back to active"
	}
	foldAllAction := "fold all"
	if m.allGroupsCollapsed() {
		foldAllAction = "unfold all"
	}
	// Ordered by what a narrow terminal must keep: moving around, making
	// something, the filters whose state only the footer reports, then the
	// keys a user already knows to look for.
	return legendSection{title: "View", quiet: true, pairs: [][2]string{
		{"↑↓/jk", "navigate"}, {"n", "new"}, {"T", "terminal"}, {"g", "group"}, {"/", "search"},
		{"t", archivedAction}, {"w", statusFilterAction}, {"e", emptyGroupsAction},
		{"?", "keys"}, {"q", "quit"},
		{"K/J", "reorder"}, {"F", foldAllAction}, {"|", "resize"}, {"s", "settings"},
	}}
}

func displayGroup(path string) string {
	if path == "" {
		return "root"
	}
	return path
}

// relSince is relTime worded as a moment in the past, for columns that
// answer "when did this last happen" rather than "how long has this run".
func relSince(t time.Time) string {
	return relTime(t) + " ago"
}

func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		// Five-second steps: a column of ages that ticks every second is
		// motion the eye chases for no information.
		return fmt.Sprintf("%ds", int(d.Seconds()/5)*5)
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
