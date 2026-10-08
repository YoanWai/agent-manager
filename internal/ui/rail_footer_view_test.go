package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"slices"
	"strings"
	"testing"
)

// Focused, the keyboard belongs to the agent: one tier with the keys the
// manager keeps, and the app-wide keys — which would go to the agent, not
// the manager — stay out.
func TestFooterInFocusMode(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "focused", t.TempDir(), "")
	m.mode = modeFocus
	footer := ansi.Strip(m.viewFooter())
	if !strings.Contains(footer, "Focused") {
		t.Fatalf("the tier should name the mode it describes:\n%s", footer)
	}
	if !strings.Contains(footer, "ctrl+q / ctrl+\\") || !strings.Contains(footer, "click its row") || !strings.Contains(footer, "mouse back") {
		t.Fatalf("focus footer should carry the reserved keys and mouse leave:\n%s", footer)
	}
	listH := lipgloss.Height(m.listFooter())
	if lipgloss.Height(m.viewFooter()) != listH {
		t.Fatalf("focus footer must keep the list footer's height %d, got %d:\n%s", listH, lipgloss.Height(m.viewFooter()), footer)
	}
	if strings.Contains(footer, "navigate") || strings.Contains(footer, "View") {
		t.Fatalf("app-wide keys go to the agent while focused, so the tier must go:\n%s", footer)
	}
	// Blank rows below hold the list footer's height so focusing never
	// resizes the pane. The keys themselves may wrap inside that budget.
	if strings.Contains(footer, "agent UI") {
		t.Fatalf("a plain focused pane should not offer mouse pass-through:\n%s", footer)
	}
	// Full screen focus paints no list, so the gesture that needs one goes.
	m.prefs.fullLayout = true
	full := ansi.Strip(m.viewFooter())
	if strings.Contains(full, "click its row") {
		t.Fatalf("full screen focus has no list to click:\n%s", full)
	}
	if !strings.Contains(full, "mouse back") {
		t.Fatalf("the button still leaves a full screen session:\n%s", full)
	}

	setFocusPaneFacts(m, railSelectedSession(m).ID, true, false, false, 0, paneCursor{})
	if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "click / alt+drag") || !strings.Contains(footer, "agent UI") {
		t.Fatalf("a mouse-tracking pane should advertise pass-through:\n%s", footer)
	}
}

func TestPagingHintFitsFullFocusFooter(t *testing.T) {
	shipped, err := config.Default()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m, sessID := focusedWithHistory(t, "paging-footer")
	m.services.cfg.Tools["gemini"] = shipped.Tools["gemini"]
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == sessID {
			m.workspace.sessions[i].Tool = "gemini"
		}
	}
	m.rebuildRows()
	m.prefs.fullLayout = true
	m.layout.width = 110
	footer := ansi.Strip(m.viewFooter())
	if !strings.Contains(footer, "pgup/pgdn scroll") {
		t.Fatalf("full focus footer omits paging: %q", footer)
	}
	if got := lipgloss.Height(m.viewFooter()); got != 1 {
		t.Fatalf("full focus footer spans %d rows, want one: %q", got, footer)
	}
}

// The scrolled notice names PgDn only while PgDn is what walks the pane
// back down.
func TestScrolledStatusNamesPgDnWhileItPages(t *testing.T) {
	m, sessID := focusedWithHistory(t, "scrolled-status")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(*Model)
	if !m.focus.pane.ScrolledBack() {
		t.Fatal("test setup: PgUp did not scroll the pane back")
	}
	if got := ansi.Strip(m.statusLine()); !strings.Contains(got, "lines back · wheel down, pgdn or type to catch up") {
		t.Fatalf("paging pane's scrolled notice = %q", got)
	}
	updateFocusPane(m, sessID, func(update *uifocus.PaneUpdate) { update.Mouse = true })
	if got := ansi.Strip(m.statusLine()); !strings.Contains(got, "lines back · wheel down or type to catch up") {
		t.Fatalf("mouse-tracking pane's scrolled notice = %q", got)
	}
}

func TestArrowStepFooterHintsFollowSetting(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.CreateGroup("arrow-group", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "arrow-hints", t.TempDir(), "")
	assertHint := func(context, hint string, enabled bool) {
		t.Helper()
		footer := ansi.Strip(m.viewFooter())
		if got := strings.Contains(footer, hint); got != enabled {
			t.Fatalf("arrow step enabled = %v: %s hint %q = %v:\n%s", enabled, context, hint, got, footer)
		}
	}

	for _, enabled := range []bool{true, false} {
		m.prefs.arrowStep = enabled
		m.mode = modeList
		m.selectSessionRow(t, "arrow-hints")
		assertHint("session", "→ focus", enabled)
		m.selectGroupRow(t, "arrow-group")
		assertHint("group", "←/→ close / open", enabled)
		m.mode = modeFocus
		assertHint("focus", "← prompt start: back", enabled)
	}
}

// Every transient tier holds the list footer's height: the footer sets the
// preview box, and a box that moves resizes every session's pane, which
// costs an agent drawing on the normal screen a full transcript redraw.
func TestTransientFootersKeepListHeight(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "sizer", t.TempDir(), "")
	m.applyCmd(t, m.refreshCmd())
	listed := lipgloss.Height(m.viewFooter())

	for _, tier := range []struct {
		name string
		open func()
	}{
		{"resize", func() { m.layout.split.resizeMode = true }},
		{"rename", func() { m.mode = modeRename }},
		{"focus", func() { m.mode = modeFocus }},
	} {
		tier.open()
		if got := lipgloss.Height(m.viewFooter()); got != listed {
			t.Errorf("%s footer = %d rows, want %d", tier.name, got, listed)
		}
		m.quick.active, m.layout.split.resizeMode, m.mode = false, false, modeList
	}
}

// With nothing under the cursor there is nothing to act on, so the footer
// carries only the app-wide tier.
func TestFooterWithoutASelectedRow(t *testing.T) {
	m := buildModel(t)
	resetRailModel(m)
	footer := ansi.Strip(m.viewFooter())
	if strings.Contains(footer, "Session") || strings.Contains(footer, "Group") {
		t.Fatalf("no row selected, no row tier:\n%s", footer)
	}
	if !strings.Contains(footer, "View") {
		t.Fatalf("the app-wide tier should stay:\n%s", footer)
	}
}

func TestFooterTierFollowsTheCursor(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "legend", t.TempDir(), "")
	m.applyCmd(t, m.refreshCmd())

	for i, row := range railRows(m) {
		if !row.isGroup {
			setRailCursor(m, i)
			break
		}
	}
	if footer := ansi.Strip(m.viewFooter()); !strings.Contains(footer, "Session") {
		t.Fatalf("a session under the cursor should title the tier Session:\n%s", footer)
	}

	for i, row := range railRows(m) {
		if row.isGroup {
			setRailCursor(m, i)
			break
		}
	}
	footer := ansi.Strip(m.viewFooter())
	if !strings.Contains(footer, "Group") {
		t.Fatalf("a group under the cursor should title the tier Group:\n%s", footer)
	}
	if strings.Contains(footer, "fork") {
		t.Fatalf("a group cannot be forked, so the key should not be offered:\n%s", footer)
	}
}

// A toggle's label names what the key will do next, not the state it is
// already in, so the footer reads as an instruction.
func TestFooterTogglesNameTheNextAction(t *testing.T) {
	m := buildModel(t)
	// Wide enough that the row budget keeps every app-wide binding.
	m.layout.width = 260
	dir := t.TempDir()
	if err := m.services.store.AddGroup("work", dir, "off", ""); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "toggles", dir, "work")
	m.applyCmd(t, m.refreshCmd())

	if footer := m.viewFooter(); !strings.Contains(footer, keyCapQuiet("F", "fold all")) {
		t.Fatalf("an open tree should offer folding:\n%s", ansi.Strip(footer))
	}
	updated, _ := m.handleKey(runeKey("F"))
	m = updated.(*Model)
	if footer := m.viewFooter(); !strings.Contains(footer, keyCapQuiet("F", "unfold all")) {
		t.Fatalf("a folded tree should offer unfolding:\n%s", ansi.Strip(footer))
	}

	group := -1
	for i, row := range railRows(m) {
		if row.isGroup && !row.isRoot() {
			group = i
			break
		}
	}
	if group < 0 {
		t.Fatalf("the fixture should list a group to fold, rows: %v", m.groupRowPaths())
	}
	setRailCursor(m, group)
	if footer := m.viewFooter(); !strings.Contains(footer, keyCap("↵", "unfold")) {
		t.Fatalf("a collapsed group should offer unfolding:\n%s", ansi.Strip(footer))
	}

	m.rail.SetArchived(true)
	if footer := m.viewFooter(); !strings.Contains(footer, keyCapQuiet("t", "back to active")) {
		t.Fatalf("the archived view should offer the way back:\n%s", ansi.Strip(footer))
	}
}

func TestLegendOffersEmptyGroupsOnlyInTheActiveView(t *testing.T) {
	m := buildModel(t)
	offered := func() bool {
		for _, pair := range m.viewLegend().pairs {
			if pair[1] == "hide empty" || pair[1] == "show empty" {
				return true
			}
		}
		return false
	}
	if !offered() {
		t.Fatal("the active view should offer hiding empty groups")
	}
	m.rail.SetHideEmptyGroups(true)
	if !offered() {
		t.Fatal("the active view should offer showing empty groups again")
	}
	m.rail.SetArchived(true)
	if offered() {
		t.Fatal("the archived view should not offer the empty-groups key")
	}
}

// The focus footer reads the key table: a moved key shows under its new
// name, and an action turned off has no hint to mislead with.
func TestFooterInFocusModeNamesTheKeyTable(t *testing.T) {
	m := buildModel(t)
	useSessionKeys(t, m, []string{"f9"}, nil, []string{"alt+e"})
	m.mode = modeFocus
	footer := ansi.Strip(m.viewFooter())
	if !strings.Contains(footer, "f9 / click its row / mouse back") || !strings.Contains(footer, "alt+e editor") {
		t.Fatalf("focus footer should name the configured keys:\n%s", footer)
	}
	if strings.Contains(footer, "review") || strings.Contains(footer, "ctrl+q") {
		t.Fatalf("focus footer should drop the review hint and the old detach key:\n%s", footer)
	}
	rule := ansi.Strip(focusTopRule(80, m.services.keys))
	if !strings.Contains(rule, "f9 back") || !strings.Contains(rule, "alt+e editor") || strings.Contains(rule, "review") {
		t.Fatalf("split focus rule should follow the table:\n%s", rule)
	}
}

func TestRowLegendDropsRestoreInActiveView(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.services.store.CreateGroup("zone", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "zone")

	m.selectSessionRow(t, "alpha")
	legend := m.rowLegend()
	for _, pair := range legend.pairs {
		if strings.Contains(pair[1], "restore") {
			t.Fatalf("session legend in active view should not offer restore, got %+v", pair)
		}
	}
	if !slices.ContainsFunc(legend.pairs, func(pair [2]string) bool { return pair[1] == "archive" }) {
		t.Fatal("session legend in active view should still offer archive")
	}

	m.selectGroupRow(t, "zone")
	legend = m.rowLegend()
	for _, pair := range legend.pairs {
		if strings.Contains(pair[1], "restore") {
			t.Fatalf("group legend in active view should not offer restore, got %+v", pair)
		}
	}
	if !slices.ContainsFunc(legend.pairs, func(pair [2]string) bool { return pair[1] == "archive" }) {
		t.Fatal("group legend in active view should still offer archive")
	}
}

// In the archived view, archiveSelected no-ops, so the legend should offer
// only restore, on both a session row and a group row.
func TestRowLegendDropsArchiveInArchivedView(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.services.store.CreateGroup("zone", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "zone")
	if err := m.services.store.SetArchived(m.sessionRows()[0].ID, true); err != nil {
		t.Fatalf("archive: %v", err)
	}

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())

	m.selectSessionRow(t, "alpha")
	legend := m.rowLegend()
	for _, pair := range legend.pairs {
		if strings.Contains(pair[1], "archive") {
			t.Fatalf("session legend in archived view should not offer archive, got %+v", pair)
		}
	}
	if !slices.ContainsFunc(legend.pairs, func(pair [2]string) bool { return pair[1] == "restore" }) {
		t.Fatal("session legend in archived view should still offer restore")
	}

	m.selectGroupRow(t, "zone")
	legend = m.rowLegend()
	for _, pair := range legend.pairs {
		if strings.Contains(pair[1], "archive") {
			t.Fatalf("group legend in archived view should not offer archive, got %+v", pair)
		}
	}
	if !slices.ContainsFunc(legend.pairs, func(pair [2]string) bool { return pair[1] == "restore" }) {
		t.Fatal("group legend in archived view should still offer restore")
	}
}

func TestQuickPromptFooterKeys(t *testing.T) {
	m := shotModel()
	m.quick.active = true
	m.quick.input = textarea.New()
	m.quick.toolNames = []string{"claude"}

	footerOne := m.viewFooter()
	if strings.Contains(footerOne, "shift+tab") || strings.Contains(footerOne, "previous tool") {
		t.Errorf("one tool enabled, footer shouldn't have shift+tab: %q", footerOne)
	}
	if !strings.Contains(footerOne, "tab tool") {
		t.Errorf("one tool enabled, missing tab pair: %q", footerOne)
	}

	m.quick.toolNames = []string{"claude", "codex"}
	footerTwo := m.viewFooter()
	if !strings.Contains(footerTwo, "shift+tab") || !strings.Contains(footerTwo, "previous tool") {
		t.Errorf("two tools enabled, missing shift+tab pair: %q", footerTwo)
	}
	if !strings.Contains(footerTwo, "tab tool") {
		t.Errorf("two tools enabled, missing tab pair: %q", footerTwo)
	}
	if !strings.Contains(footerTwo, "ctrl+t worktree") {
		t.Errorf("missing ctrl+t worktree pair: %q", footerTwo)
	}
}
