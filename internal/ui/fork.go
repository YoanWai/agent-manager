package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/agentsession"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// forkSessionFileResolver resolves a source conversation's on-disk session
// file for tools whose fork loads a file instead of an id (gemini), keyed by
// the tool's session_store. A variable so tests substitute a fixture without
// touching the real store.
var forkSessionFileResolver = agentsession.SessionFile

type forkState struct {
	source store.Session
	name   textinput.Model
}

func (m *Model) openFork() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if entry.isGroup {
		m.errBar.text = "select a session to fork"
		return
	}
	tool, ok := m.cfg.Tools[entry.sess.Tool]
	if !ok {
		m.errBar.text = fmt.Sprintf("tool %s is no longer configured", entry.sess.Tool)
		return
	}
	if err := validateForkSource(entry.sess.Tool, tool, entry.sess); err != nil {
		m.errBar.text = err.Error()
		return
	}
	name := textField("fork name", 60)
	name.SetValue(entry.sess.Name + "-fork")
	name.CursorEnd()
	name.Focus()
	m.fork = forkState{source: entry.sess, name: name}
	m.errBar.text = ""
	m.mode = modeFork
}

func (m *Model) handleForkKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.errBar.text = ""
		return m, nil
	case "enter":
		return m.submitFork()
	}
	var cmd tea.Cmd
	m.fork.name, cmd = m.fork.name.Update(msg)
	return m, cmd
}

func (m *Model) submitFork() (tea.Model, tea.Cmd) {
	name := strings.ReplaceAll(strings.TrimSpace(m.fork.name.Value()), "/", "-")
	if name == "" {
		m.errBar.text = "name cannot be empty"
		return m, nil
	}
	source, err := m.store.Get(m.fork.source.ID)
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	tool, ok := m.cfg.Tools[source.Tool]
	if !ok {
		m.errBar.text = fmt.Sprintf("tool %s is no longer configured", source.Tool)
		return m, nil
	}
	if err := validateForkSource(source.Tool, tool, source); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	if !isDir(source.Cwd) {
		m.errBar.text = "working directory no longer exists: " + source.Cwd
		return m, nil
	}

	managerID := newID()
	agentID := ""
	if strings.Contains(tool.ForkCommand, "{new_id}") {
		agentID = uuid.NewString()
	}
	sessionFile := ""
	if strings.Contains(tool.ForkCommand, "{session_file}") {
		resolved, err := forkSessionFileResolver(tool.SessionStore, source.AgentSessionID)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		sessionFile = resolved
	}
	baseCommand := expandForkCommand(tool.ForkCommand, source.AgentSessionID, agentID, name, sessionFile)
	forked := store.Session{
		ID:             managerID,
		Name:           name,
		Tool:           source.Tool,
		Cwd:            source.Cwd,
		Group:          source.Group,
		Status:         status.Starting,
		AgentSessionID: agentID,
		WorktreeRepo:   source.WorktreeRepo,
		WorktreeBranch: source.WorktreeBranch,
	}
	if err := m.launchNewSession(forked, tool, baseCommand, launchOptions{}); err != nil {
		m.reportLaunchError(err)
		return m, nil
	}
	// Forks start as starting, which attention excludes; clear so the row
	// the fork just created is on screen.
	m.statusFilter = statusFilterAll
	m.rebuildRows()
	m.mode = modeList
	m.errBar.text = ""
	m.focusSession(managerID)
	return m, m.refreshCmd()
}

func validateForkSource(toolName string, tool config.Tool, source store.Session) error {
	// A shell has no fork_command either, but saying so names a config
	// field for a row that was never going to have a conversation.
	if tool.Shell {
		return fmt.Errorf("%s is a shell, not an agent - there is no conversation to fork", source.Name)
	}
	if tool.ForkCommand == "" {
		return fmt.Errorf("tool %s has no fork_command", toolName)
	}
	usesSessionFile := strings.Contains(tool.ForkCommand, "{session_file}")
	if !strings.Contains(tool.ForkCommand, "{id}") && !usesSessionFile {
		return fmt.Errorf("tool %s fork_command must reference the source via {id} or {session_file}", toolName)
	}
	if usesSessionFile && !agentsession.SupportsSessionFile(tool.SessionStore) {
		return fmt.Errorf("tool %s fork_command uses {session_file}, which needs session_store = \"gemini\"", toolName)
	}
	if source.AgentSessionID == "" {
		return fmt.Errorf("%s has no captured conversation id", source.Name)
	}
	return nil
}

func expandForkCommand(template, sourceID, newID, name, sessionFile string) string {
	return strings.NewReplacer(
		"{id}", tmux.ShellQuote(sourceID),
		"{new_id}", tmux.ShellQuote(newID),
		"{name}", tmux.ShellQuote(name),
		"{session_file}", tmux.ShellQuote(sessionFile),
	).Replace(template)
}

func (m *Model) viewFork() string {
	body := "  source  " + valueStyle.Render(m.fork.source.Name) + "\n" +
		"  group   " + groupBadge(displayGroup(m.fork.source.Group)) + "\n" +
		formField("name", textInputView(m.fork.name), true)
	return m.card("⑂ Fork Session", body, [][2]string{{"↵", "create"}, {"esc", "cancel"}})
}
