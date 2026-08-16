package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/project"
	tea "github.com/charmbracelet/bubbletea"
)

// writeProject drops a .agent-manager/settings.toml into dir and hands back
// dir, so a test reads as "a repository configured like this".
func writeProject(t *testing.T, dir, body string) string {
	t.Helper()
	settingsDir := filepath.Join(dir, project.Dir)
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, project.File), []byte(body), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	return dir
}

func pressRunKey(t *testing.T, m *Model) {
	t.Helper()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m.applyCmd(t, cmd)
}

// onSessionIn puts the cursor on an agent session working in dir, which is
// the state every p press is made from.
func onSessionIn(t *testing.T, m *Model, dir string) {
	t.Helper()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")
}

// A repository with no settings gets the offer to create them, not an error.
func TestRunKeyWithoutSettingsOffersTheScaffold(t *testing.T) {
	m := buildModel(t)
	onSessionIn(t, m, t.TempDir())

	before := len(m.sessions)
	pressRunKey(t, m)

	if len(m.sessions) != before {
		t.Fatal("p spawned something for a project with no settings")
	}
	if m.mode != modeRunInit {
		t.Fatalf("mode = %v, want the create-settings offer", m.mode)
	}
}

func TestRunKeyWithSettingsButNoScriptsSaysSo(t *testing.T) {
	m := buildModel(t)
	dir := writeProject(t, t.TempDir(), "setup = \"true\"\n")
	onSessionIn(t, m, dir)

	pressRunKey(t, m)

	if !strings.Contains(m.errBar.text, "no run scripts") {
		t.Fatalf("error = %q, want it to mention missing run scripts", m.errBar.text)
	}
}

func TestRunKeyRunsTheOnlyScriptWithoutAsking(t *testing.T) {
	m := buildModel(t)
	dir := writeProject(t, t.TempDir(), "[run.dev]\ncommand = \"cat\"\n")
	onSessionIn(t, m, dir)

	pressRunKey(t, m)

	if m.mode != modeList {
		t.Fatalf("a single script should not open the picker, mode = %v", m.mode)
	}
	sess, ok := m.selected()
	if !ok || !m.isShell(sess.Tool) {
		t.Fatalf("cursor should be on the new run session, got %+v", sess)
	}
	// Named after both the script and the session it runs beside, so two
	// worktrees running "dev" stay tellable apart.
	if !strings.HasPrefix(sess.Name, "dev") || !strings.Contains(sess.Name, "agent") {
		t.Fatalf("run session name = %q, want it to carry dev and agent", sess.Name)
	}
}

func TestRunKeyRunsTheDefaultWithoutAsking(t *testing.T) {
	m := buildModel(t)
	dir := writeProject(t, t.TempDir(),
		"[run.dev]\ncommand = \"cat\"\ndefault = true\n\n[run.test]\ncommand = \"cat\"\n")
	onSessionIn(t, m, dir)

	pressRunKey(t, m)

	if m.mode != modeList {
		t.Fatalf("a marked default should not open the picker, mode = %v", m.mode)
	}
	sess, _ := m.selected()
	if !strings.HasPrefix(sess.Name, "dev") {
		t.Fatalf("run session name = %q, want the default script", sess.Name)
	}
}

func TestRunKeyOpensThePickerWhenAmbiguous(t *testing.T) {
	m := buildModel(t)
	dir := writeProject(t, t.TempDir(),
		"[run.dev]\ncommand = \"cat\"\n\n[run.test]\ncommand = \"cat\"\n")
	onSessionIn(t, m, dir)
	before := len(m.sessions)

	pressRunKey(t, m)

	if m.mode != modeRunPick {
		t.Fatalf("mode = %v, want the run picker", m.mode)
	}
	if len(m.sessions) != before {
		t.Fatal("the picker must not have started anything yet")
	}
	if len(m.runPick.names) != 2 {
		t.Fatalf("picker rows = %v, want both scripts", m.runPick.names)
	}
	// The card has to render without panicking on a partially built state.
	if view := m.View(); !strings.Contains(view, "dev") {
		t.Fatalf("picker view is missing its rows: %q", view)
	}
}

func TestRunPickerEscapeLeavesEverythingAlone(t *testing.T) {
	m := buildModel(t)
	dir := writeProject(t, t.TempDir(),
		"[run.dev]\ncommand = \"cat\"\n\n[run.test]\ncommand = \"cat\"\n")
	onSessionIn(t, m, dir)
	pressRunKey(t, m)
	before := len(m.sessions)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.mode != modeList {
		t.Fatalf("mode = %v, want the list back", m.mode)
	}
	if len(m.sessions) != before {
		t.Fatal("escaping the picker started something")
	}
}

func TestRunPickerEnterStartsTheSelectedScript(t *testing.T) {
	m := buildModel(t)
	dir := writeProject(t, t.TempDir(),
		"[run.api]\ncommand = \"cat\"\n\n[run.web]\ncommand = \"cat\"\n")
	onSessionIn(t, m, dir)
	pressRunKey(t, m)

	// Rows are sorted with no default, so down lands on web.
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	if m.mode != modeList {
		t.Fatalf("mode = %v, want the list after running", m.mode)
	}
	sess, ok := m.selected()
	if !ok || !strings.HasPrefix(sess.Name, "web") {
		t.Fatalf("started %q, want the web script", sess.Name)
	}
}

// The port is the feature's whole point: several worktrees serving at once.
// It has to actually reach the pane's environment, not just the struct.
func TestRunScriptReceivesThePortInItsEnvironment(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "port")
	writeProject(t, dir, "[run.dev]\ncommand = \"printf %s \\\"$"+project.EnvPort+"\\\" > "+out+" && cat\"\n")
	onSessionIn(t, m, dir)

	pressRunKey(t, m)
	if m.errBar.text != "" {
		t.Fatalf("run reported %q", m.errBar.text)
	}

	var body []byte
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
			body = b
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(body) == 0 {
		t.Fatalf("%s never reached the pane", project.EnvPort)
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil {
		t.Fatalf("%s = %q, not a number", project.EnvPort, body)
	}
	// The pane resolves symlinks, so compare against the port the resolved
	// directory name yields rather than the temp path as written.
	settings, err := project.Load(dir, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if want := settings.Port(portKey(resolved(t, dir))); port != want {
		t.Fatalf("%s = %d, want %d", project.EnvPort, port, want)
	}
}

// A run script belongs to the directory the cursor is in, so the same script
// in two worktrees is two sessions on two ports rather than one shared one.
func TestRunScriptsInTwoWorktreesGetDifferentPorts(t *testing.T) {
	settings := project.Settings{PortBase: project.DefaultPortBase}
	if a, b := settings.Port("feature-a"), settings.Port("feature-b"); a == b {
		t.Fatalf("two worktrees share port %d", a)
	}
}

// A worktree is a fresh checkout: the setup script is what makes it
// runnable, so it has to actually execute there, before the agent does.
func TestWorktreeSpawnRunsTheProjectSetupScript(t *testing.T) {
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	// The settings are committed, so the worktree checkout carries them.
	writeProject(t, repo, "setup = \"touch setup-ran\"\n")
	initGitRepo(t, repo)

	if err := m.spawnSession("claude", "wt-setup", repo, "", "", false, true); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sessions, err := m.store.ListSessions(true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	marker := filepath.Join(sessions[0].Cwd, "setup-ran")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("setup script never ran in the worktree (%s)", marker)
}

// A session spawned without a worktree works in a checkout the user already
// set up by hand, so re-running setup there would be a surprise.
func TestNonWorktreeSpawnLeavesSetupAlone(t *testing.T) {
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeProject(t, repo, "setup = \"touch setup-ran\"\n")
	initGitRepo(t, repo)

	if err := m.spawnSession("claude", "plain", repo, "", "", false, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(repo, "setup-ran")); err == nil {
		t.Fatal("setup ran for a session that is not in a worktree")
	}
}

// A setup script that fails must not hand a half-installed tree to an agent.
func TestFailingSetupDoesNotStartTheAgent(t *testing.T) {
	command := project.SetupCommand("false", "touch agent-ran")
	if !strings.Contains(command, "false") {
		t.Fatalf("setup missing from %q", command)
	}
	before, _, ok := strings.Cut(command, "else")
	if !ok {
		t.Fatalf("no failure branch in %q", command)
	}
	if !strings.Contains(before, "touch agent-ran") {
		t.Fatalf("the agent should run on the success branch, got %q", before)
	}
}

// The old behaviour was an error naming a path the reader then had to leave
// the manager to create. p is where they are when they discover the feature,
// so it offers to write it instead.
func TestRunKeyOffersToCreateSettingsWhenThereAreNone(t *testing.T) {
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	onSessionIn(t, m, repo)

	pressRunKey(t, m)

	if m.mode != modeRunInit {
		t.Fatalf("mode = %v, want the create-settings offer", m.mode)
	}
	// A long checkout path truncates in the card, so the offer shows the
	// repository name and the relative path, both of which must survive.
	view := m.View()
	if !strings.Contains(view, project.Dir) || !strings.Contains(view, "repo") {
		t.Fatalf("the offer should name where it would write: %q", view)
	}
}

func TestRunInitEscapeWritesNothing(t *testing.T) {
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	onSessionIn(t, m, repo)
	pressRunKey(t, m)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.mode != modeList {
		t.Fatalf("mode = %v, want the list back", m.mode)
	}
	if _, err := os.Stat(project.SettingsPath(repo)); err == nil {
		t.Fatal("escaping the offer wrote a settings file")
	}
}

// The file lands at the repository root, not the session's directory, so a
// session started a level down does not leave a stray settings file there.
func TestRunInitWritesAtTheRepositoryRoot(t *testing.T) {
	m := buildModel(t)
	repo := filepath.Join(t.TempDir(), "repo")
	nested := filepath.Join(repo, "cmd", "server")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	onSessionIn(t, m, nested)
	pressRunKey(t, m)

	if want := project.SettingsPath(resolved(t, repo)); m.runInit.path != want {
		t.Fatalf("would write %q, want %q", m.runInit.path, want)
	}
}

// The scaffold must be inert: creating it cannot change how anything already
// works, so p on the fresh file reports no run scripts rather than running
// something the reader never wrote.
func TestScaffoldedSettingsAreInert(t *testing.T) {
	root := t.TempDir()
	path, err := project.Scaffold(root)
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if path != project.SettingsPath(root) {
		t.Fatalf("wrote %q, want %q", path, project.SettingsPath(root))
	}
	settings, err := project.Load(root)
	if err != nil {
		t.Fatalf("the scaffold does not parse: %v", err)
	}
	if !settings.Found {
		t.Fatal("the scaffold should be found once written")
	}
	if settings.Setup != "" || len(settings.Run) != 0 {
		t.Fatalf("the scaffold should declare nothing, got %+v", settings)
	}
}

func TestScaffoldRefusesToOverwrite(t *testing.T) {
	root := t.TempDir()
	writeProject(t, root, "setup = \"mine\"\n")

	if _, err := project.Scaffold(root); err == nil {
		t.Fatal("Scaffold overwrote existing settings")
	}
	settings, err := project.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Setup != "mine" {
		t.Fatalf("existing settings were clobbered: %+v", settings)
	}
}
