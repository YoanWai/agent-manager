package launch

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type RepoAnswer struct {
	Capable bool
	At      time.Time
}

// worktreeUnavailable is what the worktree toggle reads when the target
// directory cannot host one.
const WorktreeUnavailable = "unavailable (not a git repo)"

// WorktreeLookupTTL bounds reuse for explicit toggles, so repository changes
// are discovered while a composer stays open. Paint keeps the last observation
// until a probe replaces it; time passing alone is not a new repo verdict.
const WorktreeLookupTTL = 2 * time.Second

type ProbeTarget uint8

const (
	ProbeForm ProbeTarget = iota
	ProbeQuick
)

// ProbeRequest names one directory's worktree probe, with the sequence that
// retires an older one.
type ProbeRequest struct {
	Target     ProbeTarget
	Generation int
	Sequence   uint64
	Dir        string
	Toggle     bool
	From       bool
}

type ProbeMsg struct {
	Request ProbeRequest
	Capable bool
}

// ProbeCmd answers whether a directory sits inside a repo, off the update
// path.
func ProbeCmd(request ProbeRequest, reader DirectoryPreflight) tea.Cmd {
	return func() tea.Msg {
		return ProbeMsg{Request: request, Capable: reader.RepoCapable(request.Dir)}
	}
}
