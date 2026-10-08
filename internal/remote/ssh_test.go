package remote

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeArgvEnv turns the test binary into a fake agent-manager that prints
// its arguments as JSON.
const fakeArgvEnv = "AM_REMOTE_TEST_PRINT_ARGV"

func TestMain(m *testing.M) {
	if os.Getenv(fakeArgvEnv) == "1" {
		if err := json.NewEncoder(os.Stdout).Encode(os.Args[1:]); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

var awkwardArgs = []string{
	"send",
	"with spaces",
	"it's",
	`say "hi"`,
	`'`,
	`''`,
	`\'`,
	`back\slash\\`,
	"$HOME",
	"${HOME}",
	"`whoami`",
	"$(whoami)",
	"a; echo pwned",
	"a && b | c > d",
	"line one\nline two\n",
	"tab\there",
	"héllo ✓ 日本語 🚀",
	"",
	"--json",
	"-lc",
	"--",
	"!bang",
	"*",
	"~",
	"%C",
	"#comment",
}

// The login profile is what puts the fake first on PATH, the way a user's
// profile does for the real binary, and it prints to stdout the way conda,
// nvm or a stray echo does.
func TestRemoteCommandRoundTripsThroughLoginShell(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := "#!/bin/sh\nexec " + quote(self) + ` "$@"` + "\n"
	if err := os.WriteFile(filepath.Join(bin, "agent-manager"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	profile := []byte(`PATH="$HOME/bin:$PATH"; export PATH` + "\n" +
		`echo '(base) conda activated {"version":0}'` + "\n" +
		`printf 'nvm: now using node ['` + "\n")
	for _, name := range []string{".profile", ".zprofile"} {
		if err := os.WriteFile(filepath.Join(home, name), profile, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	command := remoteCommand(append([]string{"agent-manager"}, awkwardArgs...))
	shells := []string{"/bin/sh"}
	for _, name := range []string{"bash", "zsh", "dash"} {
		if path, err := exec.LookPath(name); err == nil {
			shells = append(shells, path)
		}
	}
	for _, shell := range shells {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			cmd := exec.Command(shell, "-c", command)
			cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "SHELL=" + shell, fakeArgvEnv + "=1"}
			stdout, err := cmd.Output()
			if err != nil {
				t.Fatalf("%s -c: %v\nstdout: %s", shell, err, stdout)
			}
			out, marked := answer(stdout)
			if !marked {
				t.Fatalf("%s printed no marker: %q", shell, stdout)
			}
			var got []string
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("fake agent-manager printed %q: %v", out, err)
			}
			if !slices.Equal(got, awkwardArgs) {
				t.Fatalf("arguments changed on the way through %s:\n got %q\nwant %q", shell, got, awkwardArgs)
			}
		})
	}
}

func TestQuoteKeepsBackslashesOutsideQuotes(t *testing.T) {
	got := quote(`it's a\b`)
	want := `'it'\''s a'\\'b'`
	if got != want {
		t.Fatalf("quote = %s, want %s", got, want)
	}
}

func TestSSHArgv(t *testing.T) {
	c := New("/profile")
	got := c.sshArgv("me@gpu", false, remoteCommand([]string{"agent-manager", "snapshot", "--json"}))
	want := []string{
		"ssh",
		"-o", "BatchMode=yes",
		"-o", "ForwardAgent=no",
		"-o", "ClearAllForwardings=yes",
		"-o", "ConnectTimeout=5",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=2",
		"-o", "ControlMaster=auto",
		"-o", "ControlPersist=60",
		"-o", "ControlPath=" + c.controlDir + "/%C",
		"-T",
		"me@gpu",
		`exec "$SHELL" -lc 'printf %s '\''` + "\x1eagent-manager\x1e" +
			`'\''; exec '\''agent-manager'\'' '\''snapshot'\'' '\''--json'\'''`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("argv:\n got %q\nwant %q", got, want)
	}
}

func TestControlDirIsShortPrivateAndPerProfile(t *testing.T) {
	dir := controlDir("/home/me/.config/agent-manager")
	if dir != controlDir("/home/me/.config/agent-manager") || dir == controlDir("/home/me/other") {
		t.Fatalf("control dir %s is not one per profile", dir)
	}
	base := filepath.Base(dir)
	if !strings.HasPrefix(base, "am-ssh-") || len(base) != len("am-ssh-")+8 {
		t.Fatalf("control dir name %s is not am-ssh- and 8 hex", base)
	}
	// %C expands to 40 hex characters.
	if socket := len(dir) + 1 + 40; socket > 100 {
		t.Fatalf("control socket path would be %d bytes", socket)
	}
	scratch := filepath.Join(t.TempDir(), "am-ssh-test")
	if err := os.Mkdir(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureControlDir(scratch); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(scratch)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("control dir mode %v, want 0700", info.Mode().Perm())
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(scratch, link); err != nil {
		t.Fatal(err)
	}
	if err := ensureControlDir(link); err == nil {
		t.Fatal("a symlink was accepted as the control dir")
	}
}

func TestAttachCommand(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-501/default,1,0")
	c := newTestClient(t, nil)
	cmd, err := c.AttachCommand(Ref{Host: "gpu", ID: "a1b2c3d4"})
	if err != nil {
		t.Fatal(err)
	}
	if got := cmd.Args[len(cmd.Args)-3]; got != "-t" {
		t.Fatalf("attach requests %s, want -t", got)
	}
	if got := cmd.Args[len(cmd.Args)-2]; got != "me@gpu" {
		t.Fatalf("attach destination %s", got)
	}
	want := []string{"tmux", "-u", "-L", "agentmgr", "attach-session", "-t", "am_a1b2c3d4"}
	if got := remoteWords(t, cmd.Args); !slices.Equal(got, want) {
		t.Fatalf("attach runs %q, want %q", got, want)
	}
	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, "TMUX=") {
			t.Fatalf("attach keeps %s", entry)
		}
	}
	if _, err := c.AttachCommand(Ref{Host: "gpu", ID: "a1;rm"}); err == nil {
		t.Fatal("an unsafe id reached the attach command")
	}
	if _, err := c.AttachCommand(Ref{Host: "nowhere", ID: "a1"}); err == nil {
		t.Fatal("an unknown connection was attached")
	}
}

func TestRunSSHCapsWhatAHostPrints(t *testing.T) {
	cases := []struct {
		name, script, want string
	}{
		{"stdout", "head -c 9000000 /dev/zero", "answered with more than 8 MiB"},
		{"stderr", "head -c 70000 /dev/zero >&2", "printed more than 64 KiB of errors"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := runSSH(context.Background(), []string{"sh", "-c", tc.script})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
			if len(stdout) > 8<<20 || len(stderr) > 64<<10 {
				t.Fatalf("kept %d bytes of stdout and %d of stderr", len(stdout), len(stderr))
			}
		})
	}
	stdout, _, err := runSSH(context.Background(), []string{"sh", "-c", "head -c 8388608 /dev/zero"})
	if err != nil || len(stdout) != 8<<20 {
		t.Fatalf("an answer of exactly the cap = %d bytes, %v", len(stdout), err)
	}
}
