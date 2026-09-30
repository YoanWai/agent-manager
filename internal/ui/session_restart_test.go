package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"path/filepath"
	"strings"
	"testing"
)

// Restart is revive's opposite number: same row, same directory, same tool,
// but a conversation the agent has never seen. The old one is retired rather
// than resumed, so the resume flags revive would have used stay unused.
func TestRestartLaunchesAFreshConversation(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "phoenix", t.TempDir(), "")
	sess := m.sessionRows()[0]

	argsFile := filepath.Join(t.TempDir(), "launch-args")
	tool := m.services.cfg.Tools[sess.Tool]
	tool.Command = argCaptureCommand(argsFile)
	tool.SessionIDFlag = "--session-id"
	tool.ResumeByIDCommand = "false --resume {id}"
	tool.ReviveCommand = "false --continue"
	m.services.cfg.Tools[sess.Tool] = tool

	if err := m.services.store.SetAgentSessionID(sess.ID, "old-conversation"); err != nil {
		t.Fatal(err)
	}
	m.workspace.sessions[0].AgentSessionID = "old-conversation"
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "phoenix")

	if _, _ = m.restartSelected(); m.mode != modeConfirmDelete {
		t.Fatalf("restart should ask first, mode = %v err = %q", m.mode, m.errBar.text)
	}
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("restart: %q", m.errBar.text)
	}
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("restart should leave the session running")
	}

	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID == "" || got.AgentSessionID == "old-conversation" {
		t.Fatalf("restarted conversation id = %q, want a fresh one", got.AgentSessionID)
	}
	if got.RetiredAgentSessionID != "old-conversation" {
		t.Fatalf("retired conversation = %q, want old-conversation", got.RetiredAgentSessionID)
	}
	if got.AgentLaunchedAt.Before(got.CreatedAt) || got.AgentLaunchedAt.IsZero() {
		t.Fatalf("launch time = %v, created = %v", got.AgentLaunchedAt, got.CreatedAt)
	}
	if got.Status != status.Starting {
		t.Fatalf("after restart, status = %q want %q", got.Status, status.Starting)
	}

	// The fresh conversation id rides the launch; the resume flags revive
	// would have used never appear.
	args := readWhenWritten(t, argsFile)
	if !strings.Contains(args, "--session-id\n"+got.AgentSessionID) {
		t.Fatalf("launch arguments = %q, want the fresh session id", args)
	}
	if strings.Contains(args, "--resume") || strings.Contains(args, "--continue") {
		t.Fatalf("restart must not resume, launch arguments = %q", args)
	}
}

// A tool that mints its own conversation id has nothing to
// hand the launch, so restart clears the binding and leaves the id for the
// poller to capture once the new conversation lands.
func TestRestartClearsCapturedConversationID(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "codexish", t.TempDir(), "")
	sess := m.sessionRows()[0]
	// The precondition under test, spelled out rather than inherited from
	// whatever flags the fake tools happen to carry.
	tool := m.services.cfg.Tools[sess.Tool]
	tool.SessionIDFlag = ""
	tool.SessionStore = "codex"
	m.services.cfg.Tools[sess.Tool] = tool

	if err := m.services.store.SetAgentSessionID(sess.ID, "captured-conversation"); err != nil {
		t.Fatal(err)
	}
	m.workspace.sessions[0].AgentSessionID = "captured-conversation"
	if err := m.services.tmux.Kill(sess.ID); err != nil {
		t.Fatalf("kill: %v", err)
	}
	m.selectSessionRow(t, "codexish")

	m.restartSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("restart: %q", m.errBar.text)
	}

	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("conversation id = %q, want it cleared for capture", got.AgentSessionID)
	}
	if got.RetiredAgentSessionID != "captured-conversation" {
		t.Fatalf("retired conversation = %q", got.RetiredAgentSessionID)
	}
}

// Restart also serves the session that is still running: it ends the agent
// holding the context and brings the row back empty-handed.
func TestRestartEndsALiveAgentFirst(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "busy", t.TempDir(), "")
	sess := m.sessionRows()[0]
	tool := m.services.cfg.Tools[sess.Tool]
	tool.Command = argCaptureCommand(filepath.Join(t.TempDir(), "launch-args"))
	m.services.cfg.Tools[sess.Tool] = tool
	m.selectSessionRow(t, "busy")

	if _, _ = m.restartSelected(); m.mode != modeConfirmDelete {
		t.Fatalf("restart should ask first, mode = %v err = %q", m.mode, m.errBar.text)
	}
	if !strings.Contains(m.confirm.label, "ends the running agent") {
		t.Fatalf("confirm label = %q", m.confirm.label)
	}
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)
	if m.errBar.text != "" {
		t.Fatalf("restart: %q", m.errBar.text)
	}
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("restart should leave the session running")
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != status.Starting {
		t.Fatalf("after restart, status = %q want %q", got.Status, status.Starting)
	}
}

func TestRestartRefusesGroupRow(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("work", dir); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "member", dir, "work")
	m.selectGroupRow(t, "work")

	if _, _ = m.restartSelected(); m.mode != modeList || m.errBar.text == "" {
		t.Fatalf("group restart should refuse, mode = %v err = %q", m.mode, m.errBar.text)
	}
}

// Restart has to hold for every CLI the manager ships with, not just the one
// the fake tools stand in for: it launches each tool the way a brand new
// session does, and never reaches for a resume, continue or fork command.
func TestRestartLaunchIsAFreshStartForEveryShippedTool(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tools) < 6 {
		t.Fatalf("built-in tools = %d, expected every shipped CLI", len(cfg.Tools))
	}
	for name, tool := range cfg.Tools {
		// A shell has no conversation for a restart to start fresh.
		if tool.Shell {
			continue
		}
		command, agentSessionID := restartLaunch(tool)
		if !strings.HasPrefix(command, tool.Command) {
			t.Errorf("%s: restart command %q does not start from its launch command %q", name, command, tool.Command)
		}
		rest := strings.TrimPrefix(command, tool.Command)
		for _, resume := range []string{"resume", "continue", "fork", "--session ", "--last"} {
			if strings.Contains(rest, resume) {
				t.Errorf("%s: restart command %q carries %q", name, command, resume)
			}
		}
		if tool.SessionIDFlag == "" {
			// Some tools mint their own id; the poller captures it.
			if agentSessionID != "" || rest != "" {
				t.Errorf("%s: restart handed an id to a tool that mints its own: %q", name, command)
			}
			if tool.SessionStore == "" {
				t.Errorf("%s: no session_id_flag and no session_store leaves restart with no conversation id at all", name)
			}
			continue
		}
		if _, err := uuid.Parse(agentSessionID); err != nil {
			t.Errorf("%s: restart conversation id %q is not a uuid: %v", name, agentSessionID, err)
		}
		if want := " " + tool.SessionIDFlag + " " + agentSessionID; rest != want {
			t.Errorf("%s: restart command tail = %q, want %q", name, rest, want)
		}
		if _, second := restartLaunch(tool); second == agentSessionID {
			t.Errorf("%s: two restarts reused conversation id %q", name, agentSessionID)
		}
	}
}

func TestRestartAgentStaysSingleSession(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	m.restartSelected()
	if len(m.confirm.sessions) != 1 {
		t.Fatalf("restart confirm = %+v", m.confirm.sessions)
	}
}
