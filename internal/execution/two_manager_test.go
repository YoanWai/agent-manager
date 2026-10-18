package execution

import (
	"database/sql"
	"fmt"
	"github.com/YoanWai/agent-manager/internal/testenv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"

	"github.com/google/uuid"
)

type twoManagers struct {
	store   *store.Store
	driver  *tmux.Driver
	cfg     config.Config
	engine  *status.Engine
	hooks   *hooks.Manager
	gitDrv  *git.Driver
	dbPath  string
	runnerA *Runner
	runnerB *Runner
}

func pair(t *testing.T, sameSocket bool) *twoManagers {
	t.Helper()
	cfg := config.Config{
		Tools: map[string]config.Tool{
			"ready-tool": {
				Command:        `sh -c 'printf "❯ "; while IFS= read -r line; do printf "\n❯ "; done'`,
				DefaultStatus:  status.Idle,
				ActivityCutoff: "(?m)^❯",
			},
		},
	}
	dbPath := filepath.Join(t.TempDir(), "state.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := tmux.NewWithSocket(testSocket)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := status.NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	hooksM := hooks.NewManager(t.TempDir())
	gitDrv, _ := git.New()
	p := &twoManagers{store: st, driver: driver, cfg: cfg, engine: engine, hooks: hooksM, gitDrv: gitDrv, dbPath: dbPath}
	p.runnerA = New(Dependencies{Store: st, TMux: driver, Engine: engine, Hooks: hooksM, Git: gitDrv}, OptionsFromConfig(cfg))
	if sameSocket {
		p.runnerB = New(Dependencies{Store: st, TMux: driver, Engine: engine, Hooks: hooksM, Git: gitDrv}, OptionsFromConfig(cfg))
	} else {
		other, err := tmux.NewWithSocket("amexectest-b")
		if err != nil {
			t.Fatal(err)
		}
		p.runnerB = New(Dependencies{Store: st, TMux: other, Engine: engine, Hooks: hooksM, Git: gitDrv}, OptionsFromConfig(cfg))
	}
	t.Cleanup(func() {
		for _, sess := range listAll(t, st) {
			driver.Kill(sess.ID)
		}
		st.Close()
	})
	return p
}

func listAll(t *testing.T, st *store.Store) []store.Session {
	t.Helper()
	sessions, err := st.ListSessions(true)
	if err != nil {
		t.Fatal(err)
	}
	return sessions
}

func rawSQL(t *testing.T, dbPath, query string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("fixture SQL: %v", err)
	}
}

func (p *twoManagers) ageHeartbeat(t *testing.T, to time.Time) {
	t.Helper()
	rawSQL(t, p.dbPath, fmt.Sprintf("UPDATE settings SET value = %d WHERE key = 'poller_heartbeat';", to.UnixNano()))
}

func (p *twoManagers) spawnReady(t *testing.T) store.Session {
	t.Helper()
	id := uuid.NewString()[:8]
	tool := p.cfg.Tools["ready-tool"]
	plan := launch.Assemble("ready-tool", tool, "", false, false)
	cwd := t.TempDir()
	command, env, err := launch.Environment(p.hooks, "ready-tool", tool, plan.Command, id, cwd, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.driver.Create(id, cwd, command, env, 80, 24); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	sess := store.Session{
		ID: id, Name: "worker", Tool: "ready-tool", Cwd: t.TempDir(),
		Status: status.Starting, CreatedAt: now, LastStatusAt: now,
		TmuxSocket: p.driver.SocketPath(),
	}
	if err := p.store.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	return sess
}

func TestSecondManagerTakesOverOnlyWhenTheFirstAges(t *testing.T) {
	p := pair(t, false)
	legacy := store.Session{ID: "legacysess", Name: "legacy", Tool: "ready-tool", Cwd: t.TempDir(), Status: status.Working}
	if err := p.store.CreateSession(legacy); err != nil {
		t.Fatal(err)
	}
	if err := p.store.SetTmuxSocket(legacy.ID, ""); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	claim, err := p.store.ClaimPoller(p.driver.SocketPath(), now)
	if err != nil || claim.Holder != p.driver.SocketPath() {
		t.Fatalf("pre-stamp = %q, %v", claim.Holder, err)
	}
	p.runnerA.heartbeatAt = now

	p.ageHeartbeat(t, now.Add(-40*time.Second))
	res := p.runnerB.Step()
	if res.Err != nil {
		t.Fatalf("B first step: %v", res.Err)
	}
	if !res.Snapshot.LeadingManager {
		t.Fatal("B took the step but reads itself as not leading")
	}
	got, err := p.store.Get(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != status.Dead {
		t.Fatalf("legacy session = %q, want the new leading manager to speak for it (dead)", got.Status)
	}

	p.runnerA.heartbeatAt = time.Time{}
	res = p.runnerA.Step()
	if res.Err != nil {
		t.Fatalf("A return step: %v", res.Err)
	}
	if res.Snapshot.LeadingManager {
		t.Fatal("A read itself as leading behind B's fresh stamp")
	}
	got, err = p.store.Get(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != status.Dead {
		t.Fatalf("fenced session = %q, want it left to the leading manager", got.Status)
	}

	p.ageHeartbeat(t, now.Add(-40*time.Second))
	p.runnerA.heartbeatAt = time.Time{}
	res = p.runnerA.Step()
	if res.Err != nil {
		t.Fatalf("A reclaim step: %v", res.Err)
	}
	if !res.Snapshot.LeadingManager {
		t.Fatal("A never reclaimed after B's stamp aged")
	}
}

func TestOwnerlessClaimIsMarkedUncertainNotRedelivered(t *testing.T) {
	p := pair(t, true)
	sess := p.spawnReady(t)
	res := p.runnerA.Step()
	if res.Err != nil {
		t.Fatalf("A first step: %v", res.Err)
	}
	body := "drop me on purpose"
	id, err := p.store.Enqueue(store.InboxMessage{
		SessionID: sess.ID, SenderID: "sender01", SenderName: "payments-fix",
		Body: body, Fingerprint: body, SentAt: time.Now(),
	}, store.DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	claimInboxForTest(t, p.store, id, time.Now())

	// B delivers only once a poll reads the agent idle, which a slow psmux
	// pane can take a few ticks to show.
	deadline := time.Now().Add(10 * time.Second)
	for res = p.runnerB.Step(); res.Err == nil && time.Now().Before(deadline); res = p.runnerB.Step() {
		time.Sleep(100 * time.Millisecond)
	}
	if res.Err == nil || !strings.Contains(res.Err.Error(), "uncertain prior transport") {
		t.Fatalf("B step = %v, want the ownerless claim marked uncertain", res.Err)
	}
	state, err := p.store.Message(id, "sender01")
	if err != nil {
		t.Fatal(err)
	}
	if state.Outcome != store.DeliveryUncertain || state.DeliveredAt.IsZero() || !state.DroppedAt.IsZero() {
		t.Fatalf("retired message = %+v, want uncertain terminal marks", state)
	}
	pane, err := p.driver.CapturePane(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pane, body) {
		t.Fatalf("pane carries the retired message: %q", pane)
	}
	res = p.runnerA.Step()
	if res.Err != nil {
		t.Fatalf("A step after retirement: %v", res.Err)
	}
	pane, err = p.driver.CapturePane(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pane, body) {
		t.Fatalf("late tick typed the retired message: %q", pane)
	}
}

func TestDeliveryGuardKeepsPeerFromRetiringAnInFlightSend(t *testing.T) {
	testenv.SkipPOSIXShell(t)
	p := pair(t, true)
	sess := p.spawnReady(t)
	deadline := time.Now().Add(5 * time.Second)
	var pane string
	for {
		var err error
		pane, err = p.driver.CapturePane(sess.ID)
		if err == nil && strings.Contains(pane, "❯") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture prompt not ready: %q, %v", pane, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	body := "inflight-retirement-probe"
	id, err := p.store.Enqueue(store.InboxMessage{
		SessionID: sess.ID, SenderID: "sender01", SenderName: "race-probe",
		Body: body, Fingerprint: body, SentAt: time.Now(),
	}, store.DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := store.Open(p.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { peer.Close() })
	p.runnerB.store = peer
	realTMux, err := exec.LookPath("tmux")
	if err != nil {
		t.Fatal(err)
	}
	fixture := t.TempDir()
	blocked := filepath.Join(fixture, "blocked")
	release := filepath.Join(fixture, "release")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nfor arg do\nif [ \"$arg\" = load-buffer ]; then\n" +
		": > " + quote(blocked) + "\n" +
		"n=0\nwhile [ ! -f " + quote(release) + " ]; do\n" +
		"n=$((n+1))\n[ \"$n\" -lt 1000 ] || exit 99\nsleep 0.01\ndone\nfi\ndone\n" +
		"exec " + quote(realTMux) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(fixture, "tmux"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fixture+string(os.PathListSeparator)+os.Getenv("PATH"))
	p.runnerA.tmux, err = tmux.NewWithSocket(testSocket)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		sent bool
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		sent, err := p.runnerA.maybeDeliverInbox(sess, pane, status.Idle, true)
		done <- outcome{sent, err}
	}()
	joined := false
	t.Cleanup(func() {
		os.WriteFile(release, nil, 0o600)
		if !joined {
			select {
			case <-done:
			case <-time.After(12 * time.Second):
				t.Error("blocked delivery did not stop")
			}
		}
	})
	deadline = time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(blocked); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("delivery did not reach the post-claim paste boundary")
		}
		time.Sleep(10 * time.Millisecond)
	}
	claim, err := peer.Message(id, "sender01")
	if err != nil || claim.ClaimedAt.IsZero() {
		t.Fatalf("blocked send has no durable claim: %+v, %v", claim, err)
	}
	if sent, err := p.runnerB.maybeDeliverInbox(sess, pane, status.Idle, true); sent || err != nil {
		t.Fatalf("peer delivery while guard held = %v, %v", sent, err)
	}
	inflight, err := peer.Message(id, "sender01")
	if err != nil || inflight.Outcome != store.DeliveryInFlight || !inflight.DeliveredAt.IsZero() {
		t.Fatalf("peer changed in-flight claim = %+v, %v", inflight, err)
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var result outcome
	select {
	case result = <-done:
		joined = true
	case <-time.After(12 * time.Second):
		t.Fatal("released delivery did not finish")
	}
	final, err := peer.Message(id, "sender01")
	if err != nil || final.Outcome != store.DeliveryConfirmed || final.DeliveredAt.IsZero() || !final.DroppedAt.IsZero() {
		t.Fatalf("owner did not record confirmed delivery: %+v err=%v", final, err)
	}
	after, err := p.driver.CapturePane(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(after, body) || !result.sent || result.err != nil {
		t.Fatalf("delivery result: pasted=%v sent=%v err=%v", strings.Contains(after, body), result.sent, result.err)
	}
}

// olderHeartbeat stamps the store the way a manager older than the
// delivery guard does (v0.42.0 internal/store/inbox.go): the socket and
// the heartbeat alone, which is how this binary tells it is there.
func (p *twoManagers) olderHeartbeat(t *testing.T, socket string, at time.Time) {
	t.Helper()
	rawSQL(t, p.dbPath, fmt.Sprintf(
		"INSERT INTO settings (key, value) VALUES ('poller_socket', '%s'), ('poller_heartbeat', '%d') ON CONFLICT(key) DO UPDATE SET value = excluded.value;",
		socket, at.UnixNano()))
}

// An older manager claims with no token and retires any claim it reads as
// abandoned, so two deliverers on one server would type twice or tell a
// sender its message dropped mid-paste. While one stamps the heartbeat,
// the queue is its; once it stops, this manager takes the queue over.
func TestNewerManagerDefersDeliveryWhileAnOlderManagerStamps(t *testing.T) {
	p := pair(t, true)
	sess := p.spawnReady(t)
	body := "wait for the older manager"
	id, err := p.store.Enqueue(store.InboxMessage{
		SessionID: sess.ID, SenderID: "sender01", SenderName: "payments-fix",
		Body: body, Fingerprint: body, SentAt: time.Now(),
	}, store.DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	p.olderHeartbeat(t, p.driver.SocketPath(), time.Now())

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if res := p.runnerA.Step(); res.Err != nil {
			t.Fatalf("step beside an older manager: %v", res.Err)
		}
		if p.runnerA.ownsDelivery {
			t.Fatal("manager read delivery as its own beside a fresh older stamp")
		}
		time.Sleep(100 * time.Millisecond)
	}
	state, err := p.store.Message(id, "sender01")
	if err != nil || !state.ClaimedAt.IsZero() || !state.DeliveredAt.IsZero() {
		t.Fatalf("message touched while deferring: %+v, %v", state, err)
	}
	pane, err := p.driver.CapturePane(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pane, body) {
		t.Fatalf("deferring manager typed the message:\n%s", pane)
	}

	// The older manager stops: its last stamp ages past the window.
	rawSQL(t, p.dbPath, fmt.Sprintf("UPDATE settings SET value = '%d' WHERE key = 'legacy_poller_heartbeat';",
		time.Now().Add(-store.PollerHeartbeatStale-time.Second).UnixNano()))
	p.runnerA.heartbeatAt = time.Time{}
	deadline = time.Now().Add(10 * time.Second)
	for {
		res := p.runnerA.Step()
		if res.Err != nil {
			t.Fatalf("step after the older manager stopped: %v", res.Err)
		}
		state, err = p.store.Message(id, "sender01")
		if err != nil {
			t.Fatal(err)
		}
		if !state.DeliveredAt.IsZero() || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !p.runnerA.ownsDelivery {
		t.Fatal("manager never took delivery over")
	}
	if state.Outcome != store.DeliveryConfirmed || state.AttemptToken == "" || !state.DroppedAt.IsZero() {
		t.Fatalf("delivery after takeover = %+v, want a confirmed, tokened receipt", state)
	}
	pane, err = p.driver.CapturePane(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(pane, body) != 1 {
		t.Fatalf("message typed %d times:\n%s", strings.Count(pane, body), pane)
	}
}

// The claim that reads ownership runs once per PollerHeartbeatPeriod. An
// older manager that starts or wakes between two claims stamps and types
// on its first step, so a delivery reads the heartbeat again under the
// guard rather than trusting a snapshot up to a period old.
func TestNewerManagerDefersWhenAnOlderManagerWakesBetweenClaims(t *testing.T) {
	p := pair(t, true)
	sess := p.spawnReady(t)
	if res := p.runnerA.Step(); res.Err != nil {
		t.Fatal(res.Err)
	}
	if !p.runnerA.ownsDelivery {
		t.Fatal("manager alone did not own delivery")
	}
	body := "typed by whoever is awake"
	id, err := p.store.Enqueue(store.InboxMessage{
		SessionID: sess.ID, SenderID: "sender01", SenderName: "payments-fix",
		Body: body, Fingerprint: body, SentAt: time.Now(),
	}, store.DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	// The older manager stamps while this manager's next claim is not due.
	p.olderHeartbeat(t, p.driver.SocketPath(), time.Now())
	p.runnerA.heartbeatAt = time.Now()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if res := p.runnerA.Step(); res.Err != nil {
			t.Fatalf("step after the older manager woke: %v", res.Err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if p.runnerA.ownsDelivery {
		t.Fatal("manager kept delivery on a snapshot older than the stamp")
	}
	state, err := p.store.Message(id, "sender01")
	if err != nil || !state.ClaimedAt.IsZero() || !state.DeliveredAt.IsZero() {
		t.Fatalf("message touched on a stale ownership snapshot: %+v, %v", state, err)
	}
	pane, err := p.driver.CapturePane(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pane, body) {
		t.Fatalf("manager typed on a stale ownership snapshot:\n%s", pane)
	}
}

// A claim with no token is an older manager's, and that manager holds no
// lock while it pastes. The claim gets the grace its own peers give it,
// then retires as uncertain; the text is never typed by this manager.
func TestOlderManagerClaimGetsItsGraceThenRetiresUncertain(t *testing.T) {
	p := pair(t, true)
	sess := p.spawnReady(t)
	pane := settledReadyPane(t, p, sess.ID)
	body := "older manager mid-paste"
	id, err := p.store.Enqueue(store.InboxMessage{
		SessionID: sess.ID, SenderID: "sender01", SenderName: "payments-fix",
		Body: body, Fingerprint: body, SentAt: time.Now(),
	}, store.DefaultInboxLimits)
	if err != nil {
		t.Fatal(err)
	}
	rawSQL(t, p.dbPath, fmt.Sprintf("UPDATE session_inbox SET claimed_at = %d WHERE id = %d AND claimed_at = 0 AND delivered_at = 0;",
		time.Now().UnixNano(), id))

	if sent, err := p.runnerA.maybeDeliverInbox(sess, pane, status.Idle, true); sent || err != nil {
		t.Fatalf("fresh older claim: sent=%v err=%v, want it left alone", sent, err)
	}
	state, err := p.store.Message(id, "sender01")
	if err != nil || !state.DeliveredAt.IsZero() || state.AttemptToken != "" {
		t.Fatalf("fresh older claim touched: %+v, %v", state, err)
	}

	rawSQL(t, p.dbPath, fmt.Sprintf("UPDATE session_inbox SET claimed_at = %d WHERE id = %d;",
		time.Now().Add(-inboxClaimGrace-time.Second).UnixNano(), id))
	_, err = p.runnerA.maybeDeliverInbox(sess, pane, status.Idle, true)
	if err == nil || !strings.Contains(err.Error(), "uncertain prior transport") {
		t.Fatalf("aged older claim = %v, want it retired as uncertain", err)
	}
	state, err = p.store.Message(id, "sender01")
	if err != nil || state.Outcome != store.DeliveryUncertain || state.DeliveredAt.IsZero() || !state.DroppedAt.IsZero() {
		t.Fatalf("retired older claim = %+v, %v", state, err)
	}
	if sent, err := p.runnerA.maybeDeliverInbox(sess, pane, status.Idle, true); sent || err != nil {
		t.Fatalf("retired claim revisited: sent=%v err=%v", sent, err)
	}
	after, err := p.driver.CapturePane(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(after, body) {
		t.Fatalf("older manager's claim was retyped:\n%s", after)
	}
}

func settledReadyPane(t *testing.T, p *twoManagers, id string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := p.driver.CapturePane(id)
		if err == nil && strings.Contains(pane, "❯") {
			return pane
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture prompt not ready: %q, %v", pane, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
