package federation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCaptureStateTargetsAgentPaneAndPreservesANSI(t *testing.T) {
	c := fixture(t, []Host{{Name: "remote", SSH: "host", Controller: "ctl"}})
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		s := cmd.Args[len(cmd.Args)-1]
		if !strings.Contains(s, "'am_pane:^.0'") || !strings.Contains(s, "'capture-pane' '-p' '-e'") {
			t.Fatal(s)
		}
		return []byte("3,4,1,100,20,1,1,80,24\n\x1b[31mred\x1b[0m\n"), nil
	}
	state, err := c.CaptureState(context.Background(), Ref{"remote", "pane"}, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.Output, "\x1b[31m") || state.CursorX != 3 || state.CursorY != 4 || !state.CursorVisible || !state.Mouse || !state.Motion || !state.SGR || state.History != 20 || state.Height != 24 || state.Width != 80 {
		t.Fatalf("%+v", state)
	}
}
func TestCaptureStateScrollIsAnchoredToBottom(t *testing.T) {
	c := fixture(t, []Host{{Name: "remote", SSH: "host", Controller: "ctl"}})
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		s := cmd.Args[len(cmd.Args)-1]
		if !strings.Contains(s, "'-S' '-3' '-E' '-'") {
			t.Fatal(s)
		}
		return []byte("0,0,1,000,10,0,0,80,24\none\ntwo\nthree\nfour\nfive\n"), nil
	}
	state, err := c.CaptureState(context.Background(), Ref{"remote", "pane"}, 1, 2)
	if err != nil || state.Output != "three\nfour\n" || state.CursorVisible {
		t.Fatalf("%+v %v", state, err)
	}
}
func TestResizePreservesTeammateGeometry(t *testing.T) {
	c := fixture(t, []Host{{Name: "remote", SSH: "host", Controller: "ctl"}})
	calls := 0
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		calls++
		s := cmd.Args[len(cmd.Args)-1]
		if calls == 1 {
			return []byte("2 100 50 60 50"), nil
		}
		for _, want := range []string{"'resize-window' '-t' 'am_pane:^' '-x' '120' '-y' '30'", "'resize-pane' '-t' 'am_pane:^.0' '-x' '80' '-y' '30'"} {
			if !strings.Contains(s, want) {
				t.Fatal(s)
			}
		}
		return nil, nil
	}
	if err := c.Resize(context.Background(), Ref{"remote", "pane"}, 80, 30); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
func TestMouseDeliveryRechecksTrackingOnTargetPane(t *testing.T) {
	c := fixture(t, []Host{{Name: "remote", SSH: "host", Controller: "ctl"}})
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		s := cmd.Args[len(cmd.Args)-1]
		for _, want := range []string{"'if-shell' '-F' '-t' 'am_pane:^.0'", "'#{mouse_any_flag}'", "send-keys -t am_pane:^.0 -H 1b 5b"} {
			if !strings.Contains(s, want) {
				t.Fatal(s)
			}
		}
		return nil, nil
	}
	if err := c.SendMouse(context.Background(), Ref{"remote", "pane"}, "\x1b[<64;1;1M"); err != nil {
		t.Fatal(err)
	}
}
func TestFailedPasteCleansClipboardBufferAfterContextCancellation(t *testing.T) {
	c := fixture(t, []Host{{Name: "remote", SSH: "host", Controller: "ctl"}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	c.run = func(callCtx context.Context, cmd *exec.Cmd) ([]byte, error) {
		calls++
		s := cmd.Args[len(cmd.Args)-1]
		switch calls {
		case 1:
			return nil, nil
		case 2:
			cancel()
			return nil, errors.New("paste failed")
		case 3:
			if callCtx.Err() != nil || !strings.Contains(s, "'delete-buffer'") {
				t.Fatalf("cleanup context %v command %s", callCtx.Err(), s)
			}
			return nil, nil
		}
		t.Fatal("unexpected call")
		return nil, nil
	}
	if err := c.SendText(ctx, Ref{"remote", "pane"}, "secret paste", true); err == nil {
		t.Fatal("paste failure swallowed")
	}
	if calls != 3 {
		t.Fatal(calls)
	}
}
func TestSSHDoesNotInheritCredentialOrPortForwarding(t *testing.T) {
	cmd := remote(context.Background(), Host{SSH: "host", Controller: "ctl", Binary: "agent-manager"}, false, "sessions")
	args := strings.Join(cmd.Args, " ")
	for _, want := range []string{"ForwardAgent=no", "ClearAllForwardings=yes"} {
		if !strings.Contains(args, want) {
			t.Fatal(args)
		}
	}
}

func TestPaneStateAndResizeAgainstIsolatedTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	dir, err := os.MkdirTemp("/tmp", "fleet-pane-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "")
	tmuxRun := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("tmux", append([]string{"-L", "agentmgr"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("tmux %v: %s %v", args, out, err)
		}
	}
	tmuxRun("new-session", "-d", "-s", "am_pane", "-x", "80", "-y", "24", "sleep 30")
	defer exec.Command("tmux", "-L", "agentmgr", "kill-server").Run()
	tmuxRun("split-window", "-h", "-t", "am_pane", "sleep 30")
	c := fixture(t, []Host{{Name: "remote", SSH: "host", Controller: "ctl"}})
	c.run = func(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
		local := exec.CommandContext(ctx, "sh", "-c", cmd.Args[len(cmd.Args)-1])
		local.Stdin = cmd.Stdin
		return run(ctx, local)
	}
	if err = c.Resize(context.Background(), Ref{"remote", "pane"}, 60, 20); err != nil {
		t.Fatal(err)
	}
	state, err := c.CaptureState(context.Background(), Ref{"remote", "pane"}, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if state.Width != 60 || state.Height != 20 {
		t.Fatalf("agent pane geometry: %+v", state)
	}
	if err = c.SendMouse(context.Background(), Ref{"remote", "pane"}, "\x1b[<64;1;1M"); err != nil {
		t.Fatal(err)
	}
}
