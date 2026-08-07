package ui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func shellCount(m *Model) int {
	count := 0
	for _, sess := range m.sessions {
		if m.isShell(sess.Tool) {
			count++
		}
	}
	return count
}

func pressTerminalKey(t *testing.T, m *Model) {
	t.Helper()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	m.applyCmd(t, cmd)
}

// shellToolName is the block buildModel ships with shell = true.
const shellToolName = "terminal"

// resolved is a path as tmux reports it back: symlinks followed, which on
// macOS is what the /var temp directories sit behind.
func resolved(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve %q: %v", dir, err)
	}
	return real
}

func terminalSession(t *testing.T, m *Model) store.Session {
	t.Helper()
	for _, sess := range m.sessions {
		if m.isShell(sess.Tool) {
			return sess
		}
	}
	t.Fatalf("no shell session among %v", m.sessions)
	return store.Session{}
}

func spawnTerminal(t *testing.T, m *Model) store.Session {
	t.Helper()
	_, cmd := m.openTerminal()
	if m.errBar.text != "" {
		t.Fatalf("terminal spawn reported %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	return terminalSession(t, m)
}

func TestOpenTerminalSpawnsShellInSelectedGroup(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")

	sess := spawnTerminal(t, m)
	if sess.Group != "backend" {
		t.Fatalf("terminal group = %q, want backend", sess.Group)
	}
	if sess.Cwd != dir {
		t.Fatalf("terminal cwd = %q, want %q", sess.Cwd, dir)
	}
	if !m.tmux.Exists(sess.ID) {
		t.Fatal("terminal spawn left no tmux session")
	}
	if row, ok := m.selected(); !ok || row.ID != sess.ID {
		t.Fatalf("cursor should land on the new terminal, selected = %+v", row)
	}
}

// A terminal opened on a session row follows that session's directory, so
// the shell lands where the agent works rather than at the group default.
func TestOpenTerminalOnSessionRowUsesItsDirectory(t *testing.T) {
	m := buildModel(t)
	groupDir, sessionDir := t.TempDir(), t.TempDir()
	if err := m.store.CreateGroup("backend", groupDir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "agent", sessionDir, "backend")
	m.selectSessionRow(t, "agent")

	sess := spawnTerminal(t, m)
	if want := resolved(t, sessionDir); sess.Cwd != want {
		t.Fatalf("terminal cwd = %q, want the session's %q", sess.Cwd, want)
	}
	if sess.Group != "backend" {
		t.Fatalf("terminal group = %q, want backend", sess.Group)
	}
}

// tmux keeps reporting a pane's path after the directory is removed under
// it, so the row checks both the pane path and the recorded cwd rather than
// handing back somewhere that is no longer there.
func TestRowDirRefusesADirectoryThatIsGone(t *testing.T) {
	m := buildModel(t)
	gone := t.TempDir()
	createSession(t, m, "agent", gone, "")
	m.selectSessionRow(t, "agent")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatalf("remove dir: %v", err)
	}

	dir, ok := m.rowDir()
	if ok {
		t.Fatalf("rowDir accepted a directory that is gone: %q", dir)
	}
}

// Killing frees the shell like any other session, and revive brings it
// back on the tool's empty command rather than erroring on a missing CLI.
func TestTerminalSessionRevives(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	sess := spawnTerminal(t, m)

	if err := m.killSession(sess); err != nil {
		t.Fatalf("kill terminal: %v", err)
	}
	sess.Status = status.Dead
	if err := m.reviveSession(sess); err != nil {
		t.Fatalf("revive terminal: %v", err)
	}
	if !m.tmux.Exists(sess.ID) {
		t.Fatal("revived terminal has no tmux session")
	}
}

// The shell block is not a CLI to spawn agents with: it stays out of every
// picker, but a shell session keeps it on rename so saving cannot silently
// turn the shell into an agent.
func TestShellToolStaysOutOfPickers(t *testing.T) {
	m := buildModel(t)
	if slices.Contains(m.enabledToolNames(), shellToolName) {
		t.Fatalf("a shell should not be offered as a CLI: %v", m.enabledToolNames())
	}
	m.applyCmd(t, m.refreshCmd())
	sess := spawnTerminal(t, m)

	m.selectSessionRow(t, sess.Name)
	m.openRename()
	if got := m.renameTool(); got != shellToolName {
		t.Fatalf("rename tool = %q, want %q", got, shellToolName)
	}
}

// Nothing keys off the name: a hand-rolled [tools.terminal] block that never
// declared itself a shell stays an agent CLI, pickers included.
func TestABlockNamedTerminalIsOnlyAShellWhenItSaysSo(t *testing.T) {
	m := buildModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{
		"claude":   {Command: "cat"},
		"terminal": {Command: "my-own-cli"},
	}}
	if !slices.Contains(m.enabledToolNames(), "terminal") {
		t.Fatalf("a user's own terminal block must stay a CLI: %v", m.enabledToolNames())
	}
	if m.isShell("terminal") {
		t.Fatal("a block without shell = true is not a shell")
	}
	if _, _, ok := m.shellTool(); ok {
		t.Fatal("no block declared shell = true, so T has nothing to spawn")
	}
}

// The shipped block is found by its flag, whatever it is called.
func TestShellToolIsFoundByItsFlag(t *testing.T) {
	m := buildModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{
		"claude": {Command: "cat"},
		"zsh":    {Shell: true},
	}}
	name, tool, ok := m.shellTool()
	if !ok || name != "zsh" || tool.Command != "" {
		t.Fatalf("shellTool() = %q %+v %v, want the zsh block", name, tool, ok)
	}
}

// Holding T autorepeats into a burst of keystrokes. T spawns on the
// keystroke itself, so without a guard a held key fills the list with
// shells and tmux windows nobody asked for.
func TestTerminalKeyIgnoresAutorepeat(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())

	for i := 0; i < 5; i++ {
		pressTerminalKey(t, m)
	}

	if got := shellCount(m); got != 1 {
		t.Fatalf("a held T made %d shells, want 1", got)
	}
}

// A press once the burst is over is a second request, not a repeat.
func TestTerminalKeySpawnsAgainAfterTheWindow(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())

	pressTerminalKey(t, m)
	m.terminalKeyAt = time.Now().Add(-2 * terminalKeyWindow)
	pressTerminalKey(t, m)

	if got := shellCount(m); got != 2 {
		t.Fatalf("two separate presses made %d shells, want 2", got)
	}
}

// The footer offers what the row can do: a shell has no conversation, so
// the keys that prompt, review or fork one are left off.
func TestShellRowLegendDropsTheConversationKeys(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	sess := spawnTerminal(t, m)
	m.selectSessionRow(t, sess.Name)

	legend := m.rowLegend()
	if legend.title != "Shell" {
		t.Fatalf("legend title = %q, want Shell", legend.title)
	}
	for _, pair := range legend.pairs {
		switch pair[0] {
		case "space", "ctrl+r", "f":
			t.Fatalf("legend offers %q on a shell row, which refuses it", pair[0])
		}
	}
	if !slices.ContainsFunc(legend.pairs, func(pair [2]string) bool { return pair[0] == "R" }) {
		t.Fatal("legend should still offer the keys a shell answers, R included")
	}
}

// An agent row keeps the full set.
func TestAgentRowLegendKeepsTheConversationKeys(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "agent", t.TempDir(), "")
	m.selectSessionRow(t, "agent")

	legend := m.rowLegend()
	if legend.title != "Session" {
		t.Fatalf("legend title = %q, want Session", legend.title)
	}
	for _, key := range []string{"space", "ctrl+r", "f"} {
		if !slices.ContainsFunc(legend.pairs, func(pair [2]string) bool { return pair[0] == key }) {
			t.Fatalf("legend should offer %q on an agent row", key)
		}
	}
}
