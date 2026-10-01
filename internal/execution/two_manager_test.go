package execution

import (
	"database/sql"
	"fmt"
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

func TestStaleClaimFromABlockedManagerIsRetiredNotRedelivered(t *testing.T) {
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
	now := time.Now()
	claimed, err := p.store.ClaimMessage(id, now)
	if err != nil || !claimed {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	rawSQL(t, p.dbPath, fmt.Sprintf("UPDATE session_inbox SET claimed_at = %d WHERE id = %d;", now.Add(-31*time.Second).UnixNano(), id))

	res = p.runnerB.Step()
	if res.Err == nil || !strings.Contains(res.Err.Error(), "dropped an unconfirmed message") {
		t.Fatalf("B step = %v, want the stale claim retired with a drop report", res.Err)
	}
	state, err := p.store.Message(id, "sender01")
	if err != nil {
		t.Fatal(err)
	}
	if state.DroppedAt.IsZero() || state.DeliveredAt.IsZero() {
		t.Fatalf("retired message = %+v, want drop and terminal marks", state)
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
