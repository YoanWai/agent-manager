package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestRootRowLeadsTheList(t *testing.T) {
	m := shotModel()
	m.rebuildRows()
	if len(m.rows) == 0 {
		t.Fatal("no rows, want root")
	}
	if !m.rows[0].isRoot() {
		t.Fatalf("first row is %+v, want root", m.rows[0])
	}
	// Its sessions stay flat rather than nesting under it.
	for _, row := range m.rows[1:] {
		if !row.isGroup && row.sess.Group == "" && row.depth != 0 {
			t.Fatalf("ungrouped session %q nested at depth %d", row.sess.Name, row.depth)
		}
	}
	if !strings.Contains(ansi.Strip(m.View()), "root") {
		t.Fatal("root row is not painted")
	}
}

// Root is not a stored group, so group edits refuse it rather than running
// against an empty path.
func TestRootRowRefusesGroupEdits(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*Model)
	}{
		{"rename", func(m *Model) { m.openRename() }},
		{"delete", func(m *Model) { m.prepareDelete() }},
		{"reorder", func(m *Model) { m.reorderSelected(1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := shotModel()
			m.rebuildRows()
			m.cursor = 0
			tc.run(m)
			if m.errBar.text == "" {
				t.Fatal("no message explaining the refusal")
			}
			if m.mode != modeList {
				t.Fatalf("mode changed to %v", m.mode)
			}
			if m.confirm.label != "" {
				t.Fatalf("a confirmation was staged: %q", m.confirm.label)
			}
		})
	}
}

// Root's rollup counts top-level sessions, not the whole tree.
func TestRootRollupCountsUngroupedOnly(t *testing.T) {
	m := shotModel()
	counts := m.groupStatusCounts(rootGroup)
	total := 0
	for _, n := range counts {
		total += n
	}
	ungrouped := 0
	for _, sess := range m.sessions {
		if sess.Group == "" {
			ungrouped++
		}
	}
	if total != ungrouped {
		t.Fatalf("root rollup counts %d sessions, want %d ungrouped", total, ungrouped)
	}
}

// A launch opens on a session, not on root's rollup.
func TestCursorSkipsRootOnFirstBuild(t *testing.T) {
	m := shotModel()
	m.cursor = 0
	m.rebuildRows()
	if len(m.rows) < 2 {
		t.Fatalf("want root and a row below it, got %d rows", len(m.rows))
	}
	if m.rows[m.cursor].isRoot() {
		t.Fatal("cursor parked on root with rows available below it")
	}
	// With nothing but root to land on, it is the selection.
	bare := shotModel()
	bare.sessions, bare.rows, bare.cursor = nil, nil, 0
	bare.rebuildRows()
	if len(bare.rows) != 1 || !bare.rows[0].isRoot() || bare.cursor != 0 {
		t.Fatalf("empty list should rest on root, got %d rows cursor %d", len(bare.rows), bare.cursor)
	}
}

// Root reads quieter than the groups the user named.
func TestRootRowIsDimmerThanNamedGroups(t *testing.T) {
	// The suite's default Ascii profile strips every color sequence.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := shotModel()
	m.rebuildRows()
	if len(m.rows) == 0 {
		t.Fatal("no rows, want root")
	}
	root := m.renderTreeRow(m.rows[0], false, 40, 0, panelHex())
	dimmed := strings.TrimPrefix(fgSeq(mix(current.Accent2, current.Subtle, 0.5)), "\x1b[")
	if !strings.Contains(root, dimmed) {
		t.Fatalf("root is not painted in the dimmed tone: %q", root)
	}
	if accent := strings.TrimPrefix(fgSeq(current.Accent2), "\x1b["); strings.Contains(root, accent) {
		t.Fatalf("root still carries the group accent: %q", root)
	}
}

// Root pins a row at the top, which must not stand in for having sessions:
// an empty list still says so.
func TestEmptyListKeepsItsGuidance(t *testing.T) {
	for _, tc := range []struct {
		name     string
		setup    func(*Model)
		wantText string
	}{
		{"no sessions", func(m *Model) {}, "no sessions yet"},
		{"no matches", func(m *Model) { m.search = "nothing-matches-this" }, "no matches"},
		{"nothing archived", func(m *Model) { m.showArchived = true }, "nothing archived"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := shotModel()
			m.sessions, m.rows = nil, nil
			tc.setup(m)
			m.rebuildRows()
			rail := ansi.Strip(strings.Join(splitLines(joinContentText(m.railLines(40, 20))), "\n"))
			if !strings.Contains(rail, tc.wantText) {
				t.Fatalf("rail is missing %q:\n%s", tc.wantText, rail)
			}
			if !strings.Contains(rail, "root") {
				t.Fatalf("root row should still lead the rail:\n%s", rail)
			}
		})
	}
}

func joinContentText(lines []contentLine) string {
	var out []string
	for _, line := range lines {
		out = append(out, line.text)
	}
	return strings.Join(out, "\n")
}

// Root is pinned first, so it can never be the sibling a top-level group
// swaps with; parentGroup("") is "" too, which would otherwise match.
func TestReorderSkipsRootAsSibling(t *testing.T) {
	m := shotModel()
	m.rebuildRows()
	var groupRow, index = treeRow{}, -1
	for i, row := range m.rows {
		if row.isGroup && !row.isRoot() && parentGroup(row.group) == "" {
			groupRow, index = row, i
			break
		}
	}
	if index < 0 {
		t.Fatal("no top-level group row to test with")
	}
	m.cursor = index
	if target, ok := m.visibleReorderTarget(groupRow, -1); ok && target.isRoot() {
		t.Fatalf("root matched as a reorder sibling of %q", groupRow.group)
	}
}
