//go:build darwin || linux

package systheme

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func closeTestFile(t *testing.T, name string, file *os.File) {
	t.Helper()
	if err := file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Errorf("close %s: %v", name, err)
	}
}

func TestParseOSC11(t *testing.T) {
	tests := []struct {
		name     string
		response string
		r, g, b  int
		ok       bool
	}{
		{"xterm 16-bit, ST", "\x1b]11;rgb:0f0f/1111/1515\x1b\\", 15, 17, 21, true},
		{"8-bit, BEL", "\x1b]11;rgb:fd/f6/e3\a", 253, 246, 227, true},
		{"4-bit channels", "\x1b]11;rgb:f/f/f\a", 255, 255, 255, true},
		{"no color spec", "\x1b]11;?\a", 0, 0, 0, false},
		{"wrong channel count", "\x1b]11;rgb:aa/bb\a", 0, 0, 0, false},
		{"invalid hex", "\x1b]11;rgb:gg/00/00\a", 0, 0, 0, false},
		{"channel wider than 16 bits", "\x1b]11;rgb:00000/0/0\a", 0, 0, 0, false},
		{"garbage", "hello", 0, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, g, b, ok := parseOSC11(tt.response)
			if r != tt.r || g != tt.g || b != tt.b || ok != tt.ok {
				t.Errorf("parseOSC11(%q) = %d,%d,%d,%v want %d,%d,%d,%v",
					tt.response, r, g, b, ok, tt.r, tt.g, tt.b, tt.ok)
			}
		})
	}
}

func TestReadOSCReply(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
		ok      bool
	}{
		{"BEL terminator", strings.Repeat("x", 70) + "\a", strings.Repeat("x", 70) + "\a", true},
		{"ST terminator", "\x1b]11;rgb:00/00/00\x1b\\", "\x1b]11;rgb:00/00/00\x1b\\", true},
		{"end of stream", "", "", false},
		{"reply too long", strings.Repeat("x", 192) + "\a", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { closeTestFile(t, "reply reader", reader) })
			t.Cleanup(func() { closeTestFile(t, "reply writer", writer) })
			if _, err := writer.WriteString(tt.payload); err != nil {
				t.Fatalf("write reply: %v", err)
			}
			if err := writer.Close(); err != nil {
				t.Fatalf("close reply writer: %v", err)
			}

			got, ok := readOSCReply(int(reader.Fd()), time.Now().Add(time.Second))
			if got != tt.want || ok != tt.ok {
				t.Errorf("readOSCReply() = %q, %v, want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestReadOSCReplyHonorsExpiredDeadline(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestFile(t, "reply reader", reader) })
	t.Cleanup(func() { closeTestFile(t, "reply writer", writer) })

	if got, ok := readOSCReply(int(reader.Fd()), time.Now().Add(-time.Second)); got != "" || ok {
		t.Errorf("readOSCReply() = %q, %v, want empty response", got, ok)
	}
}

func TestReadOSCReplyHonorsTimeout(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestFile(t, "reply reader", reader) })
	t.Cleanup(func() { closeTestFile(t, "reply writer", writer) })

	if got, ok := readOSCReply(int(reader.Fd()), time.Now().Add(10*time.Millisecond)); got != "" || ok {
		t.Errorf("readOSCReply() = %q, %v, want empty response", got, ok)
	}
}

func TestQueryTerminalBgFD(t *testing.T) {
	tests := []struct {
		name     string
		response string
		r, g, b  int
		ok       bool
	}{
		{"color reply", "\x1b]11;rgb:0f/11/15\a", 15, 17, 21, true},
		{"malformed reply", "\x1b]11;?\a", 0, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
			if err != nil {
				t.Fatal(err)
			}
			query := os.NewFile(uintptr(fds[0]), "query")
			terminal := os.NewFile(uintptr(fds[1]), "terminal")
			t.Cleanup(func() { closeTestFile(t, "query socket", query) })
			t.Cleanup(func() { closeTestFile(t, "terminal socket", terminal) })

			exchange := make(chan error, 1)
			go func() {
				const want = "\x1b]11;?\x1b\\"
				buf := make([]byte, len(want))
				if _, err := io.ReadFull(terminal, buf); err != nil {
					exchange <- err
					return
				}
				if got := string(buf); got != want {
					exchange <- fmt.Errorf("query = %q, want %q", got, want)
					return
				}
				if _, err := io.WriteString(terminal, tt.response); err != nil {
					exchange <- err
					return
				}
				exchange <- nil
			}()

			r, g, b, ok := queryTerminalBgFD(int(query.Fd()), time.Now().Add(time.Second))
			if err := <-exchange; err != nil {
				t.Fatal(err)
			}
			if r != tt.r || g != tt.g || b != tt.b || ok != tt.ok {
				t.Errorf("queryTerminalBgFD() = %d,%d,%d,%v, want %d,%d,%d,%v",
					r, g, b, ok, tt.r, tt.g, tt.b, tt.ok)
			}
		})
	}
}

func TestQueryTerminalBgFDRejectsInvalidDescriptor(t *testing.T) {
	if r, g, b, ok := queryTerminalBgFD(-1, time.Now().Add(time.Second)); r != 0 || g != 0 || b != 0 || ok {
		t.Errorf("queryTerminalBgFD() = %d,%d,%d,%v, want zero values", r, g, b, ok)
	}
}

func TestParseOSC4(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     map[int]string
	}{
		{"16-bit, ST", "\x1b]4;1;rgb:cdcd/0000/0000\x1b\\", map[int]string{1: "#cd0000"}},
		{"8-bit, BEL", "\x1b]4;4;rgb:45/85/88\a", map[int]string{4: "#458588"}},
		{"4-bit channels", "\x1b]4;15;rgb:f/f/f\a", map[int]string{15: "#ffffff"}},
		{
			"several replies and the device attributes",
			"\x1b]4;0;rgb:0000/0000/0000\x1b\\\x1b]4;9;rgb:fbfb/4949/3434\a\x1b[?62;22c",
			map[int]string{0: "#000000", 9: "#fb4934"},
		},
		{"reply cut off by the deadline", "\x1b]4;2;rgb:98/97/1a\a\x1b]4;3;rgb:d7/99", map[int]string{2: "#98971a"}},
		{"index outside the 16", "\x1b]4;16;rgb:ff/ff/ff\a", map[int]string{}},
		{"unanswered query echoed", "\x1b]4;5;?\a", map[int]string{}},
		{"malformed color", "\x1b]4;6;rgb:zz/00/00\a", map[int]string{}},
		{"only the device attributes", "\x1b[?1;2c", map[int]string{}},
		{"nothing", "", map[int]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			palette := make([]string, paletteSize)
			parseOSC4(tt.response, palette)
			for index, color := range palette {
				if color != tt.want[index] {
					t.Errorf("color %d = %q, want %q", index, color, tt.want[index])
				}
			}
		})
	}
}

// terminalPair is a socket standing in for the controlling tty and the
// terminal on its other end.
func terminalPair(t *testing.T) (query, terminal *os.File) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	query = os.NewFile(uintptr(fds[0]), "query")
	terminal = os.NewFile(uintptr(fds[1]), "terminal")
	t.Cleanup(func() { closeTestFile(t, "query socket", query) })
	t.Cleanup(func() { closeTestFile(t, "terminal socket", terminal) })
	return query, terminal
}

// answerPaletteQuery reads the whole palette query off the terminal end and
// writes answer back.
func answerPaletteQuery(terminal *os.File, answer string) <-chan error {
	exchange := make(chan error, 1)
	go func() {
		var want strings.Builder
		for index := 0; index < paletteSize; index++ {
			fmt.Fprintf(&want, "\x1b]4;%d;?\x1b\\", index)
		}
		want.WriteString("\x1b[c")
		buf := make([]byte, want.Len())
		if _, err := io.ReadFull(terminal, buf); err != nil {
			exchange <- err
			return
		}
		if got := string(buf); got != want.String() {
			exchange <- fmt.Errorf("query = %q, want %q", got, want.String())
			return
		}
		_, err := io.WriteString(terminal, answer)
		exchange <- err
	}()
	return exchange
}

func TestQueryPaletteFD(t *testing.T) {
	tests := []struct {
		name    string
		answer  string
		want    map[int]string
		quickly bool
	}{
		{
			"answered colors end at the device attributes",
			"\x1b]4;1;rgb:cccc/2424/1d1d\x1b\\\x1b]4;4;rgb:4545/8585/8888\x1b\\\x1b[?62c",
			map[int]string{1: "#cc241d", 4: "#458588"},
			true,
		},
		{"a host without OSC 4 answers only the device attributes", "\x1b[?1;2c", map[int]string{}, true},
		{"a silent host costs the deadline", "", map[int]string{}, false},
		{"colors that arrive before the deadline are kept", "\x1b]4;7;rgb:a8/99/84\a", map[int]string{7: "#a89984"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query, terminal := terminalPair(t)
			exchange := answerPaletteQuery(terminal, tt.answer)

			const deadline = 300 * time.Millisecond
			palette := make([]string, paletteSize)
			started := time.Now()
			queryPaletteFD(int(query.Fd()), started.Add(deadline), palette)
			elapsed := time.Since(started)
			if err := <-exchange; err != nil {
				t.Fatal(err)
			}
			for index, color := range palette {
				if color != tt.want[index] {
					t.Errorf("color %d = %q, want %q", index, color, tt.want[index])
				}
			}
			if tt.quickly && elapsed >= deadline {
				t.Errorf("query took %v, want it to end at the device attributes reply", elapsed)
			}
		})
	}
}

// A host that starts answering before the deadline and finishes after it,
// as tmux 3.6 does while it holds the device attributes reply behind a
// forwarded color, or a slow link does, is read to the end: the late colors
// are kept and no reply is left on the tty for the TUI to take as keys.
func TestQueryPaletteFDWaitsForAHostThatStartedAnswering(t *testing.T) {
	query, terminal := terminalPair(t)
	const deadline = 100 * time.Millisecond
	exchange := answerPaletteQuery(terminal, "\x1b]4;1;rgb:cccc/2424/1d1d\x1b\\")
	late := make(chan error, 1)
	go func() {
		if err := <-exchange; err != nil {
			late <- err
			return
		}
		time.Sleep(3 * deadline)
		_, err := io.WriteString(terminal, "\x1b]4;9;rgb:fbfb/4949/3434\x1b\\\x1b[?62c")
		late <- err
	}()

	palette := make([]string, paletteSize)
	queryPaletteFD(int(query.Fd()), time.Now().Add(deadline), palette)
	if err := <-late; err != nil {
		t.Fatal(err)
	}
	if palette[1] != "#cc241d" || palette[9] != "#fb4934" {
		t.Errorf("palette[1], palette[9] = %q, %q, want the early and the late answer", palette[1], palette[9])
	}
	if leftover, _ := readReply(int(query.Fd()), time.Now().Add(50*time.Millisecond), paletteReplyLimit, func(string) bool { return false }); leftover != "" {
		t.Errorf("left %q on the tty for the TUI", leftover)
	}
}

func TestQueryPaletteFDRejectsInvalidDescriptor(t *testing.T) {
	palette := make([]string, paletteSize)
	queryPaletteFD(-1, time.Now().Add(time.Second), palette)
	for index, color := range palette {
		if color != "" {
			t.Errorf("color %d = %q, want it unanswered", index, color)
		}
	}
}
