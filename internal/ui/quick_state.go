package ui

import (
	"github.com/YoanWai/agent-manager/internal/clipboard"
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
}
