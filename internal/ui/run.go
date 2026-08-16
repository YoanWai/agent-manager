package ui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YoanWai/agent-manager/internal/project"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// runPickState is the picker p opens when a project declares more than one
// run script and none of them is the default. Rows are snapshotted at open
// so a settings file edited underneath cannot reorder them under the cursor.
type runPickState struct {
	names    []string
	settings project.Settings
	dir      string
	cursor   int
}

// runKey starts a project's run script in a terminal tab beside the session
// it belongs to. One script, or one marked default, starts straight away;
// anything ambiguous opens the picker rather than guessing, because the
// wrong script here starts a server or a test watcher, not a no-op.
func (m *Model) runKey() (tea.Model, tea.Cmd) {
	dir, ok := m.rowDir()
	if !ok {
		m.errBar.text = "no directory to run in: " + dir
		return m, nil
	}
	settings, err := project.Load(dir)
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	if len(settings.Run) == 0 {
		m.errBar.text = runSetupHint(settings)
		return m, nil
	}
	if name, ok := settings.DefaultRun(); ok {
		return m.startRun(settings, dir, name)
	}
	m.runPick = runPickState{names: settings.RunNames(), settings: settings, dir: dir}
	m.mode = modeRunPick
	m.errBar.text = ""
	return m, nil
}

// runSetupHint says what to write and where, naming the file rather than the
// feature: a project with no run scripts is the common case on first press,
// and the error is the only documentation the user is looking at.
func runSetupHint(settings project.Settings) string {
	where := filepath.Join(project.Dir, project.File)
	if settings.Found {
		where = filepath.Join(settings.Root, project.Dir, project.File)
		return "no run scripts in " + where + `: add a [run.dev] block with a command`
	}
	return "no " + where + " in this repo: add one with a [run.dev] block to run a project command here"
}

// startRun spawns the script as a terminal tab. It is a shell session like
// the one T opens, so it lands in the list with its own row, keeps its
// scrollback, and is killed, revived and archived by the same keys. The
// port is exported rather than substituted into the command so a script can
// use it or ignore it without agent-manager parsing shell syntax.
func (m *Model) startRun(settings project.Settings, dir, name string) (tea.Model, tea.Cmd) {
	run, ok := settings.Run[name]
	if !ok {
		m.errBar.text = "no run script named " + name
		return m, nil
	}
	toolName, tool, ok := m.shellTool()
	if !ok {
		m.errBar.text = `no shell configured: add a tool block with shell = true to config.toml`
		return m, nil
	}
	// Named for the session it runs beside, so several worktrees running the
	// same script stay tellable apart in the list.
	label := name
	if entry, ok := m.selectedRow(); ok && !entry.isGroup {
		label = name + "-" + entry.sess.Name
	}
	port := settings.Port(portKey(dir))
	sess := store.Session{
		ID:     newID(),
		Name:   label,
		Tool:   toolName,
		Cwd:    dir,
		Group:  m.contextGroup(),
		Status: status.Starting,
	}
	if err := m.launchNewSession(sess, tool, run.Command, launchOptions{
		env: map[string]string{project.EnvPort: strconv.Itoa(port)},
	}); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	// Starting sits outside the attention set, so the row the key just made
	// would be filtered off screen.
	m.statusFilter = statusFilterAll
	m.errBar.text = ""
	m.mode = modeList
	m.focusSession(sess.ID)
	return m, m.refreshCmd()
}

// projectSettings reads the settings governing a freshly created worktree.
//
// The worktree comes first, so settings are versioned with the branch and a
// branch that changes its own setup script gets the new one. It is a
// checkout of a commit, though, and a settings file written but not yet
// committed is not in it — which is exactly the state a project is in the
// first time anyone tries this. Falling back to the repository the worktree
// branched from makes that first attempt work instead of silently doing
// nothing.
func (m *Model) projectSettings(worktreeDir, repoRoot string) (project.Settings, error) {
	settings, err := project.Load(worktreeDir)
	if err != nil {
		return project.Settings{}, err
	}
	if settings.Found || repoRoot == "" {
		return settings, nil
	}
	uncommitted, err := project.Load(repoRoot)
	if err != nil {
		return project.Settings{}, err
	}
	return uncommitted, nil
}

// portKey is what a directory's port is derived from: the worktree's own
// directory name, so every session working in that worktree — the agent, the
// dev server, a second shell — resolves to the same port, and a worktree
// keeps it across restarts.
func portKey(dir string) string {
	return filepath.Base(dir)
}

func (m *Model) handleRunPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		m.mode = modeList
		return m, nil
	case "up", "k":
		if m.runPick.cursor > 0 {
			m.runPick.cursor--
		}
	case "down", "j":
		if m.runPick.cursor < len(m.runPick.names)-1 {
			m.runPick.cursor++
		}
	case "enter":
		if m.runPick.cursor < len(m.runPick.names) {
			return m.startRun(m.runPick.settings, m.runPick.dir, m.runPick.names[m.runPick.cursor])
		}
	}
	return m, nil
}

func (m *Model) viewRunPick() string {
	var b strings.Builder
	for i, name := range m.runPick.names {
		marker := "  "
		labelStyle := valueStyle
		if m.runPick.cursor == i {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			labelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		// The command is the description when the project wrote none: a run
		// script is picked by what it does, and its command says that.
		note := m.runPick.settings.Run[name].Description
		if note == "" {
			note = m.runPick.settings.Run[name].Command
		}
		b.WriteString(marker)
		b.WriteString(labelStyle.Render(name))
		b.WriteString(mutedStyle.Render("  " + firstLine(note)))
		b.WriteByte('\n')
	}
	port := m.runPick.settings.Port(portKey(m.runPick.dir))
	b.WriteByte('\n')
	b.WriteString(subtleStyle.Render(fmt.Sprintf("  %s=%d", project.EnvPort, port)))
	hint := [][2]string{{"↑↓", "move"}, {"↵", "run"}, {"esc", "back"}}
	return m.card("▶ Run", strings.TrimRight(b.String(), "\n"), hint)
}

// firstLine keeps a multi-line command from breaking the picker's layout.
func firstLine(s string) string {
	line, _, cut := strings.Cut(strings.TrimSpace(s), "\n")
	if cut {
		return line + " …"
	}
	return line
}
