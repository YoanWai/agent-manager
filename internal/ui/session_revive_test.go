package ui

import (
	"errors"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReviveRecreatesDeadSession(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "phoenix", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.services.store.SetAgentSessionID(sess.ID, "kept-conversation"); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "phoenix")
	sess = m.sessionRows()[0]
	if sess.AgentSessionID != "kept-conversation" {
		t.Fatalf("loaded session id = %q, want kept-conversation", sess.AgentSessionID)
	}
	previousLaunchTime := sess.LaunchTime()

	argsFile := filepath.Join(t.TempDir(), "launch-args")
	tool := m.services.cfg.Tools[sess.Tool]
	tool.ResumeByIDCommand = argCaptureCommand(argsFile) + " --resume {id}"
	m.services.cfg.Tools[sess.Tool] = tool

	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if m.services.tmux.Exists(sess.ID) {
		t.Fatal("session should be dead before revive")
	}
	m.selectSessionRow(t, "phoenix")

	if err := m.services.store.SetAcked(sess.ID, true); err != nil {
		t.Fatalf("set acked: %v", err)
	}
	m.workspace.preview = "old pane from last life\n"

	m.reviveSelected()
	m.drainEffects(t)
	if m.errBar.text != "" {
		t.Fatalf("revive: %q", m.errBar.text)
	}
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("revive should recreate the tmux session")
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Starting {
		t.Fatalf("after revive, status = %q want %q", got.Status, status.Starting)
	}
	if got.Acked {
		t.Fatal("revive should clear a leftover ack")
	}
	if got.AgentSessionID != "kept-conversation" || got.RetiredAgentSessionID != "" {
		t.Fatalf("conversation = %q retired = %q, want kept-conversation", got.AgentSessionID, got.RetiredAgentSessionID)
	}
	if got.AgentLaunchedAt.IsZero() || !got.LaunchTime().Equal(got.AgentLaunchedAt) {
		t.Fatalf("launch time = %v, created = %v", got.LaunchTime(), got.CreatedAt)
	}
	if !got.AgentLaunchedAt.After(previousLaunchTime) {
		t.Fatalf("launch time = %v, want after %v", got.AgentLaunchedAt, previousLaunchTime)
	}
	row := m.sessionRows()[0]
	if row.Status != status.Starting {
		t.Fatalf("row status = %q want %q", row.Status, status.Starting)
	}
	gotPreview := previewText(m)
	if !strings.Contains(gotPreview, "starting up") {
		t.Fatalf("revived preview should carry the launch loader, got %q", gotPreview)
	}
	if strings.Contains(gotPreview, "old pane from last life") {
		t.Fatalf("stale pane should not survive revive, got %q", gotPreview)
	}
	args := readWhenWritten(t, argsFile)
	if !strings.Contains(args, "--resume") || !strings.Contains(args, "kept-conversation") {
		t.Fatalf("revive launch arguments = %q, want --resume kept-conversation", args)
	}
}

func TestReviveKillsNewPaneWhenLaunchTimeCannotPersist(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "phoenix", t.TempDir(), "")

	sess := m.sessionRows()[0]
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if err := m.services.store.Delete(sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	err := m.reviveSession(sess)
	if !errors.Is(err, store.ErrSessionGone) {
		t.Fatalf("revive error = %v, want ErrSessionGone", err)
	}
	if m.services.tmux.Exists(sess.ID) {
		t.Fatal("failed revive must kill the newly created tmux session")
	}
}

func TestRevivedLaunchTimeSitsInsideStartingGrace(t *testing.T) {
	created := time.Now().Add(-5 * 24 * time.Hour)
	revived := store.Session{CreatedAt: created, AgentLaunchedAt: time.Now()}
	if time.Since(revived.LaunchTime()) >= 30*time.Second {
		t.Fatal("a revive that stamps launch time must still be inside the grace")
	}
	stale := store.Session{CreatedAt: created}
	if time.Since(stale.LaunchTime()) < 30*time.Second {
		t.Fatal("a 5-day-old row with no relaunch is outside the grace")
	}
}

// degradedResumeNotice exists for the revive that came back blind: a tool
// whose picker opened, or a session whose own conversation id was captured,
// resumes the right conversation, and only the blind revive_command
// fallback warrants the warning.
func TestDegradedResumeNoticeWarnsOnlyForBlindFallbacks(t *testing.T) {
	base := config.Tool{
		Command:           "claude",
		ReviveCommand:     "claude --continue",
		ResumeByIDCommand: "claude --resume {id}",
	}
	picker := base
	picker.ResumePickerCommand = "claude --resume"

	for _, tc := range []struct {
		name string
		tool config.Tool
		id   string
		want string
	}{
		{"a picker revive is not degraded", picker, "", ""},
		{"a captured id is not degraded", base, "abc-123", ""},
		{"a blind continue fallback warns", base, "", "revived reviveme with --continue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			m.services.cfg.Tools["claude"] = tc.tool
			got := m.degradedResumeNotice(store.Session{Tool: "claude", Name: "reviveme", AgentSessionID: tc.id})
			if tc.want == "" {
				if got != "" {
					t.Fatalf("degradedResumeNotice = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("degradedResumeNotice = %q, want it to contain %q", got, tc.want)
			}
		})
	}
}

func TestReviveAllRecreatesEveryDeadSession(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "alpha", dir, "")
	createSession(t, m, "beta", dir, "")

	for _, sess := range m.visibleSessions() {
		if err := m.services.tmux.Kill(sess.ID); err != nil {
			t.Fatalf("kill %s: %v", sess.Name, err)
		}
	}
	// A refresh marks the pane-less sessions dead so revive-all picks them up.
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "running", dir, "")

	if _, _ = m.reviveAllDead(); m.mode != modeConfirmDelete {
		t.Fatalf("reviving two dead sessions should ask first, mode = %v err = %q", m.mode, m.errBar.text)
	}
	if want := "revive every dead session (2)? brings them back."; m.confirm.label != want {
		t.Fatalf("label = %q, want %q", m.confirm.label, want)
	}
	if len(m.confirm.sessions) != 2 {
		t.Fatalf("confirm targets = %+v, want the two dead sessions", m.confirm.sessions)
	}
	_, cmd := m.handleConfirmKey(namedKey(tea.KeyEnter))
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("revive all: %q", m.errBar.text)
	}
	for _, sess := range m.visibleSessions() {
		if !m.services.tmux.Exists(sess.ID) {
			t.Fatalf("revive all should recreate %s", sess.Name)
		}
	}
}

func TestReviveRefusesLiveSession(t *testing.T) {
	m := buildModel(t)
	// A tool whose process stays up: revive leaves a pane alone only while
	// something is running in it.
	createSessionOn(t, m, "alive", "quietchat", t.TempDir())
	waitForAgent(t, m, m.sessionRows()[0].ID, true)
	m.selectSessionRow(t, "alive")

	_, cmd := m.reviveSelected()
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if !strings.Contains(m.errBar.text, "still running") {
		t.Fatalf("revive said %q, want it to refuse a pane that still holds its agent", m.errBar.text)
	}
	if !m.services.tmux.Exists(m.sessionRows()[0].ID) {
		t.Fatal("live session must keep running")
	}
}

func TestReviveRefusesMissingDir(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "homeless", dir, "")

	sess := m.sessionRows()[0]
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("remove dir: %v", err)
	}
	m.selectSessionRow(t, "homeless")

	m.reviveSelected()
	m.drainEffects(t)
	if m.errBar.text == "" {
		t.Fatal("revive without a working directory should error")
	}
}

func TestReviveGroupBringsBackEverySessionInside(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work", "work/api")
	createSession(t, m, "alpha", dir, "work")
	createSession(t, m, "beta", dir, "work/api")
	createSession(t, m, "outside", dir, "")

	m.selectGroupRow(t, "work")
	m.killSelected()
	confirmKill(t, m)
	createSession(t, m, "running", dir, "work")

	m.selectGroupRow(t, "work")
	m.reviveSelected()
	m.drainEffects(t)
	if m.mode != modeConfirmDelete {
		t.Fatalf("reviving a group of two dead sessions should ask first, mode = %v err = %q", m.mode, m.errBar.text)
	}
	if title := m.confirm.title(); title != "◆ Revive group" {
		t.Fatalf("title = %q, want ◆ Revive group", title)
	}
	if want := "revive group work (2 dead sessions)? brings them back."; m.confirm.label != want {
		t.Fatalf("label = %q, want %q", m.confirm.label, want)
	}
	if len(m.confirm.sessions) != 2 {
		t.Fatalf("confirm targets = %+v, want the two dead sessions", m.confirm.sessions)
	}
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("revive group: %q", m.errBar.text)
	}
	for _, sess := range m.visibleSessions() {
		if !m.services.tmux.Exists(sess.ID) {
			t.Fatalf("revive group should have brought back %s", sess.Name)
		}
	}
}

func TestReviveGroupConfirmedRevivesWhatItCan(t *testing.T) {
	m := buildModel(t)
	gone, kept := t.TempDir(), t.TempDir()
	seedGroups(t, m, "work")
	createSession(t, m, "homeless", gone, "work")
	createSession(t, m, "housed", kept, "work")
	m.selectGroupRow(t, "work")
	m.killSelected()
	confirmKill(t, m)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("remove dir: %v", err)
	}

	m.selectGroupRow(t, "work")
	m.reviveSelected()
	m.drainEffects(t)
	if m.mode != modeConfirmDelete {
		t.Fatalf("mode = %v, want the revive card (err %q)", m.mode, m.errBar.text)
	}
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	if !strings.HasPrefix(m.errBar.text, "revived 1, first error: working directory no longer exists") {
		t.Fatalf("status = %q, want the count revived and the first failure", m.errBar.text)
	}
	if m.mode != modeList {
		t.Fatalf("mode = %v, want the list", m.mode)
	}
	if !m.services.tmux.Exists(sessionRow(t, m, "housed").sess.ID) {
		t.Fatal("a failed revive must not keep the rest of the group dead")
	}
}

func TestReviveBatchCancelLeavesEverySessionDead(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyMsg
	}{
		{"n", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}},
		{"esc", namedKey(tea.KeyEsc)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			dir := t.TempDir()
			seedGroups(t, m, "work")
			createSession(t, m, "alpha", dir, "work")
			createSession(t, m, "beta", dir, "work")
			m.selectGroupRow(t, "work")
			m.killSelected()
			confirmKill(t, m)

			for _, key := range []string{"v", "V"} {
				m.selectGroupRow(t, "work")
				m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
				m.drainEffects(t)
				if m.mode != modeConfirmDelete {
					t.Fatalf("%s: mode = %v, want the revive card (err %q)", key, m.mode, m.errBar.text)
				}
				m.handleKey(tc.key)
				m.drainEffects(t)
				if m.mode != modeList {
					t.Fatalf("%s: mode = %v, want the list after %s", key, m.mode, tc.name)
				}
				for _, sess := range m.visibleSessions() {
					if m.services.tmux.Exists(sess.ID) {
						t.Fatalf("%s then %s brought back %s", key, tc.name, sess.Name)
					}
				}
			}
		})
	}
}

func TestReviveBatchSkipsASessionBackBeforeTheAnswer(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work")
	createSession(t, m, "alpha", dir, "work")
	createSession(t, m, "beta", dir, "work")
	m.selectGroupRow(t, "work")
	m.killSelected()
	confirmKill(t, m)

	m.selectGroupRow(t, "work")
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m.drainEffects(t)
	if m.mode != modeConfirmDelete {
		t.Fatalf("mode = %v, want the revive card (err %q)", m.mode, m.errBar.text)
	}
	alpha := sessionRow(t, m, "alpha").sess
	if err := m.reviveSession(alpha); err != nil {
		t.Fatalf("revive alpha while the card is open: %v", err)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)

	if m.errBar.text != "" {
		t.Fatalf("status = %q, want no error for a session that came back on its own", m.errBar.text)
	}
	for _, name := range []string{"alpha", "beta"} {
		if !m.services.tmux.Exists(sessionRow(t, m, name).sess.ID) {
			t.Fatalf("%s should be running", name)
		}
	}
}

func TestReviveOneDeadSessionSkipsTheCard(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	seedGroups(t, m, "work")
	createSession(t, m, "alpha", dir, "work")
	// beta stays live, leaving two rows in scope and only one of them dead.
	createSession(t, m, "beta", dir, "work")
	alpha := sessionRow(t, m, "alpha").sess

	for _, key := range []string{"v", "V"} {
		if err := m.services.tmux.Kill(alpha.ID); err != nil {
			t.Fatalf("kill alpha: %v", err)
		}
		m.applyCmd(t, m.refreshCmd())
		m.selectGroupRow(t, "work")
		m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m.drainEffects(t)
		if m.mode != modeList {
			t.Fatalf("%s with one dead session: mode = %v, want it revived without a card", key, m.mode)
		}
		if !m.services.tmux.Exists(alpha.ID) {
			t.Fatalf("%s should revive the one dead session at once, err = %q", key, m.errBar.text)
		}
	}
}

func TestReviveRunningAgentRevivesDeadChild(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	if err := m.services.tmux.Kill(shell.ID); err != nil {
		t.Fatalf("kill child: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "coder")
	_, cmd := m.reviveSelected()
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.mode != modeConfirmDelete {
		t.Fatalf("mode = %v, want the revive confirm", m.mode)
	}
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if !m.services.tmux.Exists(shell.ID) {
		t.Fatal("child still dead")
	}
}

func TestReviveAgentIncludesDeadChildren(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	coder, ok := m.selected()
	if !ok {
		t.Fatal("coder row missing")
	}
	if err := m.services.tmux.Kill(shell.ID); err != nil {
		t.Fatalf("kill child: %v", err)
	}
	if err := m.services.tmux.Kill(coder.ID); err != nil {
		t.Fatalf("kill agent: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "coder")
	m.reviveSelected()
	m.drainEffects(t)
	ids := map[string]bool{}
	for _, sess := range m.confirm.sessions {
		ids[sess.ID] = true
	}
	if !ids[shell.ID] {
		t.Fatal("revive confirm omitted the child")
	}
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if !m.services.tmux.Exists(coder.ID) || !m.services.tmux.Exists(shell.ID) {
		t.Fatal("confirm should revive the agent and its dead children")
	}
}

// Quitting the agent leaves the window alive on a shell, and v is what
// brings the agent back there: launched by the manager, so it carries the
// session identity, the MCP registration and the hook settings that a CLI
// started by hand from that shell has no way to pick up.
func TestReviveStartsTheAgentAgainInALivePane(t *testing.T) {
	m := buildModel(t)
	createSessionOn(t, m, "quit-and-back", "quietchat", t.TempDir())
	sess := m.sessionRows()[0]
	// Hooks give the session a status file beside its id, so the relaunch
	// has both to carry. The command runs through a shell, which is what
	// survives the settings flag a hooked tool launches with.
	hooked := m.services.cfg.Tools[sess.Tool]
	hooked.Command = "sh -c 'cat'"
	hooked.StatusSource = "claude-hooks"
	m.services.cfg.Tools[sess.Tool] = hooked
	quitAgent(t, m, sess.ID)
	if err := m.services.store.SetAcked(sess.ID, true); err != nil {
		t.Fatalf("set acked: %v", err)
	}
	m.selectSessionRow(t, "quit-and-back")

	_, cmd := m.reviveSelected()
	m.drainEffects(t)
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("revive: %q", m.errBar.text)
	}
	waitForAgent(t, m, sess.ID, true)
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("revive must keep the window it relaunched in")
	}

	wantID := "AGENT_MANAGER_SESSION_ID='" + sess.ID + "'"
	wantStatus := "AGENT_MANAGER_STATUS_FILE='" + m.services.hooks.StatusFile(sess.ID) + "'"
	// AgentRunning observes the child process as soon as it forks. tmux can
	// paint the exported command a moment later, so wait on the values this
	// assertion owns instead of treating process startup as redraw completion.
	deadline := time.Now().Add(5 * time.Second)
	var pane string
	for {
		pane, _ = m.services.tmux.CapturePane(sess.ID)
		// The pane wraps the line it was sent at its own width.
		typed := strings.ReplaceAll(ansi.Strip(pane), "\n", "")
		if strings.Contains(typed, wantID) && strings.Contains(typed, wantStatus) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("relaunch did not paint its session identity and hook status file; pane:\n%s", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Starting {
		t.Fatalf("status after revive = %q, want %q", got.Status, status.Starting)
	}
	if got.Acked {
		t.Fatal("revive must clear the ack of the agent that exited")
	}

	// The relaunch exports rather than prefixes, so the shell it lands in
	// keeps the session identity once this agent exits as well.
	if err := m.services.tmux.SendKeys(sess.ID, "C-d"); err != nil {
		t.Fatalf("send ctrl-d: %v", err)
	}
	waitForAgent(t, m, sess.ID, false)
	marker := filepath.Join(t.TempDir(), "env")
	report := `printf '%s %s\n' "$AGENT_MANAGER_SESSION_ID" "$AGENT_MANAGER_STATUS_FILE" > ` + marker + `.part && mv ` + marker + `.part ` + marker
	if err := m.services.tmux.SendKeys(sess.ID, report, "Enter"); err != nil {
		t.Fatalf("read the shell environment: %v", err)
	}
	want := sess.ID + " " + m.services.hooks.StatusFile(sess.ID)
	deadline = time.Now().Add(5 * time.Second)
	for {
		if data, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(data)) == want {
			break
		}
		if time.Now().After(deadline) {
			pane, _ := m.services.tmux.CapturePane(sess.ID)
			t.Fatalf("the shell lost the session environment after the relaunched agent exited; pane:\n%s", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
