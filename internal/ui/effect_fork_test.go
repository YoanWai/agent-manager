package ui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

// forkFileTool points the source's fork command at a file, so a test can
// read exactly what the launched fork received without reading a pane.
func forkFileTool(t *testing.T, m *Model, name string) string {
	t.Helper()
	argsFile := filepath.Join(t.TempDir(), "fork-args")
	tool := m.services.cfg.Tools[name]
	tool.ForkCommand = "printf '%s\\n' {id} {new_id} {name} > " + tmux.ShellQuote(argsFile) + "; cat"
	m.services.cfg.Tools[name] = tool
	return argsFile
}

func submitForkDialog(t *testing.T, m *Model, name string) (*Model, tea.Cmd) {
	t.Helper()
	m.openFork()
	if m.mode != modeFork {
		t.Fatalf("fork dialog did not open: err=%q", m.errBar.text)
	}
	m.fork.name.SetValue(name)
	updated, cmd := m.handleForkKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	return m, cmd
}

func forkRow(m *Model, name string) (store.Session, bool) {
	for _, sess := range m.sessionRows() {
		if sess.Name == name {
			return sess, true
		}
	}
	return store.Session{}, false
}

// The dialog submit must not read the store, resolve files, type keys or
// launch: only a queued, frozen request may follow it.
func TestForkSubmitDefersWorkOffTheUpdatePath(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	argsFile := forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m, cmd := submitForkDialog(t, m, "child fork")
	if m.mode != modeFork {
		t.Fatalf("after submit: mode = %v, want the dialog to stay open while work runs", m.mode)
	}
	if _, ok := forkRow(m, "child fork"); ok {
		t.Fatal("submit launched the fork on the update path")
	}
	if _, err := os.Stat(argsFile); err == nil {
		t.Fatal("submit ran the fork command on the update path")
	}
	if m.effects.active == nil {
		t.Fatal("no active effect, want the queued fork request")
	}
	if _, ok := m.effects.active.request.(forkRequest); !ok {
		t.Fatalf("active effect = %T, want a forkRequest", m.effects.active.request)
	}

	m.applyCmd(t, cmd)
	if m.mode != modeList || m.errBar.text != "" {
		t.Fatalf("after completion: mode=%v err=%q", m.mode, m.errBar.text)
	}
	if forked, ok := forkRow(m, "child fork"); !ok || forked.AgentSessionID == "" || forked.AgentSessionID == source.AgentSessionID {
		t.Fatalf("forked row = %+v", forked)
	}
}

// A completion must apply the durable fork row without replacing a newer
// dialog, moving the selection, or clearing its status text.
func TestForkCompletionDoesNotStealNewerDialog(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	for _, name := range []string{"alpha", "beta"} {
		m.selectSessionRow(t, name)
		sess := railSelectedSession(m)
		if err := m.services.store.SetAgentSessionID(sess.ID, sess.Name+"-conversation"); err != nil {
			t.Fatal(err)
		}
		for i := range m.workspace.sessions {
			if m.workspace.sessions[i].ID == sess.ID {
				m.workspace.sessions[i].AgentSessionID = sess.Name + "-conversation"
			}
		}
		m.rebuildRows()
		m.selectSessionRow(t, name)
	}

	m.selectSessionRow(t, "alpha")
	alpha := railSelectedSession(m)
	m, alphaCmd := submitForkDialog(t, m, "alpha fork")
	if m.fork.source.ID != alpha.ID {
		t.Fatalf("fork submitted from %q, want alpha %q", m.fork.source.ID, alpha.ID)
	}
	m.handleForkKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.selectSessionRow(t, "beta")
	beta := railSelectedSession(m)
	m.openFork()
	if m.mode != modeFork || m.fork.source.ID != beta.ID {
		t.Fatalf("newer dialog = %v / %q, want beta's fork dialog", m.mode, m.fork.source.ID)
	}

	m.applyCmd(t, alphaCmd)
	if m.mode != modeFork || m.fork.source.ID != beta.ID {
		t.Fatalf("completion replaced the newer dialog: mode=%v source=%q", m.mode, m.fork.source.ID)
	}
	if got := railSelectedSession(m); got.ID != beta.ID {
		t.Fatalf("completion moved the selection to %q, want beta kept", got.Name)
	}
	if m.errBar.text != "" {
		t.Fatalf("completion clobbered status text: %q", m.errBar.text)
	}
	if _, ok := forkRow(m, "alpha fork"); !ok {
		t.Fatal("durable fork row not applied after the newer dialog opened")
	}
}

// A window update must return while the fork worker is blocked mid-launch.
func TestForkUpdateReturnsWhileWorkerIsBlocked(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "conv-1"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "conv-1"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")
	tool := m.services.cfg.Tools["claude"]
	tool.SessionStore = "gemini"
	tool.ForkCommand = "cat {session_file}; cat"
	m.services.cfg.Tools["claude"] = tool

	started := make(chan struct{})
	release := make(chan struct{})
	var signal sync.Once
	original := forkSessionFileResolver
	forkSessionFileResolver = func(sessionStore, id string) (string, error) {
		signal.Do(func() { close(started) })
		<-release
		return "/fork/session.jsonl", nil
	}
	defer func() {
		forkSessionFileResolver = original
	}()

	m, cmd := submitForkDialog(t, m, "file fork")
	completed := make(chan tea.Msg, 1)
	go func() { completed <- cmd() }()
	<-started
	updated := make(chan struct{})
	go func() {
		m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
		close(updated)
	}()
	select {
	case <-updated:
	case <-time.After(time.Second):
		close(release)
		<-completed
		t.Fatal("window update waited on the running fork")
	}
	close(release)
	m.applyTestMsg(t, <-completed)
	m.drainEffects(t)
	if m.mode != modeList || m.errBar.text != "" {
		t.Fatalf("after completion: mode=%v err=%q", m.mode, m.errBar.text)
	}
	if _, ok := forkRow(m, "file fork"); !ok {
		t.Fatal("fork row missing after the blocked worker completed")
	}
}

// The frozen request must own copies of the source's mutable fields.
func TestForkRequestDeepCopiesMutableSourceFields(t *testing.T) {
	m := &Model{}
	source := store.Session{ID: "captured", PendingInputs: []string{"first"}, RelaunchSnapshot: map[string]int64{"conversation": 1}}
	m.enqueueEffect(forkRequest{source: source, name: "child"}, 0, false)
	source.PendingInputs[0] = "changed"
	source.RelaunchSnapshot["conversation"] = 2
	request := m.effects.pending[0].request.(forkRequest)
	if request.source.PendingInputs[0] != "first" || request.source.RelaunchSnapshot["conversation"] != 1 {
		t.Fatal("request retained mutable source aliases")
	}
}

// A fork accepted before quit drains to a durable row; a fork queued after
// quit is refused, and a late command after StopEffects touches nothing.
func TestForkQuitDrainsAcceptedJobAndRefusesLate(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m, _ = submitForkDialog(t, m, "drained fork")
	m.requestQuit()
	m.drainEffects(t)
	if _, ok := forkRow(m, "drained fork"); !ok {
		t.Fatal("quit discarded the accepted fork")
	}
	if len(m.effects.pending) != 0 {
		t.Fatal("quit accepted new user work")
	}
	if _, ok := m.nextEffectCmd()().(tea.QuitMsg); !ok {
		t.Fatal("drained queue did not quit")
	}

	m2 := buildModel(t)
	createSession(t, m2, "late", t.TempDir(), "")
	forkFileTool(t, m2, "claude")
	m2.selectSessionRow(t, "late")
	late := railSelectedSession(m2)
	if err := m2.services.store.SetAgentSessionID(late.ID, "late-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m2.workspace.sessions {
		if m2.workspace.sessions[i].ID == late.ID {
			m2.workspace.sessions[i].AgentSessionID = "late-conversation"
		}
	}
	m2.rebuildRows()
	m2.selectSessionRow(t, "late")
	m2.openFork()
	m2.fork.name.SetValue("late fork")
	_, lateCommand := m2.handleForkKey(tea.KeyMsg{Type: tea.KeyEnter})
	m2.StopEffects()
	msg := lateCommand().(effectCompletedMsg)
	if msg.err != errEffectsStopped {
		t.Fatalf("late command error=%v", msg.err)
	}
	if _, ok := forkRow(m2, "late fork"); ok {
		t.Fatal("late command launched the fork")
	}
}

// Running a fork command twice and delivering its completion twice must
// produce exactly one durable row.
func TestForkDuplicateCompletionIsIdempotent(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m, cmd := submitForkDialog(t, m, "twin fork")
	msg := cmd()
	cmd()
	m.applyTestMsg(t, msg)
	m.applyTestMsg(t, msg)
	rows := 0
	for _, sess := range m.sessionRows() {
		if sess.Name == "twin fork" {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("fork rows = %d, want one durable row", rows)
	}
	if m.mode != modeList || m.errBar.text != "" {
		t.Fatalf("after duplicate completion: mode=%v err=%q", m.mode, m.errBar.text)
	}
}

// A launch refusal must keep the captured request for the install's retry,
// not a closure that re-reads model state.
func TestForkLaunchFailureRetainsCapturedRequest(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")
	tool := m.services.cfg.Tools["claude"]
	tool.ForkCommand = "definitely-missing-fork-cli {id}"
	m.services.cfg.Tools["claude"] = tool

	m, cmd := submitForkDialog(t, m, "failed fork")
	m.applyCmd(t, cmd)
	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want the launch hint dialog", m.mode)
	}
	request, ok := m.launchHint.fix.effectRetry.(forkRequest)
	if !ok {
		t.Fatal("launch hint holds no captured fork request to re-dispatch")
	}
	if request.source.ID != source.ID || request.name != "failed fork" {
		t.Fatalf("captured request = %+v, want the frozen source and name", request)
	}
	if _, ok := forkRow(m, "failed fork"); ok {
		t.Fatal("refused launch left a durable row")
	}
}

// The worker re-reads the source by manager id, so a directory that died
// after the dialog opened is refused at execution time, not submit time.
func TestForkRefusesASourceDirectoryThatDied(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "source", dir, "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m, cmd := submitForkDialog(t, m, "gone fork")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, cmd)
	if !strings.Contains(m.errBar.text, "working directory no longer exists") {
		t.Fatalf("err = %q, want the stale directory refusal", m.errBar.text)
	}
	if _, ok := forkRow(m, "gone fork"); ok {
		t.Fatal("fork launched into a missing directory")
	}
}

// A source that restarts while the fork is queued mints a new conversation;
// the re-read validates liveness, and the changed captured identity is
// refused instead of forking the replacement conversation.
func TestForkRefusesASourceThatRestartedWhileQueued(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m, cmd := submitForkDialog(t, m, "restarted fork")
	if err := m.services.store.SetAgentSessionID(source.ID, "replacement-conversation"); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, cmd)
	if !strings.Contains(m.errBar.text, "changed while the fork was in the queue") {
		t.Fatalf("err = %q, want the captured-identity refusal", m.errBar.text)
	}
	if m.mode != modeFork {
		t.Fatalf("mode = %v, want the dialog left open to resubmit", m.mode)
	}
	if _, ok := forkRow(m, "restarted fork"); ok {
		t.Fatal("fork launched from a restarted source")
	}
}

// Reopening a fork on the same source mints a new dialog; an in-flight
// completion of the older one must apply its durable row without closing
// the newer dialog.
func TestForkCompletionAfterSameSourceReopenKeepsNewerDialog(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m, firstCmd := submitForkDialog(t, m, "first fork")
	firstGen := m.fork.gen
	m.handleForkKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.selectSessionRow(t, "source")
	m.openFork()
	if m.fork.gen == firstGen {
		t.Fatal("reopen did not mint a new dialog generation")
	}
	if m.fork.source.ID != source.ID {
		t.Fatalf("reopened dialog = %q, want the same source %q", m.fork.source.ID, source.ID)
	}

	m.applyCmd(t, firstCmd)
	if m.mode != modeFork || m.fork.gen == firstGen {
		t.Fatalf("completion closed the reopened dialog: mode=%v gen=%d", m.mode, m.fork.gen)
	}
	if m.errBar.text != "" {
		t.Fatalf("completion clobbered status text: %q", m.errBar.text)
	}
	if got := railSelectedSession(m); got.ID != source.ID {
		t.Fatalf("completion moved the selection to %q, want %q kept", got.Name, source.Name)
	}
	if _, ok := forkRow(m, "first fork"); !ok {
		t.Fatal("durable fork row not applied after the same-source reopen")
	}
}

// A launch failure after the source's own fork was recorded must not retype
// the fork keys on retry: the recorded fork is reused, the source is not
// forked a second time.
func TestForkKeysRetryDoesNotForkSourceTwice(t *testing.T) {
	m, source, argsFile := forkInSourceModel(t)
	_ = argsFile
	tool := m.services.cfg.Tools[source.Tool]
	tool.ForkCommand = "definitely-missing-fork-cli {new_id}"
	m.services.cfg.Tools[source.Tool] = tool

	writeOnce := "fork-conversation"
	writeTwice := "second-fork"
	driver := m.services.tmux
	stop := make(chan struct{})
	done := make(chan struct{})
	defer func() { close(stop); <-done }()
	go func() {
		defer close(done)
		typed := 0
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			select {
			case <-stop:
				return
			default:
			}
			pane, err := driver.CapturePane(source.ID)
			if err != nil {
				continue
			}
			if n := strings.Count(pane, "/fork"); n > typed {
				typed = n
				switch n {
				case 1:
					_ = writeMuseFork(writeOnce, source.AgentSessionID)
				case 2:
					_ = writeMuseFork(writeTwice, source.AgentSessionID)
				}
			}
		}
	}()

	m.openFork()
	m.fork.name.SetValue("child fork")
	updated, cmd := m.handleForkKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.applyCmd(t, cmd)
	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want the launch hint dialog for the missing binary", m.mode)
	}
	request, ok := m.launchHint.fix.effectRetry.(forkRequest)
	if !ok || request.knownForkID != writeOnce {
		t.Fatalf("captured retry = %+v, want it to reuse the recorded fork %q", request, writeOnce)
	}

	// The binary is "installed"; the retry must launch from the recorded
	// fork without typing the source's fork keys a second time.
	tool = m.services.cfg.Tools[source.Tool]
	tool.ForkCommand = "true {new_id}; cat"
	m.services.cfg.Tools[source.Tool] = tool
	m.enqueueEffect(request, 0, false)
	m.drainEffects(t)

	var forked store.Session
	for _, sess := range m.sessionRows() {
		if sess.Name == "child fork" {
			forked = sess
		}
	}
	if forked.ID == "" {
		t.Fatalf("retry did not launch the fork: err=%q", m.errBar.text)
	}
	if forked.ID != request.childID {
		t.Fatalf("retry changed child identity: %s != %s", forked.ID, request.childID)
	}
	if forked.AgentSessionID != writeOnce {
		t.Fatalf("forked conversation = %q, want the recorded %q; the keys were typed again", forked.AgentSessionID, writeOnce)
	}
}

// A fork must stay starting, which attention excludes, so a completed
// launch clears the status filter the way the keyboard path did.
func TestForkCompletionClearsStatusFilter(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "source", t.TempDir(), "")
	forkFileTool(t, m, "claude")
	m.selectSessionRow(t, "source")
	source := railSelectedSession(m)
	if err := m.services.store.SetAgentSessionID(source.ID, "source-conversation"); err != nil {
		t.Fatal(err)
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == source.ID {
			m.workspace.sessions[i].AgentSessionID = "source-conversation"
		}
	}
	m.rebuildRows()
	m.selectSessionRow(t, "source")

	m.rail.SetFilteringAttention(true)
	m, cmd := submitForkDialog(t, m, "filtered fork")
	m.applyCmd(t, cmd)
	if m.rail.FilteringAttention() {
		t.Fatal("attention filter still on, want it cleared so the starting fork is visible")
	}
	if _, ok := forkRow(m, "filtered fork"); !ok {
		t.Fatal("fork row missing after a filtered list")
	}
}

func TestForkRepeatedSubmitKeepsOneAcceptedJob(t *testing.T) {
	m := buildModel(t)
	m.fork.gen = 1
	m.queueFork(forkRequest{source: store.Session{ID: "source"}, name: "child", gen: m.fork.gen})
	m.queueFork(forkRequest{source: store.Session{ID: "source"}, name: "child", gen: m.fork.gen})
	if len(m.effects.pending) != 1 {
		t.Fatal("repeat submit accepted a second fork")
	}
}
