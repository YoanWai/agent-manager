package remote

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

// recorder answers every call with stdout and keeps the words each one ran.
func recorder(t *testing.T, stdout string) (*Client, *[][]string) {
	t.Helper()
	var calls [][]string
	c := newTestClient(t, func(_ context.Context, argv []string) ([]byte, []byte, error) {
		if argv[len(argv)-2] != "me@gpu" {
			t.Errorf("call went to %s", argv[len(argv)-2])
		}
		calls = append(calls, remoteWords(t, argv))
		return []byte(stdout), nil, nil
	})
	return c, &calls
}

func TestOperationArgv(t *testing.T) {
	text := "--json\nit's $HOME"
	group, root := "-team/a", ""
	yes, no := true, false
	ref := Ref{Host: "gpu", ID: "a1b2c3d4"}
	cases := []struct {
		name string
		run  func(context.Context, *Client) error
		want []string
	}{
		{"snapshot", func(ctx context.Context, c *Client) error {
			_, err := c.Snapshot(ctx, "gpu")
			return err
		}, []string{"snapshot", "--json"}},
		{"start manager", func(ctx context.Context, c *Client) error {
			_, err := c.StartManager(ctx, "gpu")
			return err
		}, []string{"serve", "--background"}},
		{"read", func(ctx context.Context, c *Client) error {
			_, err := c.ReadSession(ctx, ref)
			return err
		}, []string{"read", "--json", "--", "a1b2c3d4"}},
		{"terminal read", func(ctx context.Context, c *Client) error {
			_, err := c.ReadTerminal(ctx, ref)
			return err
		}, []string{"terminal", "read", "--json", "--", "a1b2c3d4"}},
		{"styled read", func(ctx context.Context, c *Client) error {
			_, err := c.ReadScreen(ctx, ref, false)
			return err
		}, []string{"read", "--styled", "--json", "--", "a1b2c3d4"}},
		{"styled terminal read", func(ctx context.Context, c *Client) error {
			_, err := c.ReadScreen(ctx, ref, true)
			return err
		}, []string{"terminal", "read", "--styled", "--json", "--", "a1b2c3d4"}},
		{"send", func(ctx context.Context, c *Client) error {
			_, err := c.Send(ctx, ref, text, "laptop agent")
			return err
		}, []string{"send", "--from=laptop agent", "--json", "--", "a1b2c3d4", text}},
		{"type", func(ctx context.Context, c *Client) error {
			_, err := c.Type(ctx, ref, text)
			return err
		}, []string{"type", "--json", "--", "a1b2c3d4", text}},
		{"spawn bare", func(ctx context.Context, c *Client) error {
			_, err := c.Spawn(ctx, "gpu", sessioncmd.CreateSessionOptions{})
			return err
		}, []string{"spawn", "--json"}},
		{"spawn full", func(ctx context.Context, c *Client) error {
			_, err := c.Spawn(ctx, "gpu", sessioncmd.CreateSessionOptions{
				Tool: "codex", Name: "fix-it", Group: &group, Directory: "/srv/my repo", Prompt: text,
				Worktree: &yes, Model: "m", Effort: "high", Profile: "work",
			})
			return err
		}, []string{"spawn", "--json", "--name=fix-it", "--prompt=" + text, "--tool=codex", "--model=m", "--effort=high",
			"--profile=work", "--directory=/srv/my repo", "--group=-team/a", "--worktree=true"}},
		{"spawn into root without worktree", func(ctx context.Context, c *Client) error {
			_, err := c.Spawn(ctx, "gpu", sessioncmd.CreateSessionOptions{Group: &root, Worktree: &no})
			return err
		}, []string{"spawn", "--json", "--group=", "--worktree=false"}},
		{"terminal create bare", func(ctx context.Context, c *Client) error {
			_, err := c.CreateTerminal(ctx, "gpu", sessioncmd.CreateTerminalOptions{})
			return err
		}, []string{"terminal", "create", "--json"}},
		{"terminal create full", func(ctx context.Context, c *Client) error {
			_, err := c.CreateTerminal(ctx, "gpu", sessioncmd.CreateTerminalOptions{Group: &root, Directory: "/srv", Nest: &no})
			return err
		}, []string{"terminal", "create", "--json", "--group=", "--directory=/srv", "--nest=false"}},
		{"terminal send command", func(ctx context.Context, c *Client) error {
			_, err := c.TerminalSend(ctx, ref, "ls -la; echo '$x'", nil)
			return err
		}, []string{"terminal", "send", "--json", "--command=ls -la; echo '$x'", "--", "a1b2c3d4"}},
		{"terminal send keys", func(ctx context.Context, c *Client) error {
			_, err := c.TerminalSend(ctx, ref, "", []string{"C-c", "Up", "Enter"})
			return err
		}, []string{"terminal", "send", "--json", "--keys=C-c", "--keys=Up", "--keys=Enter", "--", "a1b2c3d4"}},
		{"terminal close", func(ctx context.Context, c *Client) error {
			return c.TerminalClose(ctx, ref)
		}, []string{"terminal", "close", "--", "a1b2c3d4"}},
		{"kill", func(ctx context.Context, c *Client) error {
			_, err := c.Kill(ctx, ref)
			return err
		}, []string{"kill", "--json", "--", "a1b2c3d4"}},
		{"revive", func(ctx context.Context, c *Client) error {
			_, err := c.Revive(ctx, ref)
			return err
		}, []string{"revive", "--json", "--", "a1b2c3d4"}},
		{"archive", func(ctx context.Context, c *Client) error {
			_, err := c.Archive(ctx, ref, false)
			return err
		}, []string{"archive", "--json", "--", "a1b2c3d4"}},
		{"restore", func(ctx context.Context, c *Client) error {
			_, err := c.Archive(ctx, ref, true)
			return err
		}, []string{"archive", "--restore", "--json", "--", "a1b2c3d4"}},
		{"create group", func(ctx context.Context, c *Client) error {
			_, err := c.CreateGroup(ctx, "gpu", "-team/a", "/srv/team")
			return err
		}, []string{"create-group", "--json", "--directory=/srv/team", "--", "-team/a"}},
		{"create group without directory", func(ctx context.Context, c *Client) error {
			_, err := c.CreateGroup(ctx, "gpu", "team", "")
			return err
		}, []string{"create-group", "--json", "--", "team"}},
		{"delete group", func(ctx context.Context, c *Client) error {
			_, err := c.DeleteGroup(ctx, "gpu", "--")
			return err
		}, []string{"delete-group", "--json", "--", "--"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, calls := recorder(t, `{"version":1,"id":"a1","terminal_id":"a1","session":{"id":"a1"},"terminal":{"id":"a1"}}`)
			if err := tc.run(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			want := append([]string{"agent-manager"}, tc.want...)
			if len(*calls) != 1 || !slices.Equal((*calls)[0], want) {
				t.Fatalf("ran %q, want %q", *calls, want)
			}
		})
	}
}

func TestUnsafeIDsNeverReachSSH(t *testing.T) {
	c, calls := recorder(t, `{}`)
	ctx := context.Background()
	bad := Ref{Host: "gpu", ID: "a1; rm -rf ~"}
	errs := []error{}
	_, err := c.ReadSession(ctx, bad)
	errs = append(errs, err)
	_, err = c.ReadTerminal(ctx, bad)
	errs = append(errs, err)
	_, err = c.Send(ctx, bad, "hi", "me")
	errs = append(errs, err)
	_, err = c.Type(ctx, bad, "hi")
	errs = append(errs, err)
	_, err = c.TerminalSend(ctx, bad, "ls", nil)
	errs = append(errs, err)
	errs = append(errs, c.TerminalClose(ctx, bad))
	_, err = c.Kill(ctx, bad)
	errs = append(errs, err)
	_, err = c.Revive(ctx, bad)
	errs = append(errs, err)
	_, err = c.Archive(ctx, bad, true)
	errs = append(errs, err)
	for i, err := range errs {
		if err == nil {
			t.Errorf("call %d accepted an unsafe id", i)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("unsafe ids ran %q", *calls)
	}
}

func TestOperationsDecodeTheRemoteJSON(t *testing.T) {
	ctx := context.Background()
	ref := Ref{Host: "gpu", ID: "a1"}

	c, _ := recorder(t, `{"session":{"id":"a1","name":"w\u001b[31m","status":"idle","running":true},"output":"hello\n"}`)
	if got, err := c.ReadSession(ctx, ref); err != nil || got.Output != "hello\n" ||
		got.Session != (sessioncmd.Session{ID: "a1", Name: "w", Status: "idle", Running: true}) {
		t.Fatalf("ReadSession = %+v, %v", got, err)
	}
	c, _ = recorder(t, `{"terminal":{"id":"a1","name":"sh","running":true},"output":"$ "}`)
	if got, err := c.ReadTerminal(ctx, ref); err != nil || got.Output != "$ " ||
		got.Terminal != (sessioncmd.Terminal{ID: "a1", Name: "sh", Running: true}) {
		t.Fatalf("ReadTerminal = %+v, %v", got, err)
	}
	c, _ = recorder(t, `{"session":{"id":"a1; rm"},"output":""}`)
	if _, err := c.ReadSession(ctx, ref); err == nil {
		t.Fatal("ReadSession accepted a row with an unsafe id")
	}
	c, _ = recorder(t, `{"message_id":7,"queue_position":2,"manager_awake":true}`)
	if got, err := c.Send(ctx, ref, "hi", "me"); err != nil || got != (sessioncmd.SendResult{MessageID: 7, QueuePosition: 2, ManagerAwake: true}) {
		t.Fatalf("Send = %+v, %v", got, err)
	}
	c, _ = recorder(t, `{"started":true,"pid":4242}`)
	if got, err := c.StartManager(ctx, "gpu"); err != nil || !got {
		t.Fatalf("StartManager = %v, %v", got, err)
	}
	c, _ = recorder(t, `{"removed":["a","a/b"],"moved":["a1"]}`)
	want := sessioncmd.GroupRemoval{Removed: []string{"a", "a/b"}, Moved: []string{"a1"}}
	if got, err := c.DeleteGroup(ctx, "gpu", "a"); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("DeleteGroup = %+v, %v", got, err)
	}
	c, _ = recorder(t, "closed terminal a1\n")
	if err := c.TerminalClose(ctx, ref); err != nil {
		t.Fatalf("TerminalClose = %v", err)
	}
	c, _ = recorder(t, "not json")
	if _, err := c.Kill(ctx, ref); err == nil || err.Error() != "gpu: unreadable answer to kill: invalid character 'o' in literal null (expecting 'u')" {
		t.Fatalf("Kill on garbage = %v", err)
	}
}

func TestSnapshot(t *testing.T) {
	const tooOld = "gpu: agent-manager there is too old for SSH connections; update agent-manager on that host"
	cases := []struct {
		name    string
		stdout  string
		stderr  string
		err     error
		want    string
		trouble Trouble
	}{
		{"current", `{"version":1,"manager_awake":true,"sessions":[{"id":"a1","name":"fix"}],"terminals":[],"groups":[{"path":"team"}]}`, "", nil, "", TroubleFailed},
		{"no version", `{"sessions":[]}`, "", nil, tooOld, TroubleOutdated},
		{"newer", `{"version":2}`, "", nil, "gpu: agent-manager there is newer than this one; update agent-manager on this machine", TroubleNewer},
		{"interactive manager instead", "\x1b[?1007l\x1b]10;#ffffff\x07", "agent-manager: could not open a new TTY: open /dev/tty: device not configured\n", exitStatus(1), tooOld, TroubleOutdated},
		{"not json", "Usage: agent-manager", "", nil, "gpu: unreadable answer to snapshot: invalid character 'U' looking for beginning of value", TroubleFailed},
		{"remote failure", "", "agent-manager: database is locked\n", exitStatus(1), "gpu: database is locked", TroubleFailed},
		{"cut at the cap", `{"version":1,"sessions":[{"id":"a1","name":"`, "", errTooLong, "gpu: answered with more than 8 MiB", TroubleFailed},
		{"missing", "", "sh: agent-manager: not found\n", exitStatus(127), "gpu: agent-manager is not on the login shell's PATH there; install it, add it to PATH in the login profile, or set the connection's path", TroubleMissing},
		{"unreachable", "", "ssh: Could not resolve hostname gpu\n", exitStatus(255), "gpu: unreachable over SSH: ssh: Could not resolve hostname gpu", TroubleUnreachable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(context.Context, []string) ([]byte, []byte, error) {
				return []byte(tc.stdout), []byte(tc.stderr), tc.err
			})
			snapshot, err := c.Snapshot(context.Background(), "gpu")
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.want {
				t.Fatalf("Snapshot error = %q, want %q", got, tc.want)
			}
			if trouble := TroubleOf(err); err != nil && trouble != tc.trouble {
				t.Fatalf("TroubleOf = %d, want %d", trouble, tc.trouble)
			}
			if tc.want == "" && (!snapshot.ManagerAwake || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].ID != "a1" || snapshot.Groups[0].Path != "team") {
				t.Fatalf("Snapshot = %+v", snapshot)
			}
		})
	}
}

func TestLoginProfileOutputIsSkipped(t *testing.T) {
	const current = `{"version":1,"sessions":[{"id":"a1"}]}`
	cases := []struct {
		name   string
		stdout string
		stderr string
		err    error
		want   string
	}{
		{"noise before the marker", "(base) conda activated {\n" + outputMarker + current, "", nil, ""},
		{"a marker in the noise", "echo " + outputMarker + " [\n" + outputMarker + current, "", nil, ""},
		{"profile exited first", "(base) conda activated {\n", "", nil, "gpu: the login shell there exited before it ran agent-manager"},
		{"profile failed first", "nvm: now using node [", "nvm: command not found\n", exitStatus(1), "gpu: nvm: command not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newRawTestClient(t, func(context.Context, []string) ([]byte, []byte, error) {
				return []byte(tc.stdout), []byte(tc.stderr), tc.err
			})
			snapshot, err := c.Snapshot(context.Background(), "gpu")
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.want {
				t.Fatalf("Snapshot error = %q, want %q", got, tc.want)
			}
			if tc.want == "" && (len(snapshot.Sessions) != 1 || snapshot.Sessions[0].ID != "a1") {
				t.Fatalf("Snapshot = %+v", snapshot)
			}
		})
	}
}

// A command an older agent-manager does not know falls through to its
// interactive manager, which fails without a terminal after printing escape
// sequences.
func TestUnknownCommandIsTooOldNotUnreadable(t *testing.T) {
	c := newTestClient(t, func(context.Context, []string) ([]byte, []byte, error) {
		return []byte("\x1b[?1007l\x1b]10;#ffffff\x07"), []byte("agent-manager: could not open a new TTY\n"), exitStatus(1)
	})
	_, err := c.TerminalSend(context.Background(), Ref{Host: "gpu", ID: "a1"}, "ls", nil)
	if err == nil || err.Error() != "gpu: agent-manager there is too old for SSH connections; update agent-manager on that host" {
		t.Fatalf("unknown command = %v", err)
	}
	if _, err := c.Type(context.Background(), Ref{Host: "gpu", ID: "a1"}, "yes"); err == nil || err.Error() != "gpu: agent-manager there is too old for SSH connections; update agent-manager on that host" {
		t.Fatalf("type on a host that predates it = %v", err)
	}
	c = newTestClient(t, func(context.Context, []string) ([]byte, []byte, error) {
		return []byte("closed terminal a1\n"), nil, nil
	})
	_, err = c.TerminalSend(context.Background(), Ref{Host: "gpu", ID: "a1"}, "ls", nil)
	if err == nil || err.Error() != "gpu: unreadable answer to terminal: invalid character 'c' looking for beginning of value" {
		t.Fatalf("an answer that is not JSON = %v", err)
	}
}

// A type is uncertain when the host saw its paste start, or when the call
// was cut off after agent-manager started there; a refusal, or a failure
// before agent-manager ran, is not.
func TestTypeThatMayHaveReachedTheAgent(t *testing.T) {
	failing := func(code int, stderr string) Runner {
		return func(context.Context, []string) ([]byte, []byte, error) {
			return nil, []byte(stderr), exitStatus(code)
		}
	}
	ref := Ref{Host: "gpu", ID: "a1"}
	for _, tc := range []struct {
		name string
		c    *Client
		want bool
	}{
		{"the host saw the paste start", newTestClient(t, failing(3, "agent-manager: prompt may have reached api and was not sent again: boom")), true},
		{"ssh dropped after agent-manager started", newTestClient(t, failing(255, "Connection to gpu closed by remote host.")), true},
		{"the host refused it", newTestClient(t, failing(1, "agent-manager: prompt was not sent to api: boom")), false},
		{"ssh never reached agent-manager", newRawTestClient(t, failing(255, "ssh: connect to host gpu port 22: Connection refused")), false},
	} {
		_, err := tc.c.Type(context.Background(), ref, "hi")
		if err == nil || MayHaveReached(err) != tc.want {
			t.Errorf("%s: err %v, MayHaveReached %v, want %v", tc.name, err, MayHaveReached(err), tc.want)
		}
	}

	slow := newTestClient(t, func(ctx context.Context, _ []string) ([]byte, []byte, error) {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := slow.Type(ctx, ref, "hi"); !MayHaveReached(err) || !strings.Contains(err.Error(), "prompt may have reached the agent") {
		t.Errorf("a timed-out type = %v, want one that may have reached the agent", err)
	}
}

// A host built before reads took --styled refuses the flag; its screen is
// read again in plain text rather than shown as an error.
func TestReadScreenFallsBackToPlainOnAnOlderHost(t *testing.T) {
	var calls [][]string
	c := newTestClient(t, func(_ context.Context, argv []string) ([]byte, []byte, error) {
		words := remoteWords(t, argv)
		calls = append(calls, words)
		for _, word := range words {
			if word == "--styled" {
				return nil, []byte("flag provided but not defined: -styled\n"), exitStatus(2)
			}
		}
		return []byte(`{"session":{"id":"a1","name":"x"},"output":"\u001b[31mred\u001b[0m"}`), nil, nil
	})
	got, err := c.ReadScreen(context.Background(), Ref{Host: "gpu", ID: "a1"}, false)
	if err != nil || got != "red" || len(calls) != 2 {
		t.Fatalf("ReadScreen = %q, %v after %d calls", got, err, len(calls))
	}
}
