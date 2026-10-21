package deps

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnixNpmUsesGlobalWhenPrefixIsWritable(t *testing.T) {
	home, pathDir, prefix, log := npmFixture(t)
	out, code := runUnixNpm(t, home, pathDir, prefix, log)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	text := readLog(t, log)
	if !strings.Contains(text, "install -g @google/gemini-cli") || strings.Contains(text, "--prefix") || strings.Contains(text, "curl") {
		t.Fatalf("npm log = %q, want a global install", text)
	}
	if got, err := os.Readlink(filepath.Join(pathDir, "gemini")); err != nil || got != filepath.Join(prefix, "bin", "gemini") {
		t.Fatalf("gemini link = %q, %v", got, err)
	}
}

func TestUnixNpmUsesHomePrefixWhenGlobalIsNotWritable(t *testing.T) {
	home, pathDir, prefix, log := npmFixture(t)
	if err := os.Chmod(prefix, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(prefix, 0o755) })
	out, code := runUnixNpm(t, home, pathDir, prefix, log)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	text := readLog(t, log)
	wantPrefix := filepath.Join(home, ".local")
	if !strings.Contains(text, "--prefix "+wantPrefix) || strings.Contains(text, "curl") {
		t.Fatalf("npm log = %q, want --prefix %s", text, wantPrefix)
	}
	if got, err := os.Readlink(filepath.Join(pathDir, "gemini")); err != nil || got != filepath.Join(wantPrefix, "bin", "gemini") {
		t.Fatalf("gemini link = %q, %v", got, err)
	}
}

func TestUnixNpmFailsWhenNodeStaysInvisible(t *testing.T) {
	for _, hostNode := range []string{"/usr/bin/node", "/bin/node"} {
		if _, err := os.Stat(hostNode); err == nil {
			t.Skipf("%s stays visible to the recipe", hostNode)
		}
	}
	home, pathDir, prefix, log := npmFixture(t)
	if err := os.Remove(filepath.Join(pathDir, "node")); err != nil {
		t.Fatal(err)
	}
	_, code := runUnixNpm(t, home, pathDir, prefix, log)
	if code == 0 {
		t.Fatal("install succeeded with no node on PATH")
	}
	if strings.Contains(readLog(t, log), "curl") {
		t.Fatal("a missing node downloaded Node.js")
	}
}

func TestShellRefusesWindowsNpmPaths(t *testing.T) {
	dir := t.TempDir()
	writeExe(t, filepath.Join(dir, "npm.cmd"), "#!/bin/sh\nexit 0\n")
	writeExe(t, filepath.Join(dir, "npm"), "#!/bin/sh\nexit 0\n")
	script := shellHelpers + `
visible_bin npm.cmd "$1"
echo "cmd:$?"
visible_bin npm "$1"
echo "sh:$?"
first=$(first_writable "/mnt/c/nodejs:$1" || true)
echo "dest:$first"
first_writable "/mnt/c/nodejs" || echo "no-dest"
`
	out, code := runShell(t, script, []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}, dir)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "cmd:1") || !strings.Contains(out, "sh:0") || !strings.Contains(out, "dest:"+dir) || !strings.Contains(out, "no-dest") {
		t.Fatalf("output:\n%s", out)
	}
	for _, piece := range []string{"node.exe", "npm.cmd", "/mnt/*", "aname=drvfs"} {
		if !strings.Contains(shellHelpers, piece) {
			t.Fatalf("shell helpers omit %s", piece)
		}
	}
}

func npmFixture(t *testing.T) (home, pathDir, prefix, log string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	pathDir = filepath.Join(root, "bin")
	prefix = filepath.Join(root, "prefix")
	log = filepath.Join(root, "npm.log")
	for _, dir := range []string{home, pathDir, prefix} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExe(t, filepath.Join(pathDir, "npm"), `#!/bin/sh
printf '%s\n' "$*" >> "$NPM_LOG"
if [ "$1" = prefix ]; then
  printf '%s\n' "$NPM_PREFIX"
  exit 0
fi
dest=$NPM_PREFIX
prev=
for arg in "$@"; do
  if [ "$prev" = "--prefix" ]; then
    dest=$arg
  fi
  prev=$arg
done
mkdir -p "$dest/bin"
printf '#!/bin/sh\nexit 0\n' > "$dest/bin/$NPM_BIN"
chmod 755 "$dest/bin/$NPM_BIN"
exit 0
`)
	writeExe(t, filepath.Join(pathDir, "node"), "#!/bin/sh\nexit 0\n")
	writeExe(t, filepath.Join(pathDir, "curl"), "#!/bin/sh\nprintf 'curl %s\n' \"$*\" >> \"$NPM_LOG\"\nexit 1\n")
	return home, pathDir, prefix, log
}

func writeExe(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func runUnixNpm(t *testing.T, home, pathDir, prefix, log string) (string, int) {
	t.Helper()
	return runShell(t, unixNpm("gemini", "@google/gemini-cli"), []string{
		"PATH=" + pathDir + ":/usr/bin:/bin",
		"HOME=" + home,
		"NPM_LOG=" + log,
		"NPM_PREFIX=" + prefix,
		"NPM_BIN=gemini",
		"TMPDIR=" + t.TempDir(),
	})
}

func runShell(t *testing.T, script string, env []string, args ...string) (string, int) {
	t.Helper()
	requireDash(t)
	file := filepath.Join(t.TempDir(), "recipe.sh")
	if err := os.WriteFile(file, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return runCommand(t, exec.Command("/bin/dash", append([]string{file}, args...)...), env)
}
