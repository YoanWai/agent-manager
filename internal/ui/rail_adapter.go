package ui

import (
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uirailpersist "github.com/YoanWai/agent-manager/internal/ui/rail/persist"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
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

func (m *Model) railSnapshot() uirail.Snapshot {
	snapshot := uirail.Snapshot{
		Sessions:       make([]uirail.Session, 0, len(m.workspace.sessions)),
		Groups:         append([]string(nil), m.workspace.groups...),
		ArchivedGroups: make(map[string]bool, len(m.workspace.archivedGroups)),
		Hosts:          m.ssh.RailHosts(),
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
			AfterTurnGlyph: m.afterTurnGlyph(session.AfterTurn),
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
	if !ok || selection.Remote() {
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

func (m *Model) awaitingRename(session store.Session) bool {
	awaited, ok := m.ledger.lifecycle.AwaitedRenames[session.ID]
	if !ok {
		return false
	}
	if session.Name == awaited.Generated && session.Status != status.Dead && time.Since(session.CreatedAt) < renameGrace {
		return true
	}
	delete(m.ledger.lifecycle.AwaitedRenames, session.ID)
	return false
}

func (m *Model) displayName(session store.Session) string {
	if !m.awaitingRename(session) {
		return session.Name
	}
	if preview := promptPreview(m.ledger.lifecycle.AwaitedRenames[session.ID].Prompt); preview != "" {
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
	return uirail.SessionGlyph(m.railTheme(), m.prefs.glyphMode, uirail.Session{Status: session.Status, IsShell: m.isShell(session.Tool)}, m.startup.LoadPhase)
}

func (m *Model) prepareRailFrame(width, height int) {
	footText := m.railFootLines(width)
	foot := make([]uirail.Line, len(footText))
	for i, line := range footText {
		foot[i].Text = line
	}
	rename := uirail.Rename{}
	if s, ok := surfaceOf[*uidialogs.RenameSurface](m); ok {
		if s.IsGroup() {
			rename.Target = uirail.Selection{Kind: uirail.GroupRow, Group: s.Path()}
		} else {
			rename.Target = uirail.Selection{Kind: uirail.SessionRow, SessionID: s.SessionID()}
			if session, ok := m.sessionByID(s.SessionID()); ok {
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
		rename.View = uiframe.TextInputView(s.NameField(fieldWidth))
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
		Focused:         m.focused(),
		EnterFocuses:    m.enterFocuses(),
		StartupPhase:    m.startup.LoadPhase,
		CursorMarker:    uiframe.CursorMarker,
		Rename:          rename,
		Foot:            foot,
	}
	m.layout.displayedRail = m.rail.Render(ctx, m.layout.displayedRail)
}

func (m *Model) railTheme() uirail.Theme { return uitheme.Current() }

func (m *Model) overlayRowMenu(frame string) string { return m.layout.displayedRail.Overlay(frame) }

func (m *Model) railLines(width, height int) []contentLine {
	m.prepareRailFrame(width, height)
	return m.layout.displayedRail.Lines
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
	if !ctx.RowOK {
		return m.remoteRowLegend()
	}
	return legendOf(m.rail.RowLegend(ctx))
}

func (m *Model) viewLegend() legendSection {
	ctx := m.railLegendContext()
	switch glyph, label := m.messagesAlert(); {
	case glyph == "":
	case label == messagesReadLabel:
		ctx.Messages = [2]string{glyph, label}
	default:
		ctx.Unread = [2]string{glyph, label}
	}
	return legendOf(m.rail.ViewLegend(ctx))
}

func legendOf(legend uirail.Legend) legendSection {
	return legendSection{
		Title: legend.Title, Leads: legend.Leads, Alerts: legend.Alerts,
		Hint: legend.Hint, Pairs: legend.Pairs, Quiet: legend.Quiet,
	}
}

func (m *Model) applyRailDecision(decision uirail.Decision) (tea.Model, tea.Cmd) {
	var commands []tea.Cmd
	if decision.Error != "" {
		m.reportErr(decision.Error)
	}
	if decision.SelectionEffect != uirail.SelectionEffectNone {
		// Only a move the user made comes through here; a refresh that
		// slides the cursor does not re-aim the quick bar.
		if q := m.quick(); q != nil {
			aim, _ := m.rail.Selected()
			q.Aim = quickAim(aim)
		}
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
		commands = append(commands, m.syncRemotePreview())
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

// handleRailKey runs a list key through the Rail and then the root: the
// package consumes the keys that change only the Rail's own state.
func (m *Model) handleRailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m.applyRailKey(m.rail.Update(msg, m.layout.displayedRail, m.railKeyContext()))
}

// applyRailKey runs a decision the Rail already made, from either of the
// Rail's key readers.
func (m *Model) applyRailKey(decision uirail.Decision, consumed bool) (tea.Model, tea.Cmd) {
	if consumed {
		m.gens.foreground++
	}
	return m.applyRailDecision(decision)
}

func (m *Model) moveCursor(delta int) tea.Cmd {
	_, command := m.applyRailDecision(m.rail.Move(delta, true))
	return command
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
	if cmd, taken := m.connectionIntent(intent); taken {
		return m, cmd
	}
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

func (m *Model) queueRail(mutations []uirail.Mutation, chain uint64, first bool) {
	if len(mutations) == 0 {
		return
	}
	if first {
		// Prioritized snapshots describe current UI state. Older pending saves
		// must not overwrite them after their reveal, rename or move completes.
		for _, mutation := range mutations {
			if mutation.Kind != uirail.SaveCollapsed {
				continue
			}
			pending := m.effects.Main.Pending[:0]
			for _, job := range m.effects.Main.Pending {
				if request, ok := job.Request.(uirailpersist.Request); ok && request.Mutation.Kind == uirail.SaveCollapsed {
					continue
				}
				pending = append(pending, job)
			}
			m.effects.Main.Pending = pending
			break
		}
	}
	if chain == 0 {
		m.effects.NextChain++
		chain = m.effects.NextChain
	}
	for i := 0; i < len(mutations); i++ {
		index := i
		if first {
			index = len(mutations) - 1 - i
		}
		mutation := mutations[index]
		mutation.Collapsed = append([]string(nil), mutation.Collapsed...)
		mutation.HostCollapsed = append([]uirail.HostFold(nil), mutation.HostCollapsed...)
		mutation.GroupSiblings = append([]string(nil), mutation.GroupSiblings...)
		m.enqueueEffect(uirailpersist.Request{
			Mutation: mutation,
			Dir:      m.workspace.groupPaths[mutation.Path],
			Worktree: m.workspace.groupWorktrees[mutation.Path],
			Base:     m.workspace.groupBases[mutation.Path],
			Env:      m.workspace.groupEnvs[mutation.Path],
		}, chain, first)
	}
}
func (m *Model) applyRailEffect(result uirailpersist.Result, request uirailpersist.Request, chain uint64, err error) tea.Cmd {
	mutation := request.Mutation
	if err == nil {
		switch mutation.Kind {
		case uirail.SwapSession:
			m.swapSessionInventory(mutation.SessionID, mutation.TargetID)
		case uirail.SwapGroup:
			m.materializeGroupsLocal(mutation.GroupSiblings)
			m.swapGroupInventory(mutation.Path, mutation.TargetPath)
		case uirail.MoveGroup:
			m.renameGroupLocally(mutation.Path, result.NewPath, request.Dir, request.Worktree, request.Base, request.Env)
		}
		for _, sess := range result.Sessions {
			for i := range m.workspace.sessions {
				if m.workspace.sessions[i].ID == sess.ID {
					m.workspace.sessions[i].Group = sess.Group
					m.workspace.sessions[i].ParentID = sess.ParentID
				}
			}
		}
	} else {
		pending := m.effects.Main.Pending[:0]
		for _, each := range m.effects.Main.Pending {
			if each.Chain != chain {
				pending = append(pending, each)
			}
		}
		m.effects.Main.Pending = pending
	}
	follow := m.rail.ApplyMutation(mutation, err)
	m.queueRail(follow.Mutations, chain, true)
	follow.Mutations = nil
	_, cmd := m.applyRailDecision(follow)
	if result.Warning != nil {
		m.reportErr(result.Warning.Error())
	}
	m.rebuildRows()
	return cmd
}

func (m *Model) railMutationPending() bool {
	jobs := append([]*uieffects.Job(nil), m.effects.Main.Pending...)
	if m.effects.Main.Active != nil {
		jobs = append(jobs, m.effects.Main.Active)
	}
	for _, job := range jobs {
		if request, ok := job.Request.(uirailpersist.Request); ok && request.Mutation.Kind != uirail.SaveCollapsed {
			return true
		}
	}
	return false
}

// railServices hands a Rail change the store and pane labels it writes.
func (s effectServices) railServices() uirailpersist.Services {
	return uirailpersist.Services{Store: s.store, Labels: s.driver}
}
