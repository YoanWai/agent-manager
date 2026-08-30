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
	"github.com/YoanWai/agent-manager/internal/launch"
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
	return railTextAt(m, 60)
}

func railTextAt(m *Model, width int) []string {
	var out []string
	for _, line := range m.entryLines(m.rows, 0, width, 20) {
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
	sessionRow := m.rows[0]
	for _, row := range m.rows {
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

// A group stays one line at any density: it has neither a prompt nor a
// reply to carry.
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
	if !strings.Contains(lines[head], "●") && !strings.Contains(lines[head], "○") && !strings.Contains(lines[head], "◐") {
		t.Fatalf("group row should carry its dots inline: %q", lines[head])
	}
}

// A rail too short for the counters keeps the selected entry whole: a
// three-line row trimmed to one reads as a compact row that lost its
// message lines.
func TestComfortableRowSurvivesShortRail(t *testing.T) {
	m := buildModel(t)
	m.comfortableRows = true
	for _, name := range []string{"one", "two", "three", "four"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	m.selectSessionRow(t, "three")

	lines := m.entryLines(m.rows, 0, 60, 2)
	if len(lines) != 2 {
		t.Fatalf("entry lines = %d want 2", len(lines))
	}
	head := lineWith(t, []string{ansi.Strip(lines[0].text), ansi.Strip(lines[1].text)}, "three")
	if head != 0 {
		t.Fatalf("selected entry should start the window, got line %d", head)
	}
	if prompt := strings.TrimSpace(ansi.Strip(lines[1].text)); prompt == "" {
		t.Fatalf("selected entry lost its prompt line: %q", ansi.Strip(lines[1].text))
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

// A message waiting for a session is marked on its row in either density,
// and on no row that has nothing waiting.
func TestInboxBadgeRidesTheRowWithMessages(t *testing.T) {
	for _, comfortable := range []bool{false, true} {
		m := shotModel()
		m.comfortableRows = comfortable
		m.queuedMessages = map[string]int{"add-rate-limiting": 2}

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
	badged.queuedMessages = map[string]int{"add-rate-limiting": 2}
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
// nowhere else.
func TestInboxBadgeOutlivesTheRowMeta(t *testing.T) {
	for _, width := range []int{28, 30, 36, 44, 60} {
		m := shotModel()
		m.queuedMessages = map[string]int{"add-rate-limiting": 2}

		for _, line := range m.entryLines(m.rows, 0, width, 20) {
			if got := ansi.StringWidth(line.text); got > width {
				t.Errorf("width %d: rail line is %d wide: %q", width, got, ansi.Strip(line.text))
			}
		}
		rows := railTextAt(m, width)
		row := rows[lineWith(t, rows, "✉2")]
		if !strings.Contains(row, "add-rate-limiting") {
			t.Errorf("width %d: the badge cost the name: %q", width, row)
		}
	}

	m := shotModel()
	m.queuedMessages = map[string]int{"add-rate-limiting": 2}
	rows := railTextAt(m, 30)
	row := rows[lineWith(t, rows, "✉2")]
	if strings.Contains(row, "ago") {
		t.Fatalf("30 columns still fit the age, so the row shed nothing: %q", row)
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

// A row whose conversation id was captured names it in the comfortable
// meta, since that id is what a revive resumes; the compact row stays
// crowded and a row with nothing captured must not show an id at all.
func TestSessionRowCarriesTheCapturedConversationIDInMeta(t *testing.T) {
	const conversation = "conv-abc-123"
	compact := &Model{}
	comfortable := &Model{comfortableRows: true}
	withID := treeRow{sess: store.Session{
		ID: "s1", Name: "with-conversation", Tool: "claude", Status: status.Finished,
		CreatedAt: time.Now().Add(-3 * time.Hour), AgentSessionID: conversation,
	}}
	withoutID := treeRow{sess: store.Session{
		ID: "s2", Name: "without-conversation", Tool: "claude", Status: status.Finished,
		CreatedAt: time.Now().Add(-3 * time.Hour),
	}}

	row := ansi.Strip(comfortable.renderTreeRow(withID, false, 120, 0, panelHex()))
	if !strings.Contains(row, conversation) {
		t.Fatalf("the comfortable row should carry the captured id in its meta:\n%s", row)
	}
	row = ansi.Strip(compact.renderTreeRow(withID, false, 120, 0, panelHex()))
	if strings.Contains(row, conversation) {
		t.Fatalf("the compact row has no room for the id:\n%s", row)
	}
	row = ansi.Strip(comfortable.renderTreeRow(withoutID, false, 120, 0, panelHex()))
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
				m.awaitedRenames = map[string]awaitedRename{tc.sess.ID: {generated: generated}}
			}
			row := ansi.Strip(m.renderTreeRow(treeRow{sess: tc.sess}, false, 80, 0, panelHex()))
			if !strings.Contains(row, tc.want) {
				t.Fatalf("row is missing %q:\n%s", tc.want, row)
			}
			if tc.want != generated && strings.Contains(row, generated) {
				t.Fatalf("row still shows the generated name:\n%s", row)
			}
			if tc.want == namePlaceholder {
				return
			}
			if strings.Contains(row, namePlaceholder) {
				t.Fatalf("row still stands in for a name it has:\n%s", row)
			}
			if _, still := m.awaitedRenames[tc.sess.ID]; still {
				t.Fatal("a wait that is over should drop what it was holding")
			}
		})
	}
}

// The rail row is not the only place a name is printed, and the roster sits
// beside it in the same frame, so every reading of a session stands in for
// the awaited name together.
func TestEveryReadingOfASessionStandsInForAnAwaitedName(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	const generated = "claude-ab12"
	if err := m.spawnSession("claude", generated, dir, "backend", "do things", true, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}

	type reading struct{ where, text string }
	m.selectGroupRow(t, "backend")
	readings := []reading{{"roster", ansi.Strip(m.viewGroupAgents("backend", 112, 10))}}
	m.selectSessionRow(t, generated)
	readings = append(readings, reading{"detail", strings.Split(ansi.Strip(m.viewDetail(112)), "\n")[0]})
	m.openQuickMode()
	readings = append(readings, reading{"quick bar", ansi.Strip(m.viewQuickBar(112))})

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

// blankCapture is what tmux hands back for a session whose agent has not
// painted yet: one empty row per pane line, not an empty capture.
const blankCapture = "\n\n\n\n\n\n\n\n\n\n"

func previewModel(sessionStatus, preview string) *Model {
	return &Model{
		width: 120, height: 40, mode: modeList, preview: preview,
		rows: []treeRow{{sess: store.Session{ID: "boot", Name: "boot", Status: sessionStatus}}},
	}
}

func previewText(m *Model) string {
	var out []string
	for _, line := range m.previewLines(80, 12, "  ") {
		out = append(out, ansi.Strip(line.text))
	}
	return strings.Join(out, "\n")
}

// A launching agent paints nothing for a while, and the blank block that
// leaves reads as a broken session; the preview says it is coming up. The
// blank rows are still the session's pane, so they stay hit-testable.
func TestPreviewShowsLoaderWhileSessionStarts(t *testing.T) {
	m := previewModel(status.Starting, blankCapture)
	if got := previewText(m); !strings.Contains(got, "starting up") {
		t.Fatalf("preview should carry the launch loader, got %q", got)
	}
	if !m.pane.box.ok || m.pane.box.height != len(paneExact(blankCapture, 12, 80, -1)) {
		t.Fatalf("the loader must not cost the pane its geometry, box = %+v", m.pane.box)
	}
}

// With no capture at all there are no pane rows to ride, so the loader
// stands in for the empty-preview line.
func TestPreviewShowsLoaderBeforeTheFirstCapture(t *testing.T) {
	m := previewModel(status.Starting, "")
	got := previewText(m)
	if !strings.Contains(got, "starting up") {
		t.Fatalf("preview should carry the launch loader, got %q", got)
	}
	if strings.Contains(got, "(no output yet)") {
		t.Fatalf("a starting session should not read as empty, got %q", got)
	}
	if m.pane.box.ok {
		t.Fatalf("no pane rows painted means nothing to hit-test, box = %+v", m.pane.box)
	}
}

func TestPreviewLoaderClearsOnFirstFrame(t *testing.T) {
	m := previewModel(status.Starting, "❯ hello\n")
	got := previewText(m)
	if strings.Contains(got, "starting up") {
		t.Fatalf("a captured frame should replace the loader, got %q", got)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("preview should paint the captured frame, got %q", got)
	}
	if !m.pane.box.ok {
		t.Fatalf("captured rows must stay hit-testable, box = %+v", m.pane.box)
	}
}

// Only the launch state spins: a session that is up with a cleared pane, and
// one that never came up at all, both keep the plain preview.
func TestPreviewSkipsLoaderForSettledSessions(t *testing.T) {
	live := previewModel(status.Idle, blankCapture)
	if got := previewText(live); strings.Contains(got, "starting up") {
		t.Fatalf("an idle session must not spin, got %q", got)
	}
	if !live.pane.box.ok {
		t.Fatalf("an idle session keeps its pane rows, box = %+v", live.pane.box)
	}
	gone := previewModel(status.Dead, "")
	if got := previewText(gone); !strings.Contains(got, "(no output yet)") {
		t.Fatalf("a session that failed to start should read as empty, got %q", got)
	}
}

func TestPreviewLoaderIsCenteredAndMovesOnThePreviewTick(t *testing.T) {
	m := previewModel(status.Starting, blankCapture)
	first := previewText(m)
	lines := strings.Split(first, "\n")
	painted := make([]int, 0, 6)
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			painted = append(painted, i)
		}
	}
	if len(painted) != 6 || painted[0] != 3 || painted[5] != 8 {
		t.Fatalf("loader rows = %v, want the middle of a 12-row preview", painted)
	}
	if strings.Count(first, "●") != 1 || strings.Count(first, "•") != 1 {
		t.Fatalf("loader should show one head and one trailing dot, got %q", first)
	}
	for _, row := range painted {
		left := len(lines[row]) - len(strings.TrimLeft(lines[row], " "))
		content := strings.TrimSpace(lines[row])
		right := 80 - left - ansi.StringWidth(content)
		if diff := left - right; diff < -1 || diff > 1 {
			t.Fatalf("loader row %q is not centered: left=%d right=%d", lines[row], left, right)
		}
	}
	m.Update(startupTickMsg{})
	if next := previewText(m); next == first {
		t.Fatal("preview loader did not move on the startup tick")
	}
}

func TestStartingSessionGlyphMovesOnTheStartupTick(t *testing.T) {
	for _, tool := range []string{"agent", "shell"} {
		m := previewModel(status.Starting, blankCapture)
		m.rows[0].sess.Tool = tool
		m.cfg.Tools = map[string]config.Tool{"shell": {Shell: true}}
		first := ansi.Strip(m.sessionGlyph(m.rows[0].sess))
		m.Update(startupTickMsg{})
		second := ansi.Strip(m.sessionGlyph(m.rows[0].sess))
		if first == second {
			t.Fatalf("starting %s glyph stayed on %q", tool, first)
		}
		if second == shellGlyph {
			t.Fatal("starting shell used the resting shell glyph")
		}
	}
}

// The loader borrows no mark that already names a state: a frame caught
// mid-turn would otherwise read as that state, and the detail head above
// the preview is painting the real one.
func TestPreviewLoaderFramesAreNotStatusMarks(t *testing.T) {
	states := []string{status.Working, status.Starting, status.Waiting, status.Finished, status.Errored, status.Dead, status.Idle}
	for _, frame := range startupFrames {
		for _, state := range states {
			if frame == statusGlyph(state) {
				t.Fatalf("loader frame %q is the %s mark", frame, state)
			}
		}
	}
}

// A focused pane is the screen the user types on, so it keeps its own first
// row and the caret drawn there rather than the loader.
func TestPreviewLeavesTheFocusedPaneAlone(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := previewModel(status.Starting, blankCapture)
	m.mode = modeFocus
	m.cursorOn = true
	m.pane.cursor = paneCursor{ok: true}
	first := m.previewLines(80, 12, "  ")[0].text
	if strings.Contains(ansi.Strip(first), "starting up") {
		t.Fatalf("the loader took the focused pane's first row: %q", first)
	}
	if !strings.Contains(first, "\x1b[") {
		t.Fatalf("focused row 0 lost its caret: %q", first)
	}
}

func TestRingLoaderWrapsThePhaseRoundTheRing(t *testing.T) {
	const width, height = 30, 6
	for _, phase := range []int{startupRingPoints, startupRingPoints + 3, startupRingPoints * 4} {
		want := ringLoader(width, height, "starting up", phase%startupRingPoints)
		if got := ringLoader(width, height, "starting up", phase); !slices.Equal(got, want) {
			t.Fatalf("phase %d rendered\n%s\nwant the phase %d ring\n%s",
				phase, strings.Join(got, "\n"), phase%startupRingPoints, strings.Join(want, "\n"))
		}
	}
	lit := ringLoader(width, height, "starting up", 0)
	if slices.Equal(lit, ringLoader(width, height, "starting up", 1)) {
		t.Fatal("neighbouring phases render the same ring, so the comparison proves nothing")
	}

	const label = "reviving"
	rendered := ansi.Strip(strings.Join(ringLoader(width, height, label, 0), "\n"))
	if !strings.Contains(rendered, label) {
		t.Fatalf("the ring dropped the label it was given:\n%s", rendered)
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
				width: 120, height: 40, mode: modeList,
				sessions: []store.Session{sess}, rows: []treeRow{{sess: sess}},
				collapsed: map[string]bool{}, split: splitState{ratio: defaultSplitRatio},
				tmuxSocket: here, leadingManager: tc.leading,
			}
			view := ansi.Strip(m.View())
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
		width: 120, height: 40, mode: modeList,
		sessions: []store.Session{sess}, rows: []treeRow{{sess: sess}},
		collapsed: map[string]bool{}, split: splitState{ratio: defaultSplitRatio},
	}
	if view := ansi.Strip(m.View()); strings.Contains(view, "elsewhere") {
		t.Fatalf("nothing to compare against should mark nothing:\n%s", view)
	}
}

// A compact session row is one line wearing the reply inline; the
// comfortable density unfolds it to three — name, the last prompt, the
// last reply — and a group is one line at either density, in either
// layout.
func TestRowHeightsFollowDensity(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	m.paneLines = map[string]string{"add-rate-limiting": "Running tests… (14s · esc to interrupt)"}
	row := m.rows[4]
	row.sess.LastPrompt = "add a token bucket limiter to the public api"
	if m.comfortableRows {
		t.Fatal("this test starts at the compact density")
	}
	if got := m.entryHeight(row); got != 1 {
		t.Fatalf("compact session entry height = %d, want 1", got)
	}
	if got := m.entryHeight(m.rows[2]); got != 1 {
		t.Fatalf("group entry height = %d, want 1", got)
	}
	lines := splitLines(m.renderTreeRow(row, false, m.width-1, 4, panelHex()))
	if len(lines) != 1 {
		t.Fatalf("compact row painted %d lines, want 1", len(lines))
	}
	top := ansi.Strip(lines[0])
	for _, want := range []string{"add-rate-limiting", "Running tests", "working", "claude"} {
		if !strings.Contains(top, want) {
			t.Errorf("compact row misses %q:\n%s", want, top)
		}
	}

	m.comfortableRows = true
	if got := m.entryHeight(m.rows[2]); got != 1 {
		t.Fatalf("comfortable group entry height = %d, want 1", got)
	}
	if got := m.entryHeight(row); got != 3 {
		t.Fatalf("comfortable session entry height = %d, want 3", got)
	}
	lines = splitLines(m.renderTreeRow(row, false, m.width-1, 4, panelHex()))
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
	m.fullLayout = false
	if got := m.entryHeight(row); got != 3 {
		t.Fatalf("split comfortable session entry height = %d, want 3", got)
	}
	m.comfortableRows = false
	if got := m.entryHeight(row); got != 1 {
		t.Fatalf("split compact session entry height = %d, want 1", got)
	}
	narrow := splitLines(m.renderTreeRow(row, false, 60, 4, panelHex()))
	if len(narrow) != 1 {
		t.Fatalf("split compact row painted %d lines, want 1", len(narrow))
	}
}

func TestRowWaitingReplyWearsTheStateColor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := shotModel()
	m.fullLayout = true
	m.comfortableRows = true
	question := "Allow edits to router.go?"
	m.paneLines = map[string]string{"db-migrations": question}
	lines := splitLines(m.renderTreeRow(m.rows[0], false, m.width-1, 0, panelHex()))
	if len(lines) != 3 {
		t.Fatalf("waiting row painted %d lines, want 3", len(lines))
	}
	tinted := strings.TrimSuffix(
		lipgloss.NewStyle().Foreground(statusColor(status.Waiting)).Render(question), "\x1b[0m")
	if !strings.Contains(lines[2], tinted) {
		t.Fatalf("waiting question should wear the waiting color:\n%q", lines[2])
	}
}

func TestRowQuotesEveryStateAndDashesWhenSilent(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	m.comfortableRows = true
	m.paneLines = map[string]string{"notes": "All quiet, nothing queued."}
	lines := splitLines(m.renderTreeRow(m.rows[1], false, m.width-1, 1, panelHex()))
	if reply := strings.TrimSpace(ansi.Strip(lines[2])); reply != "↳ All quiet, nothing queued." {
		t.Fatalf("idle reply line = %q, want the last message", reply)
	}
	m.paneLines = nil
	lines = splitLines(m.renderTreeRow(m.rows[1], false, m.width-1, 1, panelHex()))
	if reply := strings.TrimSpace(ansi.Strip(lines[2])); reply != "-" {
		t.Fatalf("silent idle reply line = %q, want a dash", reply)
	}
}

func TestRowLongPromptTruncates(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	m.comfortableRows = true
	width := 80
	row := m.rows[1]
	row.sess.LastPrompt = strings.Repeat("triage the flaky integration suite and report ", 10)
	rendered := m.renderTreeRow(row, false, width, 1, panelHex())
	for _, line := range splitLines(rendered) {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("row line is %d wide, row is %d:\n%s", got, width, ansi.Strip(line))
		}
	}
	lines := splitLines(rendered)
	if prompt := ansi.Strip(lines[1]); !strings.Contains(prompt, "…") {
		t.Fatalf("long prompt should truncate with an ellipsis:\n%s", prompt)
	}
	for _, want := range []string{"idle", "grok"} {
		if !strings.Contains(ansi.Strip(lines[0]), want) {
			t.Errorf("meta should survive, misses %q:\n%s", want, ansi.Strip(lines[0]))
		}
	}
}

// The launch notes are the manager's words, not a task: a decorated first
// prompt sheds them, and a note delivered on its own records nothing.
func TestTypedPromptStripsLaunchNotes(t *testing.T) {
	decorated := launch.CoordinationNote + "\n\n" + launch.RenameDirective + "\n\nfix the login flow"
	if got := typedPrompt(decorated); got != "fix the login flow" {
		t.Fatalf("typedPrompt = %q, want the bare task", got)
	}
	if got := typedPrompt(launch.DeferredRenameDirective); got != "" {
		t.Fatalf("a bare directive should record nothing, got %q", got)
	}
	if got := typedPrompt(launch.CoordinationNote); got != "" {
		t.Fatalf("a bare note should record nothing, got %q", got)
	}
	if got := typedPrompt("plain prompt"); got != "plain prompt" {
		t.Fatalf("an undecorated prompt should pass through, got %q", got)
	}
}

// The compact cell quotes the agent's last message whenever there is
// one, whatever the state; only a session that has said nothing yet
// names the task it was given.
func TestCompactCellIsStatePicked(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	m.paneLines = map[string]string{
		"notes":         "All quiet, nothing queued.",
		"db-migrations": "Allow edits to router.go?",
	}
	idle := m.rows[1]
	idle.sess.LastPrompt = "verify the staging deploy is healthy"
	line := ansi.Strip(m.renderTreeRow(idle, false, m.width-1, 1, panelHex()))
	if !strings.Contains(line, "↳ All quiet, nothing queued.") {
		t.Fatalf("an idle session that has spoken should quote its reply:\n%s", line)
	}
	if strings.Contains(line, "verify the staging deploy") {
		t.Fatalf("the reply should win over the task:\n%s", line)
	}

	m.paneLines = map[string]string{"db-migrations": "Allow edits to router.go?"}
	line = ansi.Strip(m.renderTreeRow(idle, false, m.width-1, 1, panelHex()))
	if !strings.Contains(line, "❯ verify the staging deploy is healthy") {
		t.Fatalf("a silent idle session should name its task:\n%s", line)
	}

	line = ansi.Strip(m.renderTreeRow(m.rows[0], false, m.width-1, 0, panelHex()))
	if !strings.Contains(line, "↳ Allow edits to router.go?") {
		t.Fatalf("waiting compact row should quote its question:\n%s", line)
	}
}

// A status frozen before the archive (an older build's "working") must
// not read as alive from inside the archive.
func TestArchivedRowReadsDead(t *testing.T) {
	m := shotModel()
	m.fullLayout = true
	row := m.rows[4]
	row.sess.Archived = true
	line := ansi.Strip(m.renderTreeRow(row, false, m.width-1, 4, panelHex()))
	if !strings.Contains(line, statusLabel(status.Dead)) {
		t.Fatalf("archived row should read dead:\n%s", line)
	}
	if strings.Contains(line, statusLabel(status.Working)) {
		t.Fatalf("archived row still claims its frozen state:\n%s", line)
	}
}

func TestShellRowSkipsThePromptLine(t *testing.T) {
	m := shotModel()
	m.cfg = config.Config{Tools: map[string]config.Tool{"terminal": {Shell: true}, "claude": {}}}
	m.comfortableRows = true
	shell := m.rows[4]
	shell.sess.Tool = "terminal"
	shell.sess.Status = status.Idle
	shell.sess.LastPrompt = "this never rode a shell row"
	m.paneLines = map[string]string{shell.sess.ID: "~/dev/api $ go test ./..."}

	if got := m.entryHeight(shell); got != 2 {
		t.Fatalf("comfortable shell entry height = %d, want 2", got)
	}
	lines := splitLines(m.renderTreeRow(shell, false, m.width-1, 4, panelHex()))
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
	agent := m.rows[4]
	if got := m.entryHeight(agent); got != 3 {
		t.Fatalf("comfortable agent entry height = %d, want 3", got)
	}
	if got := len(splitLines(m.renderTreeRow(agent, false, m.width-1, 4, panelHex()))); got != 3 {
		t.Fatalf("comfortable agent row painted %d lines, want 3", got)
	}

	// Compact keeps every session on one row, shell included.
	m.comfortableRows = false
	if got := m.entryHeight(shell); got != 1 {
		t.Fatalf("compact shell entry height = %d, want 1", got)
	}
}

// railTestHeights is a comfortable list: groups on one line, a shell on
// two, sessions on three.
var railTestHeights = []int{1, 3, 3, 3, 3, 1, 3, 3, 2, 3, 1, 3, 3, 3, 3, 3, 1, 3, 3}

func railWalk(n int) []int {
	var seq []int
	for i := 0; i < n; i++ {
		seq = append(seq, i)
	}
	for i := n - 2; i >= 0; i-- {
		seq = append(seq, i)
	}
	return seq
}

func TestRailWindowHoldsStillWhileTheCursorIsOnScreen(t *testing.T) {
	for _, budget := range []int{20, 30, 40} {
		top, prevStart, prevEnd := 0, -1, -1
		for _, cursor := range railWalk(len(railTestHeights)) {
			start, end := railWindow(railTestHeights, cursor, budget, top)
			if prevStart >= 0 && cursor >= prevStart && cursor < prevEnd && start != prevStart {
				t.Fatalf("budget %d: cursor %d already sat in [%d,%d) and the list scrolled to %d",
					budget, cursor, prevStart, prevEnd, start)
			}
			top, prevStart, prevEnd = start, start, end
		}
	}
}

func TestRailWindowKeepsTheCursorsEntryWhole(t *testing.T) {
	for _, budget := range []int{6, 20, 30, 40} {
		top := 0
		for _, cursor := range railWalk(len(railTestHeights)) {
			start, end := railWindow(railTestHeights, cursor, budget, top)
			if cursor < start || cursor >= end {
				t.Fatalf("budget %d: cursor %d fell outside [%d,%d)", budget, cursor, start, end)
			}
			top = start
		}
	}
}

func TestRailWindowScrollsNoFurtherThanItMust(t *testing.T) {
	top := 0
	for cursor := 0; cursor < len(railTestHeights); cursor++ {
		start, _ := railWindow(railTestHeights, cursor, 20, top)
		if start > top {
			if end := windowEnd(railTestHeights, start-1, 20); end > cursor {
				t.Fatalf("cursor %d scrolled to %d, but %d still held it through %d",
					cursor, start, start-1, end)
			}
		}
		top = start
	}
}

func TestRailWindowFollowsTheCursorBackAboveTheTop(t *testing.T) {
	start, end := railWindow(railTestHeights, 2, 20, 11)
	if start != 2 {
		t.Fatalf("window starts at %d, want the cursor's own entry", start)
	}
	if end <= 2 {
		t.Fatalf("window [%d,%d) holds nothing", start, end)
	}
}

func TestRailWindowShowsAListThatFits(t *testing.T) {
	heights := []int{1, 3, 3}
	start, end := railWindow(heights, 2, 20, 1)
	if start != 0 || end != len(heights) {
		t.Fatalf("window = [%d,%d), want the whole list", start, end)
	}
}

func TestRailTopCarriesBetweenFrames(t *testing.T) {
	m := shotModel()
	m.comfortableRows = true
	m.fullLayout = true
	m.rows = nil
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("session-%02d", i)
		m.rows = append(m.rows, treeRow{sess: store.Session{ID: name, Name: name, Tool: "claude", Status: status.Idle}})
	}

	heights := make([]int, len(m.rows))
	for i := range m.rows {
		heights[i] = m.entryHeight(m.rows[i])
	}

	const height = 20
	tops := make([]int, len(m.rows))
	for i := range m.rows {
		prev, prevEnd := m.railTop, windowEnd(heights, m.railTop, height)
		m.cursor = i
		m.entryLines(m.rows, 0, m.width-1, height)
		tops[i] = m.railTop
		if m.railTop > i {
			t.Fatalf("cursor %d: rail starts below it at %d", i, m.railTop)
		}
		if i >= prev && i < prevEnd && m.railTop != prev {
			t.Fatalf("cursor %d already sat in [%d,%d) and the rail scrolled to %d", i, prev, prevEnd, m.railTop)
		}
	}
	if tops[0] != 0 || tops[1] != 0 {
		t.Fatalf("the rail scrolled off the first entry immediately: %v", tops[:3])
	}
	if tops[len(tops)-1] == 0 {
		t.Fatal("the rail never scrolled across 30 comfortable entries")
	}

	for i := len(m.rows) - 1; i >= 0; i-- {
		m.cursor = i
		m.entryLines(m.rows, 0, m.width-1, height)
		if m.railTop > i {
			t.Fatalf("cursor %d: rail starts below it at %d", i, m.railTop)
		}
	}
	if m.railTop != 0 {
		t.Fatalf("stepping back to the first entry left the rail at %d", m.railTop)
	}
}
