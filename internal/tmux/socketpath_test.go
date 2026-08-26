package tmux

import (
	"strings"
	"testing"
)

func TestSocketPathNamesTheRunningServer(t *testing.T) {
	driver := requireTmux(t)
	out, err := tmuxCmd("display-message", "-p", "#{socket_path}").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := strings.TrimSpace(string(out))
	if got := driver.SocketPath(); got != want {
		t.Fatalf("SocketPath = %q, want tmux's own %q", got, want)
	}
}

// The -L name is shared by every manager; only the path tells two servers
// apart, and it is the path a session is stamped with.
func TestSocketPathSeparatesServersUnderOneName(t *testing.T) {
	here := requireTmux(t)
	herePath := here.SocketPath()
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	elsewhere, err := NewWithSocket(testSocket)
	if err != nil {
		t.Fatal(err)
	}
	if herePath == elsewhere.SocketPath() {
		t.Fatalf("both servers resolved to %q", herePath)
	}
	if !strings.HasSuffix(elsewhere.SocketPath(), "/"+testSocket) {
		t.Fatalf("path %q does not end in the socket name", elsewhere.SocketPath())
	}
}
