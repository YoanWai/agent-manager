package watch

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/testenv"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// A serving control client owns an admitted command even when tmux rejects it:
// the caller may use a fallback only while no client is available.
func TestFocusWatchForwardDistinguishesUnavailableFromAdmitted(t *testing.T) {
	skipControlMode(t)
	driver := requireDriver(t)
	watch := newTestWatcher(driver, func(any) {})
	t.Cleanup(watch.Close)
	if available, err := watch.Forward("display-message -p"); available || err != nil {
		t.Fatalf("unserved forward = available %v, err %v", available, err)
	}

	id := "forward" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, os.TempDir(), "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	watch.SetFocus(id)
	deadline := time.Now().Add(5 * time.Second)
	for !watch.Serving(id) {
		if time.Now().After(deadline) {
			t.Fatal("control client never served the session")
		}
		time.Sleep(10 * time.Millisecond)
	}

	command := `display-message -p -t ` + tmux.PaneTarget(id) + ` "#{session_name}"`
	if available, err := watch.Forward(command); !available || err != nil {
		t.Fatalf("served forward = available %v, err %v", available, err)
	}
	if available, err := watch.Forward("not-a-real-tmux-command"); !available || err == nil {
		t.Fatalf("rejected forward = available %v, err %v", available, err)
	}
}

// The pane writes an invisible update before each echo, as Codex, Muse and
// Oh My Pi do.
func TestFocusWatchEchoesAQuietPaneAtOnce(t *testing.T) {
	driver, id, arrivals, _ := startFocusArrivalWatch(t)
	echo := `bash -c 'echo $((6*7))ready; while IFS= read -rsn1 key; do printf "\033[?25h"; sleep 0.01; printf "<%s>" "$key"; done'`
	if err := driver.SendText(id, echo); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	waitFocusArrival(t, arrivals, "42ready")

	// Use the fastest attempt to tolerate scheduler delays on busy hosts.
	fastest := time.Hour
	for attempt := range 5 {
		time.Sleep(2 * frameBudget)
		for len(arrivals) > 0 {
			<-arrivals
		}
		key := string(rune('a' + attempt))
		token := "<" + key + ">"
		if err := driver.SendRaw("send-keys -t " + tmux.PaneTarget(id) + " -l " + key); err != nil {
			t.Fatalf("send-keys: %v", err)
		}
		sent := time.Now()
		got := waitFocusArrival(t, arrivals, token)
		fastest = min(fastest, got.at.Sub(sent))
	}
	if fastest >= frameBudget/2 {
		t.Fatalf("fastest echo took %v, want under %v", fastest, frameBudget/2)
	}
}

func TestFocusWatchSpacesAStreamByTheFrameBudget(t *testing.T) {
	driver, id, sends, _ := startFocusArrivalWatch(t)

	waitFocusArrival(t, sends, "")

	if err := driver.SendText(id, "for i in $(seq 1 300); do echo line$i; sleep 0.005; done"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	waitFocusArrival(t, sends, "line1")
	var changes []time.Time
	shown := ""
	collect := time.After(time.Second)
	for collecting := true; collecting; {
		select {
		case got := <-sends:
			if got.preview != shown {
				shown = got.preview
				changes = append(changes, got.at)
			}
		case <-collect:
			collecting = false
		}
	}
	if len(changes) < 5 {
		t.Fatalf("stream changed the preview %d times in a second, want at least 5", len(changes))
	}
	for index := 1; index < len(changes); index++ {
		if gap := changes[index].Sub(changes[index-1]); gap < frameBudget {
			t.Fatalf("previews %d and %d came %v apart, want at least %v", index-1, index, gap, frameBudget)
		}
	}
}

func TestFocusWatchSpacesUnchangedWritesByADisplayFrame(t *testing.T) {
	driver, id, sends, _ := startFocusArrivalWatch(t)

	waitFocusArrival(t, sends, "")

	spam := `clear; echo $((6*7))spam; bash -c 'for i in $(seq 1 1500); do printf "\033[?25h"; sleep 0.001; done'`
	if err := driver.SendText(id, spam); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	waitFocusArrival(t, sends, "42spam")
	var stamps []time.Time
	collect := time.After(time.Second)
	for collecting := true; collecting; {
		select {
		case got := <-sends:
			stamps = append(stamps, got.at)
		case <-collect:
			collecting = false
		}
	}
	if len(stamps) < 5 {
		t.Fatalf("writing pane produced %d previews in a second, want at least 5", len(stamps))
	}
	for index := 1; index < len(stamps); index++ {
		if gap := stamps[index].Sub(stamps[index-1]); gap < captureGap {
			t.Fatalf("previews %d and %d came %v apart, want at least %v", index-1, index, gap, captureGap)
		}
	}
}

// Refocusing another session stops the old watcher and serves the new one.
func TestFocusWatchRefocus(t *testing.T) {
	driver := requireDriver(t)
	stamp := strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	first, second := "fw1"+stamp, "fw2"+stamp
	for _, id := range []string{first, second} {
		if err := driver.Create(id, os.TempDir(), "", nil, 80, 24); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	t.Cleanup(func() { driver.Kill(first); driver.Kill(second) })

	var mu sync.Mutex
	var got []Frame
	watch := newTestWatcher(driver, func(msg any) {
		if preview, ok := msg.(Frame); ok {
			mu.Lock()
			got = append(got, preview)
			mu.Unlock()
		}
	})
	t.Cleanup(watch.Close)

	watch.SetFocus(first)
	watch.SetFocus(second)
	watch.Close()

	mu.Lock()
	defer mu.Unlock()
	for _, preview := range got {
		if preview.SessionID != first && preview.SessionID != second {
			t.Fatalf("preview for unknown session %q", preview.SessionID)
		}
	}
}

type focusArrival struct {
	at      time.Time
	preview string
}

func startFocusArrivalWatch(t *testing.T) (*tmux.Driver, string, <-chan focusArrival, *Watcher) {
	t.Helper()
	skipControlMode(t)
	driver := requireDriver(t)
	id := "timing" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	arrivals := make(chan focusArrival, 1024)
	watch := newTestWatcher(driver, func(msg any) {
		if preview, ok := msg.(Frame); ok && preview.SessionID == id {
			arrivals <- focusArrival{time.Now(), preview.Preview}
		}
	})
	t.Cleanup(watch.Close)
	watch.SetFocus(id)
	return driver, id, arrivals, watch
}

func TestFocusWatchEchoesRepeatedInputBeforeTheFrameBudget(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			driver, id, arrivals, watch := startFocusArrivalWatch(t)
			background := ""
			if stream {
				background = `while :; do printf "\033[s\033[1;1H%05d\033[u" "$((++i))"; sleep 0.004; done & `
			}
			echo := `bash -c '` + background + `echo $((6*7))ready; while IFS= read -rsn1 key; do printf "\033[?25h"; sleep 0.025; printf "<%s>" "$key"; done'`
			if err := driver.SendText(id, echo); err != nil {
				t.Fatal(err)
			}
			waitFocusArrival(t, arrivals, "42ready")
			var echoes []time.Duration
			for attempt := range 12 {
				key := string(rune('a' + attempt))
				watch.InputSent(id)
				if available, err := watch.Forward("send-keys -t " + tmux.PaneTarget(id) + " -l " + key); !available || err != nil {
					t.Fatal("control client did not accept input")
				}
				sent := time.Now()
				got := waitFocusArrival(t, arrivals, "<"+key+">")
				echoes = append(echoes, got.at.Sub(sent))
			}
			slices.Sort(echoes)
			if median := echoes[len(echoes)/2]; median >= 3*frameBudget/4 {
				t.Fatalf("median echo took %v, want under %v", median, 3*frameBudget/4)
			}
		})
	}
}

func TestFocusWatchWaitsForPaneOutputAfterInput(t *testing.T) {
	driver, id, arrivals, watch := startFocusArrivalWatch(t)
	if err := driver.SendText(id, `stty -echo; echo $((6*7))ready; exec sleep 10`); err != nil {
		t.Fatal(err)
	}
	waitFocusArrival(t, arrivals, "42ready")
	time.Sleep(2 * frameBudget)
	for len(arrivals) > 0 {
		<-arrivals
	}
	if available, err := watch.Forward("send-keys -t " + tmux.PaneTarget(id) + " -l a"); !available || err != nil {
		t.Fatal("control client did not accept input")
	}
	watch.InputSent(id)
	select {
	case got := <-arrivals:
		t.Fatalf("input without pane output produced a preview: %q", got.preview)
	case <-time.After(2 * frameBudget):
	}
}

func TestFocusWatchBoundsInputCapturesAndRestoresTheStreamBudget(t *testing.T) {
	driver, id, arrivals, watch := startFocusArrivalWatch(t)
	if err := driver.SendText(id, "for i in $(seq 1 800); do echo line$i; sleep 0.004; done"); err != nil {
		t.Fatal(err)
	}
	waitFocusArrival(t, arrivals, "line1")
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		until := time.After(500 * time.Millisecond)
		for {
			select {
			case <-ticker.C:
				watch.InputSent(id)
			case <-until:
				return
			}
		}
	}()
	var stamps []time.Time
	for collecting := true; collecting; {
		select {
		case got := <-arrivals:
			stamps = append(stamps, got.at)
		case <-inputDone:
			collecting = false
		}
	}
	if len(stamps) < 10 {
		t.Fatalf("input produced %d previews, want at least 10", len(stamps))
	}
	for i := 1; i < len(stamps); i++ {
		if gap := stamps[i].Sub(stamps[i-1]); gap < captureGap {
			t.Fatalf("input captures came %v apart, want at least %v", gap, captureGap)
		}
	}
	time.Sleep(3 * frameBudget)
	for len(arrivals) > 0 {
		<-arrivals
	}
	previous := waitFocusArrival(t, arrivals, "line")
	for range 5 {
		got := waitFocusArrival(t, arrivals, "line")
		if gap := got.at.Sub(previous.at); gap < frameBudget {
			t.Fatalf("stream captures after input came %v apart, want at least %v", gap, frameBudget)
		}
		previous = got
	}
}

func waitFocusArrival(t *testing.T, arrivals <-chan focusArrival, contains string) focusArrival {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-arrivals:
			if strings.Contains(got.preview, contains) {
				return got
			}
		case <-deadline:
			t.Fatalf("no preview containing %q", contains)
		}
	}
}

// Regression: the UI loop calls setFocus from Update while the watcher may
// be blocked inside send (bubbletea's Send blocks until the same UI loop
// drains it). Refocusing must not wait on the watcher or the two deadlock.
func TestFocusWatchRefocusWhileSendBlocked(t *testing.T) {
	driver := requireDriver(t)
	id := "fwb" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, os.TempDir(), "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	// Unbuffered and never drained until after setFocus, exactly like the
	// bubbletea msgs channel while Update is running.
	msgs := make(chan any)
	watch := newTestWatcher(driver, func(msg any) { msgs <- msg })

	sending := make(chan struct{}, 1)
	go func() {
		// Wait for the watcher's seed capture to block in send.
		time.Sleep(200 * time.Millisecond)
		sending <- struct{}{}
	}()
	watch.SetFocus(id)
	<-sending

	done := make(chan struct{})
	go func() {
		watch.SetFocus("")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("setFocus deadlocked against a blocked send")
	}
	// Drain so the watcher goroutine can finish its blocked send and exit.
	go func() {
		for range msgs {
		}
	}()
	watch.Close()
}

// A session whose control client cannot open goes into backoff instead of
// costing one tmux fork per poll pass; a deliberate act lifts the pause.
func TestFocusWatchBacksOffAfterFailure(t *testing.T) {
	requireDriver(t)
	// A private socket: the failed attach spins up a transient tmux server
	// that is mid-exit moments later, and on the shared socket that dying
	// server takes the next test's fresh session down with it.
	driver, err := tmux.NewWithSocket(testSocket + "backoff")
	if err != nil {
		t.Fatalf("NewWithSocket: %v", err)
	}
	id := "gone" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")

	watch := newTestWatcher(driver, func(any) {})
	t.Cleanup(watch.Close)

	// The session does not exist, so the attach fails and the watcher
	// releases its claim, marking the id failed.
	watch.SetFocus(id)
	deadline := time.After(5 * time.Second)
	for watch.Watching() == id {
		select {
		case <-deadline:
			t.Fatal("failed watcher never released its claim")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// The next selection sync must not reopen inside the backoff window.
	failedAt := func() time.Time {
		watch.mu.Lock()
		defer watch.mu.Unlock()
		return watch.failedAt
	}
	firstFailure := failedAt()
	watch.SetFocus(id)
	if watch.Watching() == id {
		t.Fatal("failed session reopened without backoff")
	}

	// A deliberate focus lifts the pause: the retry spawns, fails against
	// the still-missing session, and stamps a fresh failure. The claim
	// itself may already be released again, so the new stamp is the
	// race-free evidence the attach was attempted.
	watch.RetryNow()
	watch.SetFocus(id)
	deadline = time.After(5 * time.Second)
	for !failedAt().After(firstFailure) {
		select {
		case <-deadline:
			t.Fatal("retryNow did not lift the backoff")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// The watcher must deliver real cursor coordinates, not just a preview:
// an unquoted tmux format silently answers with tmux's default status
// message instead, which is exactly how the cursor went missing.
func TestFocusWatchReportsCursor(t *testing.T) {
	driver := requireDriver(t)
	id := "cur" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, os.TempDir(), "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	msgs := make(chan any, 64)
	watch := newTestWatcher(driver, func(msg any) { msgs <- msg })
	t.Cleanup(watch.Close)
	watch.SetFocus(id)

	// Put the cursor somewhere unambiguous: a prompt line plus typed text
	// moves it off column zero.
	if err := driver.SendText(id, "echo cursor-probe"); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-msgs:
			preview, ok := msg.(Frame)
			if !ok || preview.SessionID != id {
				continue
			}
			if !preview.CursorOK {
				continue
			}
			if preview.CursorY < 0 || preview.CursorX < 0 {
				t.Fatalf("nonsense cursor: %d,%d", preview.CursorX, preview.CursorY)
			}
			if preview.CursorY == 0 && preview.CursorX == 0 {
				// Still on the first cell; keep waiting for the shell to draw.
				continue
			}
			return
		case <-deadline:
			t.Fatal("no Frame ever carried a cursor position")
		}
	}
}

func TestFocusWatchHonorsHiddenCursor(t *testing.T) {
	if !tmux.ReportsCursorFlag {
		t.Skip(tmux.Binary + " does not report whether the cursor shows")
	}
	testenv.SkipPOSIXShell(t)
	driver := requireDriver(t)
	id := "hidecur" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	// Reset the pane first: shell startup output may leave its cursor below
	// row zero, which made this exact-coordinate assertion host-dependent.
	command := "printf '\\033[H\\033[2J\\033[?25lhidden-cursor'; exec sleep 10"
	if err := driver.Create(id, os.TempDir(), command, nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	msgs := make(chan any, 64)
	watch := newTestWatcher(driver, func(msg any) { msgs <- msg })
	t.Cleanup(watch.Close)
	watch.SetFocus(id)

	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-msgs:
			preview, ok := msg.(Frame)
			if !ok || preview.SessionID != id || !strings.Contains(preview.Preview, "hidden-cursor") {
				continue
			}
			if !preview.PaneStateOK || preview.CursorOK {
				continue
			}
			if preview.CursorX != len("hidden-cursor") || preview.CursorY != 0 {
				t.Fatalf("hidden cursor position = %d,%d", preview.CursorX, preview.CursorY)
			}
			return
		case <-deadline:
			t.Fatal("no hidden-cursor preview reported the cursor hidden")
		}
	}
}

func TestApplyPaneState(t *testing.T) {
	var msg Frame
	applyPaneState(&msg, "12,34,1,010,250,0,1,0\n")
	if !msg.PaneStateOK || !msg.CursorOK || msg.CursorX != 12 || msg.CursorY != 34 {
		t.Fatalf("pane state = %+v", msg)
	}
	if !msg.Mouse {
		t.Fatal("mouse flag 1 not read as pane-owned mouse")
	}
	if msg.History != 250 {
		t.Fatalf("historySize = %d, want 250", msg.History)
	}
	if msg.Motion {
		t.Fatal("a button-tracking pane read as tracking all motion")
	}

	// tmux reports 1003 in mouse_all_flag, the apps that want a pointer
	// move with every event.
	var motion Frame
	applyPaneState(&motion, "0,0,1,100,0,1,1,0")
	if !motion.PaneStateOK || !motion.Mouse || !motion.Motion {
		t.Fatalf("all-motion pane state = %+v", motion)
	}

	var plain Frame
	applyPaneState(&plain, "0,0,1,000,0,0,0,0")
	if !plain.PaneStateOK || plain.Mouse || plain.Motion || plain.History != 0 || !plain.CursorOK {
		t.Fatalf("plain pane state = %+v", plain)
	}

	// A multiplexer that never reports the flag leaves the position as the
	// caret: hiding it on a constant 0 would hide every caret.
	var hidden Frame
	applyPaneState(&hidden, "7,8,0,000,0,0,0,0")
	if !hidden.PaneStateOK || hidden.CursorOK != !tmux.ReportsCursorFlag || hidden.CursorX != 7 || hidden.CursorY != 8 {
		t.Fatalf("hidden cursor pane state = %+v, want its coordinates kept", hidden)
	}

	// tmux answers an unquoted format with its default status message;
	// that must never read as pane state.
	var bogus Frame
	applyPaneState(&bogus, "[am_x] 0:zsh, current pane 0 - (00:43)")
	if bogus.PaneStateOK || bogus.CursorOK || bogus.Mouse || bogus.History != 0 {
		t.Fatal("applyPaneState accepted tmux's default message")
	}
	applyPaneState(&bogus, "nonsense")
	if bogus.PaneStateOK || bogus.CursorOK {
		t.Fatal("applyPaneState accepted junk")
	}

	var badCursor Frame
	applyPaneState(&badCursor, "12,y,1,010,250,0,1,0")
	if badCursor.PaneStateOK || badCursor.CursorOK {
		t.Fatal("applyPaneState accepted a non-numeric cursor")
	}
	var badHistory Frame
	applyPaneState(&badHistory, "12,34,1,010,n,0,1,0")
	if badHistory.PaneStateOK || badHistory.History != 0 {
		t.Fatal("applyPaneState accepted a non-numeric history size")
	}
	var alternate Frame
	applyPaneState(&alternate, "0,0,1,000,250,0,0,1")
	if !alternate.PaneStateOK || !alternate.Alt {
		t.Fatalf("alternate-screen flag lost: %+v", alternate)
	}
}

// Trailing blank pane rows are content: dropping them shifts every line
// up, which is what made the pushed and polled captures disagree. The
// input is shaped like the control client's reply, a newline JOIN of the
// captured rows with no terminator, so a 3-row pane whose last two rows
// are blank arrives as "top line\n\n"; the old trim ate the final blank
// row, and a caret parked on the pane's bottom row pointed past the rows.
func TestControlCaptureKeepsTrailingBlankRows(t *testing.T) {
	pane := "top line\n\n"
	if got, want := MatchExecShape(pane), "top line\n\n\n"; got != want {
		t.Fatalf("MatchExecShape(%q) = %q, want %q", pane, got, want)
	}
	if got := len(strings.Split(strings.TrimSuffix(MatchExecShape(pane), "\n"), "\n")); got != 3 {
		t.Fatalf("kept %d rows, want 3", got)
	}
	// A pane whose last row carries text gains only the terminator.
	if got, want := MatchExecShape("a\nb"), "a\nb\n"; got != want {
		t.Fatalf("MatchExecShape(%q) = %q, want %q", "a\nb", got, want)
	}
}

// A session whose terminal is already gone is an expected state, not a
// failure: selecting its row stays quiet and leaves the id unclaimed so a
// later revive opens a fresh client.
func TestFocusWatchSkipsMissingSession(t *testing.T) {
	driver := requireDriver(t)
	id := "missing" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	msgs := make(chan any, 8)
	watch := newTestWatcher(driver, func(msg any) { msgs <- msg })
	t.Cleanup(watch.Close)

	watch.SetFocus(id)
	deadline := time.After(2 * time.Second)
	for {
		watch.mu.Lock()
		claimed, failed := watch.id, watch.failedID
		watch.mu.Unlock()
		if claimed == "" && failed == id {
			break
		}
		select {
		case msg := <-msgs:
			t.Fatalf("missing session produced a notification: %#v", msg)
		case <-deadline:
			t.Fatalf("watcher still claims %q, backed off %q", claimed, failed)
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case msg := <-msgs:
		t.Fatalf("missing session produced a notification: %#v", msg)
	default:
	}
}

// Only a write to the pane a watcher polls is counted, and the watcher
// hears it land: the frame it takes next carries the count, so the caret
// can be trusted once the frame shows every counted write.
func TestFocusWatchCountsWritesToThePolledPane(t *testing.T) {
	watch := newTestWatcher(nil, func(any) {})
	if watch.CountWrite("s1") {
		t.Fatal("a write was counted with no watcher polling the pane")
	}
	input := make(chan struct{}, 1)
	watch.mu.Lock()
	watch.id, watch.polled, watch.input = "s1", true, input
	watch.mu.Unlock()
	if watch.CountWrite("s2") {
		t.Fatal("a write to another pane was counted")
	}
	if !watch.CountWrite("s1") {
		t.Fatal("a write to the polled pane was not counted")
	}
	if watch.CaughtUp(0) {
		t.Fatal("a frame taken before the write landed was caught up")
	}
	watch.WriteLanded("s1")
	select {
	case <-input:
	default:
		t.Fatal("the watcher was not woken to look for the echo")
	}
	watch.mu.Lock()
	landed := watch.landed
	watch.mu.Unlock()
	if !watch.CaughtUp(landed) {
		t.Fatal("a frame taken once the write landed was not caught up")
	}
}
