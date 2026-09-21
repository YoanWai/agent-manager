package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
)

func fixture(t *testing.T, hosts []Host) *Client {
	t.Helper()
	dir := t.TempDir()
	b, _ := json.Marshal(struct {
		Hosts []Host `json:"hosts"`
	}{hosts})
	if err := os.WriteFile(filepath.Join(dir, "fleet.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestCapturePreservesTerminalColors(t *testing.T) {
	c := fixture(t, []Host{{Name: "remote", SSH: "host", Controller: "ctl"}})
	want := "\x1b[31mred\x1b[0m\n"
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		script := cmd.Args[len(cmd.Args)-1]
		if !strings.Contains(script, "'capture-pane' '-p' '-e' '-t' 'am_target:^.0'") {
			t.Fatalf("capture must preserve ANSI colours: %s", script)
		}
		return []byte(want), nil
	}
	got, err := c.Capture(context.Background(), Ref{"remote", "target"})
	if err != nil || got != want {
		t.Fatalf("capture = %q, %v; want %q", got, err, want)
	}
}
func TestSSHQuotesEveryArgument(t *testing.T) {
	text := "hello 'there'\n$(echo BAD) `echo BAD`; --option"
	cmd := remote(context.Background(), Host{SSH: "agent@host", Controller: "abc", Binary: "printf"}, false, "%s", text)
	script := cmd.Args[len(cmd.Args)-1]
	out, err := exec.Command("sh", "-c", script).Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != text {
		t.Fatalf("argument changed: %q", out)
	}
}
func TestSnapshotNamespacesCollidingIDsAndKeepsTerminals(t *testing.T) {
	c := fixture(t, []Host{{Name: "a", SSH: "a", Controller: "ctl"}, {Name: "b", SSH: "b", Controller: "ctl"}})
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		s := cmd.Args[len(cmd.Args)-1]
		switch {
		case strings.Contains(s, "'terminal'"):
			return []byte(`[{"id":"term","name":"rails","parent_id":"same","running":true}]`), nil
		case strings.Contains(s, "'groups'"):
			return []byte(`[{"path":"empty"}]`), nil
		default:
			return []byte(`[{"id":"same","name":"agent","running":true}]`), nil
		}
	}
	snap := c.Snapshot(context.Background())
	if len(snap) != 2 || snap[0].Error != "" || snap[1].Error != "" {
		t.Fatalf("snapshot: %+v", snap)
	}
	if snap[0].Rows[0].Ref == snap[1].Rows[0].Ref {
		t.Fatal("host identity lost")
	}
	if !snap[0].Rows[1].Terminal || snap[0].Rows[1].ParentID != "same" {
		t.Fatal("terminal nesting lost")
	}
	if len(snap[0].Groups) != 1 {
		t.Fatal("empty group missing")
	}
}
func TestOfflineHostIsErrorNotDead(t *testing.T) {
	c := fixture(t, []Host{{Name: "remote", SSH: "host", Controller: "ctl"}})
	c.run = func(context.Context, *exec.Cmd) ([]byte, error) { return nil, errors.New("connection refused") }
	s := c.Snapshot(context.Background())[0]
	if s.Error == "" || s.Rows != nil {
		t.Fatalf("offline looked like sessions: %+v", s)
	}
}
func TestSendRejectsTerminalWithoutSending(t *testing.T) {
	c := fixture(t, []Host{{Name: "remote", SSH: "host", Controller: "ctl"}})
	calls := 0
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		calls++
		s := cmd.Args[len(cmd.Args)-1]
		if strings.Contains(s, "'terminal'") {
			return []byte(`[{"id":"term"}]`), nil
		}
		if strings.Contains(s, "'send'") {
			t.Fatal("typed into terminal")
		}
		return []byte(`[]`), nil
	}
	if _, err := c.Send(context.Background(), Ref{"remote", "term"}, "explain"); err == nil {
		t.Fatal("accepted terminal")
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
func TestSendRoutesThroughRemoteQueue(t *testing.T) {
	c := fixture(t, []Host{{Name: "remote", SSH: "actual-host", Controller: "ctl"}})
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		s := cmd.Args[len(cmd.Args)-1]
		if cmd.Args[len(cmd.Args)-2] != "actual-host" {
			t.Fatal(cmd.Args)
		}
		if strings.Contains(s, "'send'") {
			if !strings.Contains(s, "'AGENT_MANAGER_SESSION_ID=ctl'") || !strings.Contains(s, "'target' 'please work'") {
				t.Fatal(s)
			}
			return []byte(`{"message_id":42}`), nil
		}
		if strings.Contains(s, "'terminal'") {
			return []byte(`[]`), nil
		}
		return []byte(`[{"id":"target"}]`), nil
	}
	out, err := c.Send(context.Background(), Ref{"remote", "target"}, "please work")
	if err != nil || !strings.Contains(out, "42") {
		t.Fatalf("%s %v", out, err)
	}
}
func TestLocalSnapshotDoesNotNeedCallerOrWriteState(t *testing.T) {
	c := fixture(t, []Host{{Name: "mac"}})
	st, err := store.Open(filepath.Join(c.dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(store.Session{ID: "local-agent", Name: "local", Tool: "pi", Cwd: "/tmp", Group: "work", Status: "idle"}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	before, err := os.ReadFile(filepath.Join(c.dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	c.run = func(context.Context, *exec.Cmd) ([]byte, error) { return nil, errors.New("no server running") }
	s := c.Snapshot(context.Background())[0]
	if s.Error != "" || len(s.Rows) != 1 || s.Rows[0].Ref.Host != "mac" || s.Rows[0].Status != "dead" {
		t.Fatalf("%+v", s)
	}
	after, _ := os.ReadFile(filepath.Join(c.dir, "state.db"))
	if string(before) != string(after) {
		t.Fatal("snapshot changed database")
	}
}
func TestLoadRejectsSSHOptionsAndDuplicateHosts(t *testing.T) {
	for _, raw := range []string{`{"hosts":[{"name":"a","ssh":"-oProxyCommand=evil","controller":"ctl"}]}`, `{"hosts":[{"name":"a","ssh":"host;evil","controller":"ctl"}]}`, `{"hosts":[{"name":"a"},{"name":"a"}]}`} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "fleet.json"), []byte(raw), 0600)
		if _, err := Load(dir); err == nil {
			t.Fatal(raw)
		}
	}
}

func TestLoadRejectsAmbiguousHostNamespaces(t *testing.T) {
	for _, name := range []string{"", " ", " Mac", "Mac ", "foo/bar", "foo::bar", "foo\nbar", "foo\x00bar", "foo\tbar"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			dir := t.TempDir()
			raw, _ := json.Marshal(struct {
				Hosts []Host `json:"hosts"`
			}{[]Host{{Name: name}}})
			if err := os.WriteFile(filepath.Join(dir, "fleet.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(dir); err == nil {
				t.Fatalf("accepted ambiguous name %q", name)
			}
		})
	}
}
func TestLoadRejectsMultipleLocalHosts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fleet.json"), []byte(`{"hosts":[{"name":"Mac"},{"name":"Other"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "one local host") {
		t.Fatalf("expected local host validation, got %v", err)
	}
}
func TestStderrDoesNotContaminateJSON(t *testing.T) {
	out, err := run(context.Background(), exec.Command("sh", "-c", "printf warning >&2; printf '[]'"))
	if err != nil || string(out) != "[]" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestFocusedTextAndPasteAreNotInterpretedAsKeys(t *testing.T) {
	c := fixture(t, []Host{{Name: "remote", SSH: "host", Controller: "ctl"}})
	var commands []string
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		s := cmd.Args[len(cmd.Args)-1]
		commands = append(commands, s)
		if cmd.Stdin != nil {
			b, _ := io.ReadAll(cmd.Stdin)
			if string(b) != "first\nsecond" {
				t.Fatalf("paste changed %q", b)
			}
		}
		return nil, nil
	}
	if err := c.SendText(context.Background(), Ref{"remote", "pane"}, "Enter", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(commands[0], "'send-keys' '-l'") {
		t.Fatal(commands)
	}
	if err := c.SendText(context.Background(), Ref{"remote", "pane"}, "first\nsecond", true); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 3 || !strings.Contains(commands[2], "'paste-buffer' '-d' '-p'") {
		t.Fatal(commands)
	}
}
func TestTerminalCreateUsesSelectedAgentAsCaller(t *testing.T) {
	c := fixture(t, []Host{{Name: "remote", SSH: "host", Controller: "ctl"}})
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		s := cmd.Args[len(cmd.Args)-1]
		if !strings.Contains(s, "'AGENT_MANAGER_SESSION_ID=selected'") {
			t.Fatal(s)
		}
		return []byte(`{"id":"term","parent_id":"selected"}`), nil
	}
	v, err := c.TerminalCreateFor(context.Background(), "remote", "selected", sessioncmd.CreateTerminalOptions{})
	if err != nil || v.ParentID != "selected" {
		t.Fatalf("%+v %v", v, err)
	}
}
