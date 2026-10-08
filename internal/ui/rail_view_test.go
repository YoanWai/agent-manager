package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"strings"
	"testing"
	"time"
)

func TestArchivedViewShowsOnlyArchivedSessions(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "live-one", dir, "")
	createSession(t, m, "old-one", dir, "")

	m.selectSessionRow(t, "old-one")
	m.archiveSelected()
	_, cmd := m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyCmd(t, cmd)

	if names := sessionNames(m); len(names) != 1 || names[0] != "live-one" {
		t.Fatalf("active view = %v want [live-one]", names)
	}

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	if names := sessionNames(m); len(names) != 1 || names[0] != "old-one" {
		t.Fatalf("archived view = %v want [old-one]", names)
	}
}

func railText(t *testing.T, m *Model) []string {
	t.Helper()
	return railTextAt(m, 60)
}

func railTextAt(m *Model, width int) []string {
	var out []string
	for _, line := range m.entryLines(railRows(m), 0, width, 20) {
		out = append(out, strings.TrimRight(ansi.Strip(line.text), " "))
	}
	return out
}

func lineWith(t *testing.T, lines []string, want string) int {
	t.Helper()
	for i, line := range lines {
		if strings.Contains(line, want) {
			return i
		}
	}
	t.Fatalf("no rail line carrying %q:\n%s", want, strings.Join(lines, "\n"))
	return -1
}

// Compact keeps a session on one line; comfortable moves its meta to a
// second line under the name, and the choice persists.
func TestSettingsTogglesListDensity(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	lines := railText(t, m)
	head := lineWith(t, lines, "alpha")
	if !strings.Contains(lines[head], "claude") {
		t.Fatalf("compact row should carry its meta inline: %q", lines[head])
	}
	if got := m.entryHeight(railRows(m)[0]); got != 1 {
		t.Fatalf("compact entry height = %d want 1", got)
	}

	m.openSettings()
	if m.settings.dialog.comfortableRows {
		t.Fatal("settings should open on compact by default")
	}
	for i := 0; i < settingsFieldDensity; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settings.dialog.field != settingsFieldDensity {
		t.Fatalf("stepping down should reach the density field, got %d", m.settings.dialog.field)
	}
	if card := ansi.Strip(m.viewSettings()); !strings.Contains(card, "list density") {
		t.Fatalf("settings card has no density row:\n%s", card)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	if card := ansi.Strip(m.viewSettings()); !strings.Contains(card, "comfortable") {
		t.Fatalf("toggled card does not read comfortable:\n%s", card)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.drainEffects(t)

	if !m.prefs.comfortableRows {
		t.Fatal("model did not pick up the comfortable density")
	}
	if !storedComfortableRows(m.services.store) {
		t.Fatal("comfortable density did not persist")
	}
	sessionRow := railRows(m)[0]
	for _, row := range railRows(m) {
		if !row.isGroup {
			sessionRow = row
			break
		}
	}
	if got := m.entryHeight(sessionRow); got != 3 {
		t.Fatalf("comfortable entry height = %d want 3", got)
	}

	lines = railText(t, m)
	head = lineWith(t, lines, "alpha")
	if !strings.Contains(lines[head], "claude") {
		t.Fatalf("comfortable name line should carry the meta: %q", lines[head])
	}
	if head+2 >= len(lines) {
		t.Fatalf("comfortable row has no prompt and reply lines:\n%s", strings.Join(lines, "\n"))
	}
	if prompt := strings.TrimSpace(lines[head+1]); prompt == "" {
		t.Fatalf("comfortable row prompt line is blank:\n%s", strings.Join(lines, "\n"))
	}
	if reply := strings.TrimSpace(lines[head+2]); reply == "" {
		t.Fatalf("comfortable row reply line is blank:\n%s", strings.Join(lines, "\n"))
	}
}

// A launch opens on a session, not on root's rollup.
func TestCursorSkipsRootOnFirstBuild(t *testing.T) {
	m := shotModel()
	m.rail = uirail.New(nil)
	m.rebuildRows()
	if len(railRows(m)) < 2 {
		t.Fatalf("want root and a row below it, got %d rows", len(railRows(m)))
	}
	if railRows(m)[m.rail.Cursor()].isRoot() {
		t.Fatal("cursor parked on root with rows available below it")
	}
	// With nothing but root to land on, it is the selection.
	bare := shotModel()
	clearRailInventory(bare)
	if len(railRows(bare)) != 1 || !railRows(bare)[0].isRoot() || bare.rail.Cursor() != 0 {
		t.Fatalf("empty list should rest on root, got %d rows cursor %d", len(railRows(bare)), bare.rail.Cursor())
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
		{"no matches", func(m *Model) { m.rail.SetSearch("nothing-matches-this", false) }, "no matches"},
		{"nothing archived", func(m *Model) { m.rail.SetArchived(true) }, "nothing archived"},
		{"nothing needs attention", func(m *Model) { m.rail.SetFilteringAttention(true) }, "nothing needs attention"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := shotModel()
			clearRailInventory(m)
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
	index := -1
	for i, row := range railRows(m) {
		if row.isGroup && !row.isRoot() && parentGroup(row.group) == "" {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatal("no top-level group row to test with")
	}
	setRailCursor(m, index)
	before := strings.Join(m.groupRowPaths(), ",")
	updated, _ := m.handleKey(runeKey("K"))
	m = updated.(*Model)
	if railRows(m)[0].isRoot() == false || strings.Join(m.groupRowPaths(), ",") != before {
		t.Fatalf("root was treated as a reorder sibling, rows = %v", m.groupRowPaths())
	}
}

func forceANSI256(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// sgrOf returns the color sequence a style emits under the active profile,
// so contrast assertions track the live theme instead of a hardcoded index.
func sgrOf(rendered string) string {
	_, seq, found := strings.Cut(rendered, "\x1b[")
	if !found {
		return ""
	}
	code, _, _ := strings.Cut(seq, "m")
	return code
}

// A content separator stops at the pane's edge instead of crossing the seam.
func TestContentRuleStopsAtSeam(t *testing.T) {
	m := shotModel()
	leftWidth, _ := m.splitWidths()
	rows := strings.Split(preparedView(m), "\n")
	start, end := m.bodyYRange()

	crossings := 0
	for i := start; i < end; i++ {
		row := []rune(ansi.Strip(rows[i]))
		contentRule := row[leftWidth+2] == '─'
		railRule := row[leftWidth-2] == '─'
		if contentRule && !railRule && row[leftWidth] == '─' {
			t.Fatalf("row %d: content rule crosses the seam:\n%s", i, string(row))
		}
		if railRule && row[leftWidth] == '─' {
			crossings++
		}
	}
	// The rail's own rule still runs the width of its pane, seam included.
	if crossings == 0 {
		t.Fatal("no rail rule crossed the seam; the pane's own rules should")
	}
}

// Whatever the cursor is on has to be on screen. A window that reserves
// room for one overflow indicator but paints two loses a row at the bottom,
// and the row it loses is the one the cursor just moved to.
func TestRailCursorAlwaysPainted(t *testing.T) {
	now := time.Now()
	sessions := make([]store.Session, 40)
	rows := make([]treeRow, len(sessions))
	for i := range sessions {
		name := fmt.Sprintf("session-%02d", i)
		sessions[i] = store.Session{
			ID: name, Name: name, Tool: "claude", Status: status.Idle,
			CreatedAt: now, LastStatusAt: now,
		}
		rows[i] = treeRow{sess: sessions[i]}
	}

	for _, size := range []struct{ w, h int }{{80, 16}, {100, 24}, {120, 30}, {160, 44}} {
		for _, cursor := range []int{0, 1, len(rows) / 2, len(rows) - 2, len(rows) - 1} {
			m := &Model{
				layout: layoutState{
					width:  size.w,
					height: size.h,
					split:  splitState{ratio: defaultSplitRatio},
				},
				mode:      modeList,
				workspace: workspace{sessions: sessions},
				rail:      railModelFromRows(rows, cursor),
			}
			view := ansi.Strip(preparedView(m))
			if !strings.Contains(view, sessions[cursor].Name) {
				t.Errorf("%dx%d cursor=%d: %q is selected but never painted:\n%s",
					size.w, size.h, cursor, sessions[cursor].Name, view)
			}
		}
	}
}

func TestFilterBadgesStackOverTheList(t *testing.T) {
	m := shotModel()
	m.layout.width, m.layout.height = 120, 40
	m.rail.SetArchived(true)
	m.rail.SetHideEmptyGroups(true)
	m.rail.SetFilteringAttention(true)
	rail := ansi.Strip(railLinesText(m.railLines(36, m.listBodyHeight())))
	var painted []string
	for _, line := range strings.Split(rail, "\n") {
		if strings.TrimSpace(line) != "" {
			painted = append(painted, line)
		}
	}
	want := [][2]string{
		{"ARCHIVED", "t back to active"},
		{"ATTENTION", "w show all"},
	}
	if len(painted) < len(want) {
		t.Fatalf("rail painted %d lines, want the %d badges first:\n%s", len(painted), len(want), rail)
	}
	for i, badge := range want {
		line := painted[i]
		if !strings.Contains(line, badge[0]) || !strings.Contains(line, badge[1]) {
			t.Errorf("rail line %d = %q, want %q beside %q", i, line, badge[0], badge[1])
		}
	}
	header := ansi.Strip(strings.Join(m.viewHeaderRows(), "\n"))
	if !strings.Contains(header, "· archived") {
		t.Errorf("header should keep the plain scope word:\n%s", header)
	}
	for _, unwanted := range []string{"ARCHIVED", "ATTENTION", "HIDE EMPTY"} {
		if strings.Contains(header, unwanted) {
			t.Errorf("header still carries the %s badge:\n%s", unwanted, header)
		}
	}
}

func TestHideEmptyBadgeBelongsToTheActiveRail(t *testing.T) {
	m := shotModel()
	m.layout.width, m.layout.height = 120, 40
	m.rail.SetHideEmptyGroups(true)
	for _, tc := range []struct {
		archived bool
		want     bool
	}{{false, true}, {true, false}} {
		m.rail.SetArchived(tc.archived)
		rail := ansi.Strip(railLinesText(m.railLines(36, m.listBodyHeight())))
		if got := strings.Contains(rail, "HIDE EMPTY"); got != tc.want {
			t.Errorf("showArchived=%v: HIDE EMPTY painted = %v, want %v:\n%s", tc.archived, got, tc.want, rail)
		}
	}
}

func TestRailTopCarriesBetweenFrames(t *testing.T) {
	m := shotModel()
	m.prefs.comfortableRows = true
	m.prefs.fullLayout = true
	m.workspace.sessions = nil
	m.workspace.groups = nil
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("session-%02d", i)
		m.workspace.sessions = append(m.workspace.sessions, store.Session{ID: name, Name: name, Tool: "claude", Status: status.Idle})
	}
	m.rebuildRows()

	heights := make([]int, len(railRows(m)))
	for i := range railRows(m) {
		heights[i] = m.entryHeight(railRows(m)[i])
	}

	const height = 20
	tops := make([]int, len(railRows(m)))
	for i := range railRows(m) {
		prev, prevEnd := m.layout.displayedRail.Window.Start, m.layout.displayedRail.Window.End
		setRailCursor(m, i)
		m.entryLines(railRows(m), 0, m.layout.width-1, height)
		tops[i] = m.layout.displayedRail.Window.Start
		if m.layout.displayedRail.Window.Start > i {
			t.Fatalf("cursor %d: rail starts below it at %d", i, m.layout.displayedRail.Window.Start)
		}
		if i >= prev && i < prevEnd && m.layout.displayedRail.Window.Start != prev {
			t.Fatalf("cursor %d already sat in [%d,%d) and the rail scrolled to %d", i, prev, prevEnd, m.layout.displayedRail.Window.Start)
		}
	}
	if tops[0] != 0 || tops[1] != 0 {
		t.Fatalf("the rail scrolled off the first entry immediately: %v", tops[:3])
	}
	if tops[len(tops)-1] == 0 {
		t.Fatal("the rail never scrolled across 30 comfortable entries")
	}

	for i := len(railRows(m)) - 1; i >= 0; i-- {
		setRailCursor(m, i)
		m.entryLines(railRows(m), 0, m.layout.width-1, height)
		if m.layout.displayedRail.Window.Start > i {
			t.Fatalf("cursor %d: rail starts below it at %d", i, m.layout.displayedRail.Window.Start)
		}
	}
	if m.layout.displayedRail.Window.Start != 0 {
		t.Fatalf("stepping back to the first entry left the rail at %d", m.layout.displayedRail.Window.Start)
	}
}

func TestHiddenStatsReturnRowsToSessions(t *testing.T) {
	for _, full := range []bool{false, true} {
		m := footModel(t)
		m.layout.width, m.layout.height, m.prefs.fullLayout = 120, 34, full
		for i := 0; i < 50; i++ {
			name := fmt.Sprintf("session-%02d", i)
			m.workspace.sessions = append(m.workspace.sessions, store.Session{ID: name, Name: name, Tool: "claude", Status: status.Idle})
		}
		m.rebuildRows()
		const height = 25
		visibleRows := func() int {
			m.railLines(70, height)
			count := 0
			for _, line := range m.layout.displayedRail.Lines {
				if line.HitOK && line.Hit.Kind == uirail.SessionRow {
					count++
				}
			}
			return count
		}
		footRows := len(m.railFootLines(70)) + 1
		shown := visibleRows()
		m.prefs.hideStats = true
		if got := visibleRows(); got != shown+footRows {
			t.Fatalf("full=%v: hidden stats expose %d sessions, want %d", full, got, shown+footRows)
		}
		if len(m.notices.active(m)) == 0 {
			t.Fatal("hiding stats must retain unread notices")
		}
	}
}

// With the mouse off nothing on a row answers a click, so the rail paints
// no handle and no menu button, and the name keeps their cells.
func TestMouseOffPaintsNoHandleOrMenuButton(t *testing.T) {
	m := shotModel()
	m.prefs.mouseDisabled = true
	for _, line := range m.entryLines(railRows(m), 0, 44, 20) {
		text := ansi.Strip(line.text)
		if strings.Contains(text, reorderGrip) || strings.Contains(text, rowMenuGlyph) {
			t.Fatalf("mouse off should paint no handle or %s: %q", rowMenuGlyph, text)
		}
	}
	m.prefs.mouseDisabled = false
	painted := false
	for _, line := range m.entryLines(railRows(m), 0, 44, 20) {
		painted = painted || strings.Contains(ansi.Strip(line.text), reorderGrip)
	}
	if !painted {
		t.Fatal("test setup: the mouse on should paint handles")
	}
}

// The rail's banners (search, archived) cost the list rows, so a short
// terminal has to keep painting entries under them — and keep painting at
// all: claiming rows the list does not have used to slice past the end.
func TestRailBannersSurviveShortTerminals(t *testing.T) {
	for _, height := range []int{4, 6, 8, 10, 14} {
		for _, width := range []int{30, 60, 120} {
			for _, searching := range []bool{false, true} {
				for _, archived := range []bool{false, true} {
					m := shotModel()
					m.layout.width, m.layout.height = width, height
					m.rail.SetSearch(m.rail.Search(), searching)
					m.rail.SetArchived(archived)
					m.errBar.text = "worktree kept (has work): /Users/someone/dev/api"
					rows := strings.Split(preparedView(m), "\n")
					if len(rows) != height {
						t.Errorf("%dx%d search=%v archived=%v: frame is %d rows",
							width, height, searching, archived, len(rows))
					}
				}
			}
		}
	}
}

// A rail with room for its banners still lists entries under them.
func TestRailBannersLeaveRoomForEntries(t *testing.T) {
	m := shotModel()
	m.layout.width, m.layout.height = 120, 34
	m.rail.SetSearch("rate", true)
	rail := railLinesText(m.railLines(36, m.listBodyHeight()))
	if !strings.Contains(rail, "⌕ rate") {
		t.Fatalf("no search field in the rail:\n%s", rail)
	}
	if !strings.Contains(rail, "add-rate-limiting") {
		t.Fatalf("search banner crowded the entries out:\n%s", rail)
	}
}

// A rail too short for the padded block keeps the badges themselves; only a
// rail with no room for entries under them drops them altogether.
func TestFilterBadgesSurviveShortRails(t *testing.T) {
	for _, height := range []int{10, 14, 20} {
		m := shotModel()
		m.layout.width, m.layout.height = 120, height
		m.rail.SetArchived(true)
		rail := ansi.Strip(railLinesText(m.railLines(36, m.listBodyHeight())))
		if !strings.Contains(rail, "ARCHIVED") {
			t.Errorf("height %d dropped the archived badge:\n%s", height, rail)
		}
	}
}

func railLinesText(lines []contentLine) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line.text + "\n")
	}
	return b.String()
}

// The query is the field being typed into: a rail too tight for the padded
// block keeps the bare field, and a query longer than the rail keeps its end,
// where the caret is.
func TestSearchFieldSurvivesTightRails(t *testing.T) {
	for _, height := range []int{14, 20, 34} {
		m := shotModel()
		m.layout.width, m.layout.height = 120, height
		m.rail.SetSearch("add-rate-limiting-in-the-public-api-handler", true)
		rail := railLinesText(m.railLines(36, m.listBodyHeight()))
		if !strings.Contains(rail, "⌕") {
			t.Errorf("height %d dropped the search field:\n%s", height, rail)
		}
		if !strings.Contains(rail, "api-handler") {
			t.Errorf("height %d cut the end of the query away:\n%s", height, rail)
		}
		for _, line := range strings.Split(rail, "\n") {
			if got := ansi.StringWidth(ansi.Strip(line)); got > 36 {
				t.Errorf("height %d: rail line is %d wide: %q", height, got, ansi.Strip(line))
			}
		}
	}
}
