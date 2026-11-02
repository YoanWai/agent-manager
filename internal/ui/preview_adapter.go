package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uipreview "github.com/YoanWai/agent-manager/internal/ui/preview"
	uicapture "github.com/YoanWai/agent-manager/internal/ui/preview/capture"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// caretRoot is the root the capture runtime's caret blink reaches.
type caretRoot struct{ *Model }

func (r caretRoot) Focused() bool { return r.focused() }
func (r caretRoot) BlinkCaret()   { r.focus.pane.Blink() }

// cursorBlink re-arms the caret timer.
func (m *Model) cursorBlink() tea.Cmd { return uicapture.CaretTick() }

// previewCaptures is the root as the preview's capture runtime reaches it.
type previewCaptures struct{ *Model }

func (r previewCaptures) Selected() (store.Session, bool) { return r.selected() }

func (r previewCaptures) Generation() uint64 { return r.focus.pane.PreviewGeneration() }

func (r previewCaptures) Accepts(gen uint64) bool { return r.focus.pane.AcceptPreview(gen) }

// CaptureWanted reports whether a capture of sess would be painted. A
// session with a control client already pushes every frame, and review and
// the modal screens have no preview to feed.
func (r previewCaptures) CaptureWanted(sess store.Session) bool {
	if !r.previewFeeds() {
		return false
	}
	return !r.focus.runtime.Serving(sess.ID)
}

func (r previewCaptures) WatchSelection() { r.watchSelection() }

func (r previewCaptures) ArmTick(withRemote bool) tea.Cmd {
	if !withRemote {
		return r.workspace.capture.Tick(r)
	}
	return tea.Batch(r.tickRemotePreview(), r.workspace.capture.Tick(r))
}

func (r previewCaptures) ApplyCapture(sessID, preview string, proc sysstat.ProcStat) {
	r.setPreview(sessID, preview)
	r.workspace.proc, r.workspace.procFor = proc, sessID
}

func (r previewCaptures) ReportErr(err error) { r.reportErr(err.Error()) }

// previewInterval is the cadence the current selection earns.
func (m *Model) previewInterval() time.Duration {
	return m.workspace.capture.Interval(previewCaptures{m})
}

func (m *Model) schedulePreview() tea.Cmd {
	return m.workspace.capture.Schedule(m.focus.pane.PreviewGeneration())
}

func (m *Model) previewCmd(sess store.Session, gen uint64) tea.Cmd {
	return m.workspace.capture.Capture(sess, gen)
}

func (m *Model) holdTypedPrompt(cand uicapture.TypedPrompt) { m.workspace.capture.Hold(cand) }

// commitTypedPrompt records each stashed draft as its session's last prompt
// once that session runs with it.
func (m *Model) commitTypedPrompt() {
	m.workspace.capture.Commit(m.workspace.sessions, time.Now(), func(sess store.Session, text string) {
		m.dispatchInput(focusruntime.Request{Kind: focusruntime.Prompt, Session: sess, Text: text})
	})
}

// setPreview stores a polled or ticked pane capture, unless the session's
// control client is already pushing frames. Those two paths sample at
// different times, and a capture taken a second ago repainting over a
// pushed one is what makes typed characters blink in and out.
func (m *Model) setPreview(sessID, preview string) {
	if sessID != "" && m.focus.runtime.Serving(sessID) {
		return
	}
	// A scrolled-back pane holds still on this path too: without a control
	// client the poll is the only source of frames, and a live bottom
	// landing mid-read is the same yank the pushed frames are held back
	// from.
	if m.focus.pane.ScrolledBack() {
		return
	}
	m.workspace.preview = preview
}

func (m *Model) reviewNeedsLoader() bool {
	state := m.review.Snapshot()
	if m.base != baseReview || !state.Active {
		return false
	}
	if state.Loading && len(state.Set.Files) == 0 {
		return true
	}
	fd := m.currentFileDiff()
	return fd != nil && !fd.Loaded() && !m.diffFileHidden(fd)
}

func (m *Model) needsLoaderTick() bool {
	return m.startup.Booting || m.rail.HasStatus(status.Starting) || m.reviewNeedsLoader() || m.rail.HasWorkingLoader()
}

func (m *Model) routePreviewMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if cmd, ok := uicapture.RouteCaret(caretRoot{m}, msg); ok {
		return routed(m, cmd)
	}
	if cmd, ok := m.workspace.capture.Route(previewCaptures{m}, msg); ok {
		return routed(m, cmd)
	}
	return nil, nil, false
}

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
			out[i] = contentLine{Text: gutter + line}
		}
		return out
	}

	body := ours(uitheme.SplitLines(m.viewDetail(inner)))
	rest := height - len(body) - 1
	if rest >= 3 {
		if group, ok := m.selectedGroup(); ok {
			body = append(body, contentLine{Rule: true})
			body = append(body, ours(uitheme.SplitLines(m.viewGroupAgents(group, inner, rest)))...)
		} else {
			separator := contentLine{Rule: true}
			if m.focused() {
				separator = contentLine{Text: focusTopRule(width, m.services.keys), Raw: true}
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
	return uitheme.KeyCap(glyph, label)
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
	return startingLoader(m.glyphs(), starting, m.workspace.preview, width, height, m.startup.LoadPhase)
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
			CursorStyle:    lipgloss.NewStyle().Foreground(uitheme.ColorBg()).Background(uitheme.ColorAccent()),
			SelectionStyle: lipgloss.NewStyle().Foreground(uitheme.ColorBg()).Background(uitheme.ColorAccent2()),
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
		if len(frameOrigin) > 0 && loader == nil {
			lines := uipreview.Exact(m.workspace.preview, height, width, m.paneCaretRow())
			m.layout.previewLinks = uifocus.ReadPaneLinks(lines, uifocus.Box{
				X: m.focusPaneOriginX(), Y: frameOrigin[0] + height - len(lines),
				Width: width, Height: len(lines), Valid: true,
			})
		}
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
		return []contentLine{{Text: gutter + uitheme.MutedStyle().Render("(no output yet)")}}
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
		lines = append(lines, contentLine{Text: row, Raw: true})
	}
	for len(lines) < height {
		lines = append(lines, contentLine{Raw: true})
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
	return uipreview.FactRow(uitheme.LabelStyle().Render(uipreview.FactLabel(label)), value, right, width)
}

// viewDetail heads the content column: the selected session's name, its
// state, and the facts that place it (group, directory, age, usage).
func (m *Model) viewDetail(width int) string {
	sess, ok := m.selected()
	if !ok {
		if group, isGroup := m.selectedGroup(); isGroup {
			return m.viewGroupDetail(group, width)
		}
		return uitheme.MutedStyle().Render("Select a session to inspect it.")
	}
	tool := sess.Tool
	if s, ok := surfaceOf[*uidialogs.RenameSurface](m); ok && !s.IsGroup() && s.SessionID() == sess.ID {
		if picked := s.Tool(); picked != "" {
			tool = picked
		}
	}

	state := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).
		Render(m.glyphs().StatusGlyph(sess.Status)+" "+statusLabel(sess.Status)) +
		uitheme.SubtleStyle().Render(" · "+uiframe.RelSince(uipreview.LastActivity(sess.CreatedAt, sess.LastStatusAt))+m.elsewhereNote(sess))
	if sess.AfterTurn != "" {
		state += uitheme.SubtleStyle().Render(" · ") + m.afterTurnBadge(sess.AfterTurn) + uitheme.SubtleStyle().Render(" "+afterTurnNote[sess.AfterTurn])
	}
	// The branch a worktree session lives on is the fact that tells it apart
	// from its siblings, so it rides beside the tool while the row has room,
	// and the tool chip goes before the name does.
	name := lipgloss.NewStyle().Foreground(uitheme.ColorBright()).Bold(true).Render(m.displayName(sess))
	// Ahead of the name because FitColumns trims the head from its tail: the
	// rail still shows the name, while a trimmed badge leaves no trace that
	// anything is waiting.
	if queued := m.workspace.queuedMessages[sess.ID]; queued > 0 {
		name = inboxBadge(m.glyphs(), queued) + " " + name
	}
	withTool := name + "  " + uitheme.ChipStyle().Render(tool)
	heads := []string{withTool, name}
	if sess.WorktreeBranch != "" {
		heads = append([]string{withTool + " " + uitheme.ChipStyle().Render(m.glyphs().BranchGlyph+" "+sess.WorktreeBranch)}, heads...)
	}

	usage := ""
	if m.workspace.procFor == sess.ID && m.workspace.proc.OK {
		usage = uitheme.LabelStyle().Render("cpu ") + uitheme.ValueStyle().Render(fmt.Sprintf("%.1f%%", m.workspace.proc.CPUPercent)) +
			uitheme.SubtleStyle().Render(" · ") + uitheme.LabelStyle().Render("ram ") +
			uitheme.ValueStyle().Render(fmt.Sprintf("%.1f%%", m.workspace.proc.RamPercent)) +
			uitheme.SubtleStyle().Render(" · ") + uitheme.ValueStyle().Render(uiframe.HumanBytes(m.workspace.proc.RSS))
	}
	started := uitheme.SubtleStyle().Render("started " + uiframe.RelSince(sess.CreatedAt))
	group := lipgloss.NewStyle().Foreground(uitheme.ColorAccent2()).Render(uiframe.DisplayGroup(sess.Group))
	dir := func(room int) string {
		return uitheme.MutedStyle().Render(presentation.TruncateTail(m.sessionDir(sess), room))
	}
	return uipreview.FitColumns(heads, []string{state}, width) + "\n" +
		factRow("group", uipreview.PlainValue(group), started, width) + "\n" +
		factRow("dir", dir, usage, width)
}

// viewGroupDetail heads the content column for a group: its name, how many
// agents sit under it, where they start, and what they are all doing.
func (m *Model) viewGroupDetail(group string, width int) string {
	count := uirail.GroupSessionCount(m.railAgents(), group)
	countLabel := fmt.Sprintf("%d agents", count)
	if count == 1 {
		countLabel = "1 agent"
	}
	title := lipgloss.NewStyle().Foreground(uitheme.ColorAccent2()).Bold(true).Render(uiframe.DisplayGroup(group))
	head := uipreview.FitColumns([]string{title + "  " + uitheme.ChipStyle().Render(countLabel), title}, []string{""}, width)

	if s, ok := surfaceOf[*uidialogs.RenameSurface](m); ok && s.IsGroup() && s.Path() == group {
		return head + "\n" + m.viewRenameGroupFields(s, group, width)
	}

	path := m.workspace.groupPaths[group]
	source := ""
	if path == "" {
		path = m.capturedGroupDefaultDir(group)
		source = uitheme.SubtleStyle().Render("inherited")
	}
	dir := func(room int) string { return uitheme.MutedStyle().Render(presentation.TruncateTail(path, room)) }
	lines := []string{head, factRow("dir", dir, source, width)}
	if breakdown := uirail.GroupStatusBreakdown(m.railTheme(), m.railAgents(), group); breakdown != "" {
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

// viewGroupAgents lists a group's sessions where a session's pane preview
// would sit, so a group reads as a roster: one row per agent, its name, the
// CLI running it, and what it is doing.
func (m *Model) viewGroupAgents(group string, width, height int) string {
	total := uirail.GroupSessionCount(m.railAgents(), group)
	if total == 0 {
		none := "(none yet)"
		if prompt := m.listGlyph(keybind.Prompt); prompt != "" {
			none = "(none yet, press " + prompt + " to spawn one)"
		}
		return uitheme.SubtleStyle().Render("agents") + "\n" + uitheme.MutedStyle().Render(none)
	}

	var rows []uipreview.RosterRow
	overflow := ""
	shown := 0
	for _, sess := range m.listedAgents() {
		if !uirail.InGroupSubtree(sess.Group, group) {
			continue
		}
		if shown >= height-2 && total > shown+1 {
			overflow = uitheme.SubtleStyle().Render(fmt.Sprintf("… %d more", total-shown))
			break
		}
		tint := lipgloss.NewStyle().Foreground(statusColor(sess.Status))
		rows = append(rows, uipreview.RosterRow{
			Name:  tint.Render(m.glyphs().StatusGlyph(sess.Status)) + " " + uitheme.ValueStyle().Render(m.displayName(sess)),
			Tool:  uitheme.SubtleStyle().Render(sess.Tool),
			State: tint.Render(statusLabel(sess.Status)) + uitheme.SubtleStyle().Render(" · "+uiframe.RelSince(uipreview.LastActivity(sess.CreatedAt, sess.LastStatusAt))),
		})
		shown++
	}

	title := uipreview.RosterRow{
		Name:  uitheme.SubtleStyle().Render("agent"),
		Tool:  uitheme.SubtleStyle().Render("tool"),
		State: uitheme.SubtleStyle().Render("last activity"),
	}
	lines := uipreview.Roster(title, rows, width)
	if overflow != "" {
		lines = append(lines, overflow)
	}
	return strings.Join(lines, "\n")
}
