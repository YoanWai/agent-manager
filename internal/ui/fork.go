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

// forkDialog names a fork of the selected session. It owns the captured
// source, the name field, the dialog generation and the request it submits;
// the root runs that request on the effect lane and applies its completion.
type forkDialog struct{ forkState }

// forkHost is what the fork dialog reaches on the root.
type forkHost interface {
	selectedRow() (treeRow, bool)
	configuredTool(name string) (config.Tool, bool)
	reportErr(text string)
	clearErr()
	setMode(next mode)
	queueFork(request forkRequest)
	nextEffectCmd() tea.Cmd
	card(title, body string, hint [][2]string) string
}

var _ forkHost = (*Model)(nil)

// configuredTool looks a tool up in the loaded configuration.
func (m *Model) configuredTool(name string) (config.Tool, bool) {
	tool, ok := m.services.cfg.Tools[name]
	return tool, ok
}

func (d *forkDialog) open(h forkHost) {
	entry, ok := h.selectedRow()
	if !ok {
		return
	}
	if entry.isGroup {
		h.reportErr("select a session to fork")
		return
	}
	tool, ok := h.configuredTool(entry.sess.Tool)
	if !ok {
		h.reportErr(fmt.Sprintf("tool %s is no longer configured", entry.sess.Tool))
		return
	}
	if err := validateForkSource(entry.sess.Tool, tool, entry.sess); err != nil {
		h.reportErr(err.Error())
		return
	}
	name := textField("fork name", 60)
	name.SetValue(entry.sess.Name + "-fork")
	name.CursorEnd()
	name.Focus()
	d.forkState = forkState{source: entry.sess, name: name, gen: d.gen + 1}
	h.clearErr()
	h.setMode(modeFork)
}

func (d *forkDialog) handleKey(h forkHost, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		h.setMode(modeList)
		h.clearErr()
		return nil
	case "enter":
		return d.submit(h)
	}
	var cmd tea.Cmd
	d.name, cmd = d.name.Update(msg)
	return cmd
}

func (d *forkDialog) submit(h forkHost) tea.Cmd {
	name := strings.ReplaceAll(strings.TrimSpace(d.name.Value()), "/", "-")
	if name == "" {
		h.reportErr("name cannot be empty")
		return nil
	}
	h.queueFork(forkRequest{source: d.source, name: name, gen: d.gen})
	return h.nextEffectCmd()
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

func (d *forkDialog) view(h forkHost) string {
	body := "  source  " + valueStyle.Render(d.source.Name) + "\n" +
		"  group   " + groupBadge(displayGroup(d.source.Group)) + "\n" +
		formField("name", textInputView(d.name), true)
	return h.card("⑂ Fork Session", body, [][2]string{{"↵", "create"}, {"esc", "cancel"}})
}
