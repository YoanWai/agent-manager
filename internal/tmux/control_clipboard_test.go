package tmux

import (
	"encoding/base64"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func osc52(text, terminator string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + terminator
}

func TestClipboardScannerFindsWrites(t *testing.T) {
	for _, tt := range []struct {
		name  string
		lines []string
		want  []string
	}{
		{"BEL", []string{"before" + osc52("hello", "\a") + "after"}, []string{"hello"}},
		{"ST", []string{osc52("hello", "\x1b\\")}, []string{"hello"}},
		{"two in one line", []string{osc52("one", "\a") + osc52("two", "\x1b\\")}, []string{"one", "two"}},
		{"payload split across lines", []string{"\x1b]52;c;aGVs", "bG8=\a"}, []string{"hello"}},
		{"introducer split across lines", []string{"text\x1b]5", "2;c;aGVsbG8=\a"}, []string{"hello"}},
		{"ST split across lines", []string{"\x1b]52;c;aGVsbG8=\x1b", "\\"}, []string{"hello"}},
		{"unpadded base64", []string{"\x1b]52;c;aGVsbG8\a"}, nil},
		{"canceled by CSI", []string{"\x1b]52;c;aGVsbG8=\x1b[0m"}, nil},
		{"canceled by CAN", []string{"\x1b]52;c;aGVsbG8=\x18\a"}, nil},
		{"complete oversized write", []string{osc52(strings.Repeat("A", clipboardSequenceLimit), "\a")}, nil},
		{"primary selection", []string{"\x1b]52;p;aGVsbG8=\a"}, []string{"hello"}},
		{"a query reads the clipboard", []string{"\x1b]52;c;?\a"}, nil},
		{"an empty payload sets nothing", []string{"\x1b]52;c;\a"}, nil},
		{"invalid base64", []string{"\x1b]52;c;not base64!\a"}, nil},
		{"other OSC sequences", []string{"\x1b]0;title\a\x1b]8;;https://example.com\a"}, nil},
		{"unterminated", []string{"\x1b]52;c;aGVsbG8="}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var scanner clipboardScanner
			var got []string
			for _, line := range tt.lines {
				got = append(got, scanner.feed("%1", line)...)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("copies = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClipboardScannerKeepsPanesApart(t *testing.T) {
	var scanner clipboardScanner
	if got := scanner.feed("%1", "\x1b]52;c;aGVs"); got != nil {
		t.Fatalf("first half = %q", got)
	}
	if got := scanner.feed("%2", "bG8=\a"); got != nil {
		t.Fatalf("another pane completed the write: %q", got)
	}
	if got := scanner.feed("%1", "bG8=\a"); !slices.Equal(got, []string{"hello"}) {
		t.Fatalf("second half = %q, want hello", got)
	}
}

func TestClipboardScannerDropsAWritePastTheLimit(t *testing.T) {
	var scanner clipboardScanner
	chunk := strings.Repeat("A", 64*1024)
	scanner.feed("%1", "\x1b]52;c;")
	for range clipboardSequenceLimit/len(chunk) + 1 {
		scanner.feed("%1", chunk)
	}
	if got := scanner.feed("%1", "\a"+osc52("after", "\a")); !slices.Equal(got, []string{"after"}) {
		t.Fatalf("copies = %d, want only the write after the oversized one", len(got))
	}
	if len(scanner.pending["%1"]) != 0 {
		t.Fatalf("pending kept %d bytes", len(scanner.pending["%1"]))
	}
}

func TestUnescapeOutput(t *testing.T) {
	for _, tt := range []struct{ escaped, want string }{
		{`plain`, "plain"},
		{`\033]52;c;aGk=\007`, "\x1b]52;c;aGk=\a"},
		{`back\134slash`, `back\slash`},
		{`\015\012`, "\r\n"},
		{`trailing\03`, `trailing\03`},
	} {
		if got := unescapeOutput(tt.escaped); got != tt.want {
			t.Errorf("unescapeOutput(%q) = %q, want %q", tt.escaped, got, tt.want)
		}
	}
}

func TestControlClipboardKeepsTheLatestWrite(t *testing.T) {
	server := newFakeServer()
	server.control.WatchClipboard("%1")
	server.send("%begin 1 0 0", "%end 1 0 0")
	replied := make(chan error, 1)
	go func() {
		_, err := server.control.Command("display-message -p x")
		replied <- err
	}()
	waitWritten(t, server, "display-message")
	// The command reply synchronizes consumption of the preceding output.
	server.send(`%output %1 \033]52;c;b2xk\007`,
		`%output %1 \033]52;c;aGVsbG8=\033\134`,
		"%begin 1 1 0", "x", "%end 1 1 0")
	if err := <-replied; err != nil {
		t.Fatalf("Command: %v", err)
	}
	waitClipboard(t, server.control, "hello")
	select {
	case text := <-server.control.WatchClipboard("%1"):
		t.Fatalf("an older write was kept as well: %q", text)
	default:
	}
	select {
	case <-server.control.Events():
	default:
		t.Fatal("the output carrying the write signalled no change")
	}
}

func waitClipboard(t *testing.T, control *Control, want string) {
	t.Helper()
	select {
	case got := <-control.clipboard:
		if got.Text != want || got.PaneID == "" {
			t.Fatalf("clipboard = %+v, want %q with a pane ID", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no clipboard write, want %q", want)
	}
}

func TestControlLiveClipboard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the manager does not use psmux control mode")
	}
	driver := requireTmux(t)
	id := "clip" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	control, err := driver.OpenControl(id)
	if err != nil {
		t.Fatalf("OpenControl: %v", err)
	}
	t.Cleanup(func() { control.Close() })

	paneID, err := control.Command(`display-message -p -t ` + PaneTarget(id) + ` "#{pane_id}"`)
	if err != nil {
		t.Fatalf("pane ID: %v", err)
	}
	control.WatchClipboard(strings.TrimSpace(paneID))
	if err := driver.SendText(id, `printf '\033]52;c;%s\a' "$(printf hello | base64)"`); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	waitClipboard(t, control, "hello")
}
