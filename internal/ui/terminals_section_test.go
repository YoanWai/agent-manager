package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// rail is the sidebar alone. The content column repeats the selected
// session's name in its detail head, so asserting against a whole frame
// would pass on a row the rail never painted.
func (m *Model) rail() string {
	return ansi.Strip(railLinesText(m.railLines(40, m.listBodyHeight())))
}

func railRow(rail, needle string) int {
	at := -1
	for i, line := range strings.Split(rail, "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		if at >= 0 {
			return -2
		}
		at = i
	}
	return at
}

func groupWithShell(t *testing.T, m *Model, group string) {
	t.Helper()
	if err := m.store.CreateGroup(group, t.TempDir()); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, group)
}

// Pinned, a shell leaves its group and gathers under the Terminals divider
// at the foot of the rail.
func TestTerminalsBlockPinsShells(t *testing.T) {
	m := buildModel(t)
	groupWithShell(t, m, "backend")
	createSession(t, m, "agent-one", t.TempDir(), "backend")
	m.selectGroupRow(t, "backend")
	shell := spawnTerminal(t, m)
	pinShells(t, m)

	if m.pinnedShells != 1 {
		t.Fatalf("pinnedShells = %d, want 1", m.pinnedShells)
	}
	last := m.rows[len(m.rows)-1]
	if last.isGroup || last.sess.ID != shell.ID {
		t.Fatalf("the shell should be the last row, got %+v", last)
	}
	if last.depth != 0 {
		t.Fatalf("pinned depth = %d, want 0 so the tree's branches close above it", last.depth)
	}

	rail := m.rail()
	divider, agent, shellAt := railRow(rail, "Terminals"), railRow(rail, "agent-one"), railRow(rail, shell.Name)
	if divider < 0 || agent < 0 || shellAt < 0 {
		t.Fatalf("rail is missing a row, or painted one twice (divider %d, agent %d, shell %d):\n%s", divider, agent, shellAt, rail)
	}
	if !(agent < divider && divider < shellAt) {
		t.Fatalf("want the agent above the divider and the shell below it, got %d/%d/%d:\n%s", agent, divider, shellAt, rail)
	}
}

// The caret marks a shell only where it shares the list with agents.
func TestShellGlyphIsInlineOnly(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	spawnTerminal(t, m)
	pinShells(t, m)

	if rail := m.rail(); strings.Contains(rail, shellGlyph) {
		t.Fatalf("a pinned shell keeps its status dot:\n%s", rail)
	}
	m.shellsPinned = false
	m.rebuildRows()
	if rail := m.rail(); !strings.Contains(rail, shellGlyph) {
		t.Fatalf("an inline shell should carry the caret:\n%s", rail)
	}
}

// The caret says the shell is resting, so a shell whose pane has gone keeps
// the glyph that says otherwise.
func TestDeadInlineShellKeepsItsStatusGlyph(t *testing.T) {
	m := buildModel(t)
	m.shellsPinned = false
	m.applyCmd(t, m.refreshCmd())
	shell := spawnTerminal(t, m)

	if glyph := ansi.Strip(m.sessionGlyph(shell)); glyph != shellGlyph {
		t.Fatalf("a resting inline shell wears the caret, got %q", glyph)
	}
	for _, gone := range []string{status.Dead, status.Errored} {
		shell.Status = gone
		glyph := ansi.Strip(m.sessionGlyph(shell))
		if glyph == shellGlyph {
			t.Fatalf("a %s shell must not read as resting", gone)
		}
		if glyph != statusGlyph(gone) {
			t.Fatalf("a %s shell should wear its own glyph, got %q", gone, glyph)
		}
	}
}

// Inline puts the shell back under its group.
func TestInlinePlacementPutsShellsInTheTree(t *testing.T) {
	m := buildModel(t)
	groupWithShell(t, m, "backend")
	shell := spawnTerminal(t, m)

	m.shellsPinned = false
	m.rebuildRows()

	if m.pinnedShells != 0 {
		t.Fatalf("pinnedShells = %d, want 0 once shells are inline", m.pinnedShells)
	}
	var row treeRow
	for _, entry := range m.rows {
		if !entry.isGroup && entry.sess.ID == shell.ID {
			row = entry
		}
	}
	if row.depth != 1 {
		t.Fatalf("inline shell depth = %d, want 1 (under its group)", row.depth)
	}
	if rail := m.rail(); strings.Contains(rail, "Terminals") {
		t.Fatalf("inline placement paints no divider:\n%s", rail)
	}
}

// A terminal opened on a session hangs off it, and takes its name, so the
// worktree it belongs to is the row above rather than something to work out.
func TestShellNestsUnderTheSessionItWasOpenedOn(t *testing.T) {
	m := buildModel(t)
	groupWithShell(t, m, "backend")
	createSession(t, m, "agent-one", t.TempDir(), "backend")
	m.selectSessionRow(t, "agent-one")
	shell := spawnTerminal(t, m)
	nestShells(t, m)

	agent := rowFor(t, m, m.shellParents[shell.ID])
	if agent.sess.Name != "agent-one" {
		t.Fatalf("shell hangs off %q, want agent-one", agent.sess.Name)
	}
	row := rowFor(t, m, shell.ID)
	if row.depth != agent.depth+1 {
		t.Fatalf("shell depth = %d, want %d (one under its session)", row.depth, agent.depth+1)
	}
	if !strings.HasSuffix(shell.Name, "-agent-one") {
		t.Fatalf("shell name = %q, want it to carry the session's", shell.Name)
	}
	rail := m.rail()
	lines := strings.Split(rail, "\n")
	shellAt := railRow(rail, shell.Name)
	if shellAt < 1 || !strings.Contains(lines[shellAt-1], "agent-one") {
		t.Fatalf("want the shell painted directly under its session:\n%s", rail)
	}
}

// A group row describes the agents working in it, and so does the header,
// so the two never contradict each other in the same frame.
func TestRollupsCountAgentsOnly(t *testing.T) {
	m := buildModel(t)
	groupWithShell(t, m, "backend")
	createSession(t, m, "agent-one", t.TempDir(), "backend")
	m.selectGroupRow(t, "backend")
	shell := spawnTerminal(t, m)

	if got := m.groupSessionCount("backend"); got != 1 {
		t.Fatalf("groupSessionCount = %d, want 1 agent", got)
	}
	total := 0
	for _, count := range m.groupStatusCounts("backend") {
		total += count
	}
	if total != 1 {
		t.Fatalf("group status counts total = %d, want 1", total)
	}
	if header := ansi.Strip(m.headerScope()); !strings.HasPrefix(header, "1 agent") {
		t.Fatalf("header = %q, want it to count the agent alone", header)
	}
	roster := ansi.Strip(m.viewGroupAgents("backend", 112, 10))
	if !strings.Contains(roster, "agent-one") || strings.Contains(roster, shell.Name) {
		t.Fatalf("the roster should hold the agent and not the shell:\n%s", roster)
	}
}

// The block is not a reason to keep a group row that would paint nothing.
func TestHideEmptyGroupsDropsAShellsOnlyGroup(t *testing.T) {
	m := buildModel(t)
	groupWithShell(t, m, "backend")
	spawnTerminal(t, m)
	pinShells(t, m)

	if !hasGroupRow(m, "backend") {
		t.Fatal("the group keeps its row while empty groups are shown")
	}
	m.hideEmptyGroups = true
	m.rebuildRows()
	if hasGroupRow(m, "backend") {
		t.Fatal("a group whose only sessions are pinned has nothing under it to show")
	}
}

func hasGroupRow(m *Model, group string) bool {
	for _, entry := range m.rows {
		if entry.isGroup && entry.group == group {
			return true
		}
	}
	return false
}

// The empty state speaks for the tree it replaces, not for the block that
// is still full underneath it.
func TestEmptyStateNamesTheTreesSubject(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	shell := spawnTerminal(t, m)
	pinShells(t, m)

	for _, probe := range []struct {
		name  string
		apply func()
		want  string
	}{
		{"bare", func() {}, "no agents yet"},
		{"search", func() { m.search = shell.Name }, "no agents match"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			probe.apply()
			m.rebuildRows()
			rail := m.rail()
			if !strings.Contains(rail, probe.want) {
				t.Fatalf("want %q while the block is full:\n%s", probe.want, rail)
			}
			if !strings.Contains(rail, "Terminals") {
				t.Fatalf("the block should still be painted below it:\n%s", rail)
			}
		})
	}
}

// A search reaches the block, so a query that matches no agent still shows
// the shell it does match.
func TestSearchFiltersTheBlock(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	shell := spawnTerminal(t, m)
	pinShells(t, m)

	m.search = shell.Name
	m.rebuildRows()
	if m.pinnedShells != 1 {
		t.Fatalf("pinnedShells = %d, want the matching shell kept", m.pinnedShells)
	}
	m.search = "matches-nothing"
	m.rebuildRows()
	if m.pinnedShells != 0 {
		t.Fatalf("pinnedShells = %d, want the block emptied by the query", m.pinnedShells)
	}
	if rail := m.rail(); strings.Contains(rail, "Terminals") {
		t.Fatalf("an empty block paints no divider:\n%s", rail)
	}
}

// An idle shell never needs attention, so the filter empties the block.
func TestAttentionFilterEmptiesTheBlock(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "agent-one", t.TempDir(), "")
	spawnTerminal(t, m)
	pinShells(t, m)
	m.selectSessionRow(t, "agent-one")

	m.statusFilter = statusFilterAttention
	m.rebuildRows()

	if m.pinnedShells != 0 {
		t.Fatalf("pinnedShells = %d, want 0", m.pinnedShells)
	}
	if rail := m.rail(); strings.Contains(rail, "Terminals") {
		t.Fatalf("an empty block paints no divider:\n%s", rail)
	}
}

// The cursor can walk into the block, and the row it lands on is painted in
// the rail rather than only named by the detail head beside it.
func TestCursorInTheBlockStaysPainted(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "agent-one", t.TempDir(), "")
	first := spawnTerminal(t, m)
	second := spawnTerminal(t, m)
	if first.ID == second.ID {
		t.Fatal("the two spawns should be different shells")
	}
	pinShells(t, m)

	for _, shell := range []string{first.Name, second.Name} {
		m.selectSessionRow(t, shell)
		sess, ok := m.selected()
		if !ok || sess.Name != shell {
			t.Fatalf("selected() = %+v %v, want %s", sess, ok, shell)
		}
		for _, height := range []int{16, 24, 30, 44} {
			m.height = height
			if rail := m.rail(); railRow(rail, shell) < 0 {
				t.Fatalf("selected shell %s is unpainted at height %d:\n%s", shell, height, rail)
			}
		}
	}
}

// The block never takes more than half the list, and scrolls inside that
// half rather than pushing the tree off screen.
func TestBlockKeepsHalfTheListAtMost(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "agent-one", t.TempDir(), "")
	for i := 0; i < 12; i++ {
		spawnTerminal(t, m)
	}
	pinShells(t, m)
	m.selectSessionRow(t, "agent-one")

	const height = 30
	lines := m.railLines(40, height)
	if len(lines) != height {
		t.Fatalf("rail painted %d lines, want %d", len(lines), height)
	}
	rail := ansi.Strip(railLinesText(lines))
	if railRow(rail, "agent-one") < 0 {
		t.Fatalf("the tree must survive a long block:\n%s", rail)
	}
	shown := strings.Count(rail, "terminal-")
	if shown >= 12 {
		t.Fatalf("all %d shells painted; the block should have windowed them:\n%s", shown, rail)
	}
	if !strings.Contains(rail, "more") {
		t.Fatalf("a windowed block should offer its counter:\n%s", rail)
	}
}

// The rail hands back exactly the rows it was given, whatever the block
// costs. paintContent would pad or trim a wrong answer out of sight.
func TestRailReturnsItsBudget(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "agent-one", t.TempDir(), "")
	spawnTerminal(t, m)
	spawnTerminal(t, m)
	pinShells(t, m)

	for _, height := range []int{3, 4, 6, 8, 10, 14, 34} {
		for _, width := range []int{30, 60, 120} {
			for _, searching := range []bool{false, true} {
				m.searching = searching
				if got := len(m.railLines(width, height)); got != height {
					t.Fatalf("%dx%d searching=%v returned %d rows, want %d", width, height, searching, got, height)
				}
			}
		}
	}
}

// Where the rail cannot afford a heading, the shell rows outrank it.
func TestShortRailDropsTheLabelNotTheRows(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	shell := spawnTerminal(t, m)
	pinShells(t, m)

	lines := m.terminalSectionLines(40, 1)
	if len(lines) != 1 {
		t.Fatalf("a one-row budget should paint one row, got %d", len(lines))
	}
	rail := ansi.Strip(railLinesText(lines))
	if strings.Contains(rail, "Terminals") {
		t.Fatalf("the heading should be the first thing dropped:\n%s", rail)
	}
	if !strings.Contains(rail, shell.Name) {
		t.Fatalf("the shell row should survive:\n%s", rail)
	}
}

// Reorder pairs a row with a sibling in its own block, never across the
// divider, where a swap would write to the store invisibly.
func TestReorderStaysInsideItsBlock(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "agent-one", t.TempDir(), "")
	createSession(t, m, "agent-two", t.TempDir(), "")
	first := spawnTerminal(t, m)
	second := spawnTerminal(t, m)
	pinShells(t, m)

	m.selectSessionRow(t, second.Name)
	target, ok := m.visibleReorderTarget(m.rows[m.cursor], -1)
	if !ok || target.sess.ID != first.ID {
		t.Fatalf("a shell should pair with the shell above it, got %+v %v", target, ok)
	}

	m.selectSessionRow(t, first.Name)
	if target, ok := m.visibleReorderTarget(m.rows[m.cursor], -1); ok {
		t.Fatalf("the first shell must not reach into the tree, got %+v", target.sess.Name)
	}
	m.selectSessionRow(t, "agent-two")
	if target, ok := m.visibleReorderTarget(m.rows[m.cursor], 1); ok {
		t.Fatalf("an agent must not pair across the divider, got %+v", target.sess.Name)
	}
}

// The modal offers the row, and the choice survives a restart through the
// store rather than only through the cached field.
func TestTerminalPlacementPersists(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	spawnTerminal(t, m)

	m.openSettings()
	if body := ansi.Strip(m.viewSettings()); !strings.Contains(body, "terminal rows") || !strings.Contains(body, "pinned") {
		t.Fatalf("settings should offer the placement row on its default:\n%s", body)
	}
	for m.settings.field != settingsFieldTerminals {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})

	if chosen, err := m.store.Setting(terminalPlacementSetting); err != nil || chosen != "inline" {
		t.Fatalf("stored placement = %q err %v, want inline", chosen, err)
	}
	if storedShellsPinned(m.store) {
		t.Fatal("a fresh model should read the stored choice back as nested")
	}
	if m.shellsPinned {
		t.Fatal("the model should mirror the stored choice")
	}
	if m.pinnedShells != 0 {
		t.Fatalf("pinnedShells = %d, want the rail rebuilt on close", m.pinnedShells)
	}
}

// The pinned block stays what an install gets without choosing; nesting is
// the opt-in, so an upgrade never rearranges a list under its reader.
func TestPinnedIsTheDefaultPlacement(t *testing.T) {
	m := buildModel(t)
	if !m.shellsPinned {
		t.Fatal("a model with no stored choice should pin its shells")
	}
	if !storedShellsPinned(m.store) {
		t.Fatal("an unset placement reads as pinned")
	}
}
