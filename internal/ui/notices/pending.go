package notices

// Log is the notice the root holds back: the reason a config.toml of an
// earlier release was refused, shown as a notice until it is dismissed, and
// a notice that arrived while the list was not showing.
type Log struct {
	ImportError string
	Held        string
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

// Added is the ID of the notice a change added, empty when it added none.
// before is the set the change started from.
func Added(before map[string]bool, now []Notice) string {
	for _, n := range now {
		if !before[n.ID] {
			return n.ID
		}
	}
	return ""
}

func IDs(notices []Notice) map[string]bool {
	set := make(map[string]bool, len(notices))
	for _, n := range notices {
		set[n.ID] = true
	}
	return set
}

// Holds reports whether a list still names id, which a held notice must be
// checked against before it opens.
func Holds(notices []Notice, id string) bool {
	for _, n := range notices {
		if n.ID == id {
			return true
		}
	}
	return false
}
