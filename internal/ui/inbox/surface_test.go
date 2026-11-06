package inbox

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type fakeHost struct {
	width, height int
	popped        bool
	cardWidth     int
	title, body   string
}

func (h *fakeHost) CardSized(width int, title, body string, _ [][2]string) string {
	h.cardWidth, h.title, h.body = width, title, body
	return body
}

func (h *fakeHost) Size() (int, int) { return h.width, h.height }

func (h *fakeHost) Pop() { h.popped = true }

type fakeRoot struct {
	loads []uint64
	reads []int64
	gen   uint64
}

func (r *fakeRoot) LoadInbox(_ string, gen uint64) { r.loads = append(r.loads, gen) }

func (r *fakeRoot) ReadInboxMessage(id int64, gen uint64) {
	r.reads = append(r.reads, id)
	r.gen = gen
}

var noon = time.Date(2026, 11, 6, 12, 0, 0, 0, time.Local)

func sampleLog() Log {
	return Log{
		Queued: []Entry{{ID: 42, From: "reviewer", To: "worker", SentAt: noon.Add(-time.Hour), Subject: "review report", State: "held",
			Reason: "session ab12cd is sitting on a dialog; read that screen and answer it", Position: 1}},
		Recent: []Entry{{ID: 40, From: "worker", To: "reviewer", SentAt: noon.Add(-26 * time.Hour), Subject: "tests pass", State: "delivered"}},
	}
}

func openSurface(t *testing.T, width int) (*Surface, *fakeRoot, *fakeHost) {
	t.Helper()
	root := &fakeRoot{}
	s := NewSurface(root, "ab12cd", "worker", presentation.GlyphModeAdvanced)
	s.now = func() time.Time { return noon }
	return s, root, &fakeHost{width: width, height: 30}
}

func view(s *Surface, h *fakeHost) string {
	s.View(h, "")
	return ansi.Strip(h.body)
}

func press(t *testing.T, s *Surface, h *fakeHost, keys ...tea.KeyMsg) {
	t.Helper()
	for _, msg := range keys {
		if handled, _ := s.Key(h, msg); !handled {
			t.Fatalf("the card let %q through", msg.String())
		}
	}
}

func TestInboxShowsLoadingThenTheQueueAndTheLatest(t *testing.T) {
	s, root, h := openSurface(t, 120)
	if len(root.loads) != 1 {
		t.Fatalf("opening asked for %d loads, want 1", len(root.loads))
	}
	if got := view(s, h); !strings.Contains(got, "Loading messages") {
		t.Fatalf("before a load the card shows %q", got)
	}
	if !strings.Contains(h.title, "Inbox: worker") {
		t.Fatalf("title = %q", h.title)
	}
	s.Loaded(root.loads[0], sampleLog(), nil)
	got := view(s, h)
	for _, want := range []string{
		"Waiting (1)", "11:00  reviewer → worker  held #1  review report",
		"    session ab12cd is sitting on a dialog; read that screen and answer it",
		"Recent (1)", "Nov 5 10:00  worker → reviewer  delivered  tests pass",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("card does not show %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "Waiting") > strings.Index(got, "Recent") {
		t.Fatalf("the queue does not come first:\n%s", got)
	}
}

func TestInboxStatesItsEmptyAndFailedLoads(t *testing.T) {
	s, root, h := openSurface(t, 120)
	s.Loaded(root.loads[0], Log{}, nil)
	if got := view(s, h); !strings.Contains(got, "No messages to or from this session in the last day.") {
		t.Fatalf("empty card = %q", got)
	}
	s.Reload()
	s.Loaded(root.loads[0], sampleLog(), nil)
	if got := view(s, h); strings.Contains(got, "review report") {
		t.Fatalf("a stale load replaced the newer one: %q", got)
	}
	s.Loaded(root.loads[1], Log{}, errors.New("store is locked"))
	if got := view(s, h); !strings.Contains(got, "store is locked") {
		t.Fatalf("a failed load reads as %q", got)
	}
}

func TestInboxReadsAMessageAndGoesBack(t *testing.T) {
	s, root, h := openSurface(t, 120)
	s.Loaded(root.loads[0], sampleLog(), nil)
	press(t, s, h, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyEnter})
	if len(root.reads) != 1 || root.reads[0] != 40 {
		t.Fatalf("enter read %v, want message 40", root.reads)
	}
	if got := view(s, h); !strings.Contains(got, "Loading the message") {
		t.Fatalf("before the body lands the card shows %q", got)
	}
	s.BodyRead(root.gen, "line one\n\x1b[31mred\x1b[0m and a long line "+strings.Repeat("word ", 40), nil)
	got := view(s, h)
	if !strings.Contains(got, "worker → reviewer") || !strings.Contains(got, "line one") || !strings.Contains(got, "^[[31mred") {
		t.Fatalf("the body reads as %q", got)
	}
	for _, line := range strings.Split(h.body, "\n") {
		if width := ansi.StringWidth(line); width > innerWidth(h.width) {
			t.Fatalf("a body line is %d wide, past the card's %d: %q", width, innerWidth(h.width), line)
		}
	}
	press(t, s, h, tea.KeyMsg{Type: tea.KeyEsc})
	if h.popped {
		t.Fatal("esc on a message closed the card instead of going back to the list")
	}
	if got := view(s, h); !strings.Contains(got, "Waiting (1)") {
		t.Fatalf("esc did not go back to the list: %q", got)
	}
	press(t, s, h, tea.KeyMsg{Type: tea.KeyEsc})
	if !h.popped {
		t.Fatal("esc on the list did not close the card")
	}
}

func TestInboxClickPicksThenReads(t *testing.T) {
	s, root, h := openSurface(t, 120)
	s.Loaded(root.loads[0], sampleLog(), nil)
	lines := strings.Split(view(s, h), "\n")
	recent := -1
	for index, line := range lines {
		if strings.Contains(line, "tests pass") {
			recent = index
		}
	}
	s.Click(h, recent)
	if s.cursor != 1 || len(root.reads) != 0 {
		t.Fatalf("a first click left cursor %d reads %v", s.cursor, root.reads)
	}
	s.Click(h, recent)
	if len(root.reads) != 1 || root.reads[0] != 40 {
		t.Fatalf("a second click read %v", root.reads)
	}
}

func TestInboxFitsANarrowTerminal(t *testing.T) {
	s, root, h := openSurface(t, 30)
	s.Loaded(root.loads[0], sampleLog(), nil)
	view(s, h)
	if h.cardWidth > h.width {
		t.Fatalf("card is %d wide on a %d column terminal", h.cardWidth, h.width)
	}
	for _, line := range strings.Split(h.body, "\n") {
		if width := ansi.StringWidth(line); width > innerWidth(h.width) {
			t.Fatalf("a line is %d wide, past the card's %d: %q", width, innerWidth(h.width), ansi.Strip(line))
		}
	}
}

func TestSplitKeepsWaitingMessagesOutOfRecent(t *testing.T) {
	queued := []Entry{{ID: 2, State: "held"}}
	log := Split(queued, []Entry{{ID: 1, State: "answered"}, {ID: 2, State: "held"}, {ID: 3, State: "dropped"}})
	if len(log.Queued) != 1 || len(log.Recent) != 2 || log.Recent[0].ID != 1 || log.Recent[1].ID != 3 {
		t.Fatalf("split = %+v", log)
	}
}

func TestInboxRefreshWaitsForTheLoadInFlight(t *testing.T) {
	s, root, _ := openSurface(t, 100)
	for range 5 {
		s.Refresh()
	}
	if len(root.loads) != 1 {
		t.Fatalf("five refreshes during the first load asked for %d loads, want 1", len(root.loads))
	}
	s.Loaded(root.loads[0], sampleLog(), nil)
	s.Refresh()
	if len(root.loads) != 2 {
		t.Fatalf("a refresh after the load landed asked for %d loads in all, want 2", len(root.loads))
	}
}

func TestInboxCursorStaysOnTheMessageAReloadMoves(t *testing.T) {
	s, root, h := openSurface(t, 100)
	s.Loaded(root.loads[0], sampleLog(), nil)
	moved := sampleLog()
	delivered := moved.Queued[0]
	delivered.State, delivered.Reason, delivered.Position = "delivered", "", 0
	moved.Queued = []Entry{{ID: 43, From: "reviewer", To: "worker", SentAt: noon, Subject: "next task", State: "queued", Position: 1}}
	moved.Recent = append(moved.Recent, delivered)
	s.Refresh()
	s.Loaded(root.loads[len(root.loads)-1], moved, nil)
	press(t, s, h, tea.KeyMsg{Type: tea.KeyEnter})
	if len(root.reads) != 1 || root.reads[0] != 42 {
		t.Fatalf("enter after the reload read %v, want the message the cursor was on (42)", root.reads)
	}
}

func TestInboxWrapsALongHoldReasonSoItsCommandStaysVisible(t *testing.T) {
	for _, width := range []int{30, 60, 100} {
		s, root, h := openSurface(t, width)
		log := sampleLog()
		log.Queued[0].Reason = "session f9baee84 is not running, so nothing will type this in; revive it with agent-manager revive"
		s.Loaded(root.loads[0], log, nil)
		shown := view(s, h)
		joined := strings.Join(strings.Fields(shown), " ")
		if !strings.Contains(joined, "revive it with agent-manager revive") {
			t.Fatalf("at %d columns the hold reason lost its command:\n%s", width, shown)
		}
		for _, line := range strings.Split(h.body, "\n") {
			if lineWidth := ansi.StringWidth(line); lineWidth > innerWidth(h.width) {
				t.Fatalf("at %d columns a line is %d wide: %q", width, lineWidth, ansi.Strip(line))
			}
		}
	}
}
