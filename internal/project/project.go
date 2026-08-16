// Package project reads the per-repository settings a checked-in
// .agent-manager/settings.toml carries: the setup script a new worktree is
// bootstrapped with, and the run scripts p offers.
//
// These are repository settings rather than user settings on purpose. What
// it takes to make a fresh worktree runnable — install dependencies, copy an
// untracked .env, start the dev server — is a property of the project, the
// same for everyone working in it, and so belongs in the repository next to
// the code rather than in one developer's ~/.config.
package project

import (
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	Dir  = ".agent-manager"
	File = "settings.toml"
)

// EnvPort is exported into every run script and setup script. A project whose
// server binds it can have one worktree per feature all serving at once,
// which is the whole point of running agents in parallel.
const EnvPort = "AGENT_MANAGER_PORT"

// Run is one entry in the picker p opens.
type Run struct {
	// Command is handed to the user's shell, so it may be a pipeline, use
	// $AGENT_MANAGER_PORT, or be several lines.
	Command string `toml:"command"`
	// Description is what the picker shows beside the name. The command
	// itself is shown when this is empty.
	Description string `toml:"description"`
	// Default marks the entry p runs without asking when the picker would
	// otherwise open. Only the first one by name wins, so adding a second
	// default cannot make the key ambiguous.
	Default bool `toml:"default"`
}

// Settings is a repository's .agent-manager/settings.toml.
type Settings struct {
	// Setup runs inside a newly created worktree before its agent starts.
	Setup string `toml:"setup"`
	// PortBase is the low end of the range worktrees are given ports from.
	// Zero means DefaultPortBase.
	PortBase int `toml:"port_base"`
	// Run holds the named scripts p offers, keyed by the name shown.
	Run map[string]Run `toml:"run"`
	// Root is the directory the settings were found in, and Found reports
	// whether there was a file at all. A repository with no settings loads
	// as a zero Settings rather than an error: the feature is opt-in.
	Root  string `toml:"-"`
	Found bool   `toml:"-"`
}

// DefaultPortBase is where per-worktree port blocks start when the project
// does not choose. Above the range a plain `npm run dev` would take, so a
// worktree server and a hand-started one do not collide by default.
const DefaultPortBase = 3100

// portBlocks is how many distinct blocks the range is divided into, and
// portBlockSize how many consecutive ports each worktree gets. A project
// needing more than one port per worktree can derive the rest by adding to
// $AGENT_MANAGER_PORT, which is why a block is handed out rather than a
// single number.
const (
	portBlocks    = 100
	portBlockSize = 10
)

// maxPortBase is the highest base whose whole range still fits below the
// last usable port.
const maxPortBase = 65535 - portBlocks*portBlockSize + 1

// Load reads the settings governing dir, walking up from it so a session
// started in a subdirectory of the repository still finds them.
//
// The walk stops at root, which must be the repository dir is inside. That
// bound is a security boundary, not tidiness: Setup and every Run.Command
// are handed to a shell, so a settings file picked up from a directory above
// the repository would be someone else's code running as the user. A shared
// parent — /tmp, a home directory, a machine where checkouts sit beside each
// other — is exactly where that file would be planted. An empty root reads
// dir alone, which is the safe reading when the caller cannot say where the
// repository begins.
//
// A repository without the file is not an error; the returned Settings is
// zero and Found is false.
func Load(dir, root string) (Settings, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return Settings{}, err
	}
	// Compared as resolved paths: a repository reached through a symlink —
	// /tmp on macOS is one — would otherwise never match its own root and
	// the walk would stop before it started.
	stop := resolve(dir)
	if root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			stop = resolve(abs)
		}
	}
	for cur := resolve(dir); ; {
		path := filepath.Join(cur, Dir, File)
		if _, err := os.Stat(path); err == nil {
			return parse(path, cur)
		}
		if cur == stop {
			return Settings{}, nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Above the root without ever meeting it: dir was not inside the
			// repository it was said to be in. Refusing beats walking on.
			return Settings{}, nil
		}
		cur = parent
	}
}

// resolve follows symlinks where it can, leaving the path alone when it
// cannot: a directory that has since been removed is the caller's problem to
// report, not a reason for discovery to fail here.
func resolve(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}

func parse(path, root string) (Settings, error) {
	var settings Settings
	if _, err := toml.DecodeFile(path, &settings); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
	}
	settings.Root = root
	settings.Found = true
	if settings.PortBase == 0 {
		settings.PortBase = DefaultPortBase
	}
	// Rejected at load rather than clamped at use: a base that cannot hold
	// the whole range would otherwise hand out a port nothing can bind, and
	// the failure would surface as a server that will not start rather than
	// as the setting that caused it.
	if settings.PortBase < 1 || settings.PortBase > maxPortBase {
		return Settings{}, fmt.Errorf(
			"%s: port_base %d is outside 1-%d, which is what fits %d blocks of %d ports",
			path, settings.PortBase, maxPortBase, portBlocks, portBlockSize)
	}
	for name, run := range settings.Run {
		if strings.TrimSpace(run.Command) == "" {
			return Settings{}, fmt.Errorf("%s: run script %q has no command", path, name)
		}
	}
	return settings, nil
}

// Template is what Scaffold writes. Every line is commented out except the
// structure, so the file it produces is inert: pressing the key that creates
// it cannot change how anything already works, and the reader edits rather
// than deletes.
const Template = `# How agent-manager bootstraps and runs this project.
# Docs: https://github.com/YoanWai/agent-manager/blob/main/docs/project-settings.md
#
# Commit this file. What it takes to bootstrap the project is the same for
# everyone working on it, so it belongs next to the code.

# Runs once inside every new worktree, before its agent starts. A worktree is
# a fresh checkout, so anything git does not track is missing from it.
# On failure the agent does not start and the pane leaves you a shell.
# setup = """
# npm ci
# cp ../` + "`basename $PWD`" + `/.env .
# """

# The low end of the port range worktrees are handed. 3100 by default.
# port_base = 3100

# Scripts p offers. Mark one default = true to have p run it without asking.
# $AGENT_MANAGER_PORT is this worktree's own port, so every branch can serve
# at once; derive more with $((AGENT_MANAGER_PORT + 1)).
# [run.dev]
# command = "npm run dev -- --port $AGENT_MANAGER_PORT"
# description = "dev server"
# default = true

# [run.test]
# command = "npm test -- --watch"
`

// Write creates a repository's settings from the two things the group form
// asks for, and reports where they landed. Either may be empty; a run
// command is written as the default script, since a project asked for one
// command means that command is what p should run.
//
// An existing file is never rewritten. Round-tripping a hand-edited TOML
// file through a form would have to preserve comments, key order and the
// blocks the form has no field for, which is an editor — and the manager
// already has a key that opens the real one.
func Write(root, setup, run string) (string, error) {
	setup, run = strings.TrimSpace(setup), strings.TrimSpace(run)
	var b strings.Builder
	b.WriteString(writtenHeader)
	if setup != "" {
		b.WriteString("\n# Runs once inside every new worktree, before its agent starts.\n")
		b.WriteString("setup = " + tomlString(setup) + "\n")
	}
	if run != "" {
		b.WriteString("\n# What p runs. $PORT and $" + EnvPort + " are this worktree's own,\n")
		b.WriteString("# so every branch can serve at once.\n")
		b.WriteString("[run.dev]\ncommand = " + tomlString(run) + "\ndefault = true\n")
	}
	return create(root, b.String())
}

const writtenHeader = `# How agent-manager bootstraps and runs this project.
# Docs: https://github.com/YoanWai/agent-manager/blob/main/docs/project-settings.md
#
# Commit this file. What it takes to bootstrap the project is the same for
# everyone working on it, so it belongs next to the code.
`

// tomlString renders a value as TOML.
//
// Literal strings are preferred because these values are shell: a literal
// has no escape processing at all, so a backslash, a $ or a quote in a
// command survives being written and read back exactly as typed. A value
// carrying the literal delimiter itself falls back to a basic string, which
// can express anything once escaped.
func tomlString(s string) string {
	if !strings.Contains(s, "'''") && !strings.HasSuffix(s, "'") {
		if strings.ContainsAny(s, "\n\r") {
			// TOML trims the newline that follows the opening delimiter but
			// not the one before the closing delimiter, so only the leading
			// one is written: a trailing newline would come back as content.
			return "'''\n" + s + "'''"
		}
		if !strings.Contains(s, "'") {
			return "'" + s + "'"
		}
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func create(root, body string) (string, error) {
	dir := filepath.Join(root, Dir)
	path := filepath.Join(dir, File)
	if _, err := os.Stat(path); err == nil {
		return path, fmt.Errorf("%s already exists", path)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Scaffold writes Template into root's settings path and reports where it
// landed. An existing file is never overwritten — the point is to give a
// project its first one, and a project that already has settings has
// something worth more than a template.
func Scaffold(root string) (string, error) {
	return create(root, Template)
}

// SettingsPath is where a repository's settings live, whether or not the
// file is there yet.
func SettingsPath(root string) string {
	return filepath.Join(root, Dir, File)
}

// RunNames lists the run scripts in the order the picker shows them: the
// default first so it sits under the cursor, then the rest by name, so the
// list a project sees never depends on map iteration order.
func (s Settings) RunNames() []string {
	names := make([]string, 0, len(s.Run))
	for name := range s.Run {
		names = append(names, name)
	}
	sort.Strings(names)
	if def, ok := s.DefaultRun(); ok {
		ordered := []string{def}
		for _, name := range names {
			if name != def {
				ordered = append(ordered, name)
			}
		}
		return ordered
	}
	return names
}

// DefaultRun names the script p runs without opening the picker: the one
// marked default, or the only one there is. Two scripts marked default
// resolve to the first by name rather than to whichever the map yielded.
func (s Settings) DefaultRun() (string, bool) {
	marked := make([]string, 0, 1)
	for name, run := range s.Run {
		if run.Default {
			marked = append(marked, name)
		}
	}
	if len(marked) > 0 {
		sort.Strings(marked)
		return marked[0], true
	}
	if len(s.Run) == 1 {
		for name := range s.Run {
			return name, true
		}
	}
	return "", false
}

// Port is the first port of the block belonging to a session of this name.
// Derived from the name rather than assigned from a counter so that it
// survives restarts of both the session and the manager: a worktree keeps
// the address you bookmarked for as long as it keeps its name.
//
// A block with anything listening anywhere in it is skipped, which is what
// makes two names that happen to hash together still able to run at once.
// The whole block is checked rather than only its base, since a project
// deriving a second port from $AGENT_MANAGER_PORT would otherwise be handed
// a block whose upper ports are already someone else's.
//
// The probe is a hint, not a reservation — nothing stops a process taking
// the port between the probe and the server starting — so a project that
// cannot tolerate that should bind and fail loudly rather than share.
func (s Settings) Port(name string) int {
	base := s.PortBase
	if base == 0 {
		base = DefaultPortBase
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	start := int(hash.Sum32() % portBlocks)
	for offset := range portBlocks {
		port := base + ((start+offset)%portBlocks)*portBlockSize
		if blockFree(port) {
			return port
		}
	}
	// Every block busy: hand back the name's own, so the failure the project
	// sees is its server refusing the port rather than a silent move to
	// somewhere it was not expecting.
	return base + start*portBlockSize
}

// blockFree reports whether nothing is listening anywhere in the block that
// starts at port.
func blockFree(port int) bool {
	for offset := range portBlockSize {
		if !portFree(port + offset) {
			return false
		}
	}
	return true
}

// portFree reports whether nothing is listening on the port yet.
func portFree(port int) bool {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

// EnvPortAlias is the name most dev servers already read — node, rails,
// flask and the rest of the PORT convention. Exporting it too is what makes
// isolation free: an unmodified `npm run dev` in five worktrees serves on
// five ports without the project changing a line.
const EnvPortAlias = "PORT"

// Env is what a setup or run script is launched with on top of the session's
// own environment. A project that never opted in gets nothing: Env is only
// meaningful for settings that were actually found, and exporting PORT into
// every worktree regardless would change how unrelated projects behave.
func (s Settings) Env(name string) map[string]string {
	if !s.Found {
		return nil
	}
	port := strconv.Itoa(s.Port(name))
	return map[string]string{EnvPort: port, EnvPortAlias: port}
}

// SetupCommand wraps the setup script so a failure is visible and
// recoverable instead of silent. On success the agent runs exactly as it
// would have without a setup script. On failure it is not started at all —
// running one against a half-installed tree wastes a turn on errors that are
// not the code's — and the pane says why.
//
// Neither branch execs. The launch script this is embedded in ends with its
// own `exec $SHELL`, which is what leaves a usable prompt in the pane once
// the command finishes; exec'ing here would replace the shell and take that
// away, so a failed setup — or an agent that exits — would kill the pane
// instead of leaving somewhere to fix it from.
//
// agentCommand may be empty, for a pane that opens a shell rather than an
// agent; the setup still runs first.
func SetupCommand(setup, agentCommand string) string {
	setup = strings.TrimSpace(setup)
	if setup == "" {
		return agentCommand
	}
	// A pane with no agent still needs a command in the success branch, since
	// an empty one is a syntax error.
	success := strings.TrimSpace(agentCommand)
	if success == "" {
		success = ":"
	}
	// $? in the else branch is the setup's own status: printf is the first
	// command to run there, so nothing has overwritten it yet.
	return "if " + setup + "\nthen\n" + success + "\nelse\n" +
		"  printf '\\n\\033[31magent-manager: setup failed (exit %d)\\033[0m\\n" +
		"Fix it here, then press R to restart this session.\\n\\n' \"$?\"\n" +
		"fi"
}
