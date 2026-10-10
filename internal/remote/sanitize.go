package remote

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/charmbracelet/x/ansi"
)

// Everything a host prints is cleaned here, before it leaves this package:
// a compromised host could otherwise send the user's terminal a command in a
// session name, a group path or an error.

// cleanText drops every escape sequence, every control character and every
// byte that is not UTF-8, where a raw 8-bit CSI or OSC would hide.
func cleanText(s string) string { return clean(s, false) }

// cleanScreen is cleanText that keeps a pane's lines and tabs.
func cleanScreen(s string) string { return clean(s, true) }

func clean(s string, layout bool) string {
	return strings.Map(func(r rune) rune {
		switch {
		case layout && (r == '\n' || r == '\t'):
			return r
		case r == utf8.RuneError || unicode.IsControl(r):
			return -1
		}
		return r
	}, ansi.Strip(s))
}

func cleanSession(s sessioncmd.Session) (sessioncmd.Session, error) {
	if err := validID(s.ID); err != nil {
		return sessioncmd.Session{}, err
	}
	s.PendingInputOutcome = cleanText(s.PendingInputOutcome)
	s.Name = cleanText(s.Name)
	s.Tool = cleanText(s.Tool)
	s.Group = cleanText(s.Group)
	s.Directory = cleanText(s.Directory)
	s.Status = cleanText(s.Status)
	s.Branch = cleanText(s.Branch)
	return s, nil
}

func cleanTerminal(t sessioncmd.Terminal) (sessioncmd.Terminal, error) {
	if err := validID(t.ID); err != nil {
		return sessioncmd.Terminal{}, err
	}
	if t.ParentID != "" {
		if err := validID(t.ParentID); err != nil {
			return sessioncmd.Terminal{}, err
		}
	}
	t.Name = cleanText(t.Name)
	t.Group = cleanText(t.Group)
	t.Directory = cleanText(t.Directory)
	t.Status = cleanText(t.Status)
	t.ParentName = cleanText(t.ParentName)
	return t, nil
}

func cleanGroup(g sessioncmd.Group) sessioncmd.Group {
	g.Path = cleanText(g.Path)
	g.Directory = cleanText(g.Directory)
	g.Worktree = cleanText(g.Worktree)
	return g
}

// cleanSnapshot drops a row whose id could not be addressed safely, rather
// than the whole snapshot. The rows were decoded for this call alone, so
// they are cleaned in place.
func cleanSnapshot(snapshot sessioncmd.Snapshot) sessioncmd.Snapshot {
	sessions := snapshot.Sessions[:0]
	for _, row := range snapshot.Sessions {
		if row, err := cleanSession(row); err == nil {
			sessions = append(sessions, row)
		}
	}
	terminals := snapshot.Terminals[:0]
	for _, row := range snapshot.Terminals {
		if row, err := cleanTerminal(row); err == nil {
			terminals = append(terminals, row)
		}
	}
	for i, group := range snapshot.Groups {
		snapshot.Groups[i] = cleanGroup(group)
	}
	snapshot.Sessions, snapshot.Terminals = sessions, terminals
	return snapshot
}

func cleanRemoval(removal sessioncmd.GroupRemoval) sessioncmd.GroupRemoval {
	for i, path := range removal.Removed {
		removal.Removed[i] = cleanText(path)
	}
	moved := removal.Moved[:0]
	for _, id := range removal.Moved {
		if validID(id) == nil {
			moved = append(moved, id)
		}
	}
	removal.Moved = moved
	return removal
}
