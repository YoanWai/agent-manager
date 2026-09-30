package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
)

// Quitting one CLI in a pane and starting another there is a real thing to
// do, and every rule the row is read by follows its tool. The poll moves
// the row onto what the pane is running rather than leaving the user to
// retype it by hand.
func TestPollRetypesARowOntoTheCLIItsPaneRuns(t *testing.T) {
	m := buildModel(t)
	m.services.cfg.Tools["tail-tool"] = config.Tool{Command: "tail -f /dev/null", DefaultStatus: status.Idle}
	engine, err := status.NewEngine(m.services.cfg)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	m.services.engine = engine
	m.poller.dependencies.Engine = engine
	m.poller.options.Binaries = newToolBinaries(m.services.cfg)
	m.poller.options.StatusSources["tail-tool"] = ""

	resetExecution(m)
	createSessionOn(t, m, "swapped", "quietchat", t.TempDir())
	sess := m.sessionRows()[0]
	if err := m.services.store.SetAgentSessionID(sess.ID, "conversation-of-the-old-tool"); err != nil {
		t.Fatalf("set agent session id: %v", err)
	}
	quitAgent(t, m, sess.ID)

	if err := m.services.tmux.SendKeys(sess.ID, "tail -f /dev/null", "Enter"); err != nil {
		t.Fatalf("start the other CLI: %v", err)
	}
	waitForPaneChild(t, m, sess.ID, "tail")
	m.applyCmd(t, m.refreshCmd())

	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Tool != "tail-tool" {
		t.Fatalf("tool after the poll = %q, want tail-tool", got.Tool)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("conversation id = %q, want it dropped with the tool that minted it", got.AgentSessionID)
	}
	if row := m.sessionRows()[0]; row.Tool != "tail-tool" {
		t.Fatalf("row shows tool %q, want tail-tool", row.Tool)
	}
}

// A row whose pane still runs the CLI it launched with keeps its tool, and
// with it the conversation that tool can be revived on.
func TestPollLeavesARunningRowAlone(t *testing.T) {
	m := buildModel(t)
	m.poller.options.Binaries = newToolBinaries(m.services.cfg)
	resetExecution(m)
	createSessionOn(t, m, "steady", "quietchat", t.TempDir())
	sess := m.sessionRows()[0]
	if err := m.services.store.SetAgentSessionID(sess.ID, "kept-conversation"); err != nil {
		t.Fatalf("set agent session id: %v", err)
	}
	waitForAgent(t, m, sess.ID, true)
	m.applyCmd(t, m.refreshCmd())

	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Tool != sess.Tool {
		t.Fatalf("tool = %q, want it left on %q", got.Tool, sess.Tool)
	}
	if got.AgentSessionID != "kept-conversation" {
		t.Fatalf("conversation id = %q, want kept-conversation", got.AgentSessionID)
	}
}
