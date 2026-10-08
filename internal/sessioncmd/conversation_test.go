package sessioncmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/conversation"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

func conversationStore(t *testing.T) (string, *store.Store) {
	t.Helper()
	configDir := t.TempDir()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return configDir, st
}

func createConversationRow(t *testing.T, st *store.Store, id string) {
	t.Helper()
	if err := st.CreateSession(store.Session{ID: id, Name: id, Tool: "pi", Cwd: t.TempDir(), Status: status.Idle}); err != nil {
		t.Fatal(err)
	}
}

func storedConversation(t *testing.T, st *store.Store, id string) string {
	t.Helper()
	sess, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return sess.AgentSessionID
}

// runningAs makes this test process the hook its launch's agent ran, which
// is the test's parent.
func runningAs(t *testing.T, row string, launch int64) {
	t.Helper()
	t.Setenv(hooks.EnvSessionID, row)
	t.Setenv(hooks.EnvLaunch, strconv.FormatInt(launch, 10))
	t.Setenv(hooks.EnvAgentPID, strconv.Itoa(os.Getppid()))
}

func TestTrackConversationTakesTheIDOrAHookInputField(t *testing.T) {
	configDir, st := conversationStore(t)
	createConversationRow(t, st, "abcd1234")
	runningAs(t, "abcd1234", 0)

	if err := TrackConversation(configDir, []string{"--tool", "pi", "--id", "conv-one"}, strings.NewReader("")); err != nil {
		t.Fatalf("--id: %v", err)
	}
	if got := storedConversation(t, st, "abcd1234"); got != "conv-one" {
		t.Fatalf("after --id the row is on %q", got)
	}
	input := `{"session_id":"conv-two","source":"clear","cwd":"/repo"}`
	if err := TrackConversation(configDir, []string{"--tool", "pi", "--key", "session_id"}, strings.NewReader(input)); err != nil {
		t.Fatalf("--key: %v", err)
	}
	if got := storedConversation(t, st, "abcd1234"); got != "conv-two" {
		t.Fatalf("after --key the row is on %q", got)
	}
	for _, args := range [][]string{
		{"--id", "x"},
		{"--tool", "pi"},
		{"--tool", "pi", "--id", "x", "--key", "session_id"},
	} {
		if err := TrackConversation(configDir, args, strings.NewReader("")); err == nil {
			t.Fatalf("%v was accepted", args)
		}
	}
	if err := TrackConversation(configDir, []string{"--tool", "pi", "--key", "conversationId"}, strings.NewReader(input)); err == nil {
		t.Fatal("hook input without the field was accepted")
	}
}

// The id goes on to reach a revive's command line, so anything but a plain
// token is refused before it is stored.
func TestTrackConversationRefusesAnIDThatIsNotAPlainToken(t *testing.T) {
	configDir, st := conversationStore(t)
	createConversationRow(t, st, "abcd1234")
	runningAs(t, "abcd1234", 0)
	for _, id := range []string{
		"abc; touch pwned",
		`abc'; touch pwned; echo '`,
		"abc$(touch pwned)",
		"abc\ntouch pwned",
		"../../etc/passwd",
		"-rf",
	} {
		if err := TrackConversation(configDir, []string{"--tool", "pi", "--id", id}, strings.NewReader("")); err == nil {
			t.Errorf("%q was accepted", id)
		}
	}
	if got := storedConversation(t, st, "abcd1234"); got != "" {
		t.Fatalf("a refused id was stored: %q", got)
	}
}

// A global hook runs in every session of its CLI, and a pane an earlier
// release launched carries no launch: both keep today's behavior quietly.
func TestTrackConversationOutsideAStampedLaunchWritesNothing(t *testing.T) {
	configDir, st := conversationStore(t)
	createConversationRow(t, st, "abcd1234")
	for _, env := range []struct{ session, launch string }{{"", "0"}, {"abcd1234", ""}} {
		t.Setenv(hooks.EnvSessionID, env.session)
		t.Setenv(hooks.EnvLaunch, env.launch)
		if err := TrackConversation(configDir, []string{"--tool", "pi", "--key", "session_id"}, strings.NewReader(`{"session_id":"conv"}`)); err != nil {
			t.Fatalf("env %+v: %v", env, err)
		}
		if got := storedConversation(t, st, "abcd1234"); got != "" {
			t.Fatalf("env %+v stored %q", env, got)
		}
	}
}

// A teammate, a CLI the agent ran as a tool and one typed into the pane by
// hand carry the session's environment too, but only the launched agent
// reports.
func TestTrackConversationFromAnotherProcessWritesNothing(t *testing.T) {
	configDir, st := conversationStore(t)
	createConversationRow(t, st, "abcd1234")
	runningAs(t, "abcd1234", 0)
	for _, agentPID := range []string{strconv.Itoa(os.Getpid()), ""} {
		t.Setenv(hooks.EnvAgentPID, agentPID)
		if err := TrackConversation(configDir, []string{"--tool", "pi", "--id", "child", "--spawned"}, strings.NewReader("")); err != nil {
			t.Fatalf("agent pid %q: a report from another process is no failure: %v", agentPID, err)
		}
		if got := storedConversation(t, st, "abcd1234"); got != "" {
			t.Fatalf("agent pid %q: row is on %q, want it untouched", agentPID, got)
		}
	}
}

// A spawn creates its row once the pane is up, so the agent's first report
// can come first.
func TestTrackConversationWaitsForTheRowToLand(t *testing.T) {
	configDir, st := conversationStore(t)
	runningAs(t, "abcd1234", 0)
	created := make(chan error, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		created <- st.CreateSession(store.Session{ID: "abcd1234", Name: "late", Tool: "pi", Cwd: "/tmp", Status: status.Idle})
	}()
	if err := TrackConversation(configDir, []string{"--tool", "pi", "--id", "conv"}, strings.NewReader("")); err != nil {
		t.Fatalf("TrackConversation: %v", err)
	}
	if err := <-created; err != nil {
		t.Fatal(err)
	}
	if got := storedConversation(t, st, "abcd1234"); got != "conv" {
		t.Fatalf("row is on %q, want conv", got)
	}
}

func TestReportConversationGivesUpOnALaunchThatNeverLands(t *testing.T) {
	_, st := conversationStore(t)
	createConversationRow(t, st, "abcd1234")
	err := reportConversation(st, "abcd1234", "pi", "conv", 42, 200*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "has no launch 42") {
		t.Fatalf("err = %v, want the launch named", err)
	}
}

func TestTrackConversationFromALaunchARevivedReplacedChangesNothing(t *testing.T) {
	configDir, st := conversationStore(t)
	createConversationRow(t, st, "abcd1234")
	runningAs(t, "abcd1234", 0)
	if err := TrackConversation(configDir, []string{"--tool", "pi", "--id", "before"}, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgentLaunchedAt("abcd1234", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := TrackConversation(configDir, []string{"--tool", "pi", "--id", "late"}, strings.NewReader("")); err != nil {
		t.Fatalf("a stale report is no failure: %v", err)
	}
	if got := storedConversation(t, st, "abcd1234"); got != "before" {
		t.Fatalf("row is on %q, want before", got)
	}
}

// hermesRow is a live Hermes row in a fresh HERMES_HOME, whose terminal is
// /dev/ttys029.
func hermesRow(t *testing.T) (*store.Store, *hooks.Manager, store.Session, string) {
	t.Helper()
	configDir, st := conversationStore(t)
	hermesHome := t.TempDir()
	t.Setenv("HERMES_HOME", hermesHome)
	if err := st.CreateSession(store.Session{ID: "abcd1234", Name: "h", Tool: "hermes", Cwd: t.TempDir(), Status: status.Idle, AgentSessionID: "20261008_005433_0c8868"}); err != nil {
		t.Fatal(err)
	}
	sess, err := st.Get("abcd1234")
	if err != nil {
		t.Fatal(err)
	}
	manager := hooks.NewManager(configDir)
	if err := os.MkdirAll(manager.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	return st, manager, sess, hermesHome
}

func writeHermesCrumb(t *testing.T, hermesHome, cwd, id string, written time.Time) {
	t.Helper()
	crumb := filepath.Join(hermesHome, "terminal-sessions", "tty-dev-ttys029")
	if err := os.MkdirAll(filepath.Dir(crumb), 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf(`{"session_id": %q, "cwd": %q, "ts": %.6f}`, id, cwd, float64(written.UnixNano())/1e9)
	if err := os.WriteFile(crumb, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Hermes names a /branch or /resume only in the crumb it writes as it
// closes, so a revive reads it once the shell that started the agent has
// marked it ended.
func TestSettleConversationReadsHermesOnceItHasClosed(t *testing.T) {
	st, manager, sess, hermesHome := hermesRow(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	agentFile := manager.AgentFile(sess.ID)
	shell := exec.Command("sh", "-c", `sh -c "echo \$\$ > `+pidFile+`; exec sleep 60"; `+conversation.EndedCommand(agentFile))
	if err := shell.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		shell.Wait()
		close(exited)
	}()
	agentPID := readPIDFile(t, pidFile)
	t.Cleanup(func() {
		syscall.Kill(agentPID, syscall.SIGKILL)
		<-exited
	})
	if err := os.WriteFile(agentFile, []byte(strconv.Itoa(agentPID)+" 0 /dev/ttys029\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeHermesCrumb(t, hermesHome, sess.Cwd, "20261008_005341_0d388a", time.Now())

	settled, err := SettleConversation(st, manager, sess, "hermes", false, 0)
	if err != nil || settled.AgentSessionID != "20261008_005433_0c8868" {
		t.Fatalf("while the agent runs = %q, %v; want the row left alone", settled.AgentSessionID, err)
	}
	if settled, err := SettleConversation(st, manager, sess, "grok", false, 0); err != nil || settled.AgentSessionID != sess.AgentSessionID {
		t.Fatalf("another style = %q, %v", settled.AgentSessionID, err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		syscall.Kill(agentPID, syscall.SIGKILL)
	}()
	settled, err = SettleConversation(st, manager, sess, "hermes", false, closingAgentWait)
	if err != nil || settled.AgentSessionID != "20261008_005341_0d388a" || storedConversation(t, st, "abcd1234") != "20261008_005341_0d388a" {
		t.Fatalf("once closed = %q, %v; want the conversation it closed on", settled.AgentSessionID, err)
	}
}

// After the agent quits, its pane holds a shell again, and a Hermes typed
// into it by hand writes the same terminal's crumb. Only the crumb written
// by the time the agent's own shell marked it ended counts.
func TestSettleConversationIgnoresAHermesTypedIntoThePaneAfterTheAgent(t *testing.T) {
	st, manager, sess, hermesHome := hermesRow(t)
	agentFile := manager.AgentFile(sess.ID)
	if err := os.WriteFile(agentFile, []byte("99999 0 /dev/ttys029\nended\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ended := time.Now().Add(time.Minute)
	if err := os.Chtimes(agentFile, ended, ended); err != nil {
		t.Fatal(err)
	}
	writeHermesCrumb(t, hermesHome, sess.Cwd, "20261008_024330_09db2f", ended.Add(30*time.Second))
	settled, err := SettleConversation(st, manager, sess, "hermes", false, 0)
	if err != nil || settled.AgentSessionID != "20261008_005433_0c8868" || storedConversation(t, st, sess.ID) != "20261008_005433_0c8868" {
		t.Fatalf("hand-typed hermes = %q, %v; want the row left alone", settled.AgentSessionID, err)
	}
	writeHermesCrumb(t, hermesHome, sess.Cwd, "20261008_005341_0d388a", ended.Add(-100*time.Millisecond))
	settled, err = SettleConversation(st, manager, sess, "hermes", false, 0)
	if err != nil || settled.AgentSessionID != "20261008_005341_0d388a" {
		t.Fatalf("the agent's own close = %q, %v; want its conversation", settled.AgentSessionID, err)
	}
}

func readPIDFile(t *testing.T, path string) int {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		data, _ := os.ReadFile(path)
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			return pid
		}
	}
	t.Fatalf("no pid in %s", path)
	return 0
}
