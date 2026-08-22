package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
)

func TestToolBinaryNamesWhatARowRuns(t *testing.T) {
	cases := map[string]struct {
		tool config.Tool
		want string
	}{
		"plain command":     {config.Tool{Command: "claude"}, "claude"},
		"command with args": {config.Tool{Command: "hermes --cli"}, "hermes"},
		"absolute path":     {config.Tool{Command: "/opt/homebrew/bin/codex"}, "codex"},
		"shell":             {config.Tool{Command: "bash -l", Shell: true}, ""},
		"no command":        {config.Tool{}, ""},
		"wrapper script":    {config.Tool{Command: "sh -c 'claude'"}, ""},
	}
	for name, tc := range cases {
		if got := toolBinary(tc.tool); got != tc.want {
			t.Errorf("%s: toolBinary = %q, want %q", name, got, tc.want)
		}
	}
}

func TestDetectRelaunchedTool(t *testing.T) {
	binaries := toolBinaries{
		"claude":   "claude",
		"codex":    "codex",
		"terminal": "",
		"gemini":   "gemini",
		// Two blocks of the same CLI: nothing in a process name says which
		// of them a pane is running.
		"grok":      "grok",
		"grok-fast": "grok",
	}
	cases := map[string]struct {
		current  string
		children []string
		want     string
	}{
		"another CLI took the pane":         {"claude", []string{"/opt/homebrew/bin/codex"}, "codex"},
		"same CLI came back":                {"claude", []string{"claude"}, ""},
		"nothing agent-like running":        {"claude", []string{"vim", "-bash"}, ""},
		"empty pane":                        {"claude", nil, ""},
		"terminals stay terminals":          {"terminal", []string{"claude"}, ""},
		"unknown tool row":                  {"retired-tool", []string{"codex"}, ""},
		"two blocks claim it":               {"claude", []string{"grok"}, ""},
		"agent beside its own CLI":          {"claude", []string{"codex", "claude"}, ""},
		"one configured CLI beside another": {"claude", []string{"codex", "aider"}, "codex"},
		"two configured CLIs":               {"claude", []string{"codex", "gemini"}, ""},
		"a shell is not a CLI":              {"claude", []string{"sh", "node"}, ""},
	}
	for name, tc := range cases {
		if got := detectRelaunchedTool(tc.current, tc.children, binaries); got != tc.want {
			t.Errorf("%s: detectRelaunchedTool = %q, want %q", name, got, tc.want)
		}
	}
}

// Quitting one CLI in a pane and starting another there is a real thing to
// do, and every rule the row is read by follows its tool. The poll moves
// the row onto what the pane is running rather than leaving the user to
// retype it by hand.
func TestPollRetypesARowOntoTheCLIItsPaneRuns(t *testing.T) {
	m := buildModel(t)
	m.cfg.Tools["tail-tool"] = config.Tool{Command: "tail -f /dev/null", DefaultStatus: status.Idle}
	engine, err := status.NewEngine(m.cfg)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	m.engine, m.poller.engine = engine, engine
	m.poller.binaries = newToolBinaries(m.cfg)
	m.poller.statusSources["tail-tool"] = ""

	createSessionOn(t, m, "swapped", "quietchat", t.TempDir())
	sess := m.sessionRows()[0]
	if err := m.store.SetAgentSessionID(sess.ID, "conversation-of-the-old-tool"); err != nil {
		t.Fatalf("set agent session id: %v", err)
	}
	quitAgent(t, m, sess.ID)

	if err := m.tmux.SendKeys(sess.ID, "tail -f /dev/null", "Enter"); err != nil {
		t.Fatalf("start the other CLI: %v", err)
	}
	waitForPaneChild(t, m, sess.ID, "tail")
	m.applyCmd(t, m.refreshCmd())

	got, err := m.store.Get(sess.ID)
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
	m.poller.binaries = newToolBinaries(m.cfg)
	createSessionOn(t, m, "steady", "quietchat", t.TempDir())
	sess := m.sessionRows()[0]
	if err := m.store.SetAgentSessionID(sess.ID, "kept-conversation"); err != nil {
		t.Fatalf("set agent session id: %v", err)
	}
	waitForAgent(t, m, sess.ID, true)
	m.applyCmd(t, m.refreshCmd())

	got, err := m.store.Get(sess.ID)
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
