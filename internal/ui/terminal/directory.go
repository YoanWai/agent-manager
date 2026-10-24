// Package terminal is the terminal integration: the link seams the manager
// hands the package, the directory a shell row or the editor opens in, and
// the alternate-scroll switch flipped before the manager takes the screen.
package terminal

import (
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
)

// PanePath reads a pane's live working directory; nil reads no pane.
type PanePath func(paneID string) (string, error)

// Directory is where a shell row or the editor opens: the pane whose live
// directory wins when it still answers, then the captured fallbacks.
type Directory struct {
	PaneID    string
	RawDir    string
	Fallbacks []string
}

// DirectoryMsg is a resolved directory, and for the editor the process
// resolved off the update path with it.
type DirectoryMsg struct {
	Directory Directory
	Dir       string
	OK        bool
	Editor    uisession.Launch
}

// DirectoryReader answers where a terminal or the editor opens.
type DirectoryReader interface {
	Resolve(Directory) (string, bool)
}

// Dirs is the read-only filesystem and repository seam a spawn or a group
// creation captures before it is queued.
type Dirs interface {
	Resolve(raw string, fallbacks []string) (string, bool)
	RepoCapable(dir string) bool
}

// SystemDirectoryReader answers from the pane's live directory, then the
// captured fallbacks. The probe lives here, so a caller hands one reader
// wherever it asks where to open.
type SystemDirectoryReader struct {
	PanePath PanePath
	Dirs     Dirs
	// IsDir reports whether the pane's path still names a directory; nil
	// accepts whatever the pane answered.
	IsDir func(path string) bool
}

func (r SystemDirectoryReader) Resolve(request Directory) (string, bool) {
	if request.PaneID != "" && r.PanePath != nil {
		if path, err := r.PanePath(request.PaneID); err == nil && (r.IsDir == nil || r.IsDir(path)) {
			return path, true
		}
	}
	return r.Dirs.Resolve(request.RawDir, request.Fallbacks)
}

// Resolve answers where a row's terminal or editor opens: the pane's live
// directory when it still answers, the captured fallbacks otherwise, and for
// the editor the process resolved against whichever won.
func Resolve(request Directory, reader DirectoryReader, prepare func(dir string) uisession.Launch) DirectoryMsg {
	request.Fallbacks = append([]string(nil), request.Fallbacks...)
	dir, ok := reader.Resolve(request)
	msg := DirectoryMsg{Directory: request, Dir: dir, OK: ok}
	if ok && prepare != nil {
		msg.Editor = prepare(dir)
	}
	return msg
}

// Target is where a directory read opens: a shell row beside the list, or the
// editor over it.
type Target uint8

const (
	TargetSpawn Target = iota
	TargetEditor
)

// Screen is the frame a read was captured on: an editor completion still has
// to find the same foreground generation, screen and row selected, since the
// user may have moved on while it waited.
type Screen struct {
	ForegroundGen uint64
	Screen        uint64
	RowKey        string
	Selected      bool
}

// StillCurrent reports whether a captured editor read may still open: a shell
// read never carries a screen to check.
func (t Target) StillCurrent(captured, current Screen) bool {
	if t != TargetEditor {
		return true
	}
	return current.Selected && captured.ForegroundGen == current.ForegroundGen &&
		captured.Screen == current.Screen && captured.RowKey == current.RowKey
}

// Notice is the line a refused read reports, naming the directory for the
// editor and only the request for a shell.
func (t Target) Notice(msg DirectoryMsg) string {
	if t == TargetEditor {
		return "directory no longer exists: " + msg.Dir
	}
	return "no directory to open a terminal in"
}
