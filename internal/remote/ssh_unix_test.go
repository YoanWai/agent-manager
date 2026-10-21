//go:build unix

package remote

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// helperPID reads the pid a script echoed for the child it left holding
// the pipe, and ends that child when the test does.
func helperPID(t *testing.T, stdout []byte) int {
	t.Helper()
	pid, err := strconv.Atoi(strings.TrimSpace(string(stdout)))
	if err != nil {
		t.Fatalf("script printed %q", stdout)
	}
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
	return pid
}

// A ProxyCommand helper outlives a killed ssh; the deadline ends the whole
// process group rather than waiting on the helper's copy of the pipe.
func TestRunSSHDeadlineEndsTheProcessGroup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	stdout, _, err := runSSH(ctx, []string{"sh", "-c", "sleep 10 & echo $!; sleep 10"})
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("call outlived its deadline by %v", elapsed-200*time.Millisecond)
	}
	if err == nil {
		t.Fatal("a call killed at its deadline succeeded")
	}
	pid := helperPID(t, stdout)
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper %d survived the deadline", pid)
		}
	}
}

// ssh has exited and its answer is whole, but a helper outside its control
// still holds the pipe.
func TestRunSSHStopsWaitingForAPipeAfterSSHExits(t *testing.T) {
	start := time.Now()
	stdout, _, err := runSSH(context.Background(), []string{"sh", "-c", "sleep 10 & echo $!"})
	if elapsed := time.Since(start); elapsed > waitDelay+time.Second {
		t.Fatalf("call waited %v on a pipe held after ssh exited", elapsed)
	}
	if err != nil {
		t.Fatalf("a whole answer failed: %v", err)
	}
	helperPID(t, stdout)
}

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
		"-o", "ControlMaster=auto",
		"-o", "ControlPersist=60",
		"-o", "ControlPath=" + filepath.Join(c.controlDir, "%C"),
		"-T",
		"me@gpu",
		`exec "$SHELL" -lc 'printf %s '\''` + "\x1eagent-manager\x1e" +
			`'\''; exec '\''agent-manager'\'' '\''snapshot'\'' '\''--json'\'''`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("argv:\n got %q\nwant %q", got, want)
	}
}

// The host's review and editor keys leave their request on its tmux server
// and detach. The attach takes the request back, so the host's own manager
// never carries it out, and reports it in its exit status; any other end
// keeps tmux's own status.
func TestAttachScriptTakesBackTheHostsRequest(t *testing.T) {
	dir := t.TempDir()
	unset := filepath.Join(dir, "unset")
	fake := "#!/bin/sh\n" +
		"case \"$*\" in\n" +
		"*attach-session*) exit \"$ATTACH_STATUS\";;\n" +
		"*show-options*) printf '%s\\n' \"$REQUEST\";;\n" +
		"*set-option*) touch \"$UNSET\";;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		request, status string
		key             string
		code            int
	}{
		{"review", "0", "review", 0},
		{"editor", "0", "editor", 0},
		{"", "0", "", 0},
		{"", "1", "", 1},
	} {
		os.Remove(unset)
		cmd := exec.Command("/bin/sh", "-c", attachScript("ab12"))
		cmd.Env = append(os.Environ(), "PATH="+dir+":/usr/bin:/bin", "REQUEST="+tc.request, "ATTACH_STATUS="+tc.status, "UNSET="+unset)
		key, err := AttachKey(cmd.Run())
		code := 0
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			code = exited.ExitCode()
		}
		if key != tc.key || code != tc.code {
			t.Errorf("request %q, attach status %s: key %q, exit %d; want %q, %d", tc.request, tc.status, key, code, tc.key, tc.code)
		}
		if _, err := os.Stat(unset); (err == nil) != (tc.request != "") {
			t.Errorf("request %q: unset ran = %v", tc.request, err == nil)
		}
	}
}
