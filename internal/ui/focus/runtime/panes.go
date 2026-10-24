package runtime

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// Target is one session window a pass must resize.
type Target struct {
	ID     string
	Height int
}

// Plan is what deciding one pass's resizes reads.
type Plan struct {
	// Box is the preview panel's size, read once for the whole pass.
	Box [2]int
	// Sessions are the rows the manager holds.
	Sessions []store.Session
	// Panes is the pass's agent pane geometry.
	Panes map[string]tmux.Pane
	// FitsHeight reports whether a tool keeps its own height, which decides
	// whether a shorter box may crop it.
	FitsHeight func(tool string) bool
	// FullFocusID is the session open full screen, which the full-screen pin
	// owns instead of the box.
	FullFocusID string
}

// ResizeTargets is the windows whose box moved. Width changes and height
// growth pin eagerly; a box that lost rows, to transient chrome or a shorter
// terminal, leaves the pane tall and lets the painted view crop instead,
// because a height shrink makes Codex clear the pane's entire scrollback
// (#369). A pane on the alternate screen holds no scrollback for a shrink to
// clear, and a tool with fits_height keeps its own, so those follow the box
// down and a full-screen TUI keeps its top rows in view.
func (s *State) ResizeTargets(plan Plan) []Target {
	if plan.Box[0] <= 0 || plan.Box[1] <= 0 {
		return nil
	}
	width, height := plan.Box[0], plan.Box[1]
	var targets []Target
	for _, sess := range plan.Sessions {
		if sess.Archived || sess.ID == plan.FullFocusID {
			continue
		}
		wanted := height
		if last, sized := s.SizeOf(sess.ID); sized {
			fitsHeight := plan.Panes[sess.ID].AltScreen || plan.FitsHeight(sess.Tool)
			keepsHeight := last[1] > height && !fitsHeight
			if last[0] == width && (last[1] == height || keepsHeight) {
				continue
			}
			// A width re-pin of a taller pane keeps its height: shrinking it
			// would clear a Codex scrollback (#369); the painted view crops
			// instead.
			if keepsHeight {
				wanted = last[1]
			}
		}
		targets = append(targets, Target{ID: sess.ID, Height: wanted})
	}
	return targets
}

// Seed fills the size cache from the pass's pane listing for sessions this run
// has not sized yet, so a pane adopted from a previous run is not shrunk (and
// its Codex scrollback cleared) just to match a box it already exceeds. A split
// window is taken every pass instead: the agent opened those panes out of its
// own window, taking room from the pane the preview draws, and no manager
// action precedes that for the cache to follow. Drift the manager did not cause
// otherwise stays, so a window someone resized from another client is left
// where they put it.
func (s *State) Seed(panes map[string]tmux.Pane) {
	for id, geom := range panes {
		last, sized := s.SizeOf(id)
		// A pin this run still owes is not a measurement: a session created
		// this run carries its pre-selection launch size, not the box.
		if sized && last == [2]int{0, 0} {
			continue
		}
		if sized && geom.Panes < 2 {
			continue
		}
		s.SetSize(id, [2]int{geom.Width, geom.Height})
	}
}

// MarksFresh notices a session whose pane process changed since its geometry
// was measured: a revive outside this process put the row on a window the
// manager never sized, and the cached size of the window it replaced would
// otherwise read as already matching the box, leaving the new pane at whatever
// it was born with for the life of the run. The replacement holds no scrollback
// to protect, so it takes the exact pin rather than the adopted-pane treatment
// that keeps a taller height.
func (s *State) MarksFresh(panes map[string]tmux.Pane) {
	for id, pane := range panes {
		last := s.PIDOf(id)
		s.SetPID(id, pane.PID)
		if last != 0 && last != pane.PID {
			s.PinOwed(id)
		}
	}
}

// Window is the rows-high window offset rows up from the bottom of a history
// capture, in the capture's shape.
func Window(capture string, rows, offset int) string {
	lines := strings.Split(strings.TrimSuffix(capture, "\n"), "\n")
	end := max(len(lines)-offset, 0)
	start := max(end-rows, 0)
	return strings.Join(lines[start:end], "\n") + "\n"
}
