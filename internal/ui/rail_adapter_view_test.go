package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestArchivedViewShowsOnlyArchivedSessions(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "live-one", dir, "")
	createSession(t, m, "old-one", dir, "")

	m.selectSessionRow(t, "old-one")
	m.archiveSelected()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
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
		out = append(out, strings.TrimRight(ansi.Strip(line.Text), " "))
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
	if m.settingsCard().State().ComfortableRows {
		t.Fatal("settings should open on compact by default")
	}
	for i := 0; i < uisettings.FieldDensity; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settingsCard().State().Field != uisettings.FieldDensity {
		t.Fatalf("stepping down should reach the density field, got %d", m.settingsCard().State().Field)
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
	if !storedFlag(t, m, uisettings.ListDensitySetting) {
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
			rail := ansi.Strip(strings.Join(uitheme.SplitLines(joinContentText(m.railLines(40, 20))), "\n"))
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
		out = append(out, line.Text)
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
		if row.isGroup && !row.isRoot() && uirail.ParentGroup(row.group) == "" {
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
					split:  uigeometry.Gesture{Ratio: uigeometry.DefaultSplitRatio},
				},
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
		if len(m.activeNotices()) == 0 {
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
		text := ansi.Strip(line.Text)
		if strings.Contains(text, reorderGrip) || strings.Contains(text, rowMenuGlyph) {
			t.Fatalf("mouse off should paint no handle or %s: %q", rowMenuGlyph, text)
		}
	}
	m.prefs.mouseDisabled = false
	painted := false
	for _, line := range m.entryLines(railRows(m), 0, 44, 20) {
		painted = painted || strings.Contains(ansi.Strip(line.Text), reorderGrip)
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
		b.WriteString(line.Text + "\n")
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

func TestGroupRowRendersGroupPane(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "api-agent", dir, "backend")
	for i, row := range railRows(m) {
		if row.isGroup && row.group == "backend" {
			setRailCursor(m, i)
		}
	}

	detail := ansi.Strip(m.viewDetail(112))
	if !strings.Contains(detail, dir) {
		t.Fatalf("group detail missing path %q:\n%s", dir, detail)
	}
	if !strings.Contains(detail, "1 agent") {
		t.Fatalf("group detail missing agent count:\n%s", detail)
	}

	agents := ansi.Strip(m.viewGroupAgents("backend", 112, 10))
	if !strings.Contains(agents, "api-agent") {
		t.Fatalf("agents list missing session:\n%s", agents)
	}

	inherited := ansi.Strip(m.viewGroupDetail("backend/sub", 112))
	if !strings.Contains(inherited, dir) || !strings.Contains(inherited, "inherited") {
		t.Fatalf("subgroup should inherit the ancestor path:\n%s", inherited)
	}
}

// A group stays one line at any density: it has neither a prompt nor a
// reply to carry.
func TestComfortableGroupRowStacks(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.prefs.comfortableRows = true
	m.openGroupForm()
	m.groupSurface().SetName("fleet")
	_, cmd := m.submitGroupForm()
	if m.errBar.text != "" {
		t.Fatalf("create group: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	createSession(t, m, "beta", t.TempDir(), "fleet")

	lines := railText(t, m)
	head := lineWith(t, lines, "fleet")
	if !strings.Contains(lines[head], presentation.GlyphsFor("").StatusGlyph(status.Starting)) {
		t.Fatalf("group row should carry its starting agent inline: %q", lines[head])
	}
}

// A rail too short for the counters keeps the selected entry whole: a
// three-line row trimmed to one reads as a compact row that lost its
// message lines.
func TestComfortableRowSurvivesShortRail(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.prefs.comfortableRows = true
	for _, name := range []string{"one", "two", "three", "four"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	m.selectSessionRow(t, "three")

	lines := m.entryLines(railRows(m), 0, 60, 2)
	if len(lines) != 2 {
		t.Fatalf("entry lines = %d want 2", len(lines))
	}
	head := lineWith(t, []string{ansi.Strip(lines[0].Text), ansi.Strip(lines[1].Text)}, "three")
	if head != 0 {
		t.Fatalf("selected entry should start the window, got line %d", head)
	}
	if prompt := strings.TrimSpace(ansi.Strip(lines[1].Text)); prompt == "" {
		t.Fatalf("selected entry lost its prompt line: %q", ansi.Strip(lines[1].Text))
	}
}

// A nested entry's second line carries its ancestors' branches straight
// down, so the tree column has no gap between an entry and the next.
func TestComfortableMetaLineKeepsTreeGuides(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.prefs.comfortableRows = true
	m.openGroupForm()
	m.groupSurface().SetName("outer")
	_, cmd := m.submitGroupForm()
	if m.errBar.text != "" {
		t.Fatalf("create outer group: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	m.selectGroupRow(t, "outer")
	m.openGroupForm()
	m.groupSurface().SetName("inner")
	_, cmd = m.submitGroupForm()
	if m.errBar.text != "" {
		t.Fatalf("create inner group: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	createSession(t, m, "nested", t.TempDir(), "outer/inner")
	createSession(t, m, "sibling", t.TempDir(), "outer")

	lines := railText(t, m)
	head := lineWith(t, lines, "sibling")
	if head+1 >= len(lines) {
		t.Fatalf("entry has no meta line:\n%s", strings.Join(lines, "\n"))
	}
	name, meta := lines[head], lines[head+1]
	nameRunes, metaRunes := []rune(name), []rune(meta)
	guideAt := -1
	for i, r := range nameRunes {
		if r == '├' {
			guideAt = i
			break
		}
	}
	if guideAt < 0 {
		t.Fatalf("entry has no branch connector: %q\n%s", name, strings.Join(lines, "\n"))
	}
	if len(metaRunes) <= guideAt || metaRunes[guideAt] != '│' {
		t.Fatalf("meta line breaks the guide column at %d:\n%q\n%q", guideAt, name, meta)
	}
}

// A message waiting for a session is marked on its row in either density,
// and on no row that has nothing waiting.
func TestInboxBadgeRidesTheRowWithMessages(t *testing.T) {
	for _, comfortable := range []bool{false, true} {
		m := shotModel()
		m.prefs.comfortableRows = comfortable
		m.workspace.queuedMessages = map[string]int{"add-rate-limiting": 2}

		rows := railText(t, m)
		badged := lineWith(t, rows, "add-rate-limiting")
		if !strings.Contains(rows[badged], "✉2") {
			t.Fatalf("comfortable=%v: row lost its badge: %q", comfortable, rows[badged])
		}
		for i, row := range rows {
			if i != badged && strings.Contains(row, "✉") {
				t.Errorf("comfortable=%v: line %d wears a badge it has no messages for: %q",
					comfortable, i, row)
			}
		}
	}
}

// A session nobody has written to renders exactly as it did before the
// badge existed: no glyph, and not one cell of padding.
func TestRowsWithoutMessagesRenderUnchanged(t *testing.T) {
	bare := railText(t, shotModel())
	badged := shotModel()
	badged.workspace.queuedMessages = map[string]int{"add-rate-limiting": 2}
	marked := railText(t, badged)

	if len(bare) != len(marked) {
		t.Fatalf("badge changed the row count: %d then %d", len(bare), len(marked))
	}
	at := lineWith(t, marked, "✉2")
	for i := range bare {
		if i != at && bare[i] != marked[i] {
			t.Errorf("line %d moved for a badge it does not carry:\n%q\n%q", i, bare[i], marked[i])
		}
	}
	for _, row := range bare {
		if strings.Contains(row, "✉") {
			t.Errorf("a rail with no queued messages drew a badge: %q", row)
		}
	}
}

// A rail too narrow for the whole row gives up the tool and the age before
// the badge: those readings are on the row beside it, a waiting message is
// nowhere else. Past that the name shortens before the badge goes.
func TestInboxBadgeOutlivesTheRowMeta(t *testing.T) {
	for _, width := range []int{28, 30, 36, 44, 60} {
		m := shotModel()
		m.workspace.queuedMessages = map[string]int{"add-rate-limiting": 2}

		for _, line := range m.entryLines(railRows(m), 0, width, 20) {
			if got := ansi.StringWidth(line.Text); got > width {
				t.Errorf("width %d: rail line is %d wide: %q", width, got, ansi.Strip(line.Text))
			}
		}
		rows := railTextAt(m, width)
		row := rows[lineWith(t, rows, "✉2")]
		name := "add-rate-limiting"
		if width < 36 {
			name = "add-rate"
		}
		if !strings.Contains(row, name) {
			t.Errorf("width %d: the badge cost the name: %q", width, row)
		}
	}

	m := shotModel()
	m.workspace.queuedMessages = map[string]int{"add-rate-limiting": 2}
	rows := railTextAt(m, 30)
	row := rows[lineWith(t, rows, "✉2")]
	if strings.Contains(row, "ago") {
		t.Fatalf("30 columns still fit the age, so the row shed nothing: %q", row)
	}
}

func TestRootRowLeadsTheList(t *testing.T) {
	m := shotModel()
	m.rebuildRows()
	if len(railRows(m)) == 0 {
		t.Fatal("no rows, want root")
	}
	if !railRows(m)[0].isRoot() {
		t.Fatalf("first row is %+v, want root", railRows(m)[0])
	}
	// Its sessions stay flat rather than nesting under it.
	for _, row := range railRows(m)[1:] {
		if !row.isGroup && row.sess.Group == "" && row.depth != 0 {
			t.Fatalf("ungrouped session %q nested at depth %d", row.sess.Name, row.depth)
		}
	}
	if !strings.Contains(ansi.Strip(preparedView(m)), "root") {
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
			setRailCursor(m, 0)
			tc.run(m)
			if m.errBar.text == "" {
				t.Fatal("no message explaining the refusal")
			}
			if !m.onList() {
				t.Fatalf("mode changed to %v", m.screenID())
			}
			if pendingConfirm(m).prompt.Label != "" {
				t.Fatalf("a confirmation was staged: %q", pendingConfirm(m).prompt.Label)
			}
		})
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
	if len(railRows(m)) == 0 {
		t.Fatal("no rows, want root")
	}
	root := m.renderTreeRow(railRows(m)[0], false, 40, 0, uitheme.PanelHex())
	dimmed := strings.TrimPrefix(uitheme.FgSeq(uitheme.Mix(uitheme.Current().Accent2, uitheme.Current().Subtle, 0.5)), "\x1b[")
	if !strings.Contains(root, dimmed) {
		t.Fatalf("root is not painted in the dimmed tone: %q", root)
	}
	if accent := strings.TrimPrefix(uitheme.FgSeq(uitheme.Current().Accent2), "\x1b["); strings.Contains(root, accent) {
		t.Fatalf("root still carries the group accent: %q", root)
	}
}

func TestSelectedRowMetaUsesBrightNotSubtle(t *testing.T) {
	forceANSI256(t)

	m := &Model{}
	entry := treeRow{
		sess: store.Session{
			ID:        "s1",
			Name:      "demo-session",
			Tool:      "grok",
			Status:    status.Finished,
			CreatedAt: time.Now().Add(-3 * time.Hour),
		},
	}
	selected := m.renderTreeRow(entry, true, 80, 0, uitheme.SelectedHex())
	unselected := m.renderTreeRow(entry, false, 80, 0, uitheme.PanelHex())

	if !strings.Contains(selected, "\x1b[") {
		t.Fatal("selected row has no SGR; color profile not active")
	}
	subtleSeq := sgrOf(uitheme.SubtleStyle().Render("x"))
	brightSeq := sgrOf(lipgloss.NewStyle().Foreground(uitheme.ColorBright()).Render("x"))
	if strings.Contains(selected, subtleSeq) {
		t.Fatalf("selected row still uses the subtle fg %q:\n%q", subtleSeq, selected)
	}
	if !strings.Contains(unselected, subtleSeq) {
		t.Fatalf("unselected row should use the subtle fg %q:\n%q", subtleSeq, unselected)
	}
	if !strings.Contains(selected, brightSeq) {
		t.Fatalf("selected row missing the bright reapply fg %q:\n%q", brightSeq, selected)
	}
	if !strings.Contains(selected, " · grok") {
		t.Fatalf("selected missing meta text:\n%q", selected)
	}
}

// A row whose conversation id was captured names it in the comfortable
// meta, since that id is what a revive resumes; the compact row stays
// crowded and a row with nothing captured must not show an id at all.
func TestSessionRowCarriesTheCapturedConversationIDInMeta(t *testing.T) {
	const conversation = "conv-abc-123"
	compact := &Model{}
	comfortable := &Model{prefs: preferences{comfortableRows: true}}
	withID := treeRow{sess: store.Session{
		ID: "s1", Name: "with-conversation", Tool: "claude", Status: status.Finished,
		CreatedAt: time.Now().Add(-3 * time.Hour), AgentSessionID: conversation,
	}}
	withoutID := treeRow{sess: store.Session{
		ID: "s2", Name: "without-conversation", Tool: "claude", Status: status.Finished,
		CreatedAt: time.Now().Add(-3 * time.Hour),
	}}

	row := ansi.Strip(comfortable.renderTreeRow(withID, false, 120, 0, uitheme.PanelHex()))
	if !strings.Contains(row, conversation) {
		t.Fatalf("the comfortable row should carry the captured id in its meta:\n%s", row)
	}
	row = ansi.Strip(compact.renderTreeRow(withID, false, 120, 0, uitheme.PanelHex()))
	if strings.Contains(row, conversation) {
		t.Fatalf("the compact row has no room for the id:\n%s", row)
	}
	row = ansi.Strip(comfortable.renderTreeRow(withoutID, false, 120, 0, uitheme.PanelHex()))
	if strings.Contains(row, conversation) {
		t.Fatalf("a session with no captured id must not show one:\n%s", row)
	}
}

// A spawn hands its agent the rename directive, so the row stands in for
// the generated name until the agent answers, and settles on the generated
// one as soon as that answer can no longer come.
func TestSessionRowStandsInForAnAwaitedName(t *testing.T) {
	const generated = "claude-ab12"
	now := time.Now()
	for _, tc := range []struct {
		name    string
		sess    store.Session
		awaited bool
		want    string
	}{
		{
			name:    "waiting on the agent",
			sess:    store.Session{ID: "s1", Name: generated, Tool: "claude", Status: status.Starting, CreatedAt: now},
			awaited: true,
			want:    namePlaceholder,
		},
		{
			name:    "rename landed",
			sess:    store.Session{ID: "s1", Name: "row-placeholder", Tool: "claude", Status: status.Working, CreatedAt: now},
			awaited: true,
			want:    "row-placeholder",
		},
		{
			name:    "pane died before renaming",
			sess:    store.Session{ID: "s1", Name: generated, Tool: "claude", Status: status.Dead, CreatedAt: now},
			awaited: true,
			want:    generated,
		},
		{
			name:    "grace ran out",
			sess:    store.Session{ID: "s1", Name: generated, Tool: "claude", Status: status.Working, CreatedAt: now.Add(-renameGrace - time.Second)},
			awaited: true,
			want:    generated,
		},
		{
			name: "no directive was sent",
			sess: store.Session{ID: "s1", Name: generated, Tool: "claude", Status: status.Starting, CreatedAt: now},
			want: generated,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{}
			if tc.awaited {
				m.ledger.lifecycle.AwaitedRenames = map[string]uilifecycle.AwaitedRename{tc.sess.ID: {Generated: generated}}
			}
			row := ansi.Strip(m.renderTreeRow(treeRow{sess: tc.sess}, false, 80, 0, uitheme.PanelHex()))
			if !strings.Contains(row, tc.want) {
				t.Fatalf("row is missing %q:\n%s", tc.want, row)
			}
			if tc.want != generated && strings.Contains(row, generated) {
				t.Fatalf("row still shows the generated name:\n%s", row)
			}
			if tc.want == namePlaceholder {
				return
			}
			if strings.Contains(row, reorderGrip+" "+namePlaceholder) {
				t.Fatalf("row still stands in for a name it has:\n%s", row)
			}
			if _, still := m.ledger.lifecycle.AwaitedRenames[tc.sess.ID]; still {
				t.Fatal("a wait that is over should drop what it was holding")
			}
		})
	}
}

// The rail row is not the only place a name is printed, and the roster sits
// beside it in the same frame, so every reading of a session stands in for
// the awaited name together.
func TestEveryReadingOfASessionStandsInForAnAwaitedName(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	const generated = "claude-ab12"
	if err := m.spawnSession("claude", generated, dir, "backend", "do things", true, false, config.Choice{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}

	type reading struct{ where, text string }
	m.selectGroupRow(t, "backend")
	readings := []reading{{"roster", ansi.Strip(m.viewGroupAgents("backend", 112, 10))}}
	m.selectSessionRow(t, generated)
	readings = append(readings, reading{"detail", strings.Split(ansi.Strip(m.viewDetail(112)), "\n")[0]})
	m.openQuickMode()
	readings = append(readings, reading{"quick bar", ansi.Strip(m.viewQuickBar(112, uiquick.BarMaxRows))})

	// The prompt the spawn was given is what every reading wears until the
	// agent answers with a name of its own.
	const standIn = "do things"
	for _, shown := range readings {
		if strings.Contains(shown.text, generated) {
			t.Errorf("%s shows the generated name:\n%s", shown.where, shown.text)
		}
		if !strings.Contains(shown.text, standIn) {
			t.Errorf("%s does not stand in for the awaited name:\n%s", shown.where, shown.text)
		}
	}
}

// Every filter the list is under names itself over the list, beside the key
// that lifts it, and the header stops repeating them.
// A focused session nested three groups deep at the narrowest split rail
// keeps its inbox badge: the name goes first, then the focus badge, since
// the pane beside the rail already shows what is focused.
func TestInboxBadgeOutlivesTheNameAndTheFocusBadge(t *testing.T) {
	t.Parallel()
	for _, width := range []int{29, 27} {
		m := buildModel(t)
		m.workspace.groupPaths = map[string]string{"a/b/c": "/tmp"}
		m.workspace.sessions = []store.Session{{ID: "x", Name: "some-long-session-name", Tool: "claude", Group: "a/b/c"}}
		m.rebuildRows()
		setRailCursor(m, len(railRows(m))-1)
		m.push(newFocusSurface(m))
		m.workspace.queuedMessages = map[string]int{"x": 2}

		rows := railTextAt(m, width)
		row := rows[lineWith(t, rows, "✉2")]
		if strings.Contains(row, "FOCUS") {
			t.Errorf("width %d: the focus badge outlived the name: %q", width, row)
		}
	}
}

func TestRowMarksSessionsOnAnotherServer(t *testing.T) {
	const here, there = "/tmp/tmux-501/agentmgr", "/tmp/another-manager/agentmgr"
	for _, tc := range []struct {
		name     string
		socket   string
		leading  bool
		elsewise bool
	}{
		{name: "another server", socket: there, elsewise: true},
		{name: "this server", socket: here},
		{name: "unclaimed under this manager", socket: "", leading: true},
		{name: "unclaimed under another manager", socket: "", elsewise: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			sess := store.Session{
				ID: "away-1", Name: "away", Tool: "claude", Status: status.Working,
				CreatedAt: now, LastStatusAt: now, TmuxSocket: tc.socket,
			}
			m := &Model{
				layout: layoutState{
					width:  120,
					height: 40,
					split:  uigeometry.Gesture{Ratio: uigeometry.DefaultSplitRatio},
				},
				workspace: workspace{sessions: []store.Session{sess},

					tmuxSocket: here, leadingManager: tc.leading},
				rail: railModelFromRows([]treeRow{{sess: sess}}, 0),
			}
			view := ansi.Strip(preparedView(m))
			if strings.Contains(view, "elsewhere") != tc.elsewise {
				t.Fatalf("elsewhere marker = %v, want %v:\n%s", !tc.elsewise, tc.elsewise, view)
			}
		})
	}
}

// A manager that has not polled yet knows no server to compare against, so
// it marks nothing.
func TestRowsAreUnmarkedBeforeTheFirstPoll(t *testing.T) {
	now := time.Now()
	sess := store.Session{
		ID: "away-1", Name: "away", Tool: "claude", Status: status.Working,
		CreatedAt: now, LastStatusAt: now, TmuxSocket: "/tmp/another-manager/agentmgr",
	}
	m := &Model{
		layout: layoutState{
			width:  120,
			height: 40,
			split:  uigeometry.Gesture{Ratio: uigeometry.DefaultSplitRatio},
		},
		workspace: workspace{sessions: []store.Session{sess}},
		rail:      railModelFromRows([]treeRow{{sess: sess}}, 0),
	}
	if view := ansi.Strip(preparedView(m)); strings.Contains(view, "elsewhere") {
		t.Fatalf("nothing to compare against should mark nothing:\n%s", view)
	}
}

// A compact session row is one line wearing the reply inline; the
// comfortable density unfolds it to three — name, the last prompt, the
// last reply — and a group is one line at either density, in either
// layout.
func TestRowHeightsFollowDensity(t *testing.T) {
	m := shotModel()
	m.prefs.fullLayout = true
	m.workspace.paneLines = map[string]string{"add-rate-limiting": "Running tests… (14s · esc to interrupt)"}
	row := sessionRow(t, m, "add-rate-limiting")
	row.sess.LastPrompt = "add a token bucket limiter to the public api"
	if m.prefs.comfortableRows {
		t.Fatal("this test starts at the compact density")
	}
	if got := m.entryHeight(row); got != 1 {
		t.Fatalf("compact session entry height = %d, want 1", got)
	}
	if got := m.entryHeight(groupRow(t, m, "backend")); got != 1 {
		t.Fatalf("group entry height = %d, want 1", got)
	}
	lines := uitheme.SplitLines(m.renderTreeRow(row, false, m.layout.width-1, 4, uitheme.PanelHex()))
	if len(lines) != 1 {
		t.Fatalf("compact row painted %d lines, want 1", len(lines))
	}
	top := ansi.Strip(lines[0])
	for _, want := range []string{"add-rate-limiting", "Running tests", "working", "claude"} {
		if !strings.Contains(top, want) {
			t.Errorf("compact row misses %q:\n%s", want, top)
		}
	}

	m.prefs.comfortableRows = true
	if got := m.entryHeight(groupRow(t, m, "backend")); got != 1 {
		t.Fatalf("comfortable group entry height = %d, want 1", got)
	}
	if got := m.entryHeight(row); got != 3 {
		t.Fatalf("comfortable session entry height = %d, want 3", got)
	}
	lines = uitheme.SplitLines(m.renderTreeRow(row, false, m.layout.width-1, 4, uitheme.PanelHex()))
	if len(lines) != 3 {
		t.Fatalf("comfortable row painted %d lines, want 3", len(lines))
	}
	top = ansi.Strip(lines[0])
	for _, want := range []string{"add-rate-limiting", "working", "claude"} {
		if !strings.Contains(top, want) {
			t.Errorf("comfortable row line 1 misses %q:\n%s", want, top)
		}
	}
	if prompt := ansi.Strip(lines[1]); !strings.Contains(prompt, "❯ add a token bucket limiter") {
		t.Fatalf("line 2 should carry the last prompt behind ❯:\n%s", prompt)
	}
	if reply := ansi.Strip(lines[2]); !strings.Contains(reply, "↳ Running tests") {
		t.Fatalf("line 3 should carry the reply behind ↳:\n%s", reply)
	}

	// The same rhythm holds in the split layout.
	m.prefs.fullLayout = false
	if got := m.entryHeight(row); got != 3 {
		t.Fatalf("split comfortable session entry height = %d, want 3", got)
	}
	m.prefs.comfortableRows = false
	if got := m.entryHeight(row); got != 1 {
		t.Fatalf("split compact session entry height = %d, want 1", got)
	}
	narrow := uitheme.SplitLines(m.renderTreeRow(row, false, 60, 4, uitheme.PanelHex()))
	if len(narrow) != 1 {
		t.Fatalf("split compact row painted %d lines, want 1", len(narrow))
	}
}

func TestRowWaitingReplyWearsTheStateColor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := shotModel()
	m.prefs.fullLayout = true
	m.prefs.comfortableRows = true
	question := "Allow edits to router.go?"
	m.workspace.paneLines = map[string]string{"db-migrations": question}
	lines := uitheme.SplitLines(m.renderTreeRow(sessionRow(t, m, "db-migrations"), false, m.layout.width-1, 0, uitheme.PanelHex()))
	if len(lines) != 3 {
		t.Fatalf("waiting row painted %d lines, want 3", len(lines))
	}
	tinted := strings.TrimSuffix(
		lipgloss.NewStyle().Foreground(statusColor(status.Waiting)).Render(question), "\x1b[0m")
	if !strings.Contains(lines[2], tinted) {
		t.Fatalf("waiting question should wear the waiting color:\n%q", lines[2])
	}
}

// A status frozen before the archive (an older build's "working") must
// not read as alive from inside the archive.
func TestArchivedRowReadsDead(t *testing.T) {
	m := shotModel()
	m.prefs.fullLayout = true
	row := sessionRow(t, m, "add-rate-limiting")
	row.sess.Archived = true
	line := ansi.Strip(m.renderTreeRow(row, false, m.layout.width-1, 4, uitheme.PanelHex()))
	if !strings.Contains(line, statusLabel(status.Dead)) {
		t.Fatalf("archived row should read dead:\n%s", line)
	}
	if strings.Contains(line, statusLabel(status.Working)) {
		t.Fatalf("archived row still claims its frozen state:\n%s", line)
	}
}

func TestShellRowSkipsThePromptLine(t *testing.T) {
	m := shotModel()
	m.services.cfg = config.Config{Tools: map[string]config.Tool{"terminal": {Shell: true}, "claude": {}}}
	m.prefs.comfortableRows = true
	shell := sessionRow(t, m, "add-rate-limiting")
	shell.sess.Tool = "terminal"
	shell.sess.Status = status.Idle
	shell.sess.LastPrompt = "this never rode a shell row"
	m.workspace.paneLines = map[string]string{shell.sess.ID: "~/dev/api $ go test ./..."}

	if got := m.entryHeight(shell); got != 2 {
		t.Fatalf("comfortable shell entry height = %d, want 2", got)
	}
	lines := uitheme.SplitLines(m.renderTreeRow(shell, false, m.layout.width-1, 4, uitheme.PanelHex()))
	if len(lines) != 2 {
		t.Fatalf("comfortable shell row painted %d lines, want 2:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if reply := ansi.Strip(lines[1]); !strings.Contains(reply, "↳ ~/dev/api $ go test") {
		t.Fatalf("line 2 should carry the shell's own last line behind ↳:\n%s", reply)
	}
	if body := ansi.Strip(strings.Join(lines, "\n")); strings.Contains(body, "this never rode a shell row") {
		t.Fatalf("a shell row has no prompt line to paint:\n%s", body)
	}

	// An agent beside it keeps all three.
	agent := sessionRow(t, m, "add-rate-limiting")
	if got := m.entryHeight(agent); got != 3 {
		t.Fatalf("comfortable agent entry height = %d, want 3", got)
	}
	if got := len(uitheme.SplitLines(m.renderTreeRow(agent, false, m.layout.width-1, 4, uitheme.PanelHex()))); got != 3 {
		t.Fatalf("comfortable agent row painted %d lines, want 3", got)
	}

	// Compact keeps every session on one row, shell included.
	m.prefs.comfortableRows = false
	if got := m.entryHeight(shell); got != 1 {
		t.Fatalf("compact shell entry height = %d, want 1", got)
	}
}

// spawnUnnamed launches through the New Session form with the name left
// blank, which is what makes a spawn auto-named and sends the agent the
// directive to name itself.
func spawnUnnamed(t *testing.T, m *Model, prompt string) store.Session {
	t.Helper()
	m.openForm()
	m.form().SetName("")
	m.form().SetDir(t.TempDir())
	m.formPromptFor().Input.SetValue(prompt)
	setFormTool(m, 0)
	_, cmd := m.submitForm()
	if m.errBar.text != "" {
		t.Fatalf("submit: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	if !m.onList() {
		t.Fatalf("submit left screen = %v err=%q", m.screenID(), m.errBar.text)
	}
	rows := m.sessionRows()
	if len(rows) != 1 {
		t.Fatalf("session rows = %d, want the one just spawned", len(rows))
	}
	return rows[0]
}

func TestAWaitingRowIsNamedByThePromptItWasGiven(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	sess := spawnUnnamed(t, m, "add cursor pagination to the sessions list endpoint")

	got := ansi.Strip(m.displayName(sess))
	if !strings.HasPrefix(got, "add cursor") {
		t.Fatalf("displayName = %q, want it to open with the prompt", got)
	}
}

func TestAWaitingRowNeverShowsTheRenameDirective(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	sess := spawnUnnamed(t, m, "rate limit the public search route")

	got := ansi.Strip(m.displayName(sess))
	for _, leaked := range []string{"Run this exact", "agent-manager rename", "Other agent sessions"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("displayName = %q, leaked the launch directive %q", got, leaked)
		}
	}
}

func TestAWaitingRowSpawnedWithNoPromptKeepsThePlaceholder(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	sess := spawnUnnamed(t, m, "")

	if got := ansi.Strip(m.displayName(sess)); got != namePlaceholder {
		t.Fatalf("displayName = %q, want the bare placeholder %q", got, namePlaceholder)
	}
}

func TestAWaitingRowKeepsThePromptToOneShortLine(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	sess := spawnUnnamed(t, m, "split the staging workspace\nout of the prod state file\nand re-run the plan")

	got := ansi.Strip(m.displayName(sess))
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("displayName = %q, want one line", got)
	}
	if width := ansi.StringWidth(got); width > placeholderPromptWidth {
		t.Fatalf("displayName = %q is %d wide, want at most %d", got, width, placeholderPromptWidth)
	}
}

func TestAPromptNamedRowFallsBackToItsGeneratedNamePastTheGrace(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	sess := spawnUnnamed(t, m, "cache the session lookup for 30 seconds")

	sess.CreatedAt = time.Now().Add(-renameGrace - time.Second)
	got := ansi.Strip(m.displayName(sess))
	if got != sess.Name || !strings.HasPrefix(got, "claude-") {
		t.Fatalf("displayName past the grace = %q, want the generated name %q", got, sess.Name)
	}
}

func TestAWaitingRowIsNamedByTheWordsNotThePastedImage(t *testing.T) {
	// A chip reaches the agent as the path the picture was written to, so an
	// image-first prompt would otherwise name every such row the same thing.
	pasted, err := clipboard.SaveToTemp([]byte("not really a png"), "png")
	if err != nil {
		t.Fatalf("save paste: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(pasted) })

	m := buildModel(t)
	sess := spawnUnnamed(t, m, pasted+" fix the flaky auth middleware test")

	got := ansi.Strip(m.displayName(sess))
	if strings.Contains(got, "/") || strings.Contains(got, "paste-") {
		t.Fatalf("displayName = %q, want the words rather than the pasted path", got)
	}
	if !strings.HasPrefix(got, "fix the") {
		t.Fatalf("displayName = %q, want it to open with what was typed", got)
	}
}

func TestSearchFieldFillsOnlyWhileSearching(t *testing.T) {
	forceANSI256(t)
	m := shotModel()
	m.rail.SetSearch("note", m.rail.Searching())
	fieldLine := func() contentLine {
		for _, line := range m.railLines(40, 14) {
			if strings.Contains(ansi.Strip(line.Text), "⌕") {
				return line
			}
		}
		t.Fatal("rail painted no search field")
		return contentLine{}
	}

	m.rail.SetSearch(m.rail.Search(), true)
	open := fieldLine()
	if open.Tone != uitheme.SearchFieldHex() {
		t.Fatalf("open field tone = %q, want %q", open.Tone, uitheme.SearchFieldHex())
	}
	if !strings.Contains(open.Text, uitheme.BgSeq(uitheme.SearchFieldHex())) {
		t.Fatalf("open field line carries no fill:\n%q", open.Text)
	}

	m.rail.SetSearch(m.rail.Search(), false)
	closed := fieldLine()
	if closed.Tone != "" {
		t.Fatalf("closed field with a query applied should keep the panel tone, got %q", closed.Tone)
	}
	if strings.Contains(closed.Text, uitheme.BgSeq(uitheme.SearchFieldHex())) {
		t.Fatalf("closed field line should not be filled:\n%q", closed.Text)
	}
}

func TestSearchLightsTheQueryInsideASessionName(t *testing.T) {
	forceANSI256(t)
	entry := treeRow{sess: store.Session{
		ID: "s1", Name: "alpha-build", Tool: "grok", Status: status.Idle, CreatedAt: time.Now(),
	}}
	lit := uitheme.SearchMatchStyle().Render("bui")

	m := &Model{rail: railModelSearching(nil, 0, "BUI")}
	row := m.renderTreeRow(entry, false, 80, 0, uitheme.PanelHex())
	if !strings.Contains(row, lit) {
		t.Fatalf("query should light its span in the name's own case:\n%q", row)
	}
	if !strings.Contains(ansi.Strip(row), "alpha-build") {
		t.Fatalf("name lost text around the lit span:\n%q", ansi.Strip(row))
	}

	selected := m.renderTreeRow(entry, true, 80, 0, uitheme.SelectedHex())
	bright := lipgloss.NewStyle().Foreground(uitheme.ColorBright()).Bold(true)
	if !strings.Contains(selected, bright.Render("alpha-")) || !strings.Contains(selected, lit) {
		t.Fatalf("selected row should keep its bright name around the lit span:\n%q", selected)
	}

	m.rail.SetSearch("grok", m.rail.Searching())
	row = m.renderTreeRow(entry, false, 80, 0, uitheme.PanelHex())
	if strings.Contains(row, sgrOf(lit)) {
		t.Fatalf("a match on the tool alone should leave the name plain:\n%q", row)
	}

	m.rail.SetSearch("", m.rail.Searching())
	row = m.renderTreeRow(entry, false, 80, 0, uitheme.PanelHex())
	if !strings.Contains(row, uitheme.ValueStyle().Render("alpha-build")) {
		t.Fatalf("no query should render the plain name:\n%q", row)
	}
}

func TestSearchLightsTheQueryInsideAGroupName(t *testing.T) {
	forceANSI256(t)
	m := &Model{rail: railModelSearching(nil, 0, "END")}
	row := m.renderTreeRow(treeRow{isGroup: true, group: "work/backend"}, false, 80, 0, uitheme.PanelHex())
	if !strings.Contains(row, uitheme.SearchMatchStyle().Render("end")) {
		t.Fatalf("group name should light the query:\n%q", row)
	}
	if !strings.Contains(ansi.Strip(row), "backend") {
		t.Fatalf("group name lost text around the lit span:\n%q", ansi.Strip(row))
	}
}

func searchModel() *Model {
	m := &Model{
		layout: layoutState{
			width:  120,
			height: 30,
		},
		rail: railModelFromRows(nil, 0),
	}
	m.workspace.sessions = []store.Session{
		{ID: "1", Name: "api-server", Status: status.Idle},
		{ID: "2", Name: "web-ui", Status: status.Idle},
		{ID: "3", Name: "docs", Status: status.Idle},
	}
	m.rebuildRows()
	return m
}

func railHead(m *Model) string {
	var b strings.Builder
	for _, line := range m.railLines(40, 24) {
		b.WriteString(ansi.Strip(line.Text) + "\n")
	}
	return b.String()
}

// A query that outlives its field has to keep saying so: without the row the
// filtered-away sessions read as sessions that are gone.
func TestSearchFieldOutlivesTheOpenField(t *testing.T) {
	m := searchModel()
	m.rail.SetSearch("api", true)
	m.rebuildRows()
	if !strings.Contains(railHead(m), "⌕") {
		t.Fatal("an open field should be in the rail")
	}
	m.rail.SetSearch(m.rail.Search(), false)
	rail := railHead(m)
	if !strings.Contains(rail, "⌕") {
		t.Fatalf("a query still filtering should stay in the rail:\n%s", rail)
	}
	if !strings.Contains(rail, "clear") {
		t.Fatalf("a closed field should offer the way out:\n%s", rail)
	}
}

func TestSearchRailIsCleanWithNoQuery(t *testing.T) {
	if rail := railHead(searchModel()); strings.Contains(rail, "⌕") {
		t.Fatalf("no query should mean no field:\n%s", rail)
	}
}

func TestEnterKeepsTheQueryAndEscClearsIt(t *testing.T) {
	m := searchModel()
	m.rail.SetSearch("api", true)
	m.rebuildRows()
	filtered := len(railRows(m))

	m.handleSearchKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.rail.Searching() || m.rail.Search() != "api" || len(railRows(m)) != filtered {
		t.Fatalf("enter should close the field and keep the filter, got searching=%v query=%q rows=%d",
			m.rail.Searching(), m.rail.Search(), len(railRows(m)))
	}

	// Through handleKey rather than clearSearch: the binding is half of what
	// this covers, so a test that skips it would pass with esc unbound.
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.rail.Search() != "" {
		t.Fatalf("esc should clear the query, got %q", m.rail.Search())
	}
	if len(railRows(m)) <= filtered {
		t.Fatalf("clearing should bring the sessions back, still %d rows", len(railRows(m)))
	}
}

func TestEscInTheFieldClearsIt(t *testing.T) {
	m := searchModel()
	m.rail.SetSearch("api", true)
	m.rebuildRows()
	m.handleSearchKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.rail.Searching() || m.rail.Search() != "" {
		t.Fatalf("esc should close and clear, got searching=%v query=%q", m.rail.Searching(), m.rail.Search())
	}
	if strings.Contains(railHead(m), "⌕") {
		t.Fatal("a cleared search should leave no field behind")
	}
}

// railFrame is the sidebar alone. The content column repeats the selected
// session's name in its detail head, so asserting against a whole frame
// would pass on a row the rail never painted.
func (m *Model) railFrame() string {
	return ansi.Strip(railLinesText(m.railLines(40, m.listBodyHeight())))
}

func railRow(rail, needle string) int {
	at := -1
	for i, line := range strings.Split(rail, "\n") {
		if !strings.Contains(line, needle+" ") {
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
	if err := m.services.store.CreateGroup(group, t.TempDir()); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, group)
}

func TestRestingShellUsesCaretGlyph(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	shell := spawnTerminal(t, m)
	setRailSessionStatus(m, shell.ID, status.Idle)

	if rail := m.railFrame(); !strings.Contains(rail, m.glyphs().ShellGlyph) {
		t.Fatalf("a resting shell should carry the caret:\n%s", rail)
	}
}

func TestDeadInlineShellKeepsItsStatusGlyph(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	shell := spawnTerminal(t, m)
	shell.Status = status.Idle

	if glyph := ansi.Strip(m.sessionGlyph(shell)); glyph != m.glyphs().ShellGlyph {
		t.Fatalf("a resting shell wears the caret, got %q", glyph)
	}
	for _, gone := range []string{status.Dead, status.Errored} {
		shell.Status = gone
		glyph := ansi.Strip(m.sessionGlyph(shell))
		if glyph == m.glyphs().ShellGlyph {
			t.Fatalf("a %s shell must not read as resting", gone)
		}
		if glyph != presentation.GlyphsFor("").StatusGlyph(gone) {
			t.Fatalf("a %s shell should wear its own glyph, got %q", gone, glyph)
		}
	}
}

func TestUnnestedShellSitsInItsGroup(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	groupWithShell(t, m, "backend")
	shell := spawnTerminal(t, m)

	var row treeRow
	for _, entry := range railRows(m) {
		if !entry.isGroup && entry.sess.ID == shell.ID {
			row = entry
		}
	}
	if row.depth != 1 {
		t.Fatalf("unnested shell depth = %d, want 1 (under its group)", row.depth)
	}
	if rail := m.railFrame(); strings.Contains(rail, "Terminals") {
		t.Fatalf("the list paints no divider:\n%s", rail)
	}
}

func TestRollupsCountAgentsOnly(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	groupWithShell(t, m, "backend")
	createSession(t, m, "agent-one", t.TempDir(), "backend")
	m.selectGroupRow(t, "backend")
	shell := spawnTerminal(t, m)

	if got := uirail.GroupSessionCount(m.railAgents(), "backend"); got != 1 {
		t.Fatalf("group count = %d, want 1 agent", got)
	}
	total := 0
	for _, count := range uirail.GroupStatusCounts(m.railAgents(), "backend") {
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

func TestCursorOnShellStaysPainted(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "agent-one", t.TempDir(), "")
	first := spawnTerminal(t, m)
	second := spawnTerminal(t, m)
	if first.ID == second.ID {
		t.Fatal("the two spawns should be different shells")
	}

	for _, shell := range []string{first.Name, second.Name} {
		m.selectSessionRow(t, shell)
		sess, ok := m.selected()
		if !ok || sess.Name != shell {
			t.Fatalf("selected() = %+v %v, want %s", sess, ok, shell)
		}
		for _, height := range []int{16, 24, 30, 44} {
			m.layout.height = height
			if rail := m.railFrame(); railRow(rail, shell) < 0 {
				t.Fatalf("selected shell %s is unpainted at height %d:\n%s", shell, height, rail)
			}
		}
	}
}

func TestRailReturnsItsBudget(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "agent-one", t.TempDir(), "")
	spawnTerminal(t, m)
	spawnTerminal(t, m)

	for _, height := range []int{3, 4, 6, 8, 10, 14, 34} {
		for _, width := range []int{30, 60, 120} {
			for _, searching := range []bool{false, true} {
				m.rail.SetSearch(m.rail.Search(), searching)
				if got := len(m.railLines(width, height)); got != height {
					t.Fatalf("%dx%d searching=%v returned %d rows, want %d", width, height, searching, got, height)
				}
			}
		}
	}
}

// The selected row's band runs under every cell of the row, not only up to
// the first glyph whose style ends in a reset.
func TestSelectedRowBandFillsTheWholeRow(t *testing.T) {
	useTrueColor(t)
	selections := map[string]func(*Model){
		"backend":           func(m *Model) { m.selectGroupRow(t, "backend") },
		"add-rate-limiting": func(m *Model) { m.selectSessionRow(t, "add-rate-limiting") },
	}
	for _, full := range []bool{false, true} {
		for target, selectRow := range selections {
			m := shotModel()
			m.prefs.fullLayout = full
			selectRow(m)
			frame := preparedView(m)
			railWidth := m.layout.displayedRail.Width
			y := -1
			for index, line := range strings.Split(frame, "\n") {
				if strings.Contains(ansi.Strip(ansi.Cut(line, 0, 1+railWidth)), target) {
					y = index
					break
				}
			}
			if y < 0 {
				t.Fatalf("full=%v: no rail row shows %q", full, target)
			}
			backgrounds, _ := cellColors(frame, m.layout.width, m.layout.height)
			for x := 1; x <= railWidth; x++ {
				if !sameColor(backgrounds[y][x], hexColor(uitheme.SelectedHex())) {
					t.Errorf("full=%v %s: col %d background = %v, want the selected band %s",
						full, target, x, backgrounds[y][x], uitheme.SelectedHex())
				}
			}
		}
	}
}
