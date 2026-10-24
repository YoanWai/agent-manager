package dialogs

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// Move picks a new parent for one session or group. It owns the captured
// row and the picker keys; the targets are the root's group picker, which
// the root shapes when the dialog opens and reads back on submit.
type Move struct {
	sessionID string
	group     string
}

func MoveSession(id string) Move { return Move{sessionID: id} }

func MoveGroup(path string) Move { return Move{group: path} }

// SessionID is the captured session, empty for a group move.
func (d Move) SessionID() string { return d.sessionID }

func (d Move) IsGroup() bool { return d.group != "" }

type MoveAction uint8

const (
	MoveStay MoveAction = iota
	MoveCancel
	MoveCursor
	MoveSubmit
)

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

func MoveTitle(mode presentation.GlyphMode) string {
	return presentation.GlyphsFor(mode).MoveGlyph + " Move"
}

func MoveHints() [][2]string {
	return [][2]string{{"↑↓", "pick"}, {"↵", "move"}, {"esc", "cancel"}}
}

// Placement is the effect a submit asks the root to run: the Rail mutation
// that places the row, and the follow-up that closes the card it came from.
type Placement struct {
	SessionID string
	Group     string
	ParentID  string
	GroupPath string
}

func (p Placement) IsGroup() bool { return p.GroupPath != "" }

// PlacementOf resolves a submit against the picked row, reporting false when
// a group would land where it already is, which only closes the card.
func PlacementOf(d Move, picker *spawn.Picker) (Placement, bool) {
	pick := MovePick{Group: picker.GroupPath()}
	if !d.IsGroup() {
		opt := picker.Picked()
		pick = MovePick{Group: opt.Path, ParentID: opt.SessionID}
	}
	request, ok := d.Place(pick)
	if !ok {
		return Placement{}, false
	}
	if request.IsGroup() {
		return Placement{GroupPath: request.GroupPath, Group: request.Group}, true
	}
	return Placement{SessionID: request.SessionID, Group: request.Group, ParentID: request.ParentID}, true
}
