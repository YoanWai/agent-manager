//go:build unix

package remote

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote/remotetest"
)

// managerScript stands in for an agent-manager build: it answers
// --version, snapshot and serve the way that build would.
func managerScript(snapshotVersion string) string {
	return "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"--version) echo 'agent-manager 0.41.0' ;;\n" +
		"snapshot) echo '{\"version\":" + snapshotVersion + ",\"manager_awake\":true,\"sessions\":[{\"id\":\"a1\"}]}' ;;\n" +
		"serve) echo '{\"started\":false}' ;;\n" +
		"*) exit 2 ;;\n" +
		"esac\n"
}

// shellHost runs every remote command through a real /bin/sh under its
// own HOME, as sshd would hand it to the login shell, with whatever the
// call streams on stdin.
type shellHost struct {
	t     *testing.T
	home  string
	path  string
	uname string
	// mangle changes the upload on its way, as a broken link would.
	mangle func([]byte) []byte
	words  [][]string
}

func newShellHost(t *testing.T, onPath bool) *shellHost {
	t.Helper()
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	path := "/usr/bin:/bin:/usr/sbin:/sbin"
	if onPath {
		path = bin + ":" + path
	}
	return &shellHost{t: t, home: home, path: path, uname: "Linux x86_64"}
}

func (h *shellHost) binary() string { return filepath.Join(h.home, ".local", "bin", "agent-manager") }

func (h *shellHost) place(script string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(h.binary()), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(h.binary(), []byte(script), 0o755); err != nil {
		h.t.Fatal(err)
	}
}

func (h *shellHost) run(ctx context.Context, argv []string, stdin io.Reader) ([]byte, []byte, error) {
	words, err := remotetest.Words(argv)
	if err != nil {
		h.t.Fatal(err)
	}
	h.words = append(h.words, words)
	if words[0] == "uname" {
		return remotetest.Answer([]byte(h.uname + "\n")), nil, nil
	}
	if stdin != nil && h.mangle != nil {
		sent, err := io.ReadAll(stdin)
		if err != nil {
			h.t.Fatal(err)
		}
		stdin = bytes.NewReader(h.mangle(sent))
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", append([]string{"-c", `exec "$@"`, "sh"}, words...)...)
	cmd.Env = []string{"HOME=" + h.home, "PATH=" + h.path}
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	return remotetest.Answer(stdout.Bytes()), stderr.Bytes(), err
}

func (h *shellHost) ran(command string) bool {
	for _, words := range h.words {
		if words[0] == command {
			return true
		}
	}
	return false
}

// releaseFetcher hands out a build as the verified download would, and
// records what it was asked for.
type releaseFetcher struct {
	build string
	err   error
	asked []string
}

func (f *releaseFetcher) fetch(_ context.Context, tag, goos, goarch, dir string) (string, error) {
	f.asked = append(f.asked, tag+" "+goos+"/"+goarch)
	if f.err != nil {
		return "", f.err
	}
	binary := filepath.Join(dir, "agent-manager")
	return binary, os.WriteFile(binary, []byte(f.build), 0o755)
}

func newShellClient(t *testing.T, host *shellHost, fetcher *releaseFetcher) *Client {
	t.Helper()
	c := New(t.TempDir(), WithInputRunner(host.run), WithFetcher(fetcher.fetch))
	t.Cleanup(func() { os.RemoveAll(c.controlDir) })
	c.SetConnections([]Connection{{Name: "gpu", Destination: "me@gpu"}})
	return c
}

func TestInstallUpgradesAnOutdatedHostAndTheNextRefreshSucceeds(t *testing.T) {
	host := newShellHost(t, true)
	host.place(managerScript("0"))
	fetcher := &releaseFetcher{build: managerScript("1")}
	c := newShellClient(t, host, fetcher)
	ctx := context.Background()

	if state := c.Refresh(ctx, "gpu"); state.OK || !Installable(state.Err) {
		t.Fatalf("outdated host refreshed as %+v, want an installable failure", state)
	}
	installed, err := c.Install(ctx, "gpu", "0.41.0")
	if err != nil {
		t.Fatal(err)
	}
	want := Installed{Version: "v0.41.0", Platform: Platform{OS: "linux", Arch: "amd64"}, Path: host.binary(), Found: host.binary(), OnPath: true}
	if installed != want || installed.PathAdvice() != "" {
		t.Fatalf("Install = %+v, want %+v", installed, want)
	}
	if len(fetcher.asked) != 1 || fetcher.asked[0] != "v0.41.0 linux/amd64" {
		t.Fatalf("downloaded %q, want this build's release for the host's platform", fetcher.asked)
	}
	if got, _ := os.ReadFile(host.binary()); string(got) != managerScript("1") {
		t.Fatalf("the host holds %q after the install", got)
	}
	if state := c.Refresh(ctx, "gpu"); !state.OK || len(state.Snapshot.Sessions) != 1 {
		t.Fatalf("refresh after the install = %+v", state)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(host.binary()), ".agent-manager.*")); len(leftovers) != 0 {
		t.Fatalf("the install left %q beside the binary", leftovers)
	}
}

func TestInstallOnAHostWithoutAgentManagerNamesTheMissingPathEntry(t *testing.T) {
	host := newShellHost(t, false)
	host.uname = "Darwin arm64"
	fetcher := &releaseFetcher{build: managerScript("1")}
	c := newShellClient(t, host, fetcher)
	ctx := context.Background()

	if state := c.Refresh(ctx, "gpu"); !Installable(state.Err) {
		t.Fatalf("a host without agent-manager refreshed as %+v", state)
	}
	installed, err := c.Install(ctx, "gpu", "0.41.0")
	if err != nil {
		t.Fatal(err)
	}
	if installed.OnPath || installed.Found != "" || installed.Platform != (Platform{OS: "darwin", Arch: "arm64"}) {
		t.Fatalf("Install = %+v, want darwin/arm64 off PATH", installed)
	}
	if advice := installed.PathAdvice(); advice != "add "+filepath.Dir(host.binary())+" to PATH in the login profile there" {
		t.Fatalf("advice %q", advice)
	}
}

func TestInstallChecksumMismatchLeavesTheHostUntouched(t *testing.T) {
	old := managerScript("0")
	for _, tc := range []struct {
		name    string
		fetcher *releaseFetcher
		mangle  func([]byte) []byte
		want    string
		upload  bool
	}{
		{
			name:    "the release download",
			fetcher: &releaseFetcher{err: errors.New("update: checksum mismatch: got 00, want ff")},
			want:    "checksum mismatch",
		},
		{
			name:    "the upload",
			fetcher: &releaseFetcher{build: managerScript("1")},
			mangle:  func(sent []byte) []byte { return sent[:len(sent)/2] },
			want:    "the upload did not arrive whole; nothing was installed",
			upload:  true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := newShellHost(t, true)
			host.place(old)
			host.mangle = tc.mangle
			c := newShellClient(t, host, tc.fetcher)
			_, err := c.Install(context.Background(), "gpu", "0.41.0")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Install = %v, want %q", err, tc.want)
			}
			if host.ran("sh") != tc.upload {
				t.Fatalf("upload ran %v, want %v: %q", host.ran("sh"), tc.upload, host.words)
			}
			if got, _ := os.ReadFile(host.binary()); string(got) != old {
				t.Fatalf("the host's binary changed to %q", got)
			}
			if entries, _ := os.ReadDir(filepath.Dir(host.binary())); len(entries) != 1 {
				t.Fatalf("the refused install left %v beside the binary", entries)
			}
		})
	}
}
