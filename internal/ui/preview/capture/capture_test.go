package capture

import (
	"errors"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	tea "github.com/charmbracelet/bubbletea"
)

// fakeRoot is the frame a routed message reaches, with just the answers a
// capture asks for.
type fakeRoot struct {
	selected store.Session
	hasSel   bool
	gen      uint64
	feeds    bool
	serving  bool
	watched  bool
	errs     []error
	applied  []applied
}

// applied is one capture the root took.
type applied struct {
	sessID, preview string
}

func (r *fakeRoot) Selected() (store.Session, bool)  { return r.selected, r.hasSel }
func (r *fakeRoot) Generation() uint64               { return r.gen }
func (r *fakeRoot) Accepts(gen uint64) bool          { return gen == r.gen || gen == 0 }
func (r *fakeRoot) CaptureWanted(store.Session) bool { return r.feeds && !r.serving }
func (r *fakeRoot) ArmTick(bool) tea.Cmd             { return nil }
func (r *fakeRoot) WatchSelection()                  { r.watched = true }

func (r *fakeRoot) ApplyCapture(sessID, preview string, _ sysstat.ProcStat) {
	r.applied = append(r.applied, applied{sessID: sessID, preview: preview})
}

func (r *fakeRoot) ReportErr(err error) { r.errs = append(r.errs, err) }

func TestIntervalFollowsTheSelectedStatus(t *testing.T) {
	s := State{}
	tests := []struct {
		name string
		sess store.Session
		ok   bool
		want time.Duration
	}{
		{"starting", store.Session{Status: status.Starting}, true, IntervalLive},
		{"working", store.Session{Status: status.Working}, true, IntervalLive},
		{"idle", store.Session{Status: status.Idle}, true, IntervalCalm},
		{"no selection", store.Session{}, false, IntervalCalm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := &fakeRoot{selected: tt.sess, hasSel: tt.ok}
			if got := s.Interval(root); got != tt.want {
				t.Fatalf("interval = %v, want %v", got, tt.want)
			}
		})
	}
}

// A settle that arrives after the cursor moved on belongs to a capture the
// screen no longer shows, and must not open the control client on the new
// selection.
func TestStaleSettleNeverWatchesOrCaptures(t *testing.T) {
	root := &fakeRoot{hasSel: true, selected: store.Session{ID: "b"}, gen: 2, feeds: true}
	s := New(nil, nil)
	cmd, ok := s.Route(root, SettleMsg{Gen: 1})
	if !ok || cmd != nil {
		t.Fatalf("stale settle = %v, %v; want handled with no work", cmd, ok)
	}
	if root.watched {
		t.Fatal("a stale settle opened the control client")
	}
}

func TestCurrentSettleWatchesThenCaptures(t *testing.T) {
	root := &fakeRoot{hasSel: true, selected: store.Session{ID: "a"}, gen: 1, feeds: true}
	s := New(nil, nil)
	if _, ok := s.Route(root, SettleMsg{Gen: 1}); !ok {
		t.Fatal("settle was not routed")
	}
	if !root.watched {
		t.Fatal("the settle that matches the generation never opened the control client")
	}
}

// A session the watcher already serves needs no capture: the result would be
// discarded, so the tick only re-arms.
func TestTickSkipsACaptureTheWatcherAlreadyFeeds(t *testing.T) {
	root := &fakeRoot{hasSel: true, selected: store.Session{ID: "a"}, gen: 1, feeds: true, serving: true}
	s := New(nil, nil)
	if cmd, ok := s.Route(root, TickMsg{}); !ok || cmd != nil {
		t.Fatalf("served tick = %v, %v; want handled with no capture", cmd, ok)
	}
}

func TestTickKeepsTheTimerWhileTheListIsHidden(t *testing.T) {
	root := &fakeRoot{hasSel: true, selected: store.Session{ID: "a"}, gen: 1}
	s := New(nil, nil)
	if _, ok := s.Route(root, TickMsg{}); !ok {
		t.Fatal("a tick with no preview on screen was not handled")
	}
}

func TestCaptureResultForAnotherSelectionIsDropped(t *testing.T) {
	root := &fakeRoot{hasSel: true, selected: store.Session{ID: "b"}}
	s := New(nil, nil)
	if _, ok := s.Route(root, Msg{SessID: "a", Preview: "old"}); !ok {
		t.Fatal("capture was not routed")
	}
	if len(root.applied) != 0 {
		t.Fatalf("capture for another selection was applied: %+v", root.applied)
	}
}

// A capture scheduled before the cursor moved carries the frame the user is
// no longer looking at, even when its session is still the selected one.
func TestCaptureFromAnOlderGenerationIsDropped(t *testing.T) {
	root := &fakeRoot{hasSel: true, selected: store.Session{ID: "a"}, gen: 3}
	s := New(nil, nil)
	if _, ok := s.Route(root, Msg{SessID: "a", Preview: "old", Gen: 2}); !ok {
		t.Fatal("capture was not routed")
	}
	if len(root.applied) != 0 {
		t.Fatalf("a stale capture was applied: %+v", root.applied)
	}
	s.Route(root, Msg{SessID: "a", Preview: "fresh", Gen: 3})
	if len(root.applied) != 1 || root.applied[0].preview != "fresh" {
		t.Fatalf("applied = %+v, want the current generation's capture", root.applied)
	}
}

func TestCaptureErrorReachesTheStatusBar(t *testing.T) {
	root := &fakeRoot{}
	s := New(nil, nil)
	want := errors.New("no pane")
	if _, ok := s.Route(root, Msg{Err: want}); !ok {
		t.Fatal("capture error was not routed")
	}
	if len(root.errs) != 1 || root.errs[0] != want {
		t.Fatalf("reported errors = %v, want the capture's", root.errs)
	}
}

func TestHeldDraftWaitsForItsSessionToWork(t *testing.T) {
	s := New(nil, nil)
	now := time.Now()
	s.Hold(TypedPrompt{ID: "a", Text: "draft", At: now})
	s.Hold(TypedPrompt{ID: "b", Text: "stale", At: now.Add(-2 * TypedPromptGrace)})
	var recorded []string
	record := func(_ store.Session, text string) { recorded = append(recorded, text) }
	sessions := []store.Session{{ID: "a", Status: status.Idle}, {ID: "b", Status: status.Working}}

	s.Commit(sessions, now, record)
	if len(recorded) != 0 {
		t.Fatalf("committed drafts with no session working: %v", recorded)
	}
	if _, ok := s.Pending["b"]; ok {
		t.Fatal("an expired draft was kept")
	}
	sessions[0].Status = status.Working
	s.Commit(sessions, now, record)
	if len(recorded) != 1 || recorded[0] != "draft" {
		t.Fatalf("recorded = %v, want the draft that waited for working", recorded)
	}
	if len(s.Pending) != 0 {
		t.Fatalf("a committed draft stayed held: %v", s.Pending)
	}
}

// A draft whose session left the list has nothing left to commit.
func TestHeldDraftForAGoneSessionIsDropped(t *testing.T) {
	s := New(nil, nil)
	now := time.Now()
	s.Hold(TypedPrompt{ID: "gone", Text: "draft", At: now})
	s.Commit(nil, now, func(store.Session, string) { t.Fatal("a gone session's draft was recorded") })
	if len(s.Pending) != 0 {
		t.Fatalf("a gone session's draft stayed held: %v", s.Pending)
	}
}
