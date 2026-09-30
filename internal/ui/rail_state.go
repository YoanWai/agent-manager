package ui

import (
	"time"
)

type railState struct {
	rows []treeRow
	// listClickAt and listClickKey remember the last rail press so two
	// presses on the same row inside multiClickWindow count as a double click.
	listClickAt  time.Time
	listClickKey string
	// clickFocusKey is the split rail session row a press landed on; its
	// release on that same row focuses it, so a drag can still claim it.
	clickFocusKey string
	reorder       reorderState
	// lifts numbers each row lift, so a tick one drag scheduled is not
	// taken for the next drag's.
	lifts int
	// railWidth is the rail content width the last frame painted, which
	// places every row's menu button.
	railWidth int
	// railEnd is one past the last row the rail window painted.
	railEnd int
	// handleX is the screen column of each row's drag handle as the last
	// frame painted it; the handle's place follows the row's tree depth.
	handleX map[string]int
	menu    rowMenu
	cursor  int
	// railTop is the entry the rail paints first, carried between frames.
	// Deriving it from the cursor alone cannot hold still: rows are of
	// uneven height, so every step would re-solve the window and slide the
	// list under a highlight that should have simply moved down.
	railTop int
	// railHits maps each line the rail painted this frame to the m.rows
	// index a click there selects, -1 for chrome (search field, badges,
	// padding, meters) a click cannot select. Recorded by recordRailHits
	// at paint time, the way m.pane.box is for the focused pane, so a
	// click handler never has to re-derive the rail's layout and drift
	// from it.
	railHits        []int
	showArchived    bool
	hideEmptyGroups bool
	statusFilter    statusFilter
	collapsed       map[string]bool
	search          string
	searching       bool
}
