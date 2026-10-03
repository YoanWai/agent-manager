//go:build windows

package ui

import (
	"os"

	"golang.org/x/sys/windows"
)

// rawConsole turns off the console's own line editing, echo, and ctrl+c
// handling, so the fixture's line discipline sees every key, and turns on
// VT input and output so keys arrive and escapes leave as a tty's would.
// It returns the restore of the modes it found.
func rawConsole() func() {
	in := windows.Handle(os.Stdin.Fd())
	var inMode uint32
	if windows.GetConsoleMode(in, &inMode) != nil {
		return func() {}
	}
	raw := inMode&^(windows.ENABLE_LINE_INPUT|windows.ENABLE_ECHO_INPUT|windows.ENABLE_PROCESSED_INPUT) |
		windows.ENABLE_VIRTUAL_TERMINAL_INPUT
	windows.SetConsoleMode(in, raw)
	out := windows.Handle(os.Stdout.Fd())
	var outMode uint32
	outOK := windows.GetConsoleMode(out, &outMode) == nil
	if outOK {
		windows.SetConsoleMode(out, outMode|windows.ENABLE_PROCESSED_OUTPUT|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}
	return func() {
		windows.SetConsoleMode(in, inMode)
		if outOK {
			windows.SetConsoleMode(out, outMode)
		}
	}
}

// shortPath is a path's 8.3 form, which carries no spaces, or the path
// itself when it has none to shorten or the volume keeps no short names.
func shortPath(path string) string {
	long, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return path
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetShortPathName(long, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return path
	}
	return windows.UTF16ToString(buf[:n])
}
