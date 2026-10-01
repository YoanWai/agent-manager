package execution

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/keybind"
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
		SessionKeys: keybind.DefaultSession(),
		ListKeys:    keybind.DefaultList(),
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
	command, env, err := launch.Environment(p.hooks, "ready-tool", tool, plan.Command, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.driver.Create(id, t.TempDir(), command, env, 80, 24); err != nil {
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
	holder, err := p.store.ClaimPoller(p.driver.SocketPath(), now, 2*time.Second)
	if err != nil || holder != p.driver.SocketPath() {
		t.Fatalf("pre-stamp = %q, %v", holder, err)
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

	res = p.runnerB.Step()
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
