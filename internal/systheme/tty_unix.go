//go:build darwin || linux

package systheme

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// queryDeadline bounds the whole OSC 11 round trip. A terminal that
// answers does so in a frame or two; tmux with no attached client simply
// never replies, and the deadline is what turns that silence into unknown.
const queryDeadline = 200 * time.Millisecond

// paletteReplyLimit holds 16 color replies at their longest plus the
// device attributes reply.
const paletteReplyLimit = 1024

// paletteReplyCeiling bounds the palette read once the host has started
// answering. tmux 3.6 and later forward an unset color to its client and
// hold the device attributes reply behind it for up to 500 ms, and a slow
// link delivers the rest after the first bytes: waiting for the closing
// reply keeps those bytes out of the TUI, which would read them as keys.
// A host that answers nothing still costs only queryDeadline.
const paletteReplyCeiling = time.Second

var deviceAttributesReply = regexp.MustCompile(`\x1b\[\?[0-9;]*c`)

// queryTerminalBg asks the terminal for its background color (OSC 11) on
// the controlling tty, in raw mode, before the TUI owns the terminal.
// Inside tmux the pane's query is answered by tmux itself from its
// client's terminal, so no passthrough is involved.
func queryTerminalBg() (r, g, b int, ok bool) {
	withRawTTY(func(fd int) {
		r, g, b, ok = queryTerminalBgFD(fd, time.Now().Add(queryDeadline))
	})
	return r, g, b, ok
}

// QueryPalette asks the terminal for its 16 standard colors (OSC 4) the
// same way, before the TUI owns the terminal. Index i holds color i as
// "#rrggbb", empty where the terminal gave no answer. A tty that cannot be
// opened answers nothing, the same as a silent host.
func QueryPalette() []string {
	palette := make([]string, paletteSize)
	withRawTTY(func(fd int) {
		queryPaletteFD(fd, time.Now().Add(queryDeadline), palette)
	})
	return palette
}

func withRawTTY(query func(fd int)) {
	fd, err := unix.Open("/dev/tty", unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return
	}
	defer unix.Close(fd)

	saved, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return
	}
	raw := *saved
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &raw); err != nil {
		return
	}
	defer unix.IoctlSetTermios(fd, ioctlWriteTermios, saved)

	query(fd)
}

func queryTerminalBgFD(fd int, deadline time.Time) (r, g, b int, ok bool) {
	if _, err := unix.Write(fd, []byte("\x1b]11;?\x1b\\")); err != nil {
		return 0, 0, 0, false
	}

	response, ok := readOSCReply(fd, deadline)
	if !ok {
		return 0, 0, 0, false
	}
	return parseOSC11(response)
}

// queryPaletteFD sends one query per color, since tmux 3.3 to 3.5 answers
// only the first color of a query that lists several. A terminal answers
// the closing device attributes request after any color it answers, so
// that reply ends the wait well before the deadline. Once any reply has
// arrived the wait for it stretches to paletteReplyCeiling, and whatever
// the tty still holds afterwards is discarded rather than handed to the
// TUI as keystrokes.
func queryPaletteFD(fd int, deadline time.Time, palette []string) {
	var query strings.Builder
	for index := range palette {
		fmt.Fprintf(&query, "\x1b]4;%d;?\x1b\\", index)
	}
	query.WriteString("\x1b[c")
	if _, err := unix.Write(fd, []byte(query.String())); err != nil {
		return
	}
	response, complete := readReply(fd, deadline, paletteReplyLimit, hasDeviceAttributes)
	if !complete && response != "" && len(response) <= paletteReplyLimit {
		rest, _ := readReply(fd, deadline.Add(paletteReplyCeiling-queryDeadline), paletteReplyLimit-len(response), func(more string) bool {
			return hasDeviceAttributes(response + more)
		})
		response += rest
	}
	discardInput(fd)
	parseOSC4(response, palette)
}

func hasDeviceAttributes(response string) bool {
	return deviceAttributesReply.MatchString(response)
}

// readOSCReply collects bytes until an OSC terminator (BEL or ST) or the
// deadline.
func readOSCReply(fd int, deadline time.Time) (string, bool) {
	response, complete := readReply(fd, deadline, 128, func(s string) bool {
		return strings.HasSuffix(s, "\a") || strings.HasSuffix(s, "\x1b\\")
	})
	if !complete {
		return "", false
	}
	return response, true
}

// readReply collects bytes until complete accepts them, the deadline, or
// more than limit bytes, and returns what arrived either way. Select gates
// every read so a silent tty costs the deadline, never a blocked read the
// TUI would inherit.
func readReply(fd int, deadline time.Time, limit int, complete func(string) bool) (string, bool) {
	var response []byte
	buf := make([]byte, 64)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return string(response), false
		}
		tv := unix.NsecToTimeval(remaining.Nanoseconds())
		var readfds unix.FdSet
		readfds.Set(fd)
		n, err := unix.Select(fd+1, &readfds, nil, nil, &tv)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n == 0 {
			return string(response), false
		}
		count, err := unix.Read(fd, buf)
		if err != nil || count == 0 {
			return string(response), false
		}
		response = append(response, buf[:count]...)
		if complete(string(response)) {
			return string(response), true
		}
		if len(response) > limit {
			return string(response), false
		}
	}
}
