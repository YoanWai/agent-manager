package tmux

import (
	"strings"
	"testing"
)

func TestHexBytesSpellsEveryReportByte(t *testing.T) {
	if got, want := hexBytes("\x1b[<64;1;1M"), "1b 5b 3c 36 34 3b 31 3b 31 4d"; got != want {
		t.Errorf("hexBytes = %q, want %q", got, want)
	}
}

func TestSendKeysBuilders(t *testing.T) {
	cases := []struct{ name, got, want string }{
		{"hex", SendKeysHex("am_x", []byte("hi")), "send-keys -t am_x -H 68 69"},
		{"utf8", SendKeysHex("am_x", []byte("ש")), "send-keys -t am_x -H d7 a9"},
		{"alt", SendKeysHex("am_x", []byte("\x1bx")), "send-keys -t am_x -H 1b 78"},
		{"name", SendKeyName("am_x", "M-Up"), "send-keys -t am_x M-Up"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestGuardedMouseReportWrapsTheSend(t *testing.T) {
	command, args := GuardedMouseReport(PaneTarget("abc"), "\x1b[<64;3;2M")
	if !strings.HasPrefix(command, "if-shell -F -t "+PaneTarget("abc")+" '#{mouse_any_flag}' 'send-keys") {
		t.Fatalf("control-pipe command = %q", command)
	}
	if len(args) != 7 || args[0] != "if-shell" || args[4] != "#{mouse_any_flag}" {
		t.Fatalf("fallback args = %q", args)
	}
	// The nested send has to stay one argument, which is why it cannot ride
	// the whitespace-splitting SendRaw.
	if !strings.HasPrefix(args[5], "send-keys -t ") || !strings.Contains(args[5], " -H ") {
		t.Fatalf("nested command = %q", args[5])
	}
}
