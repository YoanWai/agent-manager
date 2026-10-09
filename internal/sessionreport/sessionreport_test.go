package sessionreport

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

const oddExe = `/opt/a b/it's "here"/agent-manager`

func testTarget(t *testing.T) Target {
	t.Helper()
	hooksDir := t.TempDir()
	return Target{Exe: oddExe, HooksDir: hooksDir, TelemetryFile: filepath.Join(hooksDir, "row.otel"), Cwd: t.TempDir()}
}

func TestHookCommandQuotesTheBinary(t *testing.T) {
	got := HookCommand(oddExe, "codex", "session_id")
	if want := "exec " + tmux.ShellQuote(oddExe) + " track-conversation --tool codex --key session_id"; got != want {
		t.Fatalf("HookCommand = %q, want %q", got, want)
	}
}

func TestPiAndOmpLoadAnExtensionBesideTheUsers(t *testing.T) {
	for _, tc := range []struct {
		style       string
		events      []string
		reportFlags string
	}{
		{"pi", []string{"session_start"}, `...["--tool","pi","--spawned"]`},
		{"omp", []string{"session_start", "session_switch", "session_branch"}, `...["--tool","omp"]`},
	} {
		t.Run(tc.style, func(t *testing.T) {
			target := testTarget(t)
			command, err := Apply(tc.style, target, "pi --session 'abc' -e mine.ts", map[string]string{})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(target.HooksDir, "track-"+tc.style+".ts")
			if want := "pi --session 'abc' -e mine.ts -e " + tmux.ShellQuote(path); command != want {
				t.Fatalf("command = %q, want %q", command, want)
			}
			source := readFile(t, path)
			eventList, _ := json.Marshal(tc.events)
			exe, _ := json.Marshal(oddExe)
			for _, want := range []string{string(eventList), string(exe), tc.reportFlags, "ctx.sessionManager.getSessionId()", "if (!ctx.hasUI) return;"} {
				if !strings.Contains(source, want) {
					t.Fatalf("extension lacks %s:\n%s", want, source)
				}
			}
		})
	}
}

func TestOpencodeLoadsATUIPluginThroughItsEnvironment(t *testing.T) {
	t.Setenv("OPENCODE_TUI_CONFIG", "")
	target := testTarget(t)
	env := map[string]string{}
	command, err := Apply("opencode", target, "opencode --session x", env)
	if err != nil {
		t.Fatal(err)
	}
	if command != "opencode --session x" {
		t.Fatalf("command = %q, want it untouched", command)
	}
	var tuiConfig struct {
		Plugin []string `json:"plugin"`
	}
	if err := json.Unmarshal([]byte(readFile(t, env["OPENCODE_TUI_CONFIG"])), &tuiConfig); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(target.HooksDir, "track-opencode.ts")
	if !slices.Equal(tuiConfig.Plugin, []string{plugin}) {
		t.Fatalf("tui config plugins = %v, want %s", tuiConfig.Plugin, plugin)
	}
	exe, _ := json.Marshal(oddExe)
	if source := readFile(t, plugin); !strings.Contains(source, string(exe)) || !strings.Contains(source, "info.parentID") {
		t.Fatalf("plugin source:\n%s", source)
	}
}

func TestOpencodePluginReportsSwitchesAndRetriesFailedReports(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	t.Setenv("OPENCODE_TUI_CONFIG", "")
	target := testTarget(t)
	reports := filepath.Join(target.HooksDir, "reports")
	retry := filepath.Join(target.HooksDir, "retry")
	target.Exe = filepath.Join(target.HooksDir, "report.sh")
	script := "#!/bin/sh\nif [ \"$5\" = second ] && [ ! -e " + tmux.ShellQuote(retry) + " ]; then touch " + tmux.ShellQuote(retry) + "; exit 1; fi\nprintf '%s\\n' \"$5\" >> " + tmux.ShellQuote(reports) + "\n"
	writeFile(t, target.Exe, script, 0o755)
	if _, err := Apply("opencode", target, "opencode", map[string]string{}); err != nil {
		t.Fatal(err)
	}
	plugin := readFile(t, filepath.Join(target.HooksDir, "track-opencode.ts"))
	writeFile(t, filepath.Join(target.HooksDir, "plugin.mjs"), strings.ReplaceAll(plugin, "api: any", "api"), 0o644)
	reportsJSON, _ := json.Marshal(reports)
	check := `import plugin from './plugin.mjs';
import { readFileSync } from 'node:fs';
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
let dispose;
let route = { name: 'session', params: { sessionID: 'first' } };
plugin.tui({
  route: { get current() { return route; } },
  state: { session: { get: id => ({ parentID: id === 'child' ? 'first' : undefined }) } },
  lifecycle: { onDispose: fn => { dispose = fn; } },
});
const reports = () => readFileSync(REPORTS, 'utf8').trim().split('\n');
const waitFor = async expected => {
  const deadline = Date.now() + 3000;
  while (JSON.stringify(reports()) !== JSON.stringify(expected)) {
    if (Date.now() > deadline) throw new Error(JSON.stringify(reports()));
    await sleep(50);
  }
};
await waitFor(['first']);
route = { name: 'session', params: { sessionID: 'second' } };
await waitFor(['first', 'second']);
route = { name: 'session', params: { sessionID: 'child' } };
await sleep(300);
route = { name: 'session', params: { sessionID: 'third' } };
await waitFor(['first', 'second', 'third']);
dispose();
route = { name: 'session', params: { sessionID: 'fourth' } };
await sleep(300);
if (JSON.stringify(reports()) !== JSON.stringify(['first', 'second', 'third'])) {
  throw new Error(JSON.stringify(reports()));
}
`
	path := filepath.Join(target.HooksDir, "check.mjs")
	writeFile(t, path, strings.ReplaceAll(check, "REPORTS", string(reportsJSON)), 0o644)
	if out, err := exec.Command(node, path).CombinedOutput(); err != nil {
		t.Fatalf("plugin failed: %v\n%s", err, out)
	}
}

func TestCommandCodeLoadsAModForTheSession(t *testing.T) {
	target := testTarget(t)
	command, err := Apply("command-code", target, "cmd --session abc --mod mine.ts", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(target.HooksDir, "track-cmd.ts")
	if want := "cmd --session abc --mod mine.ts --mod " + tmux.ShellQuote(path); command != want {
		t.Fatalf("command = %q, want %q", command, want)
	}
	source := readFile(t, path)
	exe, _ := json.Marshal(oddExe)
	for _, want := range []string{string(exe), `"--tool", "command-code", "--spawned"`, "onSessionStart", "info.sessionId"} {
		if !strings.Contains(source, want) {
			t.Fatalf("mod lacks %s:\n%s", want, source)
		}
	}
}

// The handler is the same text on every launch, which is what codex keeps
// the user's review of, and each -c value is TOML codex reads as a hook list.
// It reports with --spawned, since Codex from npm fires its hooks from the
// native binary its node launcher spawns below the launched pid.
func TestCodexAddsHooksForTheLaunchOnly(t *testing.T) {
	command, err := Apply("codex", testTarget(t), "codex resume abc", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	args, err := exec.Command("sh", "-c", `printarg() { for arg in "$@"; do printf '%s\0' "$arg"; done; }; printarg`+strings.TrimPrefix(command, "codex resume abc")).Output()
	if err != nil {
		t.Fatalf("unquote %q: %v", command, err)
	}
	words := strings.Split(strings.TrimSuffix(string(args), "\x00"), "\x00")
	if len(words) != 4 || words[0] != "-c" || words[2] != "-c" || !strings.HasPrefix(command, "codex resume abc -c ") {
		t.Fatalf("command = %q, want two -c values after the line", command)
	}
	for i, event := range []string{"SessionStart", "UserPromptSubmit"} {
		var parsed struct {
			Hooks map[string][]struct {
				Hooks []struct {
					Type    string `toml:"type"`
					Command string `toml:"command"`
				} `toml:"hooks"`
			} `toml:"hooks"`
		}
		if _, err := toml.Decode(words[2*i+1], &parsed); err != nil {
			t.Fatalf("-c %q: %v", words[2*i+1], err)
		}
		groups := parsed.Hooks[event]
		if len(groups) != 1 || len(groups[0].Hooks) != 1 || groups[0].Hooks[0].Type != "command" ||
			groups[0].Hooks[0].Command != HookCommand(oddExe, "codex", "session_id")+" --spawned" {
			t.Fatalf("-c %q = %+v, want our one handler for %s", words[2*i+1], parsed, event)
		}
	}
}

func TestGeminiLogsTelemetryToAFileOfTheManagers(t *testing.T) {
	isolateGemini(t)
	target := testTarget(t)
	writeFile(t, target.TelemetryFile, "from an earlier launch", 0o644)
	env := map[string]string{}
	command, err := Apply("gemini", target, "gemini --resume abc", env)
	follower := tmux.ShellQuote(oddExe) + " follow-telemetry --file " + tmux.ShellQuote(target.TelemetryFile) + " </dev/null >/dev/null 2>&1 & "
	if err != nil || command != follower+"gemini --resume abc" {
		t.Fatalf("Apply = %q, %v; want the follower started beside the agent", command, err)
	}
	want := map[string]string{
		"GEMINI_TELEMETRY_ENABLED":        "true",
		"GEMINI_TELEMETRY_TARGET":         "local",
		"GEMINI_TELEMETRY_OUTFILE":        target.TelemetryFile,
		"GEMINI_TELEMETRY_LOG_PROMPTS":    "false",
		"GEMINI_TELEMETRY_TRACES_ENABLED": "false",
	}
	for key, value := range want {
		if env[key] != value {
			t.Fatalf("env = %v, want %s=%s", env, key, value)
		}
	}
	if _, err := os.Stat(target.TelemetryFile); !os.IsNotExist(err) {
		t.Fatalf("an earlier launch's telemetry is still there: %v", err)
	}
}

// Gemini's environment overrides its settings, so a user with telemetry of
// their own keeps it and is told this session reports nothing.
func TestGeminiKeepsTheUsersOwnTelemetry(t *testing.T) {
	enabled := "{\n  // mine\n  \"telemetry\": {\"enabled\": true, \"target\": \"gcp\"} /* on */\n}"
	for _, tc := range []struct {
		name   string
		setup  func(t *testing.T, home, system, cwd string)
		wired  bool
		reason string
	}{
		{"no settings", func(*testing.T, string, string, string) {}, true, ""},
		{"telemetry off", func(t *testing.T, home, _, _ string) {
			writeFile(t, filepath.Join(home, ".gemini", "settings.json"), `{"telemetry": {"enabled": false}, "url": "http://x//y"}`, 0o644)
		}, true, ""},
		{"user settings", func(t *testing.T, home, _, _ string) {
			writeFile(t, filepath.Join(home, ".gemini", "settings.json"), enabled, 0o644)
		}, false, "its own telemetry settings are in use"},
		{"workspace settings", func(t *testing.T, _, _, cwd string) {
			writeFile(t, filepath.Join(cwd, ".gemini", "settings.json"), enabled, 0o644)
		}, false, "its own telemetry settings are in use"},
		{"system settings", func(t *testing.T, _, system, _ string) {
			writeFile(t, system, enabled, 0o644)
		}, false, "its own telemetry settings are in use"},
		{"system defaults", func(t *testing.T, _, system, _ string) {
			writeFile(t, filepath.Join(filepath.Dir(system), "system-defaults.json"), enabled, 0o644)
		}, false, "its own telemetry settings are in use"},
		{"environment", func(t *testing.T, _, _, _ string) {
			t.Setenv("GEMINI_TELEMETRY_TARGET", "gcp")
		}, false, "its own telemetry settings are in use"},
		{"workspace .env", func(t *testing.T, _, _, cwd string) {
			writeFile(t, filepath.Join(cwd, ".env"), "OTHER=1\nexport GEMINI_TELEMETRY_ENABLED=true\n", 0o644)
		}, false, "its own telemetry settings are in use"},
		{"home .gemini/.env", func(t *testing.T, home, _, _ string) {
			writeFile(t, filepath.Join(home, ".gemini", ".env"), "GEMINI_TELEMETRY_TARGET=gcp\n", 0o644)
		}, false, "its own telemetry settings are in use"},
		{".env without telemetry", func(t *testing.T, _, _, cwd string) {
			writeFile(t, filepath.Join(cwd, ".env"), "GEMINI_API_KEY=x\n", 0o644)
		}, true, ""},
		{"unreadable settings", func(t *testing.T, home, _, _ string) {
			writeFile(t, filepath.Join(home, ".gemini", "settings.json"), `{"telemetry": `, 0o644)
		}, false, "settings.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, system := isolateGemini(t)
			target := testTarget(t)
			tc.setup(t, home, system, target.Cwd)
			env := map[string]string{}
			command, err := Apply("gemini", target, "gemini", env)
			if err != nil {
				t.Fatal(err)
			}
			if wired := env["GEMINI_TELEMETRY_OUTFILE"] != ""; wired != tc.wired {
				t.Fatalf("wired = %v, want %v (env %v)", wired, tc.wired, env)
			}
			if tc.wired {
				return
			}
			if len(env) != 0 || !strings.HasSuffix(command, "; gemini") || !strings.Contains(command, tc.reason) {
				t.Fatalf("command = %q env %v, want a notice naming %q", command, env, tc.reason)
			}
		})
	}
}

// isolateGemini points every settings file gemini reads at fresh paths and
// clears the user's own telemetry variables.
func isolateGemini(t *testing.T) (home, system string) {
	t.Helper()
	home = t.TempDir()
	system = filepath.Join(t.TempDir(), "settings.json")
	t.Setenv("HOME", home)
	t.Setenv("GEMINI_CLI_HOME", home)
	t.Setenv("GEMINI_CLI_SYSTEM_SETTINGS_PATH", system)
	t.Setenv("GEMINI_CLI_SYSTEM_DEFAULTS_PATH", "")
	for _, variable := range os.Environ() {
		if name, _, _ := strings.Cut(variable, "="); strings.HasPrefix(name, "GEMINI_TELEMETRY_") {
			t.Setenv(name, "")
			os.Unsetenv(name)
		}
	}
	return home, system
}

func TestStripJSONCommentsKeepsStrings(t *testing.T) {
	in := "{\"a\": \"// not a comment\", /* gone */ \"b\": \"/* kept */\" // gone\n, \"c\": \"\\\"//\"}"
	var got map[string]string
	if err := json.Unmarshal(stripJSONComments([]byte(in)), &got); err != nil {
		t.Fatalf("stripped %q: %v", stripJSONComments([]byte(in)), err)
	}
	if got["a"] != "// not a comment" || got["b"] != "/* kept */" || got["c"] != `"//` {
		t.Fatalf("strings = %v", got)
	}
}

// No launch writes a file the user or a CLI owns. Every shipped tool is
// wired under a home that holds each CLI's own settings, and afterwards
// only the hooks directory has changed.
func TestNoLaunchWritesOutsideTheHooksDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, variable := range []string{"GEMINI_CLI_HOME", "GROK_HOME", "HERMES_HOME", "OPENCODE_TUI_CONFIG", "GEMINI_CLI_SYSTEM_DEFAULTS_PATH"} {
		t.Setenv(variable, "")
	}
	t.Setenv("GEMINI_CLI_SYSTEM_SETTINGS_PATH", filepath.Join(home, "system", "settings.json"))
	cwd := filepath.Join(home, "project")
	for _, path := range []string{
		".gemini/settings.json", ".gemini/config/hooks.json", ".grok/config.toml", ".commandcode/settings.json",
		".hermes/config.yaml", ".codex/config.toml", ".config/opencode/tui.json", ".pi/agent/settings.json",
		".omp/agent/config.yml", ".claude/settings.json", "project/.gemini/settings.json",
	} {
		writeFile(t, filepath.Join(home, path), "{}", 0o644)
	}
	before := snapshot(t, home)
	hooksDir := filepath.Join(home, ".config", "agent-manager", "hooks")
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	for name, tool := range cfg.Tools {
		target := Target{Exe: oddExe, HooksDir: hooksDir, TelemetryFile: filepath.Join(hooksDir, name+".otel"), Cwd: cwd}
		if _, err := Apply(tool.SessionReport, target, "exec "+tool.Command, map[string]string{}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	after := snapshot(t, home)
	for path, stamp := range after {
		if strings.HasPrefix(path, hooksDir+string(filepath.Separator)) || path == hooksDir || strings.HasPrefix(hooksDir, path+string(filepath.Separator)) {
			continue
		}
		if was, existed := before[path]; !existed || was != stamp {
			t.Errorf("a launch wrote %s", path)
		}
	}
	for path := range before {
		if _, kept := after[path]; !kept {
			t.Errorf("a launch removed %s", path)
		}
	}
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stamp := info.ModTime().Format(time.RFC3339Nano) + " " + info.Mode().String()
		if !entry.IsDir() {
			stamp += " " + readFile(t, path)
		}
		files[path] = stamp
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestStylesWithNoWiringLeaveTheLaunchAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, style := range []string{StyleClaude, StyleNone, "", "muse", "grok", "antigravity", "hermes"} {
		target := testTarget(t)
		env := map[string]string{}
		command, err := Apply(style, target, "claude --resume x", env)
		if err != nil || command != "claude --resume x" || len(env) != 0 {
			t.Fatalf("Apply(%q) = %q %v %v", style, command, env, err)
		}
		if entries, _ := os.ReadDir(target.HooksDir); len(entries) != 0 {
			t.Fatalf("Apply(%q) wrote %v", style, entries)
		}
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatalf("a style with no wiring touched the home: %v", entries)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
}
