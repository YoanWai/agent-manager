// Package capture is the runtime behind the session preview: the timer that
// re-reads the selected pane, the settle that collapses a held cursor burst
// into one capture, and the composer drafts it holds until their session
// turns working. Package preview paints what a capture brings back.
package capture

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

// The selected session's pane is re-captured on its own timer. The full poll
// is deliberately slow (it lists panes, samples every process tree and writes
// the store), which left the preview refreshing on the poll cadence and
// reading as a still image of a live agent. One capture of one pane is cheap,
// but it is still a tmux exec, so the rate follows the session: an agent that
// is producing output earns a fast cadence, one that is waiting on a human
// does not.
const (
	IntervalLive = 300 * time.Millisecond
	IntervalCalm = 1200 * time.Millisecond
)

// Settle is how long a capture waits after the last cursor move, short
// enough to feel instant and long enough to collapse a held j/k burst.
const Settle = 50 * time.Millisecond

// TypedPromptGrace is how long a candidate waits for its session to turn
// working before it is judged a menu enter and dropped.
const TypedPromptGrace = 5 * time.Second

// Msg is one pane capture and the process reading that came with it. Gen is
// the preview generation the capture was scheduled for, so a newer cursor
// move can discard it.
type Msg struct {
	SessID  string
	Preview string
	Proc    sysstat.ProcStat
	Gen     uint64
	Err     error
}

type SettleMsg struct{ Gen uint64 }

// TickMsg drives the capture timer.
type TickMsg struct{}

// TypedPrompt is a composer draft snapshotted as enter went into a focused
// pane, held until the session shows the send went through.
type TypedPrompt struct {
	Session store.Session
	ID      string
	Text    string
	At      time.Time
}

type Root interface {
	Selected() (store.Session, bool)
	// Generation is the preview generation the current selection is on.
	Generation() uint64
	// Accepts reports whether a capture scheduled for gen still belongs to
	// what the screen shows.
	Accepts(gen uint64) bool
	// CaptureWanted reports whether a capture of sess would be painted: the
	// screen on top feeds a pane and no control client already pushes one.
	CaptureWanted(sess store.Session) bool
	// ArmTick starts the next capture timer, and the remote read beside it
	// when withRemote asks: that one keeps running where the pane is hidden.
	ArmTick(withRemote bool) tea.Cmd
	WatchSelection()
	ApplyCapture(sessID, preview string, proc sysstat.ProcStat)
	ReportErr(err error)
}

// State is the preview's own runtime: the drafts waiting on their sessions
// and the timers that re-read the pane.
type State struct {
	store   *store.Store
	tmux    *tmux.Driver
	Pending map[string]TypedPrompt
}

func New(st *store.Store, driver *tmux.Driver) State {
	return State{store: st, tmux: driver}
}

// Interval is the cadence the selected session earns: an agent producing
// output refreshes fast, one waiting on a human slowly.
func (s *State) Interval(root Root) time.Duration {
	if sess, ok := root.Selected(); ok {
		switch sess.Status {
		case status.Working, status.Starting:
			return IntervalLive
		}
	}
	return IntervalCalm
}

func (s *State) Tick(root Root) tea.Cmd {
	return tea.Tick(s.Interval(root), func(time.Time) tea.Msg { return TickMsg{} })
}

// Schedule arms one capture after Settle. Call after bumping the preview
// generation so earlier timers and in-flight captures go stale.
func (s *State) Schedule(gen uint64) tea.Cmd {
	return tea.Tick(Settle, func(time.Time) tea.Msg { return SettleMsg{Gen: gen} })
}

// Capture reads one session's pane and process stats off the render loop.
// Size pins stay on resize / create / attach — not on every look — so a
// settle capture is one capture-pane, not a resize+capture+pid storm.
func (s *State) Capture(sess store.Session, gen uint64) tea.Cmd {
	return func() tea.Msg {
		msg := Msg{SessID: sess.ID, Gen: gen}
		if sess.Archived || !s.tmux.Exists(sess.ID) {
			snapshot, err := execution.StoredPreview(s.store, s.tmux, sess.ID)
			if err != nil {
				return Msg{Err: err}
			}
			msg.Preview = snapshot
			return msg
		}
		if pane, err := s.tmux.CapturePane(sess.ID); err == nil {
			msg.Preview = pane
		}
		if pid, err := s.tmux.PanePID(sess.ID); err == nil {
			memTotal, _ := sysstat.MemTotalBytes()
			msg.Proc = sysstat.Trees([]int{pid})[pid].ScaleToHost(sysstat.LogicalCPUs(), memTotal)
		}
		return msg
	}
}

// Hold keeps cand as its session's candidate, replacing only an earlier one
// for the same session.
func (s *State) Hold(cand TypedPrompt) {
	if s.Pending == nil {
		s.Pending = make(map[string]TypedPrompt)
	}
	s.Pending[cand.ID] = cand
}

// Commit records each stashed draft as its session's last prompt once that
// session runs with it: an enter that opened a menu or answered a dialog
// never turns the session working while its draft is fresh, so that
// candidate just expires.
func (s *State) Commit(sessions []store.Session, now time.Time, record func(store.Session, string)) {
	for id, cand := range s.Pending {
		if now.Sub(cand.At) > TypedPromptGrace {
			delete(s.Pending, id)
			continue
		}
		i := indexSession(sessions, id)
		if i < 0 {
			delete(s.Pending, id)
			continue
		}
		if sessions[i].Status != status.Working {
			continue
		}
		record(cand.Session, cand.Text)
		delete(s.Pending, id)
	}
}

func indexSession(sessions []store.Session, id string) int {
	for i, sess := range sessions {
		if sess.ID == id {
			return i
		}
	}
	return -1
}

func (s *State) Route(root Root, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case TickMsg:
		// Only the list keeps a live pane on screen; review and the modal
		// screens have no preview to feed, so they just keep the timer alive.
		sess, ok := root.Selected()
		if !ok {
			return root.ArmTick(true), true
		}
		if !root.CaptureWanted(sess) {
			return root.ArmTick(false), true
		}
		return tea.Batch(s.Capture(sess, root.Generation()), root.ArmTick(false)), true

	case SettleMsg:
		if root.Generation() != msg.Gen {
			return nil, true
		}
		sess, ok := root.Selected()
		if !ok {
			return nil, true
		}
		// The cursor has come to rest: this is where the control client is
		// worth opening.
		root.WatchSelection()
		return s.Capture(sess, msg.Gen), true

	case Msg:
		if msg.Err != nil {
			root.ReportErr(msg.Err)
			return nil, true
		}
		if !root.Accepts(msg.Gen) {
			return nil, true
		}
		if sess, ok := root.Selected(); ok && sess.ID == msg.SessID {
			root.ApplyCapture(msg.SessID, msg.Preview, msg.Proc)
		}
		return nil, true
	}
	return nil, false
}
