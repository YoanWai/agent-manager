package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

type workspace struct {
	sessions []store.Session
	// tmuxSocket is the server the last poll read panes from; rows stamped
	// with another server belong to a manager running against it, and rows
	// stamped by none belong to whichever manager holds the store.
	tmuxSocket     string
	leadingManager bool
	groups         []string
	groupPaths     map[string]string
	groupWorktrees map[string]string
	groupBases     map[string]string
	archivedGroups map[string]bool
	snap           sysstat.Snapshot
	proc           sysstat.ProcStat
	procFor        string
	preview        string
	agents         agentStats
	// queuedMessages is replaced whole on every refresh rather than merged,
	// so a delivered message's badge clears itself.
	queuedMessages map[string]int
	// paneLines holds each session's last meaningful pane line, which the
	// full screen row's second line quotes. Merged rather than replaced, so
	// a session that lost its window keeps its last words.
	paneLines map[string]string
	// panePrompts holds the last prompt each session's transcript echoes,
	// which the full screen row shows as the task it is on.
	panePrompts map[string]string
	// panes is the last pass's agent pane geometry, which the poller reads
	// off the UI loop alongside its liveness listing.
	panes map[string]tmux.Pane
	net   netStats
	// lastListedAt is the newest listing applied; an older one still carries
	// focus, turn ends and pane facts, but not the rows it saw.
	lastListedAt time.Time
}
