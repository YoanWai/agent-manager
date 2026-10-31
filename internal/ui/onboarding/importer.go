package onboarding

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

// ImportWindows are how many days back an import reaches. Without a bound,
// months of chats would land as hundreds of sessions.
var ImportWindows = []int{7, 14, 30}

// ImportRequest is the import the last step starts: the CLIs whose saved
// conversations come in, how many days back, and the agent CLI that does it.
type ImportRequest struct {
	From   []string
	Days   int
	Runner string
}

// importer is the import step's picks. Its rows are the installed CLIs,
// then how far back, then the CLI that runs it.
type importer struct {
	from          map[string]bool
	window        int
	runner        int
	cursor, first int
}

// importSources are the installed agents, the CLIs with conversations of
// their own to bring in.
func (m Model) importSources() []string {
	var names []string
	for _, agent := range m.agents {
		if agent.Installed {
			names = append(names, agent.Name)
		}
	}
	return names
}

// importRunners are the agents a spawn could start now: checked and
// installed.
func (m Model) importRunners() []string {
	var names []string
	for _, agent := range m.agents {
		if agent.Installed && !m.chosen.Hidden[agent.Name] {
			names = append(names, agent.Name)
		}
	}
	return names
}

func (m Model) windowRow() int { return len(m.importSources()) }

func (m Model) runnerRow() int { return m.windowRow() + 1 }

// pendingImport is the import as picked so far. ok is false while it has
// nothing to bring in or nothing to run it.
func (m Model) pendingImport() (request ImportRequest, ok bool) {
	request.Days = ImportWindows[m.imports.window]
	for _, name := range m.importSources() {
		if m.imports.from[name] {
			request.From = append(request.From, name)
		}
	}
	if runners := m.importRunners(); len(runners) > 0 {
		request.Runner = runners[min(m.imports.runner, len(runners)-1)]
	}
	return request, len(request.From) > 0 && request.Runner != ""
}

func (m Model) importReady() bool {
	_, ok := m.pendingImport()
	return ok
}

func (m *Model) importMove(delta int) {
	if len(m.importSources()) == 0 {
		return
	}
	count := m.runnerRow() + 1
	m.imports.cursor = (m.imports.cursor + delta + count) % count
}

// importToggle checks the CLI under the cursor, or steps the value on a
// value row forward.
func (m *Model) importToggle() {
	sources := m.importSources()
	if m.imports.cursor < len(sources) {
		name := sources[m.imports.cursor]
		if m.imports.from == nil {
			m.imports.from = make(map[string]bool)
		}
		m.imports.from[name] = !m.imports.from[name]
		return
	}
	m.importStep(1)
}

// importStep steps the value row under the cursor.
func (m *Model) importStep(delta int) {
	switch m.imports.cursor {
	case m.windowRow():
		count := len(ImportWindows)
		m.imports.window = (m.imports.window + delta + count) % count
	case m.runnerRow():
		if count := len(m.importRunners()); count > 0 {
			m.imports.runner = (min(m.imports.runner, count-1) + delta + count) % count
		}
	}
}

func (m Model) onValueRow() bool {
	return m.Step() == Import && len(m.importSources()) > 0 && m.imports.cursor >= m.windowRow()
}

// importColumn is the import step's rows: the CLIs to bring chats in from,
// then how far back and which agent does it.
func (m *Model) importColumn(p palette, width, rows int, add func(string, int)) {
	sources := m.importSources()
	if len(sources) == 0 {
		add("  "+p.dim.Render("No agent CLI was found on PATH, so there is nothing to bring in yet."), -1)
		return
	}
	add("  "+p.subtle.Render("Bring in chats from"), -1)
	visible := min(len(sources), max(rows-6, 3))
	m.imports.first = presentation.Follow(m.imports.first, min(m.imports.cursor, len(sources)-1), len(sources), visible)
	for i := m.imports.first; i < m.imports.first+visible; i++ {
		name := p.text
		if i == m.imports.cursor {
			name = p.bright
		}
		add(selectable(p, i == m.imports.cursor, checkbox(p, m.imports.from[sources[i]])+" "+name.Render(sources[i]), width), i)
	}
	add("", -1)
	request, _ := m.pendingImport()
	add(m.valueRow(p, m.windowRow(), "Going back", fmt.Sprintf("%d days", request.Days), width), m.windowRow())
	runner := request.Runner
	if runner == "" {
		runner = "no checked agent is installed"
	}
	add(m.valueRow(p, m.runnerRow(), "Run it with", runner, width), m.runnerRow())
}

func (m Model) valueRow(p palette, row int, label, value string, width int) string {
	on := m.imports.cursor == row
	style := p.text
	if on {
		style = p.bright
	}
	chrome := presentation.ChromeFor(p.mode)
	choice := p.subtle.Render(chrome.ChoiceLeftGlyph+" ") + style.Render(value) + p.subtle.Render(" "+chrome.ChoiceRightGlyph)
	return selectable(p, on, presentation.PadRight(p.dim.Render(label), 14)+choice, width)
}

// ImportPrompt is what the import agent is asked. It reads each CLI's own
// storage, so the manager carries no listing code of its own.
func ImportPrompt(request ImportRequest) string {
	clis := joinNames(request.From)
	return fmt.Sprintf("Bring my past %s conversations from the last %d days into Agent Manager. "+
		"Find each conversation %s saved on this machine that was active in the last %d days, reading each CLI's own storage on disk for the conversation id, the directory it ran in and when it was last used. Change nothing there. "+
		"Call list_sessions and skip every conversation whose id is already listed as an agent_session_id. Skip this conversation too. "+
		"For each one left, call create_session with tool set to the CLI, resume set to the conversation id, directory set to the folder it ran in, and name set to a short kebab-case title saying what it was about. Leave prompt and worktree unset. "+
		"From a shell, `agent-manager sessions` lists the sessions and `agent-manager spawn --tool <cli> --directory <dir> --resume <id> --name <name>` brings one in. "+
		"Finish with how many you brought in for each CLI, and every one the manager refused with its reason.",
		clis, request.Days, clis, request.Days)
}

// joinNames lists names the way a sentence does: "a", "a and b", "a, b and c".
func joinNames(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
