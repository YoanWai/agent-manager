package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) View() string {
	if m.width == 0 {
		return m.syncCursorAnchor("loading...")
	}
	var frame string
	switch m.mode {
	case modeForm:
		frame = m.viewForm()
	case modeHelp:
		frame = m.viewHelp()
	case modeConfirmDelete:
		frame = m.viewConfirm()
	case modeLaunchHint:
		frame = m.viewLaunchHint()
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
	return m.syncCursorAnchor(clampFrame(frame, m.height))
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

// paneTargetSize is the tmux window size sessions are pinned to: the
// preview panel's box in the split, the whole terminal body in the full
// screen layout, which paints captures across the full width.
func (m *Model) paneTargetSize() (int, int) {
	if m.fullLayout {
		width, height := m.width, m.listBodyHeight()
		if width < 1 {
			width = 1
		}
		if height < 3 {
			height = 3
		}
		return width, height
	}
	return m.previewPaneWidth(), m.previewPaneHeight()
}

// previewPaneHeight is the rows of session pane content the Preview
// section can show with nothing transient over it, which is what tmux is
// pinned to: the painted view crops a taller pane, where resizing it for
// a passing overlay would cost an agent a full transcript redraw.
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
	// Mirrors contentLines: the detail head, the seam, then the pane
	// filling everything below.
	rest := avail - lipgloss.Height(m.viewDetail(inner)) - 1
	if rest < 3 {
		// Preview section is hidden; keep a tiny pane for create/attach paths.
		return 3
	}
	return rest
}

// fullFocus reports whether the focused session owns the whole terminal
// body, which is how the full screen layout opens a session.
func (m *Model) fullFocus() bool {
	return m.fullLayout && m.mode == modeFocus
}

// focusPaneRows is the rows of pane content the focused view paints: the
// whole body when the session is open full screen, the preview panel's
// rows in the split.
func (m *Model) focusPaneRows() int {
	if m.fullFocus() {
		return m.listBodyHeight()
	}
	return m.previewPaneHeight()
}

// statusLine is the transient message: prompts, search, and self-dismissing
// errors. It floats in a card over the frame rather than taking a row, so the
// body keeps its height whether or not a notice is up.
func (m *Model) statusLine() string {
	switch {
	// Errors outrank the focus notices: a scrolled or focused pane must
	// not hide a failure report.
	case m.mode == modeFocus && m.errBar.text != "":
		return m.statusMessage("✕", "●")
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
		return m.statusMessage("✕", "●")
	case m.diff.notice != "":
		return doneStyle.Render("● " + escapeControlsInline(m.diff.notice))
	default:
		return ""
	}
}

// statusMessage styles whatever sits on the status bar: an action that went
// through reads as an outcome, everything else as a failure, in the glyphs
// the calling surface marks the two with.
func (m *Model) statusMessage(fail, done string) string {
	text := escapeControlsInline(m.errBar.text)
	if m.errBar.worked() {
		return doneStyle.Render(done + " " + text)
	}
	return errStyle.Render(fail + " " + text)
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
	return lead + " " + textInputView(m.rename.input)
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
// costs agents like Codex their whole scrollback (#369), so a plain crop
// would show the blank tail such a pane leaves under short output and drop
// the output itself. The window instead ends at the last painted row, but
// never retreats past the panel's own row count (short output keeps its
// top anchor, and a not-yet-painted pane keeps its blank rows hit-testable)
// or past the caret's row, which is the cell someone is typing into.
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
	if end < n {
		end = n
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
		return m.transientFooter(legendSection{title: "Prompt", pairs: [][2]string{
			{"↵", "send"}, {"↑↓", "switch target"}, {"tab", "tool: " + m.quickTool()},
			{"shift+tab", "worktree: " + worktreeHint}, {"esc", "close"},
		}})
	}
	if m.split.resizeMode {
		return m.transientFooter(legendSection{title: "Resize", pairs: [][2]string{
			{"←→", "nudge"}, {"drag", "divider"}, {"| / release", "commit"}, {"esc", "cancel"},
		}})
	}
	if m.mode == modeRename {
		pairs := [][2]string{{"↵", "save"}, {"esc", "cancel"}}
		if m.rename.isGroup {
			pairs = [][2]string{{"tab", "name / path"}, {"↵", "save"}, {"esc", "cancel"}}
		} else if tool := m.renameTool(); tool != "" {
			pairs = [][2]string{{"tab", "tool: " + tool}, {"↵", "save"}, {"esc", "cancel"}}
		}
		return m.transientFooter(legendSection{title: "Rename", pairs: pairs})
	}
	// Focused, the keyboard belongs to the agent: the tier says so in its
	// title, carries the few keys the manager keeps, and drops the app-wide
	// tier, which would name keys the agent receives.
	if m.mode == modeFocus {
		pairs := [][2]string{
			{"typing", "to agent"},
			{m.keys.Binding(keybind.Detach).Label(), "back"},
		}
		if m.arrowStep {
			pairs = append(pairs, [2]string{"←", "prompt start: back"})
		}
		if label := m.keys.Binding(keybind.Review).Label(); label != "" {
			pairs = append(pairs, [2]string{label, "review"})
		}
		if label := m.keys.Binding(keybind.Editor).Label(); label != "" {
			pairs = append(pairs, [2]string{label, "editor"})
		}
		// The footer holds one row: the word and line gestures are in the
		// key map, where there is room to name all three.
		pairs = append(pairs, [2]string{"drag / click", "copy"})
		if m.pane.mouse {
			pairs = append(pairs, [2]string{"click / alt+drag", "agent UI"})
		}
		return m.transientFooter(legendSection{title: "Focused", pairs: pairs})
	}
	return m.listFooter()
}

func (m *Model) listFooter() string {
	return legendBar([]legendSection{m.rowLegend(), m.viewLegend()}, m.width)
}

// transientFooter renders one tier at the list footer's height: the footer
// sets the preview box, and a box that moves resizes every session's pane,
// which costs an agent drawing on the normal screen a full transcript redraw.
// The full screen layout has no preview box to hold still, so a tier there
// takes the one row it needs and hands the rest to the body.
func (m *Model) transientFooter(section legendSection) string {
	bar := legendBar([]legendSection{section}, m.width)
	if m.fullLayout {
		return bar
	}
	return padToHeight(bar, lipgloss.Height(m.listFooter()))
}

// rowLegend is the tier for the entry under the cursor: what this session or
// this group can be told to do.
func (m *Model) rowLegend() legendSection {
	k := m.listGlyph
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
		pairs := [][2]string{{k(keybind.Open), foldAction}}
		if m.arrowStep {
			pairs = append(pairs, [2]string{k(keybind.StepOut) + k(keybind.StepIn), "close / open"})
		}
		pairs = append(pairs, [][2]string{
			{k(keybind.Editor), "editor"}, {k(keybind.Rename), "rename"}, {k(keybind.Move), "move"},
			m.legendPair(keybind.Kill, "kill", keybind.KillAll, "all"), m.legendPair(keybind.Revive, "revive", keybind.ReviveAll, "all"),
			m.legendPair(keybind.Archive, "archive", keybind.Restore, "restore"), {k(keybind.Delete), "delete"},
		}...)
		return legendSection{title: "Group", pairs: legendPairsBound(pairs)}
	}
	title := "Session"
	conversation := [][2]string{{k(keybind.Prompt), "prompt"}, {k(keybind.Review), "review"}, {k(keybind.Fork), "fork"}}
	if m.isShell(row.sess.Tool) {
		// A shell has no conversation, so the keys that would prompt,
		// review or fork one are left off rather than offered and refused.
		title, conversation = "Shell", nil
	}
	pairs := [][2]string{{k(keybind.Open), enterHint}, {k(keybind.Attach), attachHint}}
	if m.arrowStep {
		pairs = append(pairs, [2]string{k(keybind.StepIn), "focus"})
	}
	if row.sess.Status == status.Finished && !row.sess.Archived {
		pairs = append(pairs, [2]string{k(keybind.MarkIdle), "mark idle"})
	}
	pairs = append(pairs, conversation...)
	// o sits outside the conversation keys: a shell's directory is worth
	// opening as much as an agent's.
	pairs = append(pairs, [][2]string{
		{k(keybind.Editor), "editor"}, {k(keybind.Rename), "rename"}, {k(keybind.Move), "move"},
		m.legendPair(keybind.Kill, "kill", keybind.KillAll, "all"), m.legendPair(keybind.Revive, "revive", keybind.ReviveAll, "all"), {k(keybind.Restart), "restart"},
		m.legendPair(keybind.Archive, "archive", keybind.Restore, "restore"), {k(keybind.Delete), "delete"},
	}...)
	return legendSection{title: title, pairs: legendPairsBound(pairs)}
}

// legendPair is one hint for two sibling actions, "x/X kill / all"; with
// one of them turned off the hint keeps only the half that still answers.
func (m *Model) legendPair(first, firstLabel, second, secondLabel string) [2]string {
	switch {
	case m.listGlyph(first) == "":
		return [2]string{m.listGlyph(second), secondLabel}
	case m.listGlyph(second) == "":
		return [2]string{m.listGlyph(first), firstLabel}
	}
	return [2]string{m.listGlyph(first, second), firstLabel + " / " + secondLabel}
}

// legendPairsBound drops the hints for actions with no key left: a footer
// naming a key that does nothing would send the reader the wrong way.
func legendPairsBound(pairs [][2]string) [][2]string {
	kept := pairs[:0]
	for _, pair := range pairs {
		if pair[0] != "" {
			kept = append(kept, pair)
		}
	}
	return kept
}

// listGlyph is the footer's spelling of the keys on one or two list
// actions, the pair joined the way "x/X" always was.
func (m *Model) listGlyph(actions ...string) string {
	joined := ""
	for _, action := range actions {
		glyph := m.listKeys.Binding(action).Glyph("/")
		if glyph == "" {
			continue
		}
		if joined != "" {
			joined += "/"
		}
		joined += glyph
	}
	return joined
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
	// something, the filters, then the keys a user already knows to look for.
	k := m.listGlyph
	pairs := [][2]string{{strings.TrimSpace(k(keybind.Up) + " " + k(keybind.Down)), "navigate"}}
	pairs = append(pairs, [][2]string{
		{k(keybind.NewSession), "new"}, {k(keybind.Terminal), "terminal"}, {k(keybind.NewGroup), "group"}, {k(keybind.Search), "search"},
		{k(keybind.Archived), archivedAction}, {k(keybind.Filter), statusFilterAction}, {k(keybind.EmptyGroups), emptyGroupsAction},
		{k(keybind.Help), "keys"}, {k(keybind.Quit), "quit"},
		{k(keybind.ReorderUp, keybind.ReorderDown), "reorder"}, {k(keybind.FoldAll), foldAllAction}, {k(keybind.Resize), "resize"}, {k(keybind.Settings), "settings"},
	}...)
	return legendSection{title: "View", quiet: true, pairs: legendPairsBound(pairs)}
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

// shortHome writes a path under the home directory the way a shell prompt
// does, so a narrow slot spends its room on the part that tells the
// directories apart rather than on the same prefix every session shares.
func shortHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || path == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// truncateTail keeps the end of the string (best for paths).
func truncateTail(s string, max int) string {
	runes := []rune(s)
	if max <= 0 {
		return ""
	}
	if len(runes) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return "…" + string(runes[len(runes)-max+1:])
}

func truncatePath(path string, limit int) string {
	runes := []rune(path)
	if limit <= 0 {
		return ""
	}
	if len(runes) <= limit {
		return path
	}
	if limit == 1 {
		return "…"
	}
	tail := string(runes[len(runes)-limit+1:])
	if i := strings.IndexByte(tail, '/'); i >= 0 && i < len(tail)-1 {
		return "…" + tail[i:]
	}
	return "…" + tail
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
