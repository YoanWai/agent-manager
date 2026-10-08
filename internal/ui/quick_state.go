package ui

import (
	"github.com/YoanWai/agent-manager/internal/clipboard"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

// captureClipboardImage is the seam the quick bar uses to save a pasted
// image to a temp file; tests swap it for a fake.
var captureClipboardImage = clipboard.SaveImage

// quickState is the inline prompt bar docked under the preview: active
// across cursor moves, so the target follows the selection. The tool is
// the spawn CLI for group targets, cycled with tab. A pasted image lands
// at the caret as an "[Image #N]" token that renders as a chip and steps,
// deletes, and wraps as one unit; on submit each token becomes its path.
type quickState struct {
	active bool
	composer
	toolNames      []string
	toolIndex      int
	closeAfterSend bool
	worktree       bool
	// worktreeTouched marks an explicit toggle this run; until then the
	// hint and spawn follow the target group's default.
	worktreeTouched bool
	// defaultsTouched protects an explicit tool or worktree choice from the
	// external settings refresh queued when this quick bar opened.
	defaultsTouched bool
	choice          choice
	// picking is the list open above the prompt, which takes the typing.
	picking int
	// aim is the row the user last pointed the bar at. A refresh that
	// drops a connection's row moves the cursor on its own, and enter must
	// not follow it to another agent or host.
	aim uirail.Selection
	// hits are relative to the origin, the bar's first painted line.
	hits             []quickHit
	originX, originY int
}
