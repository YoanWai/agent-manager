package quick

import (
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// Effects is what a bar key or click asks of the root once the bar has
// taken its own share: the rail cursor, the worktree probe, the saved
// prompts read, the send, the spawn, and the way off screen.
type Effects interface {
	Host
	MoveCursor(delta int) tea.Cmd
	ToggleWorktree() tea.Cmd
	Close()
	LoadSaved() tea.Cmd
	Send(text string) tea.Cmd
	Spawn(request spawn.Request) tea.Cmd
}

// Decide runs a key on the bar and returns the root work it asked for.
func Decide(h Effects, q *Surface, msg tea.KeyMsg) tea.Cmd {
	cmd, request := q.Bar.HandleKey(h, msg)
	return Execute(h, q, request, cmd)
}

// Execute runs the root side of what a key or click asked.
func Execute(h Effects, q *Surface, request Request, cmd tea.Cmd) tea.Cmd {
	var follow tea.Cmd
	switch {
	case request.Move != 0:
		follow = h.MoveCursor(request.Move)
	case request.Toggle:
		follow = h.ToggleWorktree()
	case request.Close:
		h.Close()
	case request.Send:
		follow = h.Send(request.Text)
	case request.Spawn != nil:
		follow = h.Spawn(*request.Spawn)
	case request.LoadSaved:
		follow = h.LoadSaved()
	case request.InsertSaved != nil:
		q.InsertSaved(*request.InsertSaved)
	}
	return tea.Batch(cmd, follow)
}

// Spawning says the bar's target is a group, so a submit starts a session
// rather than answers one.
func Spawning(h Host, q *Surface) bool {
	target, ok := h.Target()
	return ok && target.IsGroup
}
