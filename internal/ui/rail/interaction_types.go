package rail

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

type dropKind uint8

const (
	dropInto dropKind = iota + 1
	dropUnder
	dropBefore
)

type dropTarget struct {
	kind  dropKind
	key   string
	group string
	id    string
	label string
}

type autoscrollState struct {
	edge     int
	running  bool
	x, y     int
	anchor   int
	anchored bool
}

type reorderState struct {
	active     bool
	lift       int
	key        string
	dragging   bool
	moved      bool
	offset     int
	drop       *dropTarget
	autoscroll autoscrollState
}

type menuItem struct {
	label  string
	action ActionKind
	danger bool
}

type rowMenu struct {
	active           bool
	held             bool
	key              string
	title            string
	items            []menuItem
	index            int
	anchorX, anchorY int
}

type ActionKind uint8

const (
	NoAction ActionKind = iota
	Quit
	Focus
	Attach
	NewSession
	NewGroup
	Fork
	Revive
	MarkIdle
	ReviveAll
	Restart
	Kill
	KillAll
	Archive
	Restore
	Delete
	Prompt
	CopyReply
	OpenSettings
	Resize
	NewTerminal
	OpenEditor
	RenameAction
	MoveToGroup
	OpenMessages
	OpenHelp
	OpenReview
	CancelEnd
)

type Intent struct {
	Kind   ActionKind
	Target Selection
}

type MutationKind uint8

const (
	SaveCollapsed MutationKind = iota + 1
	SwapSession
	SwapGroup
	PlaceSession
	PlaceSessionBefore
	MoveGroup
)

// Mutation is a caller-shaped persistence request. Root executes it with
// captured concrete services off-loop and calls ApplyMutation on completion.
type Mutation struct {
	Kind          MutationKind
	SessionID     string
	TargetID      string
	Group         string
	ParentID      string
	Path          string
	TargetPath    string
	GroupSiblings []string
	Collapsed     []string
	OffsetDelta   int
	Refresh       bool
	FinishReorder bool
}

type AutoScrollRequest struct {
	Lift  int
	After time.Duration
}

type AutoScrollTick struct {
	Lift int
}

type KeyContext struct {
	ListKeys     keybind.Table
	ArrowStep    bool
	EnterFocuses bool
}

type MouseContext struct {
	BodyOriginY int
	DividerX    int
	FullWidth   bool
	FullLayout  bool
	Now         time.Time
}
