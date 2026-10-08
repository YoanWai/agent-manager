package remote

import (
	"context"
	"reflect"
	"slices"
	"testing"

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
			_, err := c.Read(ctx, ref, false)
			return err
		}, []string{"read", "--json", "--", "a1b2c3d4"}},
		{"terminal read", func(ctx context.Context, c *Client) error {
			_, err := c.Read(ctx, ref, true)
			return err
		}, []string{"terminal", "read", "--json", "--", "a1b2c3d4"}},
		{"send", func(ctx context.Context, c *Client) error {
			_, err := c.Send(ctx, ref, text, "laptop agent")
			return err
		}, []string{"send", "--from=laptop agent", "--json", "--", "a1b2c3d4", text}},
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
			c, calls := recorder(t, `{"version":1}`)
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
	_, err := c.Read(ctx, bad, false)
	errs = append(errs, err)
	_, err = c.Send(ctx, bad, "hi", "me")
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

	c, _ := recorder(t, `{"session":{"id":"a1"},"output":"hello\n"}`)
	if got, err := c.Read(ctx, ref, false); err != nil || got != "hello\n" {
		t.Fatalf("Read = %q, %v", got, err)
	}
	c, _ = recorder(t, `{"terminal":{"id":"a1"},"output":"$ "}`)
	if got, err := c.Read(ctx, ref, true); err != nil || got != "$ " {
		t.Fatalf("terminal Read = %q, %v", got, err)
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
		name   string
		stdout string
		stderr string
		err    error
		want   string
	}{
		{"current", `{"version":1,"manager_awake":true,"sessions":[{"id":"a1","name":"fix"}],"terminals":[],"groups":[{"path":"team"}]}`, "", nil, ""},
		{"no version", `{"sessions":[]}`, "", nil, tooOld},
		{"newer", `{"version":2}`, "", nil, "gpu: agent-manager there is newer than this one; update agent-manager on this machine"},
		{"interactive manager instead", "\x1b[?1007l\x1b]10;#ffffff\x07", "agent-manager: could not open a new TTY: open /dev/tty: device not configured\n", exitStatus(1), tooOld},
		{"not json", "Usage: agent-manager", "", nil, tooOld},
		{"remote failure", "", "agent-manager: database is locked\n", exitStatus(1), "gpu: database is locked"},
		{"unreachable", "", "ssh: Could not resolve hostname gpu\n", exitStatus(255), "gpu: unreachable over SSH: ssh: Could not resolve hostname gpu"},
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
			if tc.want == "" && (!snapshot.ManagerAwake || len(snapshot.Sessions) != 1 || snapshot.Sessions[0].ID != "a1" || snapshot.Groups[0].Path != "team") {
				t.Fatalf("Snapshot = %+v", snapshot)
			}
		})
	}
}
