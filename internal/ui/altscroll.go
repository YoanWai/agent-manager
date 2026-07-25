package ui

import "os"

// DECSET 1007 (alternate scroll) makes the terminal translate wheel events
// into arrow keys while the alt screen is up. The manager keeps mouse
// tracking off so click+drag selects text natively; without 1007 the wheel
// would scroll the host's scrollback and push the manager out of view.
const (
	altScrollOn  = "\x1b[?1007h"
	altScrollOff = "\x1b[?1007l"
)

func EnableAlternateScroll() error {
	_, err := os.Stdout.WriteString(altScrollOn)
	return err
}

func DisableAlternateScroll() error {
	_, err := os.Stdout.WriteString(altScrollOff)
	return err
}
