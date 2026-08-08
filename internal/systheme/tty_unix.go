//go:build darwin || linux

package systheme

import (
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// queryDeadline bounds the whole OSC 11 round trip. A terminal that
// answers does so in a frame or two; tmux with no attached client simply
// never replies, and the deadline is what turns that silence into unknown.
const queryDeadline = 200 * time.Millisecond

// queryTerminalBg asks the terminal for its background color (OSC 11) on
// the controlling tty, in raw mode, before the TUI owns the terminal.
// Inside tmux the pane's query is answered by tmux itself from its
// client's terminal, so no passthrough is involved.
func queryTerminalBg() (r, g, b int, ok bool) {
	fd, err := unix.Open("/dev/tty", unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return 0, 0, 0, false
	}
	defer unix.Close(fd)

	saved, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return 0, 0, 0, false
	}
	raw := *saved
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &raw); err != nil {
		return 0, 0, 0, false
	}
	defer unix.IoctlSetTermios(fd, ioctlWriteTermios, saved)

	if _, err := unix.Write(fd, []byte("\x1b]11;?\x1b\\")); err != nil {
		return 0, 0, 0, false
	}

	response, ok := readOSCReply(fd, time.Now().Add(queryDeadline))
	if !ok {
		return 0, 0, 0, false
	}
	return parseOSC11(response)
}

// readOSCReply collects bytes until an OSC terminator (BEL or ST) or the
// deadline. Select gates every read so a silent tty costs the deadline,
// never a blocked read the TUI would inherit.
func readOSCReply(fd int, deadline time.Time) (string, bool) {
	var response []byte
	buf := make([]byte, 64)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return "", false
		}
		tv := unix.NsecToTimeval(remaining.Nanoseconds())
		var readfds unix.FdSet
		readfds.Set(fd)
		n, err := unix.Select(fd+1, &readfds, nil, nil, &tv)
		if err == unix.EINTR {
			continue
		}
		if err != nil || n == 0 {
			return "", false
		}
		count, err := unix.Read(fd, buf)
		if err != nil || count == 0 {
			return "", false
		}
		response = append(response, buf[:count]...)
		s := string(response)
		if strings.HasSuffix(s, "\a") || strings.HasSuffix(s, "\x1b\\") {
			return s, true
		}
		if len(response) > 128 {
			return "", false
		}
	}
}

// parseOSC11 extracts the color from a "\x1b]11;rgb:RRRR/GGGG/BBBB"
// reply, scaling each channel down from however many hex digits the
// terminal chose to answer with.
func parseOSC11(response string) (r, g, b int, ok bool) {
	start := strings.Index(response, "]11;")
	if start < 0 {
		return 0, 0, 0, false
	}
	spec := response[start+len("]11;"):]
	spec = strings.TrimSuffix(strings.TrimSuffix(spec, "\a"), "\x1b\\")
	spec = strings.TrimPrefix(strings.TrimSpace(spec), "rgb:")
	channels := strings.Split(spec, "/")
	if len(channels) != 3 {
		return 0, 0, 0, false
	}
	var out [3]int
	for i, channel := range channels {
		value, err := strconv.ParseUint(channel, 16, 32)
		if err != nil || len(channel) == 0 || len(channel) > 4 {
			return 0, 0, 0, false
		}
		max := uint64(1)<<(4*len(channel)) - 1
		out[i] = int(value * 255 / max)
	}
	return out[0], out[1], out[2], true
}
