package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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

// spawnTerminal returns the shell this call made, which openTerminal leaves
// the cursor on; terminalSession would answer with the oldest one.
func spawnTerminal(t *testing.T, m *Model) store.Session {
	t.Helper()
	_, cmd := m.openTerminal()
	if m.errBar.text != "" {
		t.Fatalf("terminal spawn reported %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	sess, ok := m.selected()
	if !ok || !m.isShell(sess.Tool) {
		t.Fatalf("spawn should leave the cursor on the new shell, got %+v", sess)
	}
	return sess
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

// A shell gets no prompt and no rename directive, so the name it is given
// is its real one and the row shows it from the first frame.
func TestTerminalRowShowsItsNameImmediately(t *testing.T) {
	m := buildModel(t)
	if err := m.store.CreateGroup("backend", t.TempDir()); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")

	sess := spawnTerminal(t, m)
	if m.awaitingRename(sess) {
		t.Fatalf("shell %q has no rename to wait for", sess.Name)
	}
	row := ansi.Strip(m.renderTreeRow(treeRow{sess: sess}, false, 80, 0, panelHex()))
	if !strings.Contains(row, sess.Name) {
		t.Fatalf("shell row is missing its name %q:\n%s", sess.Name, row)
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

// slowSpawn puts a tmux ahead of the real one on PATH that stalls
// new-session, so a model built after it spawns as slowly as a loaded
// machine does.
func slowSpawn(t *testing.T, delay time.Duration) {
	t.Helper()
	realTmux, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux not installed")
	}
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\ncase \" $* \" in *' new-session '*) sleep %.3f ;; esac\nexec %s \"$@\"\n", delay.Seconds(), realTmux)
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatalf("write slow tmux: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// Holding T autorepeats into a burst of keystrokes. T spawns on the
// keystroke itself, so without a guard a held key fills the list with
// shells and tmux windows nobody asked for. The spawn here takes longer
// than the window, the way it does on a loaded machine, and the burst
// queued behind it still has to collapse into one shell.
func TestTerminalKeyIgnoresAutorepeat(t *testing.T) {
	slowSpawn(t, terminalKeyWindow+50*time.Millisecond)
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

// A terminal opened while the cursor is already on one joins it as a
// sibling: a shell is not something to hang another shell off.
func TestSecondShellJoinsTheFirstRatherThanNesting(t *testing.T) {
	m := buildModel(t)
	groupWithShell(t, m, "backend")
	createSession(t, m, "agent-one", t.TempDir(), "backend")
	m.selectSessionRow(t, "agent-one")
	first := spawnTerminal(t, m)
	second := spawnTerminal(t, m)
	nestShells(t, m)

	if m.shellParents[second.ID] != m.shellParents[first.ID] {
		t.Fatalf("second shell hangs off %q, want the first's parent %q",
			m.shellParents[second.ID], m.shellParents[first.ID])
	}
	if rowFor(t, m, second.ID).depth != rowFor(t, m, first.ID).depth {
		t.Fatal("siblings sit at the same depth")
	}
	// Spelled out rather than merely "different": the second shell is meant
	// to read as the second one, which any unique suffix would satisfy
	// without saying.
	if first.Name != "terminal-agent-one" {
		t.Fatalf("first shell name = %q, want terminal-agent-one", first.Name)
	}
	if second.Name != "terminal-agent-one-2" {
		t.Fatalf("second shell name = %q, want terminal-agent-one-2", second.Name)
	}
}

// A shell opened before the link was stored still finds its session, by the
// directory both were launched in — for a worktree, the one agent there.
func TestShellWithoutALinkNestsByDirectory(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	groupWithShell(t, m, "backend")
	createSession(t, m, "agent-one", dir, "backend")
	m.selectSessionRow(t, "agent-one")
	shell := spawnTerminal(t, m)

	for i := range m.sessions {
		if m.sessions[i].ID == shell.ID {
			m.sessions[i].ParentID = ""
			m.sessions[i].Cwd = dir
		}
	}
	nestShells(t, m)

	agent := rowFor(t, m, m.shellParents[shell.ID])
	if agent.sess.Name != "agent-one" {
		t.Fatalf("shell fell back to %q, want the agent sharing its directory", agent.sess.Name)
	}
}

// Nested, the session the shell hangs off is the row above it, so the row
// says nothing a reader can already see. Only a shell with no session to
// hang off spends the column, on the directory it was launched in.
func TestNestedShellLeavesTheColumnToItsSession(t *testing.T) {
	m := buildModel(t)
	groupWithShell(t, m, "backend")
	createSession(t, m, "agent-one", t.TempDir(), "backend")
	m.selectSessionRow(t, "agent-one")
	nested := spawnTerminal(t, m)

	m.selectGroupRow(t, "backend")
	loose := spawnTerminal(t, m)
	nestShells(t, m)

	if label := m.shellOriginLabel(nested); label != "" {
		t.Fatalf("label = %q, want nothing where the session is the row above", label)
	}
	if label := m.shellOriginLabel(loose); label != filepath.Base(loose.Cwd) {
		t.Fatalf("label = %q, want the directory %q", label, filepath.Base(loose.Cwd))
	}
}

// The delete reads the store, not the rail: a shell the archived view holds
// is still one this session owns, and skipping it would strand exactly the
// shell nobody can see to clean up.
func TestShellsOpenedForReachesWhatTheRailDoesNot(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "agent-one", t.TempDir(), "")
	createSession(t, m, "agent-two", t.TempDir(), "")
	m.selectSessionRow(t, "agent-one")
	mine := spawnTerminal(t, m)
	m.selectSessionRow(t, "agent-two")
	theirs := spawnTerminal(t, m)

	if err := m.store.SetArchived(mine.ID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	parent, ok := m.sessionByID(mine.ParentID)
	if !ok || parent.Name != "agent-one" {
		t.Fatalf("terminal %s lost its recorded session", mine.Name)
	}
	shells, err := m.shellsOpenedFor(parent.ID)
	if err != nil {
		t.Fatalf("shellsOpenedFor: %v", err)
	}
	if len(shells) != 1 || shells[0].ID != mine.ID {
		t.Fatalf("want the archived terminal %s, got %+v", mine.Name, shells)
	}
	if shells[0].ID == theirs.ID {
		t.Fatal("another session's terminal must not be swept in")
	}
}

// Two groups can point at one checkout — a working group and a review group
// over the same directory. The directory fallback picks the oldest agent in
// it, and a shell never nests outside the group it carries, so the older
// agent in the other group must not take the slot and leave the shell
// looking parentless when its own group had one.
func TestDirectoryFallbackIgnoresAnOlderAgentInAnotherGroup(t *testing.T) {
	m := buildModel(t)
	shared := t.TempDir()
	if err := m.store.CreateGroup("review", shared); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := m.store.CreateGroup("work", shared); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "reviewer", shared, "review")
	createSession(t, m, "worker", shared, "work")
	m.selectSessionRow(t, "worker")
	shell := spawnTerminal(t, m)

	// The reviewer is the older of the two, so a fallback keyed by directory
	// alone would hand it this shell and then reject it for its group.
	for i := range m.sessions {
		switch m.sessions[i].Name {
		case "reviewer":
			m.sessions[i].CreatedAt = time.Now().Add(-time.Hour)
		case shell.Name:
			m.sessions[i].ParentID = ""
			m.sessions[i].Cwd = shared
		}
	}
	nestShells(t, m)

	parent, ok := m.sessionByID(m.shellParents[shell.ID])
	if !ok {
		t.Fatal("the shell should still find the agent in its own group")
	}
	if parent.Name != "worker" {
		t.Fatalf("shell nested under %q, want worker", parent.Name)
	}
}
