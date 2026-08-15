package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestTerminalVerbsDispatch(t *testing.T) {
	fake := &fakeTerminals{}
	out := &bytes.Buffer{}
	if err := runTerminalList(out, fake, nil, "cafe0001"); err != nil {
		t.Fatalf("terminal list: %v", err)
	}
	if !strings.Contains(out.String(), "- build (t1) in root at /repo; status=idle; running=true") {
		t.Fatalf("terminal list output = %q", out.String())
	}

	created := &bytes.Buffer{}
	if err := runTerminalCreate(created, fake, []string{"--directory", "/repo"}, "cafe0001"); err != nil {
		t.Fatalf("terminal create: %v", err)
	}
	if fake.opts.Directory != "/repo" || fake.opts.Group != nil {
		t.Fatalf("terminal create opts = %+v", fake.opts)
	}

	sent := &bytes.Buffer{}
	if err := runTerminalSend(sent, fake, []string{"t1", "--command", "go test ./..."}, "cafe0001"); err != nil {
		t.Fatalf("terminal send: %v", err)
	}
	if fake.command != "go test ./..." || sent.String() != "sent command to terminal t1\n" {
		t.Fatalf("terminal send got %q, printed %q", fake.command, sent.String())
	}

	keyed := &bytes.Buffer{}
	if err := runTerminalSend(keyed, fake, []string{"t1", "--keys", "C-c,Up"}, "cafe0001"); err != nil {
		t.Fatalf("terminal send keys: %v", err)
	}
	if len(fake.keys) != 2 || fake.keys[0] != "C-c" || keyed.String() != "sent keys to terminal t1\n" {
		t.Fatalf("terminal send keys = %v, printed %q", fake.keys, keyed.String())
	}

	read := &bytes.Buffer{}
	if err := runTerminalRead(read, fake, []string{"t1"}, "cafe0001"); err != nil {
		t.Fatalf("terminal read: %v", err)
	}
	if read.String() != "build passed\n" {
		t.Fatalf("terminal read output = %q", read.String())
	}

	if err := dispatch(&bytes.Buffer{}, "terminal", terminalVerbs(), []string{"tail"}, "cafe0001", t.TempDir()); err == nil {
		t.Fatal("an unknown terminal verb should not dispatch")
	}
}

// The contract the session commands already keep: an id the caller left out
// is answered with the usage line, never sent to the layer as an empty one.
func TestMissingTerminalIDIsAUsageError(t *testing.T) {
	for name, run := range map[string]func(*bytes.Buffer, *fakeTerminals, []string) error{
		"send": func(out *bytes.Buffer, f *fakeTerminals, args []string) error {
			return runTerminalSend(out, f, args, "cafe0001")
		},
		"read": func(out *bytes.Buffer, f *fakeTerminals, args []string) error {
			return runTerminalRead(out, f, args, "cafe0001")
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeTerminals{}
			err := run(&bytes.Buffer{}, fake, nil)
			if err == nil {
				t.Fatal("a command with no terminal id should not reach the layer")
			}
			if !strings.HasPrefix(err.Error(), "usage: agent-manager terminal "+name) {
				t.Fatalf("error = %q, want the usage line", err)
			}
			if fake.callerID != "" {
				t.Fatal("the layer was called despite the missing id")
			}
		})
	}
}
