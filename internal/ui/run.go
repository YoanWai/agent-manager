package ui

import (
	"fmt"
	"path/filepath"
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
	settings, err := project.Load(dir, m.repoRootOf(dir))
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	// Nothing to run because the project has never been told how. Offering to
	// write the file beats an error naming a path the reader then has to
	// leave the manager to create: p is where they already are when they
	// discover the feature.
	if !settings.Found {
		return m.openRunInit(dir)
	}
	// A file with no run scripts is a dead end otherwise: the reader has
	// already opted in, and what they need is the file open.
	if len(settings.Run) == 0 {
		return m.openSettingsFile(settings, runSetupHint(settings))
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
// feature: the error is the only documentation the reader is looking at.
func runSetupHint(settings project.Settings) string {
	return "no run scripts in " + project.SettingsPath(settings.Root) +
		": add a [run.dev] block with a command"
}

// openSettingsFile opens a project's settings and says why, keeping the
// reason on the status bar when there turns out to be no editor to open it
// with. The reason carries the path, which is the half a reader on such a
// machine actually needs; an editor failure alone would leave them knowing
// only that nothing happened.
func (m *Model) openSettingsFile(settings project.Settings, reason string) (tea.Model, tea.Cmd) {
	m.reportDone(reason)
	model, cmd := m.openInEditor(project.SettingsPath(settings.Root))
	if !m.errBar.worked() {
		m.errBar.text = reason + " — " + m.errBar.text
	}
	return model, cmd
}

// runInitState is the offer to give a project its first settings file: where
// it would go, and whether the repository root could be found at all.
type runInitState struct {
	root string
	path string
}

// openRunInit offers to scaffold settings for the repository the cursor sits
// in. The file belongs at the repository root rather than the session's
// directory, so a session started in a subdirectory still configures the
// project rather than leaving a stray settings file down a level.
func (m *Model) openRunInit(dir string) (tea.Model, tea.Cmd) {
	root := dir
	if m.gitDrv != nil {
		if found, err := m.gitDrv.RepoRoot(dir); err == nil && found != "" {
			root = found
		}
	}
	m.runInit = runInitState{root: root, path: project.SettingsPath(root)}
	m.mode = modeRunInit
	m.errBar.text = ""
	return m, nil
}

// createRunSettings writes the starter file and opens it, so the reader lands
// in the thing they just agreed to fill in rather than being told where it
// is. A machine with no editor still gets the file and is told the path.
func (m *Model) createRunSettings() (tea.Model, tea.Cmd) {
	path, err := project.Scaffold(m.runInit.root)
	m.mode = modeList
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	m.reportDone("created " + path)
	return m.openInEditor(path)
}

func (m *Model) handleRunInitKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "ctrl+c", "n":
		m.mode = modeList
		return m, nil
	case "enter", "y":
		return m.createRunSettings()
	}
	return m, nil
}

func (m *Model) viewRunInit() string {
	var b strings.Builder
	b.WriteString(mutedStyle.Render("This project has no run scripts or setup script yet."))
	b.WriteString("\n\n")
	// Repository name plus the relative path rather than the absolute one: a
	// checkout under a long path truncates in the card, and the half that
	// survives would be the half the reader already knows.
	b.WriteString(valueStyle.Render(
		filepath.Join(filepath.Base(m.runInit.root), project.Dir, project.File)))
	b.WriteString("\n\n")
	b.WriteString(subtleStyle.Render("A commented starter, so nothing changes until you fill it in:"))
	b.WriteString("\n")
	b.WriteString(subtleStyle.Render("  setup    bootstraps every new worktree before its agent starts"))
	b.WriteString("\n")
	b.WriteString(subtleStyle.Render("  [run.*]  commands p offers, each on its own $" + project.EnvPort))
	hint := [][2]string{{"↵", "create and open it"}, {"esc", "cancel"}}
	return m.card("▶ Project settings", b.String(), hint)
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
	// Probed before the script starts, or its own server is what we find.
	busy := settings.BlockBusy(portKey(dir))
	sess := store.Session{
		ID:     newID(),
		Name:   label,
		Tool:   toolName,
		Cwd:    dir,
		Group:  m.contextGroup(),
		Status: status.Starting,
	}
	if err := m.launchNewSession(sess, tool, run.Command, launchOptions{
		env: settings.Env(portKey(dir)),
	}); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	// Starting sits outside the attention set, so the row the key just made
	// would be filtered off screen.
	m.statusFilter = statusFilterAll
	// The port is the one thing the reader wanted and could not otherwise
	// see: the row shows a name, the preview shows output, and neither says
	// which of the worktrees' ports this one got.
	port := settings.Port(portKey(dir))
	message := fmt.Sprintf("running %s on :%d · O opens it", name, port)
	if busy {
		// Said plainly rather than worked around: a server failing to bind a
		// port you can see beats one quietly serving somewhere you cannot.
		message = fmt.Sprintf("running %s on :%d · something is already listening there",
			name, port)
	}
	m.reportDone(message)
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
// Both reads are bounded by the tree they read: a worktree is its own git
// toplevel, so neither lookup can climb out of the repository into a
// directory whose settings would be someone else's shell commands.
func (m *Model) projectSettings(worktreeDir, repoRoot string) (project.Settings, error) {
	settings, err := project.Load(worktreeDir, worktreeDir)
	if err != nil {
		return project.Settings{}, err
	}
	if settings.Found || repoRoot == "" {
		return settings, nil
	}
	uncommitted, err := project.Load(repoRoot, repoRoot)
	if err != nil {
		return project.Settings{}, err
	}
	return uncommitted, nil
}

// repoRootOf is the repository a directory belongs to, and so how far up
// settings discovery may walk from it. A directory outside any repository
// bounds the walk to itself, which is the safe reading: nothing above a
// loose directory is a project of the user's.
func (m *Model) repoRootOf(dir string) string {
	if m.gitDrv == nil {
		return dir
	}
	root, err := m.gitDrv.RepoRoot(dir)
	if err != nil || root == "" {
		return dir
	}
	return root
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
	case "e":
		// The picker is where a reader stands when they notice a command is
		// wrong, so it is where the file that holds it should open from.
		m.mode = modeList
		return m.openInEditor(project.SettingsPath(m.runPick.settings.Root))
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
	hint := [][2]string{{"↑↓", "move"}, {"↵", "run"}, {"e", "edit"}, {"esc", "back"}}
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

// writeGroupSettings turns what the group form was told about the project
// into its settings file, and reports the path when one was written.
//
// Nothing is written for a project that already has settings, or when both
// fields were left empty: the form asks about the project, and a reader who
// answered neither question has not asked for a file.
func (m *Model) writeGroupSettings(path string) (string, error) {
	setup := strings.TrimSpace(m.groupForm.setup.Value())
	run := strings.TrimSpace(m.groupForm.run.Value())
	if m.groupForm.settingsExist || (setup == "" && run == "") {
		return "", nil
	}
	return project.Write(m.repoRootOf(path), setup, run)
}

// openKey shows whatever this worktree is currently running, which is two
// different things depending on the project and should not be two keys.
//
// A worktree serving HTTP opens in a browser. One running a TUI — this
// manager testing a build of itself is the case that prompted it — has
// nothing to open in a browser, so its pane takes the terminal instead.
// Neither needs configuring: what is running is a fact that can be observed,
// so it is, rather than being asked about in the settings file.
//
// o opens the worktree's directory in an editor; O opens what it is doing.
func (m *Model) openKey() (tea.Model, tea.Cmd) {
	dir, ok := m.rowDir()
	if !ok {
		m.errBar.text = "no directory to open anything for: " + dir
		return m, nil
	}
	settings, err := project.Load(dir, m.repoRootOf(dir))
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	if !settings.Found {
		m.errBar.text = "no project settings here: press p to create them"
		return m, nil
	}
	// Serving wins over the pane: a project with both a server and a pane
	// full of its log is one whose interesting half is in the browser.
	name := portKey(dir)
	if port := settings.Port(name); project.Listening(port) {
		url := settings.URL(name)
		m.reportDone("opening " + url)
		return m, openLink(url)
	}
	if sess, ok := m.runSessionIn(dir); ok {
		m.focusSession(sess.ID)
		m.reportDone("attaching to " + sess.Name)
		return m, m.attachCmd(sess.ID)
	}
	m.errBar.text = fmt.Sprintf(
		"nothing running here: no server on :%d and no run session — press p to start one",
		settings.Port(name))
	return m, nil
}

// runSessionIn finds the live run session working in a directory: the shell
// sessions p creates, newest first, so a script started twice opens the one
// still meant to be looked at. Directories are compared resolved, since a
// pane reports its path with symlinks followed and the row may not.
func (m *Model) runSessionIn(dir string) (store.Session, bool) {
	want := resolvePath(dir)
	var found store.Session
	for _, sess := range m.sessions {
		if sess.Archived || !m.isShell(sess.Tool) {
			continue
		}
		if resolvePath(sess.Cwd) != want || !m.tmux.Exists(sess.ID) {
			continue
		}
		if found.ID == "" || sess.CreatedAt.After(found.CreatedAt) {
			found = sess
		}
	}
	return found, found.ID != ""
}

// resolvePath follows symlinks where it can. A tmux pane reports its
// directory with symlinks already followed while a stored Cwd may not, so
// the two only compare equal once both are put through this.
func resolvePath(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}
