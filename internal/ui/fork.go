package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/agentsession"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// forkSessionFileResolver resolves a source conversation's on-disk session
// file for tools whose fork loads a file instead of an id (gemini), keyed by
// the tool's session_store. A variable so tests substitute a fixture without
// touching the real store.
var forkSessionFileResolver = agentsession.SessionFile

type forkState struct {
	source store.Session
	name   textinput.Model
	// gen counts opened dialogs, so a completion only closes the dialog it
	// was submitted from; reopening a fork on the same source mints a new
	// one that an older in-flight completion leaves alone.
	gen int
}

func (m *Model) openFork() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if entry.isGroup {
		m.reportErr("select a session to fork")
		return
	}
	tool, ok := m.services.cfg.Tools[entry.sess.Tool]
	if !ok {
		m.reportErr(fmt.Sprintf("tool %s is no longer configured", entry.sess.Tool))
		return
	}
	if err := validateForkSource(entry.sess.Tool, tool, entry.sess); err != nil {
		m.reportErr(err.Error())
		return
	}
	name := textField("fork name", 60)
	name.SetValue(entry.sess.Name + "-fork")
	name.CursorEnd()
	name.Focus()
	m.fork = forkState{source: entry.sess, name: name, gen: m.fork.gen + 1}
	m.clearErr()
	m.mode = modeFork
}

func (m *Model) handleForkKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.clearErr()
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
		m.reportErr("name cannot be empty")
		return m, nil
	}
	m.queueFork(m.fork.source, name)
	return m, m.nextEffectCmd()
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
	if !strings.Contains(tool.ForkCommand, "{id}") && !usesSessionFile && tool.ForkKeys == "" {
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
