package ui

import "os"

// DECSET 1007 (alternate scroll) makes the terminal translate wheel events
// into arrow keys while the alt screen is up. The manager turns it off: the
// arrows are indistinguishable from real ones, so a wheel notch walked the
// session cursor. Mouse reporting carries the wheel instead, and the list
// swallows it until real mouse handling lands (#110).
const altScrollOff = "\x1b[?1007l"

func DisableAlternateScroll() error {
	_, err := os.Stdout.WriteString(altScrollOff)
	return err
}
