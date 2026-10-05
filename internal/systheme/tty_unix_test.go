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
