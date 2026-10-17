package ui

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	collapsedSetting       = "collapsed_groups"
	namePlaceholder        = "…"
	placeholderPromptWidth = 12
	renameGrace            = time.Minute
)

type treeRow struct {
	isGroup bool
	group   string
	depth   int
	sess    store.Session
}

func (row treeRow) isRoot() bool { return row.isGroup && row.group == "" }

func rowKey(row treeRow) string {
	if row.isGroup {
		return "g:" + row.group
	}
	return "s:" + row.sess.ID
}

func loadCollapsed(st *store.Store) []string {
	raw, err := st.Setting(collapsedSetting)
	if err != nil || raw == "" {
		return nil
	}
	var paths []string
	if json.Unmarshal([]byte(raw), &paths) != nil {
		return nil
	}
	return paths
}

func (m *Model) railSnapshot() uirail.Snapshot {
	snapshot := uirail.Snapshot{
		Sessions:       make([]uirail.Session, 0, len(m.workspace.sessions)),
		Groups:         append([]string(nil), m.workspace.groups...),
		ArchivedGroups: make(map[string]bool, len(m.workspace.archivedGroups)),
	}
	for path, archived := range m.workspace.archivedGroups {
		snapshot.ArchivedGroups[path] = archived
	}
	for _, session := range m.workspace.sessions {
		prompt := m.workspace.panePrompts[session.ID]
		if prompt == "" {
			prompt = session.LastPrompt
		}
		if prompt == "" {
			prompt = launch.DeliveredPrompt(session.LaunchPrompt)
		}
		snapshot.Sessions = append(snapshot.Sessions, uirail.Session{
			ID:             session.ID,
			Name:           session.Name,
			DisplayName:    m.displayName(session),
			Tool:           session.Tool,
			Cwd:            session.Cwd,
			Group:          session.Group,
			Status:         session.Status,
			Archived:       session.Archived,
			Acked:          session.Acked,
			CreatedAt:      session.CreatedAt,
			LastStatusAt:   session.LastStatusAt,
			LaunchAt:       session.LaunchTime(),
			AgentSessionID: session.AgentSessionID,
			ParentID:       session.ParentID,
			Prompt:         prompt,
			PaneLine:       m.workspace.paneLines[session.ID],
			PanePrompt:     m.workspace.panePrompts[session.ID],
			Queued:         m.workspace.queuedMessages[session.ID],
			IsShell:        m.isShell(session.Tool),
			Elsewhere:      m.elsewhereNote(session) != "",
			AfterTurn:      session.AfterTurn,
			AfterTurnGlyph: afterTurnGlyphs[session.AfterTurn],
		})
	}
	return snapshot
}

func (m *Model) rebuildRows() uirail.SelectionChange {
	return m.rail.Reconcile(m.railSnapshot())
}

func (m *Model) selected() (store.Session, bool) {
	id, ok := m.rail.SelectedSession()
	if !ok {
		return store.Session{}, false
	}
	return m.sessionByID(id)
}

func (m *Model) selectedRow() (treeRow, bool) {
	selection, ok := m.rail.Selected()
	if !ok {
		return treeRow{}, false
	}
	if selection.Kind == uirail.GroupRow {
		return treeRow{isGroup: true, group: selection.Group}, true
	}
	session, ok := m.sessionByID(selection.SessionID)
	if !ok {
		return treeRow{}, false
	}
	return treeRow{group: session.Group, sess: session}, true
}

func (m *Model) selectedGroup() (string, bool) { return m.rail.SelectedGroup() }

func (m *Model) focusSession(id string) bool { return m.rail.FocusSession(id) }

func (m *Model) sessionByID(id string) (store.Session, bool) {
	for _, session := range m.workspace.sessions {
		if session.ID == id {
			return session, true
		}
	}
	return store.Session{}, false
}

func (m *Model) listedSessions() []store.Session {
	ids := m.rail.ListedSessionIDs()
	listed := make([]store.Session, 0, len(ids))
	for _, id := range ids {
		if session, ok := m.sessionByID(id); ok {
			listed = append(listed, session)
		}
	}
	return listed
}

func (m *Model) listedAgents() []store.Session {
	ids := m.rail.ListedAgentIDs()
	agents := make([]store.Session, 0, len(ids))
	for _, id := range ids {
		if session, ok := m.sessionByID(id); ok {
			agents = append(agents, session)
		}
	}
	return agents
}

// railAgents copies the identity, group and status a group rollup reads
// from each listed agent's current record.
func (m *Model) railAgents() []uirail.Session {
	listed := m.listedAgents()
	agents := make([]uirail.Session, len(listed))
	for i, session := range listed {
		agents[i] = uirail.Session{ID: session.ID, Group: session.Group, Status: session.Status}
	}
	return agents
}

func (m *Model) groupSessionCount(path string) int {
	return uirail.GroupSessionCount(m.railAgents(), path)
}

func (m *Model) groupStatusBreakdown(group string) string {
	return uirail.GroupStatusBreakdown(m.railTheme(), m.railAgents(), group)
}

func (m *Model) renamingGroup(group string) bool {
	return m.mode == modeRename && m.rename.IsGroup() && m.rename.Path() == group
}

// Root features read group paths the way Rail does.
func inGroupSubtree(sessionGroup, group string) bool {
	return uirail.InGroupSubtree(sessionGroup, group)
}

func parentGroup(group string) string { return uirail.ParentGroup(group) }

func baseName(path string) string { return uirail.BaseName(path) }

func (m *Model) awaitingRename(session store.Session) bool {
	awaited, ok := m.ledger.awaitedRenames[session.ID]
	if !ok {
		return false
	}
	if session.Name == awaited.generated && session.Status != status.Dead && time.Since(session.CreatedAt) < renameGrace {
		return true
	}
	delete(m.ledger.awaitedRenames, session.ID)
	return false
}

func (m *Model) displayName(session store.Session) string {
	if !m.awaitingRename(session) {
		return session.Name
	}
	if preview := promptPreview(m.ledger.awaitedRenames[session.ID].prompt); preview != "" {
		return preview
	}
	return namePlaceholder
}

func promptPreview(prompt string) string {
	words := make([]string, 0, len(strings.Fields(prompt)))
	for _, word := range strings.Fields(prompt) {
		if !clipboard.IsPastePath(word) {
			words = append(words, word)
		}
	}
	return ansi.Truncate(strings.Join(words, " "), placeholderPromptWidth, "…")
}

func (m *Model) sessionGlyph(session store.Session) string {
	return uirail.SessionGlyph(m.railTheme(), m.prefs.glyphMode, uirail.Session{Status: session.Status, IsShell: m.isShell(session.Tool)}, m.startup.startupPhase)
}

func (m *Model) prepareRailFrame(width, height int) {
	footText := m.railFootLines(width)
	foot := make([]uirail.Line, len(footText))
	for i, line := range footText {
		foot[i].Text = line
	}
	rename := uirail.Rename{}
	if m.mode == modeRename {
		if m.rename.IsGroup() {
			rename.Target = uirail.Selection{Kind: uirail.GroupRow, Group: m.rename.Path()}
		} else {
			rename.Target = uirail.Selection{Kind: uirail.SessionRow, SessionID: m.rename.SessionID()}
			if session, ok := m.sessionByID(m.rename.SessionID()); ok {
				rename.Target.Group = session.Group
			}
		}
		fieldWidth := width - 4
		for _, row := range m.rail.Rows() {
			if row.Selection == rename.Target {
				fieldWidth -= 3 * row.Depth
				break
			}
		}
		rename.View = textInputView(m.rename.NameField(fieldWidth))
	}
	ctx := uirail.RenderContext{
		GlyphMode:       m.prefs.glyphMode,
		Width:           width,
		Height:          max(height, 0),
		TerminalWidth:   m.layout.width,
		TerminalHeight:  m.layout.height,
		BodyOriginY:     m.listChromeRows(),
		Theme:           m.railTheme(),
		ListKeys:        m.services.listKeys,
		ComfortableRows: m.prefs.comfortableRows,
		MouseDisabled:   m.prefs.mouseDisabled,
		Focused:         m.mode == modeFocus,
		EnterFocuses:    m.enterFocuses(),
		StartupPhase:    m.startup.startupPhase,
		CursorMarker:    cursorAnchorMarker,
		Rename:          rename,
		Foot:            foot,
	}
	m.layout.displayedRail = m.rail.Render(ctx, m.layout.displayedRail)
}

func (m *Model) railTheme() uirail.Theme { return current() }

func railContentLines(lines []uirail.Line) []contentLine {
	out := make([]contentLine, len(lines))
	for i, line := range lines {
		out[i] = contentLine{text: line.Text, tone: line.Tone, rule: line.Rule, raw: line.Raw}
	}
	return out
}

func (m *Model) overlayRowMenu(frame string) string { return m.layout.displayedRail.Overlay(frame) }

func (m *Model) railLines(width, height int) []contentLine {
	m.prepareRailFrame(width, height)
	return railContentLines(m.layout.displayedRail.Lines)
}

func (m *Model) railLegendContext() uirail.LegendContext {
	return uirail.LegendContext{
		ListKeys:      m.services.listKeys,
		EnterFocuses:  m.enterFocuses(),
		MouseDisabled: m.prefs.mouseDisabled,
		ArrowStep:     m.prefs.arrowStep,
		FullLayout:    m.prefs.fullLayout,
	}
}

// rowLegend reads the row from root's current record rather than Rail's
// last snapshot, so the tier never lags an edit awaiting reconciliation.
func (m *Model) rowLegend() legendSection {
	ctx := m.railLegendContext()
	if row, ok := m.selectedRow(); ok {
		ctx.RowOK = true
		ctx.Row = uirail.Selection{Kind: uirail.GroupRow, Group: row.group}
		if !row.isGroup {
			ctx.Row = uirail.Selection{Kind: uirail.SessionRow, SessionID: row.sess.ID, Group: row.sess.Group}
			ctx.Session = uirail.Session{
				ID: row.sess.ID, Group: row.sess.Group, Tool: row.sess.Tool, Status: row.sess.Status,
				Archived: row.sess.Archived, AfterTurn: row.sess.AfterTurn, IsShell: m.isShell(row.sess.Tool),
			}
		}
	}
	return legendOf(m.rail.RowLegend(ctx))
}

func (m *Model) viewLegend() legendSection {
	ctx := m.railLegendContext()
	if glyph, label := m.messagesAlert(); glyph != "" {
		ctx.Unread = [2]string{glyph, label}
	}
	return legendOf(m.rail.ViewLegend(ctx))
}

func (m *Model) reorderFooter() string {
	return m.transientFooter(legendOf(m.rail.ReorderLegend()))
}

func legendOf(legend uirail.Legend) legendSection {
	return legendSection{
		title: legend.Title, leads: legend.Leads, alerts: legend.Alerts,
		hint: legend.Hint, pairs: legend.Pairs, quiet: legend.Quiet,
	}
}

func (m *Model) applyRailDecision(decision uirail.Decision) (tea.Model, tea.Cmd) {
	var commands []tea.Cmd
	if decision.Error != "" {
		m.reportErr(decision.Error)
	}
	if decision.SelectionEffect != uirail.SelectionEffectNone {
		m.workspace.preview = ""
		m.workspace.proc = sysstat.ProcStat{}
		m.workspace.procFor = ""
		_, selectedSession := m.selected()
		if decision.SelectionEffect == uirail.SelectionEffectFilter || selectedSession {
			m.focus.pane.MovePreview()
		}
		if decision.SelectionEffect == uirail.SelectionEffectFilter && m.poller != nil {
			m.syncPollInput()
		}
		if selectedSession {
			commands = append(commands, m.schedulePreview())
		}
	}
	dependent := false
	for _, mutation := range decision.Mutations {
		if mutation.Kind != uirail.SaveCollapsed {
			dependent = true
		}
	}
	if dependent && m.railMutationPending() {
		decision.Mutations = nil
		m.reportErr("previous Rail change is still saving")
	}
	m.queueRail(decision.Mutations, 0, false)
	commands = append(commands, m.nextEffectCmd())
	if decision.Refresh {
		m.requestRefresh()
	}
	if decision.AutoScroll != nil {
		request := *decision.AutoScroll
		commands = append(commands, tea.Tick(request.After, func(time.Time) tea.Msg {
			return uirail.AutoScrollTick{Lift: request.Lift}
		}))
	}
	if decision.Intent.Kind != uirail.NoAction {
		model, command := m.runRailIntent(decision.Intent)
		commands = append(commands, command)
		return model, tea.Batch(commands...)
	}
	return m, tea.Batch(commands...)
}

func (m *Model) moveCursor(delta int) tea.Cmd {
	_, command := m.applyRailDecision(m.rail.Move(delta, true))
	return command
}

func (m *Model) applyRailStateDecision(decision uirail.Decision) {
	m.queueRail(decision.Mutations, 0, true)
}

func (m *Model) swapSessionInventory(id, targetID string) {
	current, target := -1, -1
	for index, session := range m.workspace.sessions {
		switch session.ID {
		case id:
			current = index
		case targetID:
			target = index
		}
	}
	if current >= 0 && target >= 0 {
		m.workspace.sessions[current], m.workspace.sessions[target] = m.workspace.sessions[target], m.workspace.sessions[current]
	}
}

func (m *Model) swapGroupInventory(path, targetPath string) {
	current, target := -1, -1
	for index, group := range m.workspace.groups {
		switch group {
		case path:
			current = index
		case targetPath:
			target = index
		}
	}
	if current >= 0 && target >= 0 {
		m.workspace.groups[current], m.workspace.groups[target] = m.workspace.groups[target], m.workspace.groups[current]
	}
}

func (m *Model) runRailIntent(intent uirail.Intent) (tea.Model, tea.Cmd) {
	switch intent.Kind {
	case uirail.Quit:
		return m.requestQuit()
	case uirail.Focus:
		return m.focusSelected()
	case uirail.Attach:
		return m.attachSelected()
	case uirail.NewSession:
		return m, m.openForm()
	case uirail.NewGroup:
		m.openGroupForm()
	case uirail.Fork:
		m.openFork()
	case uirail.Revive:
		return m.reviveSelected()
	case uirail.MarkIdle:
		return m.acknowledgeSelected()
	case uirail.ReviveAll:
		return m.reviveAllDead()
	case uirail.Restart:
		return m.restartSelected()
	case uirail.Kill:
		return m.killSelected()
	case uirail.KillAll:
		return m.killAllLive()
	case uirail.Archive:
		return m.archiveSelected()
	case uirail.Restore:
		return m.restoreSelected()
	case uirail.Delete:
		m.prepareDelete()
	case uirail.Prompt:
		return m, m.openQuickMode()
	case uirail.CopyReply:
		return m.copyReplySelected()
	case uirail.CancelEnd:
		return m.cancelEndSelected()
	case uirail.OpenSettings:
		return m, m.openSettings()
	case uirail.Resize:
		return m.enterResizeMode()
	case uirail.NewTerminal:
		return m.terminalKey()
	case uirail.OpenEditor:
		return m.openEditor()
	case uirail.RenameAction:
		m.openRename()
	case uirail.MoveToGroup:
		m.openMove()
	case uirail.OpenMessages:
		m.openNotices("")
	case uirail.OpenHelp:
		m.openHelp()
	case uirail.OpenReview:
		return m, m.openDiff()
	}
	return m, nil
}
