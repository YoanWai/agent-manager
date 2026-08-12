package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestComputerLinesTemperatures(t *testing.T) {
	cases := []struct {
		name string
		snap sysstat.Snapshot
		want string
	}{
		{
			name: "cpu and gpu",
			snap: sysstat.Snapshot{CPUTempOK: true, CPUTemp: 61, GPUTempOK: true, GPUTemp: 55},
			want: "temp cpu 61°C gpu 55°C",
		},
		{
			name: "soc alone",
			snap: sysstat.Snapshot{SoCTempOK: true, SoCTemp: 60.4},
			want: "temp soc 60°C",
		},
		{
			name: "no sensors",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{width: 120, height: 34, snap: tc.snap}
			var temp string
			for _, line := range m.computerLines(40) {
				plain := strings.TrimSpace(ansi.Strip(line))
				if strings.HasPrefix(plain, "temp") {
					temp = strings.Join(strings.Fields(plain), " ")
				}
			}
			if tc.want == "" {
				if temp != "" {
					t.Fatalf("expected no temp row, got %q", temp)
				}
				return
			}
			if temp != tc.want {
				t.Fatalf("temp row = %q, want %q", temp, tc.want)
			}
		})
	}
}

// The separator carries its own reset, so a reading cannot inherit color.
func TestTemperatureReadingsEachKeepTheirColor(t *testing.T) {
	forceANSI256(t)

	row := tempReadings(sysstat.Snapshot{CPUTempOK: true, CPUTemp: 61, GPUTempOK: true, GPUTemp: 55})
	want := sgrOf(valueStyle.Render("x"))
	for _, reading := range []string{"cpu 61°C", "gpu 55°C"} {
		before, _, found := strings.Cut(row, reading)
		if !found {
			t.Fatalf("row %q is missing %q", row, reading)
		}
		if got := lastSGR(before); got != want {
			t.Fatalf("%q renders under %q, want %q", reading, got, want)
		}
	}
}

func lastSGR(s string) string {
	idx := strings.LastIndex(s, "\x1b[")
	if idx < 0 {
		return ""
	}
	code, _, _ := strings.Cut(s[idx+2:], "m")
	return code
}

func TestGroupRowRendersGroupPane(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "api-agent", dir, "backend")
	for i, row := range m.rows {
		if row.isGroup && row.group == "backend" {
			m.cursor = i
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

	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	if names := sessionNames(m); len(names) != 1 || names[0] != "old-one" {
		t.Fatalf("archived view = %v want [old-one]", names)
	}
}

func railText(t *testing.T, m *Model) []string {
	t.Helper()
	var out []string
	for _, line := range m.entryLines(m.treeRows(), 0, 60, 20) {
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
	if got := m.entryHeight(m.rows[0]); got != 1 {
		t.Fatalf("compact entry height = %d want 1", got)
	}

	m.openSettings()
	if m.settings.comfortableRows {
		t.Fatal("settings should open on compact by default")
	}
	for i := 0; i < settingsFieldDensity; i++ {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.settings.field != settingsFieldDensity {
		t.Fatalf("stepping down should reach the density field, got %d", m.settings.field)
	}
	if card := ansi.Strip(m.viewSettings()); !strings.Contains(card, "list density") {
		t.Fatalf("settings card has no density row:\n%s", card)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	if card := ansi.Strip(m.viewSettings()); !strings.Contains(card, "comfortable") {
		t.Fatalf("toggled card does not read comfortable:\n%s", card)
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})

	if !m.comfortableRows {
		t.Fatal("model did not pick up the comfortable density")
	}
	if !storedComfortableRows(m.store) {
		t.Fatal("comfortable density did not persist")
	}
	if got := m.entryHeight(m.rows[0]); got != 2 {
		t.Fatalf("comfortable entry height = %d want 2", got)
	}

	lines = railText(t, m)
	head = lineWith(t, lines, "alpha")
	if strings.Contains(lines[head], "claude") {
		t.Fatalf("comfortable name line should not carry meta: %q", lines[head])
	}
	if head+1 >= len(lines) {
		t.Fatalf("comfortable row has no meta line:\n%s", strings.Join(lines, "\n"))
	}
	meta := lines[head+1]
	if !strings.Contains(meta, "claude") || !strings.Contains(meta, statusLabel(m.rows[0].sess.Status)) {
		t.Fatalf("meta line = %q", meta)
	}
	if indent := len(meta) - len(strings.TrimLeft(meta, " ")); indent < railInset+2 {
		t.Fatalf("meta line should sit under the name, indent = %d: %q", indent, meta)
	}
}

// Groups follow the same density so the list keeps one rhythm.
func TestComfortableGroupRowStacks(t *testing.T) {
	m := buildModel(t)
	m.comfortableRows = true
	m.openGroupForm()
	m.groupForm.name.SetValue("fleet")
	if _, _ = m.submitGroupForm(); m.errBar.text != "" {
		t.Fatalf("create group: %q", m.errBar.text)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "beta", t.TempDir(), "fleet")

	lines := railText(t, m)
	head := lineWith(t, lines, "fleet")
	if head+1 >= len(lines) || strings.TrimSpace(lines[head+1]) == "" {
		t.Fatalf("group row has no meta line:\n%s", strings.Join(lines, "\n"))
	}
}

// A rail too short for the counters keeps the selected entry whole: a
// two-line row trimmed to one reads as a compact row that lost its meta.
func TestComfortableRowSurvivesShortRail(t *testing.T) {
	m := buildModel(t)
	m.comfortableRows = true
	for _, name := range []string{"one", "two", "three", "four"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	m.selectSessionRow(t, "three")

	lines := m.entryLines(m.treeRows(), 0, 60, 2)
	if len(lines) != 2 {
		t.Fatalf("entry lines = %d want 2", len(lines))
	}
	head := lineWith(t, []string{ansi.Strip(lines[0].text), ansi.Strip(lines[1].text)}, "three")
	if head != 0 {
		t.Fatalf("selected entry should start the window, got line %d", head)
	}
	meta := ansi.Strip(lines[1].text)
	if !strings.Contains(meta, "claude") {
		t.Fatalf("selected entry lost its meta line: %q", meta)
	}
}

// A nested entry's second line carries its ancestors' branches straight
// down, so the tree column has no gap between an entry and the next.
func TestComfortableMetaLineKeepsTreeGuides(t *testing.T) {
	m := buildModel(t)
	m.comfortableRows = true
	m.openGroupForm()
	m.groupForm.name.SetValue("outer")
	if _, _ = m.submitGroupForm(); m.errBar.text != "" {
		t.Fatalf("create outer group: %q", m.errBar.text)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "outer")
	m.openGroupForm()
	m.groupForm.name.SetValue("inner")
	if _, _ = m.submitGroupForm(); m.errBar.text != "" {
		t.Fatalf("create inner group: %q", m.errBar.text)
	}
	m.applyCmd(t, m.refreshCmd())
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
		{"nothing needs attention", func(m *Model) { m.statusFilter = statusFilterAttention }, "nothing needs attention"},
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
	selected := m.renderTreeRow(entry, true, 80, 0, selectedHex())
	unselected := m.renderTreeRow(entry, false, 80, 0, panelHex())

	if !strings.Contains(selected, "\x1b[") {
		t.Fatal("selected row has no SGR; color profile not active")
	}
	subtleSeq := sgrOf(subtleStyle.Render("x"))
	brightSeq := sgrOf(lipgloss.NewStyle().Foreground(colorBright).Render("x"))
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

// A content separator stops at the pane's edge instead of crossing the seam.
func TestContentRuleStopsAtSeam(t *testing.T) {
	m := shotModel()
	leftWidth, _ := m.splitWidths()
	rows := strings.Split(m.View(), "\n")
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
				width: size.w, height: size.h, mode: modeList,
				sessions: sessions, rows: rows, cursor: cursor,
				collapsed: map[string]bool{}, split: splitState{ratio: defaultSplitRatio},
			}
			view := ansi.Strip(m.View())
			if !strings.Contains(view, sessions[cursor].Name) {
				t.Errorf("%dx%d cursor=%d: %q is selected but never painted:\n%s",
					size.w, size.h, cursor, sessions[cursor].Name, view)
			}
		}
	}
}

// The same rule for review's file list: tabbing to the last file has to
// bring it on screen, not just select it.
func TestDiffFileListCursorAlwaysPainted(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithManyFiles(t, 40)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	if m.diff.loading || len(m.diff.set.Files) < 2 {
		t.Fatalf("diff did not load: %q", m.diff.errText)
	}

	last := len(m.diff.set.Files) - 1
	for _, size := range []struct{ w, h int }{{80, 20}, {100, 30}, {140, 44}} {
		m.width, m.height = size.w, size.h
		m.diff.fileIdx = last
		m.drainCmds(t, m.loadCurrentDiffFile())
		view := ansi.Strip(m.viewDiffFull())
		name := m.diff.set.Files[last].File.Path
		if !strings.Contains(view, name) {
			t.Errorf("%dx%d: file %q is selected but never painted:\n%s", size.w, size.h, name, view)
		}
	}
}

// gitRepoWithManyFiles is a repo with n changed files, enough to overflow
// review's file list.
func gitRepoWithManyFiles(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	for i := 0; i < n; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file-%02d.txt", i))
		if err := os.WriteFile(path, []byte("seed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-m", "init")
	for i := 0; i < n; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file-%02d.txt", i))
		if err := os.WriteFile(path, []byte("seed\nchanged\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Every filter the list is under names itself over the list, beside the key
// that lifts it, and the header stops repeating them.
func TestFilterBadgesStackOverTheList(t *testing.T) {
	m := shotModel()
	m.width, m.height = 120, 40
	m.showArchived, m.hideEmptyGroups = true, true
	m.statusFilter = statusFilterAttention
	rail := ansi.Strip(railLinesText(m.railLines(36, m.listBodyHeight())))
	var painted []string
	for _, line := range strings.Split(rail, "\n") {
		if strings.TrimSpace(line) != "" {
			painted = append(painted, line)
		}
	}
	// The badges are what the rail paints first, one filter per line with the
	// key beside it, widest narrowing on top.
	want := [][2]string{
		{"ARCHIVED", "t back to active"},
		{"ATTENTION", "w show all"},
		{"HIDE EMPTY", "e show empty"},
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
