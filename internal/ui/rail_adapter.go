package ui

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	collapsedSetting       = "collapsed_groups"
	namePlaceholder        = "…"
	placeholderPromptWidth = 12
	renameGrace            = time.Minute
)

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
	if session.Status == status.Starting {
		return lipgloss.NewStyle().Foreground(statusColor(status.Starting)).
			Render(startupFrames[m.startup.startupPhase%len(startupFrames)])
	}
	if m.isShell(session.Tool) && session.Status != status.Dead && session.Status != status.Errored {
		return subtleStyle.Render(shellGlyph)
	}
	return lipgloss.NewStyle().Foreground(statusColor(session.Status)).Render(statusGlyph(session.Status))
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
		m.fork.open(m)
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
		m.notices.open(m, "")
	case uirail.OpenHelp:
		m.openHelp()
	case uirail.OpenReview:
		return m, m.openDiff()
	}
	return m, nil
}
