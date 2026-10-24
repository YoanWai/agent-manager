package notices

// Log is the notice the root holds back: the reason a config.toml of an
// earlier release was refused, shown as a notice until it is dismissed, and
// a notice that arrived while the list was not showing.
type Log struct {
	ImportError string
	// Held is the notice waiting for the list to come back.
	Held string
}

// Stage says what a change in the active notices asks the root to do: open
// the panel on the notice it just added, or hold it until the list returns.
// A panel already up is the caller's to move, so it stages nothing.
type Stage struct {
	Open string
	Hold string
}

// Staged decides where a notice that just appeared goes. added is the ID of
// the notice the change added, empty when it added none.
func (l *Log) Staged(added string, panelOpen, listReady bool) Stage {
	switch {
	case added == "" || panelOpen:
		return Stage{}
	case listReady:
		return Stage{Open: added}
	default:
		return Stage{Hold: added}
	}
}

// Take is the held notice, cleared, once the list can show it.
func (l *Log) Take() string {
	id := l.Held
	l.Held = ""
	return id
}
