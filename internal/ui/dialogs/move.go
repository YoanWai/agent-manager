package dialogs

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
)

// Move picks a new parent for one session or group. It owns the captured
// row and the picker keys; the targets are the root's group picker, which
// the root shapes when the dialog opens and reads back on submit.
type Move struct {
	sessionID string
	group     string
}

// MoveSession captures a session row to place.
func MoveSession(id string) Move { return Move{sessionID: id} }

// MoveGroup captures a group row to re-parent.
func MoveGroup(path string) Move { return Move{group: path} }

// SessionID is the captured session, empty for a group move.
func (d Move) SessionID() string { return d.sessionID }

// IsGroup reports whether the captured row is a group.
func (d Move) IsGroup() bool { return d.group != "" }

// MoveAction is what a key asks the root to do.
type MoveAction uint8

const (
	MoveStay MoveAction = iota
	MoveCancel
	// MoveCursor steps the root's group picker by Delta.
	MoveCursor
	// MoveSubmit asks the root for the picked row and passes it to Place.
	MoveSubmit
)

// MoveKey is a key's outcome.
type MoveKey struct {
	Action MoveAction
	Delta  int
}

func (d Move) Key(msg tea.KeyMsg) MoveKey {
	switch msg.String() {
	case "esc":
		return MoveKey{Action: MoveCancel}
	case "up":
		return MoveKey{Action: MoveCursor, Delta: -1}
	case "down":
		return MoveKey{Action: MoveCursor, Delta: 1}
	case "enter":
		return MoveKey{Action: MoveSubmit}
	}
	return MoveKey{}
}

// MovePick is the picker row a submit lands on.
type MovePick struct {
	// Group is the picked row's group; for a group move it is the new parent.
	Group string
	// ParentID is the agent row a shell nests under, empty for a group row.
	ParentID string
}

// MoveRequest is the placement a submit asks the root to run. A session
// move fills SessionID; a group move fills GroupPath and puts it under
// Group.
type MoveRequest struct {
	SessionID string
	GroupPath string
	Group     string
	ParentID  string
}

// IsGroup reports whether the request re-parents a group.
func (r MoveRequest) IsGroup() bool { return r.GroupPath != "" }

// Place resolves a submit against the picked row. It reports false when a
// group would land where it already is, which only closes the dialog.
func (d Move) Place(pick MovePick) (MoveRequest, bool) {
	if d.group == "" {
		return MoveRequest{SessionID: d.sessionID, Group: pick.Group, ParentID: pick.ParentID}, true
	}
	newPath := baseName(d.group)
	if pick.Group != "" {
		newPath = pick.Group + "/" + newPath
	}
	if newPath == d.group {
		return MoveRequest{}, false
	}
	return MoveRequest{GroupPath: d.group, Group: pick.Group}, true
}

// MoveTitle and MoveHints frame the card around the root's group picker.
func MoveTitle(mode presentation.GlyphMode) string {
	return presentation.GlyphsFor(mode).MoveGlyph + " Move"
}

func MoveHints() [][2]string {
	return [][2]string{{"↑↓", "pick"}, {"↵", "move"}, {"esc", "cancel"}}
}
