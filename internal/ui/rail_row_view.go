package ui

import (
	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"time"
)

// treeGuidesAt is the ancestry trail left of a nested entry: a branch
// connector — ├─ mid-list, ╰─ for the last child — behind one guide per
// ancestor, so a group's children hang off its branch the way the legacy
// tree drew them. A slot goes quiet once its level has no further
// siblings below, which is what closes a branch off.
func (m *Model) treeGuidesAt(index int) string {
	if index < 0 || index >= len(m.rail.rows) {
		return ""
	}
	depth := m.rail.rows[index].depth
	if depth <= 0 {
		return ""
	}
	var guides strings.Builder
	for slot := 1; slot <= depth; slot++ {
		continues := m.slotContinues(index, slot)
		switch {
		case slot < depth && continues:
			guides.WriteString("│  ")
		case slot < depth:
			guides.WriteString("   ")
		case continues:
			guides.WriteString("├─ ")
		default:
			guides.WriteString("╰─ ")
		}
	}
	return subtleStyle.Render(guides.String())
}

// treeGuideTrail is the same ancestry read one line lower: every branch
// the entry's own row connected to carries straight down past its second
// line, so a two-line entry cannot leave a gap in the tree.
func (m *Model) treeGuideTrail(index int) string {
	if index < 0 || index >= len(m.rail.rows) {
		return ""
	}
	depth := m.rail.rows[index].depth
	if depth <= 0 {
		return ""
	}
	var guides strings.Builder
	for slot := 1; slot <= depth; slot++ {
		if m.slotContinues(index, slot) {
			guides.WriteString("│  ")
			continue
		}
		guides.WriteString("   ")
	}
	return subtleStyle.Render(guides.String())
}

// slotContinues reports whether the level named by slot has another entry
// below index. A slot goes quiet once its level has no further siblings,
// which is what closes a branch off.
func (m *Model) slotContinues(index, slot int) bool {
	for j := index + 1; j < len(m.rail.rows); j++ {
		if m.rail.rows[j].depth < slot {
			return false
		}
		if m.rail.rows[j].depth == slot {
			return true
		}
	}
	return false
}

// entryRowLines paints one entry, its lines tagged with the row a click on
// them selects and ended by the entry's menu button. With the mouse off
// nothing on the row answers a click, so it keeps those cells for itself.
// The handle's column is read off the painted head, where the row's tree
// depth put it. The rail starts one column in, past its edge cell.
func (m *Model) entryRowLines(entry treeRow, index, width int, tone string) []contentLine {
	selected := index == m.rail.cursor
	button := !m.renamingRow(entry) && !m.prefs.mouseDisabled
	rowWidth := width
	if button {
		rowWidth -= menuButtonWidth
	}
	var lines []contentLine
	for n, line := range splitLines(m.renderTreeRow(entry, selected, rowWidth, index, tone)) {
		if n == 0 {
			if plain := ansi.Strip(line); strings.Contains(plain, reorderGrip) {
				m.rail.handleX[rowKey(entry)] = 1 + ansi.StringWidth(plain[:strings.Index(plain, reorderGrip)])
			}
		}
		switch {
		case button && n == 0:
			line += menuButton(selected, tone)
		case button:
			line += paint("", menuButtonWidth, tone)
		}
		lines = append(lines, contentLine{text: line, tone: tone, row: index + 1})
	}
	return lines
}

// renderTreeRow paints one entry: a status dot, the name, and what the
// entry is doing set against the row's far edge. The selected entry lifts
// onto its own band instead of wearing a marker.
func (m *Model) renderTreeRow(entry treeRow, selected bool, width, index int, bg string) string {
	pad := strings.Repeat(" ", railInset)
	guides := m.treeGuidesAt(index)
	trail := m.treeGuideTrail(index)

	if m.renamingRow(entry) {
		line := pad + guides + m.renameRowInput(entry, width-railGutter-ansi.StringWidth(guides))
		row := paint(line, width, selectedHex())
		for held := m.entryHeight(entry); held > 1; held-- {
			row += "\n" + paint(pad+trail, width, selectedHex())
		}
		return row
	}

	if entry.isGroup {
		return m.renderGroupEntry(entry, selected, width, pad, guides, trail, bg)
	}
	return m.renderSessionEntry(entry, selected, width, pad, guides, trail, bg)
}

// A shell takes a caret rather than an idle dot it would never leave, but
// a pane that has gone still has to say so.
func (m *Model) sessionGlyph(sess store.Session) string {
	if sess.Status == status.Starting {
		return lipgloss.NewStyle().Foreground(statusColor(status.Starting)).
			Render(startupFrames[m.startup.startupPhase%len(startupFrames)])
	}
	resting := sess.Status != status.Dead && sess.Status != status.Errored
	if resting && m.isShell(sess.Tool) {
		return subtleStyle.Render(shellGlyph)
	}
	return lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusGlyph(sess.Status))
}

// namePlaceholder stands in for the name a spawn generated while the agent
// it asked to name itself has not answered, so the row settles on one name
// instead of flashing a throwaway one first.
const namePlaceholder = "…"

// placeholderPromptWidth caps the prompt a waiting row borrows. It is what
// the narrowest rail affords beside a starting row's state, tool and age, so
// the row keeps the shape every other row has instead of pushing a column
// off its own end.
const placeholderPromptWidth = 12

// renameGrace caps the wait for that answer. It spans the whole way there,
// the boot, the directive reaching the agent, and the command it runs, so it
// is generous; past it the session keeps the name it was given.
const renameGrace = time.Minute

// awaitedRename is what a spawn launched with: the name generated for it,
// which it falls back to, and the prompt it was given, which says which task
// the row is while it has no name of its own.
type awaitedRename struct {
	generated string
	prompt    string
}

// awaitingRename drops the record it reads as soon as the wait is over, so
// a session settling on its name needs nothing to sweep the map after it.
func (m *Model) awaitingRename(sess store.Session) bool {
	awaited, ok := m.ledger.awaitedRenames[sess.ID]
	if !ok {
		return false
	}
	if sess.Name == awaited.generated && sess.Status != status.Dead &&
		time.Since(sess.CreatedAt) < renameGrace {
		return true
	}
	delete(m.ledger.awaitedRenames, sess.ID)
	return false
}

// displayName is what every reading of a session prints, so the rail row and
// the columns beside it never disagree about who an agent is.
func (m *Model) displayName(sess store.Session) string {
	if !m.awaitingRename(sess) {
		return sess.Name
	}
	// The stored LaunchPrompt is the decorated one, carrying the rename
	// directive the agent was sent; what the row wants is what was typed.
	if preview := promptPreview(m.ledger.awaitedRenames[sess.ID].prompt); preview != "" {
		return preview
	}
	return namePlaceholder
}

// promptPreview flattens a prompt into the one short line a row can wear as
// a name, so five agents spawned in a burst say which is which right away.
// A pasted image reaches the agent as the path it was written to, which
// would name every image-first spawn the same thing, so the pictures drop
// out of the preview and the words stay.
func promptPreview(prompt string) string {
	words := make([]string, 0, len(strings.Fields(prompt)))
	for _, word := range strings.Fields(prompt) {
		if clipboard.IsPastePath(word) {
			continue
		}
		words = append(words, word)
	}
	return ansi.Truncate(strings.Join(words, " "), placeholderPromptWidth, "…")
}

func (m *Model) renderSessionEntry(entry treeRow, selected bool, width int, pad, guides, trail, bg string) string {
	sess := entry.sess
	// An archived session's pane was killed on its way in; a status frozen
	// by an older build (a "working" from before the kill recorded dead)
	// must not read as alive from inside the archive.
	if sess.Archived {
		sess.Status = status.Dead
	}
	dot := m.sessionGlyph(sess)
	nameStyle := valueStyle
	if selected {
		nameStyle = lipgloss.NewStyle().Foreground(colorBright).Bold(true)
	}
	lead := pad + guides + dot + " "
	handle := m.rowHandle(entry, selected)
	var focus, inbox string
	if selected && m.mode == modeFocus {
		focus = " " + focusBadgeStyle.Render(" FOCUS ")
	}
	if queued := m.workspace.queuedMessages[sess.ID]; queued > 0 {
		inbox = " " + inboxBadge(queued)
	}
	// A rail too narrow for the whole head shortens the name before it
	// loses a badge: a waiting message shows nowhere else on the row. With
	// no room left for any name, the focus badge goes instead: the pane
	// beside the rail already shows what is focused.
	name := m.displayName(sess)
	room := width - railGutter - ansi.StringWidth(lead+handle+focus+inbox)
	if room <= 0 {
		room += ansi.StringWidth(focus)
		focus = ""
	}
	if ansi.StringWidth(name) > room {
		name = ansi.Truncate(name, max(room, 0), "…")
	}
	head := lead + handle + m.highlightQuery(name, nameStyle) + focus + inbox

	metaStyle := subtleStyle
	if selected {
		metaStyle = mutedStyle
	}
	// A session names its state in words as well as in its dot; a group,
	// whose row rolls several states together, is left to its dots.
	meta := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusLabel(sess.Status)) +
		metaStyle.Render(" · "+sess.Tool)
	// The id only fits the roomy layout; the compact meta is already crowded.
	if sess.AgentSessionID != "" && m.prefs.comfortableRows {
		meta += metaStyle.Render(" · " + sess.AgentSessionID)
	}
	meta += metaStyle.Render(" · " + relSince(lastActivity(sess)) + m.elsewhereNote(sess))

	if m.prefs.comfortableRows {
		indent := metaIndent(pad, trail) + strings.Repeat(" ", ansi.StringWidth(handle))
		return m.tallRow(sess, head, meta, indent, selected, width, bg)
	}
	return m.compactRow(sess, head, meta, selected, width, bg)
}

// rowPromptFloor is the narrowest slot worth printing a prompt into: any
// tighter and the row shows an ellipsis where a task should be.
const rowPromptFloor = 8

// compactRow is the one-line session entry: the state picks the value
// riding between the name and the meta, the way Claude's agent view
// tells a fleet apart at a glance.
func (m *Model) compactRow(sess store.Session, head, meta string, selected bool, width int, bg string) string {
	quiet := subtleStyle
	if selected {
		quiet = mutedStyle
	}
	const gap = 2
	room := width - railGutter - ansi.StringWidth(head) - ansi.StringWidth(meta) - 2*gap
	if room >= rowPromptFloor {
		if cell := m.compactCell(sess, quiet, room); cell != "" {
			head += strings.Repeat(" ", gap) + cell
		}
	}
	return paint(rowColumns(head, meta, width-railGutter), width, bg)
}

// compactCell is what the one-line row quotes: the agent's last message
// whenever it has said anything — the question it waits on, its
// progress, its result — and the task it was given only while it has
// not. A session with neither says nothing rather than holding a dash
// mid-row.
func (m *Model) compactCell(sess store.Session, quiet lipgloss.Style, room int) string {
	if m.workspace.paneLines[sess.ID] != "" || sess.Status == status.Working {
		return m.replyCell(sess, quiet, room)
	}
	if prompt := oneLine(m.rowPrompt(sess)); prompt != "" {
		return rowGlyphStyle(current.Accent).Render("❯ ") +
			rowPromptStyle().Render(ansi.Truncate(prompt, max(room-2, 1), "…"))
	}
	return ""
}

// tallRow is the comfortable session entry: the name and meta alone on
// top, your last prompt under it, the agent's last reply under that. A
// shell keeps the name and its last output and skips the prompt line in
// between, having no one on the other side of it to quote.
func (m *Model) tallRow(sess store.Session, head, meta, indent string, selected bool, width int, bg string) string {
	quiet := subtleStyle
	if selected {
		quiet = mutedStyle
	}
	top := rowColumns(head, meta, width-railGutter)
	room := width - railGutter - ansi.StringWidth(indent) - 2
	lines := []string{paint(top, width, bg)}
	if !m.isShell(sess.Tool) {
		promptLine := indent + quiet.Render("-")
		if prompt := oneLine(m.rowPrompt(sess)); prompt != "" && room >= rowPromptFloor {
			promptLine = indent + rowGlyphStyle(current.Accent).Render("❯ ") +
				rowPromptStyle().Render(ansi.Truncate(prompt, room, "…"))
		}
		lines = append(lines, paint(promptLine, width, bg))
	}
	lines = append(lines, paint(indent+m.replyCell(sess, quiet, room+2), width, bg))
	return strings.Join(lines, "\n")
}

func rowGlyphStyle(hex string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex))
}

// rowReplyStyle washes the reply in its state's color at half strength;
// waiting stays at full strength because it needs the user.
func rowReplyStyle(state string) lipgloss.Style {
	if state == status.Waiting {
		return lipgloss.NewStyle().Foreground(statusColor(state))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(string(statusColor(state)), current.Subtle, 0.5)))
}

// rowPromptStyle leans the prompt toward the accent: visibly not chrome,
// visibly not the reply under it.
func rowPromptStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(current.Accent, current.Dim, 0.5)))
}

// rowPrompt is the prompt a full screen row carries beside the name: the
// last one the session's own transcript echoes, whoever typed it and from
// wherever; else the last one delivered through the manager, else the one
// the session launched with, stripped of the notes launch prepends.
func (m *Model) rowPrompt(sess store.Session) string {
	if prompt := m.workspace.panePrompts[sess.ID]; prompt != "" {
		return prompt
	}
	if sess.LastPrompt != "" {
		return sess.LastPrompt
	}
	return launch.DeliveredPrompt(sess.LaunchPrompt)
}

// typedPrompt is a delivered prompt with the launch notes peeled off: the
// rename directives and the coordination note are the manager's words, not
// a task, and a note delivered on its own leaves nothing typed at all.

// replyCell quotes the start of the agent's last message behind a static
// ↳, with the text washed in the state's hue so states read apart at a
// glance — the name's own dot already says the state, so the glyph does
// not repeat it; waiting keeps full strength because it needs the user.
// A working session with nothing quotable yet animates a loader, and a
// silent one holds the cell with a dim dash.
func (m *Model) replyCell(sess store.Session, quiet lipgloss.Style, room int) string {
	line := m.workspace.paneLines[sess.ID]
	if sess.Status == status.Working && line == "" {
		frame := startupFrames[m.startup.startupPhase%len(startupFrames)]
		return lipgloss.NewStyle().Foreground(statusColor(status.Working)).Render(frame + " working")
	}
	if line == "" {
		return quiet.Render("-")
	}
	line = ansi.Truncate(line, max(room-2, 1), "…")
	return subtleStyle.Render("↳ ") + rowReplyStyle(sess.Status).Render(line)
}

// metaIndent lines a second row line up under the name on the first, past
// the entry's guides and the glyph column ahead of it.
func metaIndent(pad, trail string) string {
	return pad + trail + "  "
}

func (m *Model) renderGroupEntry(entry treeRow, selected bool, width int, pad, guides, trail, bg string) string {
	marker := "▾"
	if m.rail.collapsed[entry.group] {
		marker = "▸"
	}
	nameStyle := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	if selected {
		nameStyle = nameStyle.Foreground(colorBright)
	}
	name := baseName(entry.group)
	if entry.isRoot() {
		// Nothing nests under root, so the marker is a blank that holds the column.
		marker, name = " ", "root"
		if !selected {
			nameStyle = nameStyle.Foreground(lipgloss.Color(mix(current.Accent2, current.Subtle, 0.5)))
		}
	}
	lead := pad + guides + subtleStyle.Render(marker) + " "
	head := lead + m.rowHandle(entry, selected) + m.highlightQuery(name, nameStyle)

	// What the group is doing rides on the same line as its name, so a
	// folded group still reports its subtree without being opened. It is
	// written in dots rather than words: the counts state the size too.
	meta := m.groupStatusGlyphs(entry.group)
	if meta == "" {
		meta = subtleStyle.Render("no agents yet")
	}

	return paint(rowColumns(head, meta, width-railGutter), width, bg)
}
