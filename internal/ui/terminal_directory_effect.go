package ui

import (
	"github.com/YoanWai/agent-manager/internal/tmux"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
)

// systemTerminalDirectoryReader answers where a terminal or the editor opens:
// the pane's live directory when it still answers, then the captured fallbacks.
type systemTerminalDirectoryReader struct {
	tmux *tmux.Driver
	dirs directoryPreflight
}

func (r systemTerminalDirectoryReader) resolve(request terminalDirectoryRequest) (string, bool) {
	if request.paneID != "" {
		if path, err := r.tmux.PaneCurrentPath(request.paneID); err == nil && uilaunch.IsDir(path) {
			return path, true
		}
	}
	return r.dirs.resolve(request.rawDir, request.fallbacks)
}
