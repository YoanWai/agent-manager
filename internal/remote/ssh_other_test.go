//go:build !unix

package remote

import (
	"context"
	"os"
	"slices"
	"testing"
)

func TestSSHArgv(t *testing.T) {
	c := New("/profile")
	got := c.sshArgv("me@gpu", false, remoteCommand(program(Connection{}), []string{"snapshot", "--json"}))
	want := []string{
		"ssh",
		"-o", "BatchMode=yes",
		"-o", "ForwardAgent=no",
		"-o", "ForwardX11=no",
		"-o", "ClearAllForwardings=yes",
		"-o", "ConnectTimeout=5",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=2",
		"-T",
		"me@gpu",
		`exec "$SHELL" -lc 'printf %s '\''` + "\x1eagent-manager\x1e" +
			`'\''; exec '\''agent-manager'\'' '\''snapshot'\'' '\''--json'\'''`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("argv:\n got %q\nwant %q", got, want)
	}
}

// With no master to share, neither a call nor an attach makes a control
// directory.
func TestCallsMakeNoControlDirectory(t *testing.T) {
	c := newTestClient(t, func(context.Context, []string) ([]byte, []byte, error) {
		return []byte(`{"id":"a1"}`), nil, nil
	})
	if _, err := c.Kill(context.Background(), Ref{Host: "gpu", ID: "a1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AttachCommand(Ref{Host: "gpu", ID: "a1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.controlDir); !os.IsNotExist(err) {
		t.Fatalf("control dir %s: %v, want none", c.controlDir, err)
	}
}
