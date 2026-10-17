package execution

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/notify"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

// waitForCalls blocks until n notifications have landed. Delivery runs off
// the refresh path, so assertions have to give it a moment.
func waitForCalls(t *testing.T, rec *notifyRecorder, n int) []notify.Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		calls := rec.all()
		if len(calls) >= n {
			return calls
		}
		if time.Now().After(deadline) {
			t.Fatalf("want %d notifications, got %v", n, calls)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newNotifyTestPoller(t *testing.T) (*Runner, store.Session, *notifyRecorder) {
	t.Helper()
	p, sess := newTestPollerWithSession(t)
	rec := &notifyRecorder{}
	p.notifyFn = rec.fn()
	p.commandFn = func(notify.Event, string, error) {}
	p.showCommandFn = func(string) {}
	return p, sess, rec
}

type commandCall struct {
	command string
	event   notify.Event
	readErr error
}

type commandRecorder struct {
	mu    sync.Mutex
	calls []commandCall
}

func (r *commandRecorder) fn() func(notify.Event, string, error) {
	return func(event notify.Event, command string, readErr error) {
		r.mu.Lock()
		r.calls = append(r.calls, commandCall{command, event, readErr})
		r.mu.Unlock()
	}
}

func (r *commandRecorder) all() []commandCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]commandCall(nil), r.calls...)
}

func waitForCommands(t *testing.T, rec *commandRecorder, n int) []commandCall {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		calls := rec.all()
		if len(calls) >= n {
			return calls
		}
		if time.Now().After(deadline) {
			t.Fatalf("want %d commands, got %v", n, calls)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newCommandTestPoller(t *testing.T, command string) (*Runner, store.Session, *notifyRecorder, *commandRecorder) {
	t.Helper()
	p, sess, rec := newNotifyTestPoller(t)
	commands := &commandRecorder{}
	p.commandFn = commands.fn()
	if err := p.store.SetSetting(notifyCommandSetting, command); err != nil {
		t.Fatal(err)
	}
	return p, sess, rec, commands
}

// The command is additive: the native banner still goes up, and the
// command gets the same event with the session's directory and branch.
func TestNotifyTransitionRunsTheCommandBesideTheBanner(t *testing.T) {
	p, sess, rec, commands := newCommandTestPoller(t, ` curl -d "$AM_BODY" ntfy.sh/topic `)
	sess.WorktreeBranch = "am/fix-login"
	p.notifyTransition(sess, status.Waiting)
	want := notify.Event{ID: sess.ID, Session: sess.Name, Tool: sess.Tool, Kind: notify.Waiting, Dir: sess.Cwd, Branch: "am/fix-login"}
	if calls := waitForCalls(t, rec, 1); calls[0] != want {
		t.Fatalf("banner got %+v, want %+v", calls[0], want)
	}
	calls := waitForCommands(t, commands, 1)
	if calls[0].command != `curl -d "$AM_BODY" ntfy.sh/topic` || calls[0].event != want || calls[0].readErr != nil {
		t.Fatalf("command got %+v, want the trimmed command with %+v", calls[0], want)
	}
}

func TestNotifyTransitionWithoutACommandRunsNone(t *testing.T) {
	p, sess, rec, commands := newCommandTestPoller(t, "")
	p.notifyTransition(sess, status.Waiting)
	waitForCalls(t, rec, 1)
	settle()
	if calls := commands.all(); len(calls) != 0 {
		t.Fatalf("no command is set, got %v", calls)
	}
}

// A command the store cannot hand back is not taken for an unset one: the
// banner still goes up, and the read failure goes where command failures
// are logged.
func TestNotifyTransitionReportsAnUnreadableCommand(t *testing.T) {
	p, sess, rec, commands := newCommandTestPoller(t, "true")
	if err := p.store.Close(); err != nil {
		t.Fatal(err)
	}
	p.notifyTransition(sess, status.Waiting)
	waitForCalls(t, rec, 1)
	calls := waitForCommands(t, commands, 1)
	if calls[0].readErr == nil || calls[0].command != "" || calls[0].event.Kind != notify.Waiting {
		t.Fatalf("want the read error handed on for the waiting event, got %+v", calls[0])
	}
}

// The command follows the same toggles as the banner: off silences both,
// and finished needs the opt-in.
func TestNotifyCommandFollowsTheNotificationToggles(t *testing.T) {
	p, sess, _, commands := newCommandTestPoller(t, "true")
	p.notifyTransition(sess, status.Finished)
	settle()
	if calls := commands.all(); len(calls) != 0 {
		t.Fatalf("finished without the opt-in ran %v", calls)
	}
	if err := p.store.SetSetting(notifyFinishedSetting, "on"); err != nil {
		t.Fatal(err)
	}
	p.notifyTransition(sess, status.Finished)
	if calls := waitForCommands(t, commands, 1); calls[0].event.Kind != notify.Finished {
		t.Fatalf("want a finished command after the opt-in, got %v", calls)
	}
	if err := p.store.SetSetting(notificationsSetting, "off"); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{status.Waiting, status.Errored, status.Finished} {
		p.notifyTransition(sess, st)
	}
	settle()
	if calls := commands.all(); len(calls) != 1 {
		t.Fatalf("notifications off should silence the command too, got %v", calls)
	}
}

type deliveryLog struct {
	mu    sync.Mutex
	steps []string
}

func (l *deliveryLog) add(step string) {
	l.mu.Lock()
	l.steps = append(l.steps, step)
	l.mu.Unlock()
}

func (l *deliveryLog) wait(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		l.mu.Lock()
		steps := slices.Clone(l.steps)
		l.mu.Unlock()
		if len(steps) >= n {
			return steps
		}
		if time.Now().After(deadline) {
			t.Fatalf("want %d delivery steps, got %v", n, steps)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func loggedCommandPoller(t *testing.T, command string) (*Runner, store.Session, *deliveryLog) {
	t.Helper()
	p, sess, _, _ := newCommandTestPoller(t, command)
	log := &deliveryLog{}
	p.notifyFn = func(notify.Event) { log.add("banner") }
	p.showCommandFn = func(command string) { log.add("shown " + command) }
	p.commandFn = func(_ notify.Event, command string, _ error) { log.add("ran " + command) }
	return p, sess, log
}

// Nothing on screen lists the notify command, so a new one shows itself
// on a banner before its first run, and only then.
func TestANewNotifyCommandIsShownOnceBeforeItRuns(t *testing.T) {
	p, sess, log := loggedCommandPoller(t, " say done ")
	p.notifyTransition(sess, status.Waiting)
	log.wait(t, 3)
	p.notifyTransition(sess, status.Errored)
	want := []string{"banner", "shown say done", "ran say done", "banner", "ran say done"}
	if got := log.wait(t, 5); !slices.Equal(got, want) {
		t.Fatalf("deliveries = %v, want %v", got, want)
	}
}

// The banner follows the last command it showed, so a command swapped in
// and back again is shown each time it changes.
func TestAChangedNotifyCommandIsShownAgain(t *testing.T) {
	p, sess, log := loggedCommandPoller(t, "say one")
	steps := 0
	for _, command := range []string{"say one", "say two", "say one"} {
		if err := p.store.SetSetting(notifyCommandSetting, command); err != nil {
			t.Fatal(err)
		}
		p.notifyTransition(sess, status.Waiting)
		steps += 3
		log.wait(t, steps)
	}
	got := log.wait(t, steps)
	want := []string{
		"banner", "shown say one", "ran say one",
		"banner", "shown say two", "ran say two",
		"banner", "shown say one", "ran say one",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("deliveries = %v, want %v", got, want)
	}
}

// The shown command is stored, so another manager on the same store, or
// this one after a restart, does not show it again.
func TestAShownNotifyCommandStaysShownAcrossManagers(t *testing.T) {
	p, sess, log := loggedCommandPoller(t, "say done")
	p.notifyTransition(sess, status.Waiting)
	log.wait(t, 3)
	if (&Runner{store: p.store}).claimUnshownCommand("say done") {
		t.Fatal("a second manager would show a command the first already showed")
	}
}

func TestNoNotifyCommandShowsNoBanner(t *testing.T) {
	p, sess, log := loggedCommandPoller(t, "")
	p.notifyTransition(sess, status.Waiting)
	log.wait(t, 1)
	settle()
	if got := log.wait(t, 1); !slices.Equal(got, []string{"banner"}) {
		t.Fatalf("deliveries = %v, want the session banner alone", got)
	}
}

func TestNotifyTransitionFiresOnWaitingAndErrored(t *testing.T) {
	p, sess, rec := newNotifyTestPoller(t)
	p.notifyTransition(sess, status.Waiting)
	p.notifyTransition(sess, status.Errored)
	calls := waitForCalls(t, rec, 2)
	want := map[notify.Event]bool{
		{ID: sess.ID, Session: sess.Name, Tool: sess.Tool, Kind: notify.Waiting, Dir: sess.Cwd}: true,
		{ID: sess.ID, Session: sess.Name, Tool: sess.Tool, Kind: notify.Errored, Dir: sess.Cwd}: true,
	}
	for _, call := range calls {
		delete(want, call)
	}
	if len(want) != 0 {
		t.Fatalf("missing notifications %v, got %v", want, calls)
	}
}

func TestNotifyTransitionCarriesCustomToolName(t *testing.T) {
	p, sess, rec := newNotifyTestPoller(t)
	sess.Tool = "my-custom-agent"
	p.notifyTransition(sess, status.Waiting)
	calls := waitForCalls(t, rec, 1)
	if calls[0] != (notify.Event{ID: sess.ID, Session: sess.Name, Tool: "my-custom-agent", Kind: notify.Waiting, Dir: sess.Cwd}) {
		t.Fatalf("configured tool identity should reach the backend, got %v", calls)
	}
}

func TestNotifyTransitionSkipsUnattentionStatuses(t *testing.T) {
	p, sess, rec := newNotifyTestPoller(t)
	for _, st := range []string{status.Working, status.Idle, status.Dead, status.Starting} {
		p.notifyTransition(sess, st)
	}
	settle()
	if calls := rec.all(); len(calls) != 0 {
		t.Fatalf("no notification should fire for working/idle/dead/starting, got %v", calls)
	}
}

// Finished is routine for most turn ends, so it only pings when the user
// opted in.
func TestNotifyTransitionFinishedOptIn(t *testing.T) {
	p, sess, rec := newNotifyTestPoller(t)
	p.notifyTransition(sess, status.Finished)
	settle()
	if calls := rec.all(); len(calls) != 0 {
		t.Fatalf("finished should stay quiet by default, got %v", calls)
	}
	if err := p.store.SetSetting(notifyFinishedSetting, "on"); err != nil {
		t.Fatal(err)
	}
	p.notifyTransition(sess, status.Finished)
	calls := waitForCalls(t, rec, 1)
	if calls[0] != (notify.Event{ID: sess.ID, Session: sess.Name, Tool: sess.Tool, Kind: notify.Finished, Dir: sess.Cwd}) {
		t.Fatalf("want one finished notification after opt-in, got %v", calls)
	}
}

func TestNotifyTransitionSilencedBySetting(t *testing.T) {
	p, sess, rec := newNotifyTestPoller(t)
	if err := p.store.SetSetting(notificationsSetting, "off"); err != nil {
		t.Fatal(err)
	}
	p.notifyTransition(sess, status.Waiting)
	p.notifyTransition(sess, status.Errored)
	settle()
	if calls := rec.all(); len(calls) != 0 {
		t.Fatalf("notifications off should silence everything, got %v", calls)
	}
}

// The full path a waiting transition travels in the running app: hooks
// file → refreshOnce → store update → notification. Fires once; a status
// that stays waiting across polls never re-fires.
func TestRefreshNotifiesWaitingTransitionOnce(t *testing.T) {
	m := buildModel(t)
	hooked := m.cfg.Tools["claude-hooked"]
	// Windows' stand-in already ignores the --settings the launch appends.
	if runtime.GOOS != "windows" {
		hooked.Command = `sh -c 'exec cat' --`
	}
	m.cfg.Tools["claude-hooked"] = hooked
	if err := m.spawnSession("claude-hooked", "needy", t.TempDir(), "", "boot-marker", false, false); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, nil)
	sess := m.sessionRows()[0]
	waitForPane(t, m, sess.ID, "boot-marker")

	// Pin the pre-state: whatever earlier refreshes derived, the
	// transition into waiting is observed by the next refresh and only
	// there.
	if err := m.store.UpdateStatus(sess.ID, status.Idle); err != nil {
		t.Fatal(err)
	}

	rec := &notifyRecorder{}
	m.poller.notifyFn = rec.fn()

	statusFile := m.poller.hooks.StatusFile(sess.ID)
	if err := os.MkdirAll(filepath.Dir(statusFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statusFile, []byte(status.Waiting), 0o644); err != nil {
		t.Fatal(err)
	}

	m.applyCmd(t, m.refreshCmd())
	calls := waitForCalls(t, rec, 1)
	if calls[0] != (notify.Event{ID: sess.ID, Session: "needy", Tool: "claude-hooked", Kind: notify.Waiting, Dir: sess.Cwd}) {
		t.Fatalf("want one waiting notification titled with the session name, got %v", calls)
	}

	m.applyCmd(t, m.refreshCmd())
	settle()
	if calls := rec.all(); len(calls) != 1 {
		t.Fatalf("a steady waiting status should not re-fire, got %v", calls)
	}
}

// Every manager on the server derives the same transition from the same
// pane. The command runs from the notification, behind the same check on
// whether this manager's write moved the stored status, so a second
// manager polling the same pane runs nothing. The race where both read
// the old status is the store's to settle, and its tests cover it.
func TestNotifyCommandRunsOnceAcrossManagers(t *testing.T) {
	m := buildModel(t)
	hooked := m.cfg.Tools["claude-hooked"]
	hooked.Command = `sh -c 'exec cat' --`
	m.cfg.Tools["claude-hooked"] = hooked
	createSessionOn(t, m, "needy", "claude-hooked", t.TempDir())
	sess := m.sessionRows()[0]
	waitForPane(t, m, sess.ID, "boot-marker")
	if err := m.store.UpdateStatus(sess.ID, status.Idle); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetSetting(notifyCommandSetting, "true"); err != nil {
		t.Fatal(err)
	}
	second := New(Dependencies{Store: m.store, TMux: m.tmux, Engine: m.poller.engine, Hooks: m.poller.hooks, Git: m.poller.gitDrv}, OptionsFromConfig(m.cfg))
	commands := &commandRecorder{}
	for _, p := range []*Runner{m.poller, second} {
		p.notifyFn = func(notify.Event) {}
		p.commandFn = commands.fn()
	}
	statusFile := m.poller.hooks.StatusFile(sess.ID)
	if err := os.MkdirAll(filepath.Dir(statusFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statusFile, []byte(status.Waiting), 0o644); err != nil {
		t.Fatal(err)
	}
	m.poller.Step()
	second.Step()
	waitForCommands(t, commands, 1)
	settle()
	if calls := commands.all(); len(calls) != 1 {
		t.Fatalf("two managers ran the command %d times: %v", len(calls), calls)
	}
}
