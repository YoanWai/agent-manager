package deps

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func stubUID(t *testing.T, uid int) {
	t.Helper()
	original := getUID
	getUID = func() int { return uid }
	t.Cleanup(func() { getUID = original })
}

func stubPath(t *testing.T, present ...string) {
	t.Helper()
	found := make(map[string]bool, len(present))
	for _, name := range present {
		found[name] = true
	}
	original := lookPath
	lookPath = func(name string) (string, error) {
		if found[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = original })
}

func TestInstallCommandPrefersNativeManagerOverBrew(t *testing.T) {
	stubUID(t, 1)
	stubPath(t, "brew", "apt-get", "sudo")
	if got, want := installCommand("linux", "tmux"), "sudo apt-get update && sudo apt-get install -y tmux"; got != want {
		t.Fatalf("installCommand = %q, want %q", got, want)
	}
}

func TestInstallCommandOnDarwinUsesBrew(t *testing.T) {
	stubPath(t, "brew", "apt-get")
	if got, want := installCommand("darwin", "git"), "brew install git"; got != want {
		t.Fatalf("installCommand = %q, want %q", got, want)
	}
}

func TestHintFallsBackWithoutAKnownManager(t *testing.T) {
	stubPath(t)
	if got, want := hint("linux", "tmux"), "install tmux with your package manager"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
}

func TestHintNamesTheCommand(t *testing.T) {
	stubUID(t, 1)
	stubPath(t, "pacman", "sudo")
	if got, want := hint("linux", "tmux"), "install it with: sudo pacman -S --needed tmux"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
}

func TestInstallCommandRootDropsSudo(t *testing.T) {
	stubUID(t, 0)
	stubPath(t, "apt-get")
	if got, want := installCommand("linux", "tmux"), "apt-get update && apt-get install -y tmux"; got != want {
		t.Fatalf("installCommand = %q, want %q", got, want)
	}
}

func TestInstallCommandSkipsAptWithoutSudo(t *testing.T) {
	stubUID(t, 1)
	stubPath(t, "apt-get", "brew")
	if got, want := installCommand("linux", "tmux"), "brew install tmux"; got != want {
		t.Fatalf("installCommand = %q, want %q", got, want)
	}
}

func TestInstallCommandApkAsRootHasNoSudo(t *testing.T) {
	stubUID(t, 0)
	stubPath(t, "apk")
	if got, want := installCommand("linux", "tmux"), "apk add tmux"; got != want {
		t.Fatalf("installCommand = %q, want %q", got, want)
	}
}

func TestInstallCommandApkAsNonRootTakesSudo(t *testing.T) {
	stubUID(t, 1)
	stubPath(t, "apk", "sudo")
	if got, want := installCommand("linux", "tmux"), "sudo apk add tmux"; got != want {
		t.Fatalf("installCommand = %q, want %q", got, want)
	}
}

func TestInstallCommandSkipsApkWithoutSudo(t *testing.T) {
	stubUID(t, 1)
	stubPath(t, "apk")
	if got := installCommand("linux", "tmux"); got != "" {
		t.Fatalf("installCommand = %q, want empty", got)
	}
}

func TestHintPrefersOfficialInstallerOverPackageManager(t *testing.T) {
	stubPath(t, "brew")
	got := hint("darwin", "claude")
	if !strings.Contains(got, "claude.ai/install.sh") {
		t.Fatalf("hint = %q, want the official installer", got)
	}
	if strings.Contains(got, "brew install") {
		t.Fatalf("official installer must win over brew, got %q", got)
	}
}

func TestOfficialInstallIsPortable(t *testing.T) {
	if len(official) == 0 {
		t.Fatal("no official installers")
	}
	for name, got := range official {
		if strings.Contains(got, "brew") || strings.Contains(got, "apt") || strings.Contains(got, "winget") {
			t.Errorf("%s install is OS-specific: %s", name, got)
		}
	}
}

func TestCommandIsTheVendorInstaller(t *testing.T) {
	if got := Command("claude"); got != installers(runtime.GOOS)["claude"] {
		t.Fatalf("Command = %q, want the official installer", got)
	}
}

// The curl | bash installers do not run on native Windows, so a hint there
// names the PowerShell installer, a Windows package manager, or nothing.
func TestHintOnWindowsAvoidsUnixInstallers(t *testing.T) {
	stubPath(t, "scoop", "brew", "apt-get")
	if got := hint("windows", "claude"); !strings.Contains(got, "claude.ai/install.ps1") {
		t.Fatalf("hint = %q, want the PowerShell installer", got)
	}
	if got, want := hint("windows", "git"), "install it with: scoop install git"; got != want {
		t.Fatalf("hint = %q, want %q", got, want)
	}
	for _, tool := range []string{"claude", "codex", "gemini", "grok", "hermes", "muse", "opencode", "pi", "agy", "omp"} {
		if got := hint("windows", tool); strings.Contains(got, "curl") {
			t.Fatalf("hint for %s = %q, want no Unix installer", tool, got)
		}
	}
}

// Every agent CLI the manager ships has a runnable Windows install, so a
// spawn finding a missing one can offer the fix on any platform. psmux and
// the plain-shell rows are not agent CLIs; cmd is documented as unsupported
// on native Windows.
func TestEveryBuiltinAgentHasAWindowsInstall(t *testing.T) {
	agents := []string{"claude", "codex", "gemini", "grok", "hermes", "muse", "opencode", "pi", "agy", "omp"}
	for _, tool := range agents {
		if got := installers("windows")[tool]; got == "" {
			t.Errorf("%s has no Windows install", tool)
		}
	}
	if got := installers("windows")["cmd"]; got == "" {
		t.Error("cmd lost its npm install, which does run on Windows")
	}
}

// A package-manager line names the tool's own command, so running one for
// a tool we do not know would install whatever package shares that name.
// The hint still says it; only the runnable command is withheld.
func TestCommandWithholdsPackageManagerLines(t *testing.T) {
	stubUID(t, 1)
	stubPath(t, "pacman", "sudo")
	for _, tool := range []string{"tmux", "acme"} {
		if got := Command(tool); got != "" {
			t.Fatalf("Command(%q) = %q, want none", tool, got)
		}
	}
	if got := hint("linux", "tmux"); !strings.Contains(got, "pacman") {
		t.Fatalf("hint = %q, want the package manager still suggested", got)
	}
}

func TestInstallCommandOnDarwinBootstrapsHomebrew(t *testing.T) {
	stubPath(t)
	got := installCommand("darwin", "tmux")
	if got != homebrewSetup+"tmux" {
		t.Fatalf("installCommand = %q, want the Homebrew bootstrap", got)
	}
}

func TestInstallCommandOnWindowsUsesWingetIDs(t *testing.T) {
	stubPath(t, "winget", "scoop")
	if got, want := installCommand("windows", "git"), "winget install --id Git.Git -e"; got != want {
		t.Fatalf("installCommand(git) = %q, want %q", got, want)
	}
	if got, want := installCommand("windows", "pwsh"), "winget install --id Microsoft.PowerShell -e"; got != want {
		t.Fatalf("installCommand(pwsh) = %q, want %q", got, want)
	}
	if got, want := installCommand("windows", "acme"), "scoop install acme"; got != want {
		t.Fatalf("installCommand(acme) = %q, want %q", got, want)
	}
}

func TestWindowsRecipes(t *testing.T) {
	exact := map[string]string{
		"pi":    windowsNpm("--ignore-scripts @earendil-works/pi-coding-agent"),
		"codex": "& ([scriptblock]::Create((irm https://chatgpt.com/codex/install.ps1)))",
		"agy":   "irm https://antigravity.google/cli/install.ps1 | iex",
		"omp":   "irm https://omp.sh/install.ps1 | iex",
		"psmux": "winget install --id marlocarlo.psmux -e",
	}
	for name, want := range exact {
		if got := windowsOfficial[name]; got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	if !strings.Contains(windowsOfficial["opencode"], "@opencode/cli") || strings.Contains(windowsOfficial["opencode"], "opencode-ai") {
		t.Fatalf("opencode = %q, want @opencode/cli", windowsOfficial["opencode"])
	}
	if !strings.Contains(windowsOfficial["cmd"], "command-code") {
		t.Fatalf("cmd = %q, want the npm package", windowsOfficial["cmd"])
	}
}

func TestUnixRecipes(t *testing.T) {
	if got, want := official["pi"], "curl -fsSL https://pi.dev/install.sh | sh"; got != want {
		t.Fatalf("pi = %q, want %q", got, want)
	}
	if !strings.Contains(official["opencode"], "https://opencode.ai/v2/install") || strings.Contains(official["opencode"], "opencode-ai") {
		t.Fatalf("opencode = %q, want the 2.x installer", official["opencode"])
	}
	for _, name := range []string{"gemini", "cmd"} {
		got := official[name]
		if !strings.Contains(got, `--prefix "$HOME/.local"`) || !strings.Contains(got, "node.exe") {
			t.Fatalf("%s = %q, want a prefix fallback that refuses a Windows npm", name, got)
		}
	}
	cmd := official["cmd"]
	head, _, _ := strings.Cut(cmd, "on_windows_path")
	if !strings.Contains(head, "npm install -g command-code") {
		t.Fatalf("cmd lead = %q, want npm install -g command-code", head)
	}
}

func TestHaveTTYWithoutATerminalDoesNotAbort(t *testing.T) {
	script := "set -eu\n" + installFuncs(t, "have_tty") + `
if have_tty; then
  echo has-tty
else
  echo no-tty
fi
echo survived
`
	out, code := runDashNoTTY(t, script, []string{"PATH=/usr/bin:/bin:/usr/local/bin"})
	if code != 0 || !strings.Contains(out, "no-tty") || !strings.Contains(out, "survived") {
		t.Fatalf("have_tty without a terminal: exit %d\n%s", code, out)
	}
}

func TestConfirmHonorsTheDepsFlag(t *testing.T) {
	body := "set -eu\n" + installFuncs(t, "have_tty", "confirm") + "confirm 'install now?' && echo yes || echo no\n"
	yes, code := runDash(t, body, []string{"PATH=/usr/bin:/bin", "AGENT_MANAGER_INSTALL_DEPS=1"})
	if code != 0 || !strings.Contains(yes, "yes") {
		t.Fatalf("flag 1: exit %d\n%s", code, yes)
	}
	no, code := runDash(t, body, []string{"PATH=/usr/bin:/bin", "AGENT_MANAGER_INSTALL_DEPS=0"})
	if code != 0 || !strings.Contains(no, "no") {
		t.Fatalf("flag 0: exit %d\n%s", code, no)
	}
}

func TestConfirmDeclinesWithoutATerminal(t *testing.T) {
	script := "set -eu\n" + installFuncs(t, "have_tty", "confirm") + "confirm 'install now?' && echo yes || echo no\necho survived\n"
	out, code := runDashNoTTY(t, script, []string{"PATH=/usr/bin:/bin:/usr/local/bin"})
	if code != 0 || !strings.Contains(out, "no") || !strings.Contains(out, "survived") || strings.Contains(out, "yes") {
		t.Fatalf("confirm without a terminal: exit %d\n%s", code, out)
	}
}

func TestDepInstallCmdOnDarwin(t *testing.T) {
	body := "set -eu\n" + installFuncs(t, "package_for", "map_packages", "set_sudo", "have_tty", "dep_install_cmd") + `
detect_os() { echo darwin; }
dep_install_cmd "tmux git"
`
	got, code := runDashNoTTY(t, body, []string{"PATH=/usr/bin:/bin"})
	if code != 0 || got != "NONINTERACTIVE=1 "+homebrewSetup+"tmux git" {
		t.Fatalf("darwin without brew: exit %d\n%q", code, got)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "brew"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, code = runDash(t, body, []string{"PATH=" + dir + ":/usr/bin:/bin"})
	if code != 0 || got != "brew install tmux git" {
		t.Fatalf("darwin with brew: exit %d\n%q", code, got)
	}
}

func TestDepInstallCmdMapsDesktopPackages(t *testing.T) {
	body := "set -eu\n" + installFuncs(t, "package_for", "map_packages", "set_sudo", "have_tty", "dep_install_cmd") + `
detect_os() { echo linux; }
dep_install_cmd "tmux notify-send"
`
	dir := t.TempDir()
	for _, name := range []string{"apt-get", "zypper", "sudo"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExe(t, filepath.Join(dir, "id"), "#!/bin/sh\nprintf '1000\\n'\n")
	apt, code := runDash(t, body, []string{"PATH=" + dir})
	if code != 0 || apt != "sudo apt-get update && sudo apt-get install -y tmux libnotify-bin" {
		t.Fatalf("apt: exit %d\n%q", code, apt)
	}
	zypperDir := t.TempDir()
	for _, name := range []string{"zypper", "id", "sudo"} {
		if err := os.WriteFile(filepath.Join(zypperDir, name), []byte("#!/bin/sh\nprintf '1000\\n'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	zypper, code := runDash(t, body, []string{"PATH=" + zypperDir})
	if code != 0 || zypper != "sudo zypper install -y tmux libnotify-tools" {
		t.Fatalf("zypper: exit %d\n%q", code, zypper)
	}
}

func TestDesktopDepsFollowTheSession(t *testing.T) {
	skipOnWSLHost(t)
	body := "set -eu\n" + installFuncs(t, "in_wsl", "desktop_deps") + `
detect_os() { echo linux; }
missing=
desktop_deps
printf '%s\n' "$missing"
`
	requireDash(t)
	bin := t.TempDir()
	if err := os.Symlink("/usr/bin/grep", filepath.Join(bin, "grep")); err != nil {
		t.Fatal(err)
	}
	base := []string{"PATH=" + bin}
	headless, code := runDash(t, body, base)
	if code != 0 || strings.TrimSpace(headless) != "" {
		t.Fatalf("headless: exit %d\n%q", code, headless)
	}
	wayland, code := runDash(t, body, append(base, "WAYLAND_DISPLAY=wayland-0"))
	if code != 0 || !strings.Contains(wayland, "wl-clipboard") || !strings.Contains(wayland, "notify-send") || strings.Contains(wayland, "xclip") {
		t.Fatalf("wayland: exit %d\n%q", code, wayland)
	}
	x11, code := runDash(t, body, append(base, "DISPLAY=:0"))
	if code != 0 || !strings.Contains(x11, "xclip") || !strings.Contains(x11, "notify-send") || strings.Contains(x11, "wl-clipboard") {
		t.Fatalf("x11: exit %d\n%q", code, x11)
	}
	wsl, code := runDash(t, body, append(base, "DISPLAY=:0", "WSL_DISTRO_NAME=Ubuntu"))
	if code != 0 || strings.TrimSpace(wsl) != "" {
		t.Fatalf("wsl: exit %d\n%q", code, wsl)
	}
}

func TestInWSLDoesNotAbort(t *testing.T) {
	skipOnWSLHost(t)
	body := "set -eu\n" + installFuncs(t, "in_wsl") + "in_wsl && echo wsl || echo not-wsl\necho survived\n"
	out, code := runDash(t, body, []string{"PATH=/usr/bin:/bin"})
	if code != 0 || !strings.Contains(out, "not-wsl") || !strings.Contains(out, "survived") {
		t.Fatalf("not wsl: exit %d\n%s", code, out)
	}
	out, code = runDash(t, body, []string{"PATH=/usr/bin:/bin", "WSL_DISTRO_NAME=Ubuntu"})
	if code != 0 || !strings.HasPrefix(out, "wsl\n") || !strings.Contains(out, "survived") {
		t.Fatalf("wsl: exit %d\n%s", code, out)
	}
}

func installFuncs(t *testing.T, names ...string) string {
	t.Helper()
	source, err := os.ReadFile(repoFile(t, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for _, name := range names {
		body.WriteString(extractFunc(t, string(source), name))
		body.WriteByte('\n')
	}
	return body.String()
}

func repoFile(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("%s not found from %s", name, dir)
		}
		dir = parent
	}
}

func TestExtractFuncToleratesCRLF(t *testing.T) {
	source := "\nhave_tty() {\r\n\treturn 1\r\n}\r\n"
	got := extractFunc(t, source, "have_tty")
	if !strings.Contains(got, "return 1") || strings.Contains(got, "\r") {
		t.Fatalf("extract = %q", got)
	}
}

func extractFunc(t *testing.T, source, name string) string {
	t.Helper()
	marker := "\n" + name + "() {"
	start := strings.Index(source, marker)
	if start < 0 {
		t.Fatalf("install.sh has no %s", name)
	}
	lines := strings.Split(source[start+1:], "\n")
	var body strings.Builder
	for index, line := range lines {
		if index > 0 {
			body.WriteByte('\n')
		}
		line = strings.TrimRight(line, "\r")
		body.WriteString(line)
		if index > 0 && line == "}" {
			return body.String()
		}
	}
	t.Fatalf("%s never closed", name)
	return ""
}

func requireDash(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/bin/dash"); err != nil {
		t.Skip("/bin/dash is not installed")
	}
}

// skipOnWSLHost leaves out the cases whose in_wsl answer the host decides
// through /proc/version, which no environment variable can hide.
func skipOnWSLHost(t *testing.T) {
	t.Helper()
	version, err := os.ReadFile("/proc/version")
	if err == nil && strings.Contains(strings.ToLower(string(version)), "microsoft") {
		t.Skip("this host is WSL, so in_wsl reads it from /proc/version")
	}
}

func runDash(t *testing.T, script string, env []string) (string, int) {
	t.Helper()
	requireDash(t)
	return runCommand(t, exec.Command("/bin/dash", "-c", script), env)
}

func runDashNoTTY(t *testing.T, script string, env []string) (string, int) {
	t.Helper()
	requireDash(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to drop the controlling terminal")
	}
	python := `
import os, sys
pid = os.fork()
if pid == 0:
    os.setsid()
    os.execv("/bin/dash", ["dash", "-c", sys.argv[1]])
_, status = os.waitpid(pid, 0)
if os.WIFEXITED(status):
    raise SystemExit(os.WEXITSTATUS(status))
raise SystemExit(1)
`
	return runCommand(t, exec.Command("python3", "-c", python, script), env)
}

func runCommand(t *testing.T, cmd *exec.Cmd, env []string) (string, int) {
	t.Helper()
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode()
	}
	t.Fatal(err)
	return "", 1
}

func TestVerifyAttestationMatchesPowerShellCase(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "gh.log")
	writeExe(t, filepath.Join(dir, "gh"), "#!/bin/sh\nprintf 'prompt=%s\\n' \"${GH_PROMPT_DISABLED-unset}\" >> \"$GH_LOG\"\nprintf '%s\\n' \"$*\" >> \"$GH_LOG\"\nexit \"${GH_EXIT:-0}\"\n")
	body := "set -eu\nREPO=YoanWai/agent-manager\n" + installFuncs(t, "err", "verify_attestation") + "verify_attestation /tmp/archive.tar.gz\nprintf 'done\\n'\n"
	path := "PATH=" + dir
	base := []string{path, "GH_LOG=" + log}

	out, code := runDash(t, body, base)
	if code != 0 || !strings.Contains(out, "done") || fileExists(log) {
		t.Fatalf("unset flag called gh: exit %d\n%s", code, out)
	}

	for _, flag := range []string{"1", "y", "Y", "yes", "YES", "Yes", "true", "TRUE", "True"} {
		if err := os.Remove(log); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		out, code = runDash(t, body, append(append([]string{}, base...), "AGENT_MANAGER_VERIFY_ATTESTATION="+flag, "GH_EXIT=0"))
		got := readLog(t, log)
		if code != 0 || !strings.Contains(out, "done") || !strings.Contains(got, "prompt=1\nattestation verify /tmp/archive.tar.gz --repo YoanWai/agent-manager\n") {
			t.Fatalf("flag %s: exit %d\n%s\nlog %q", flag, code, out, got)
		}
	}

	for _, flag := range []string{"", "0", "no", "yesterday", "truth"} {
		if err := os.Remove(log); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		out, code = runDash(t, body, append(append([]string{}, base...), "AGENT_MANAGER_VERIFY_ATTESTATION="+flag, "GH_EXIT=1"))
		if code != 0 || !strings.Contains(out, "done") || fileExists(log) {
			t.Fatalf("flag %q must skip gh: exit %d\n%s", flag, code, out)
		}
	}

	if err := os.Remove(log); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	out, code = runDash(t, body, append(append([]string{}, base...), "AGENT_MANAGER_VERIFY_ATTESTATION=1", "GH_EXIT=1"))
	if code != 1 || !strings.Contains(out, "error: attestation verification failed for /tmp/archive.tar.gz") {
		t.Fatalf("rejected attestation: exit %d\n%s", code, out)
	}

	out, code = runDash(t, body, []string{"PATH=" + t.TempDir(), "AGENT_MANAGER_VERIFY_ATTESTATION=yes"})
	if code != 1 || !strings.Contains(out, "error: AGENT_MANAGER_VERIFY_ATTESTATION is set but gh is not installed") {
		t.Fatalf("missing gh: exit %d\n%s", code, out)
	}
}

func TestReleaseFooterShowsTheScriptBeforeRunning(t *testing.T) {
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh is not installed")
	}
	fence := powershellFence(t, ".goreleaser.yaml", "install.ps1")
	show := strings.Index(fence, "Get-Content .\\install.ps1")
	run := strings.Index(fence, "powershell -NoProfile -ExecutionPolicy Bypass -File .\\install.ps1")
	if show < 0 || run < 0 || show > run {
		t.Fatalf("release footer runs install.ps1 without showing it:\n%s", fence)
	}
	script := "$ErrorActionPreference = 'Continue'\n" +
		"Remove-Item Alias:\\irm -Force\n" +
		"function scoop { }\n" +
		"function irm { param($Url, $OutFile) Set-Content -LiteralPath $OutFile -Value 'script-body' }\n" +
		"function Unblock-File { param($Path) }\n" +
		"function powershell { 'RAN' }\n" +
		fence
	out, code := runPwsh(t, script, "")
	shown := strings.Index(out, "script-body")
	ran := strings.Index(out, "RAN")
	if code != 0 || shown < 0 || ran < 0 || shown > ran {
		t.Fatalf("footer did not show the script before running it: exit %d\n%s", code, out)
	}
}

func TestPowerShellAttestationExampleRequiresGh(t *testing.T) {
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh is not installed")
	}
	for _, path := range []string{"README.md", "docs/install.md"} {
		fence := powershellFence(t, path, "gh attestation verify .\\install.ps1")
		if !strings.Contains(fence, "Get-Command gh -ErrorAction Stop") {
			t.Fatalf("%s attestation example does not require gh", path)
		}
		out, code := runPwsh(t, missingGhPrelude()+fence, "")
		if strings.Contains(out, "RAN") || code == 0 {
			t.Fatalf("%s missing gh ran the installer: exit %d\n%s", path, code, out)
		}
	}
	zip := powershellFence(t, "docs/install.md", "gh attestation verify $archive")
	out, code := runPwsh(t, "$global:LASTEXITCODE = 0\n$archive = 'archive.zip'\n"+zip+"'PASSED'\n", "")
	if strings.Contains(out, "PASSED") || code == 0 {
		t.Fatalf("zip verify treated a missing gh as success: exit %d\n%s", code, out)
	}
	readme := powershellFence(t, "README.md", "gh attestation verify .\\install.ps1")
	out, code = runPwsh(t, missingGhPrelude()+readme, writeGhStub(t, 0))
	if code != 0 || !strings.Contains(out, "RAN") {
		t.Fatalf("verify success should install: exit %d\n%s", code, out)
	}
	out, code = runPwsh(t, missingGhPrelude()+readme, writeGhStub(t, 1))
	if strings.Contains(out, "RAN") || code == 0 {
		t.Fatalf("verify failure should stop: exit %d\n%s", code, out)
	}
}

func missingGhPrelude() string {
	return "$ErrorActionPreference = 'Continue'\n" +
		"Remove-Item Alias:\\irm -Force\n" +
		"function irm { param($Url, $OutFile) }\n" +
		"function Unblock-File { param($Path) }\n" +
		"function powershell { 'RAN' }\n" +
		"Set-Content -LiteralPath install.ps1 -Value 'ok'\n" +
		"$global:LASTEXITCODE = 0\n"
}

func writeGhStub(t *testing.T, exit int) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		writeExe(t, filepath.Join(dir, "gh.cmd"), fmt.Sprintf("@exit /b %d\r\n", exit))
		return dir
	}
	writeExe(t, filepath.Join(dir, "gh"), fmt.Sprintf("#!/bin/sh\nexit %d\n", exit))
	return dir
}

func powershellFence(t *testing.T, path, marker string) string {
	t.Helper()
	source, err := os.ReadFile(repoFile(t, path))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.ReplaceAll(string(source), "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "```powershell" {
			continue
		}
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		var body []string
		closed := false
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "```" {
				closed = true
				i = j
				break
			}
			line := lines[j]
			if len(line) >= indent && strings.TrimLeft(line[:indent], " ") == "" {
				line = line[indent:]
			}
			body = append(body, line)
		}
		if !closed {
			t.Fatalf("%s fence never closed", path)
		}
		text := strings.Join(body, "\n")
		if strings.Contains(text, marker) {
			return text + "\n"
		}
	}
	t.Fatalf("%s has no powershell fence containing %s", path, marker)
	return ""
}

func runPwsh(t *testing.T, script, pathDir string) (string, int) {
	t.Helper()
	cmd := exec.Command("pwsh", "-NoProfile", "-Command", script)
	cmd.Dir = t.TempDir()
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(strings.ToUpper(entry), "PATH=") {
			continue
		}
		env = append(env, entry)
	}
	// A cleared environment drops SYSTEMROOT, and pwsh on Windows then
	// fails before the script runs. Only the search path changes, so a
	// stub gh is the one Get-Command finds.
	cmd.Env = append(env, "PATH="+pathDir)
	return runCommand(t, cmd, cmd.Env)
}

func TestDetectOSOnWindowsNamesTheReadableInstaller(t *testing.T) {
	body := "set -eu\n" + installFuncs(t, "err", "detect_os") + "uname() { echo Windows_NT; }\ndetect_os\n"
	out, code := runDash(t, body, []string{"PATH=/usr/bin:/bin"})
	const want = "https://raw.githubusercontent.com/YoanWai/agent-manager/main/install.ps1"
	if code != 1 || !strings.Contains(out, want) || strings.Contains(out, "releases/latest/download/install.ps1") {
		t.Fatalf("windows hint: exit %d\n%s", code, out)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestHintShowsOneLineForScriptRecipes(t *testing.T) {
	for _, tool := range []string{"gemini", "cmd", "opencode"} {
		got := hint("linux", tool)
		if strings.Contains(got, "\n") || got != "install it with: "+shown[tool] {
			t.Fatalf("%s hint = %q, want the one line %q", tool, got, shown[tool])
		}
		if !strings.Contains(official[tool], "publish_file") {
			t.Fatalf("%s recipe = %q, want the npm helper", tool, official[tool])
		}
	}
	if got := hint("windows", "gemini"); got != "install it with: "+windowsOfficial["gemini"] {
		t.Fatalf("windows gemini hint = %q", got)
	}
}
