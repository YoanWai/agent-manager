package remote

import (
	"context"
	"reflect"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

// hostile carries what a compromised host would put in a string to drive
// the user's terminal: CSI, OSC, an 8-bit CSI as UTF-8, a carriage return,
// a backspace and a charset shift.
const hostile = `x\u001b[2J\u001b]0;pwn\u0007\u009b\r\b\u000ey`

func TestCleanText(t *testing.T) {
	cases := map[string]string{
		"plain":                                  "plain",
		"héllo ✓ 日本語":                            "héllo ✓ 日本語",
		"a\x1b[31mred\x1b[0m":                    "ared",
		"a\x1b]0;title\x07b":                     "ab",
		"a\x1b]8;;http://x\x1b\\l\x1b]8;;\x1b\\": "al",
		"a\x1bPq#0\x1b\\b":                       "ab",
		"a\x9b31mb":                              "ab",
		"a\u009b31mb":                            "a31mb",
		"a\r\b\x0e\x0f\x00\x7fb":                 "ab",
		"line\nnext\ttab":                        "linenexttab",
		"bad\xffutf8":                            "badutf8",
	}
	for in, want := range cases {
		if got := cleanText(in); got != want {
			t.Errorf("cleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanScreenKeepsLinesAndTabs(t *testing.T) {
	in := "$ ls\r\n\x1b[1;34mdir\x1b[0m\tfile\x1b]0;pwn\x07\n\x08\x0e\u009b2J\x1b[?1049hend\n"
	want := "$ ls\ndir\tfile\n2Jend\n"
	if got := cleanScreen(in); got != want {
		t.Fatalf("cleanScreen = %q, want %q", got, want)
	}
}

func TestSnapshotIsCleaned(t *testing.T) {
	c, _ := recorder(t, `{"version":1,"sessions":[
		{"id":"a1","name":"`+hostile+`","tool":"`+hostile+`","group":"`+hostile+`","directory":"`+hostile+`",
		 "status":"`+hostile+`","branch":"`+hostile+`","pending_input_outcome":"`+hostile+`"},
		{"id":"a1; rm -rf ~","name":"evil"},
		{"id":"","name":"blank"}],
	"terminals":[
		{"id":"t1","name":"`+hostile+`","group":"`+hostile+`","directory":"`+hostile+`","status":"`+hostile+`",
		 "parent_id":"a1","parent_name":"`+hostile+`"},
		{"id":"t2"},
		{"id":"t3","parent_id":"$(whoami)"},
		{"id":"t4\n"}],
	"groups":[{"path":"`+hostile+`","directory":"`+hostile+`","worktree":"`+hostile+`"}]}`)
	got, err := c.Snapshot(context.Background(), "gpu")
	if err != nil {
		t.Fatal(err)
	}
	const clean = "xy"
	want := sessioncmd.Snapshot{
		Version: 1,
		Sessions: []sessioncmd.Session{{ID: "a1", Name: clean, Tool: clean, Group: clean, Directory: clean,
			Status: clean, Branch: clean, PendingInputOutcome: clean}},
		Terminals: []sessioncmd.Terminal{
			{ID: "t1", Name: clean, Group: clean, Directory: clean, Status: clean, ParentID: "a1", ParentName: clean},
			{ID: "t2"},
		},
		Groups: []sessioncmd.Group{{Path: clean, Directory: clean, Worktree: clean}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot =\n%+v\nwant\n%+v", got, want)
	}
}

func TestResultsAreCleaned(t *testing.T) {
	ctx := context.Background()
	ref := Ref{Host: "gpu", ID: "a1"}
	session := `{"id":"s1","name":"` + hostile + `","status":"` + hostile + `"}`
	wantSession := sessioncmd.Session{ID: "s1", Name: "xy", Status: "xy"}
	lifecycles := map[string]func(c *Client) (sessioncmd.Session, error){
		"spawn": func(c *Client) (sessioncmd.Session, error) {
			return c.Spawn(ctx, "gpu", sessioncmd.CreateSessionOptions{})
		},
		"kill":    func(c *Client) (sessioncmd.Session, error) { return c.Kill(ctx, ref) },
		"revive":  func(c *Client) (sessioncmd.Session, error) { return c.Revive(ctx, ref) },
		"archive": func(c *Client) (sessioncmd.Session, error) { return c.Archive(ctx, ref, false) },
	}
	for name, run := range lifecycles {
		c, _ := recorder(t, session)
		if got, err := run(c); err != nil || got != wantSession {
			t.Errorf("%s = %+v, %v", name, got, err)
		}
		c, _ = recorder(t, `{"id":"s1;reboot"}`)
		if got, err := run(c); err == nil {
			t.Errorf("%s accepted an unsafe id: %+v", name, got)
		}
	}

	c, _ := recorder(t, `{"id":"t1","name":"`+hostile+`","parent_id":"a1","parent_name":"`+hostile+`"}`)
	if got, err := c.CreateTerminal(ctx, "gpu", sessioncmd.CreateTerminalOptions{}); err != nil ||
		got != (sessioncmd.Terminal{ID: "t1", Name: "xy", ParentID: "a1", ParentName: "xy"}) {
		t.Errorf("CreateTerminal = %+v, %v", got, err)
	}
	c, _ = recorder(t, `{"id":"t1","parent_id":"a 1"}`)
	if got, err := c.CreateTerminal(ctx, "gpu", sessioncmd.CreateTerminalOptions{}); err == nil {
		t.Errorf("CreateTerminal accepted an unsafe parent id: %+v", got)
	}

	c, _ = recorder(t, `{"path":"`+hostile+`","directory":"`+hostile+`"}`)
	if got, err := c.CreateGroup(ctx, "gpu", "team", ""); err != nil || got != (sessioncmd.Group{Path: "xy", Directory: "xy"}) {
		t.Errorf("CreateGroup = %+v, %v", got, err)
	}

	c, _ = recorder(t, `{"removed":["`+hostile+`"],"moved":["a1","a1;x","b2"]}`)
	if got, err := c.DeleteGroup(ctx, "gpu", "team"); err != nil ||
		!reflect.DeepEqual(got, sessioncmd.GroupRemoval{Removed: []string{"xy"}, Moved: []string{"a1", "b2"}}) {
		t.Errorf("DeleteGroup = %+v, %v", got, err)
	}

	c, _ = recorder(t, `{"terminal_id":"a1","sent":"`+hostile+`"}`)
	if got, err := c.TerminalSend(ctx, ref, "ls", nil); err != nil || got != (sessioncmd.TerminalInput{TerminalID: "a1", Sent: "xy"}) {
		t.Errorf("TerminalSend = %+v, %v", got, err)
	}
	c, _ = recorder(t, `{"terminal_id":"a1\u001b","sent":"command"}`)
	if got, err := c.TerminalSend(ctx, ref, "ls", nil); err == nil {
		t.Errorf("TerminalSend accepted an unsafe id: %+v", got)
	}

	for _, terminal := range []bool{false, true} {
		c, _ = recorder(t, `{"output":"$ ls\r\n\u001b[31mred\u001b[0m\tx\b\u000e\n"}`)
		if got, err := c.Read(ctx, ref, terminal); err != nil || got != "$ ls\nred\tx\n" {
			t.Errorf("Read(terminal %v) = %q, %v", terminal, got, err)
		}
	}
}

func TestRemoteErrorTextIsCleaned(t *testing.T) {
	c := newTestClient(t, func(context.Context, []string) ([]byte, []byte, error) {
		return nil, []byte("agent-manager: no session \x1b]0;pwn\x07named \x1b[31mx\r\x9b2J\n"), exitStatus(1)
	})
	_, err := c.Kill(context.Background(), Ref{Host: "gpu", ID: "a1"})
	if err == nil || err.Error() != "gpu: no session named x" {
		t.Fatalf("error = %q", err)
	}
	c = newTestClient(t, func(context.Context, []string) ([]byte, []byte, error) {
		return nil, []byte("ssh: \x1b[2Jconnect refused\n"), exitStatus(255)
	})
	_, err = c.Kill(context.Background(), Ref{Host: "gpu", ID: "a1"})
	if err == nil || err.Error() != "gpu: unreachable over SSH: ssh: connect refused" {
		t.Fatalf("error = %q", err)
	}
	if got := (&Error{Host: "g\x1b]0;x\x07pu", Err: context.Canceled}).Error(); got != "gpu: context canceled" {
		t.Fatalf("Error() = %q", got)
	}
}
