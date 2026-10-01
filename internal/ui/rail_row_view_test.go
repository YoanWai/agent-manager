package ui

import (
	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGroupRowRendersGroupPane(t *testing.T) {
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
	m := buildModel(t)
	m.prefs.comfortableRows = true
	m.openGroupForm()
	m.groupForm.name.SetValue("fleet")
	_, cmd := m.submitGroupForm()
	if m.errBar.text != "" {
		t.Fatalf("create group: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	createSession(t, m, "beta", t.TempDir(), "fleet")

	lines := railText(t, m)
	head := lineWith(t, lines, "fleet")
	if !strings.Contains(lines[head], statusGlyph(status.Starting)) {
		t.Fatalf("group row should carry its starting agent inline: %q", lines[head])
	}
}

// A rail too short for the counters keeps the selected entry whole: a
// three-line row trimmed to one reads as a compact row that lost its
// message lines.
func TestComfortableRowSurvivesShortRail(t *testing.T) {
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
	m.prefs.comfortableRows = true
	m.openGroupForm()
	m.groupForm.name.SetValue("outer")
	_, cmd := m.submitGroupForm()
	if m.errBar.text != "" {
		t.Fatalf("create outer group: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	m.selectGroupRow(t, "outer")
	m.openGroupForm()
	m.groupForm.name.SetValue("inner")
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
			if got := ansi.StringWidth(line.text); got > width {
				t.Errorf("width %d: rail line is %d wide: %q", width, got, ansi.Strip(line.text))
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
	for _, sess := range m.workspace.sessions {
		if sess.Group == "" {
			ungrouped++
		}
	}
	if total != ungrouped {
		t.Fatalf("root rollup counts %d sessions, want %d ungrouped", total, ungrouped)
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
	root := m.renderTreeRow(railRows(m)[0], false, 40, 0, panelHex())
	dimmed := strings.TrimPrefix(fgSeq(mix(current.Accent2, current.Subtle, 0.5)), "\x1b[")
	if !strings.Contains(root, dimmed) {
		t.Fatalf("root is not painted in the dimmed tone: %q", root)
	}
	if accent := strings.TrimPrefix(fgSeq(current.Accent2), "\x1b["); strings.Contains(root, accent) {
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
	comfortable := &Model{prefs: preferences{comfortableRows: true}}
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
				m.ledger.awaitedRenames = map[string]awaitedRename{tc.sess.ID: {generated: generated}}
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
			if strings.Contains(row, reorderGrip+" "+namePlaceholder) {
				t.Fatalf("row still stands in for a name it has:\n%s", row)
			}
			if _, still := m.ledger.awaitedRenames[tc.sess.ID]; still {
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
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
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
	readings = append(readings, reading{"quick bar", ansi.Strip(m.viewQuickBar(112, quickBarMaxRows))})

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
	for _, width := range []int{29, 27} {
		m := buildModel(t)
		m.workspace.groupPaths = map[string]string{"a/b/c": "/tmp"}
		m.workspace.sessions = []store.Session{{ID: "x", Name: "some-long-session-name", Tool: "claude", Group: "a/b/c"}}
		m.rebuildRows()
		setRailCursor(m, len(railRows(m))-1)
		m.mode = modeFocus
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
				width: 120, height: 40, mode: modeList,

				split: splitState{ratio: defaultSplitRatio}, workspace: workspace{sessions: []store.Session{sess},

					tmuxSocket: here, leadingManager: tc.leading}, rail: railModelFromRows([]treeRow{{sess: sess}}, 0),
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
		width: 120, height: 40, mode: modeList,

		split: splitState{ratio: defaultSplitRatio}, workspace: workspace{sessions: []store.Session{sess}}, rail: railModelFromRows([]treeRow{{sess: sess}}, 0),
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

	m.prefs.comfortableRows = true
	if got := m.entryHeight(groupRow(t, m, "backend")); got != 1 {
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
	m.prefs.fullLayout = false
	if got := m.entryHeight(row); got != 3 {
		t.Fatalf("split comfortable session entry height = %d, want 3", got)
	}
	m.prefs.comfortableRows = false
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
	m.prefs.fullLayout = true
	m.prefs.comfortableRows = true
	question := "Allow edits to router.go?"
	m.workspace.paneLines = map[string]string{"db-migrations": question}
	lines := splitLines(m.renderTreeRow(sessionRow(t, m, "db-migrations"), false, m.width-1, 0, panelHex()))
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
	m.prefs.fullLayout = true
	m.prefs.comfortableRows = true
	m.workspace.paneLines = map[string]string{"notes": "All quiet, nothing queued."}
	lines := splitLines(m.renderTreeRow(sessionRow(t, m, "notes"), false, m.width-1, 1, panelHex()))
	if reply := strings.TrimSpace(ansi.Strip(lines[2])); reply != "↳ All quiet, nothing queued." {
		t.Fatalf("idle reply line = %q, want the last message", reply)
	}
	m.workspace.paneLines = nil
	lines = splitLines(m.renderTreeRow(sessionRow(t, m, "notes"), false, m.width-1, 1, panelHex()))
	if reply := strings.TrimSpace(ansi.Strip(lines[2])); reply != "-" {
		t.Fatalf("silent idle reply line = %q, want a dash", reply)
	}
}

func TestRowLongPromptTruncates(t *testing.T) {
	m := shotModel()
	m.prefs.fullLayout = true
	m.prefs.comfortableRows = true
	width := 80
	row := sessionRow(t, m, "notes")
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

// The compact cell quotes the agent's last message whenever there is
// one, whatever the state; only a session that has said nothing yet
// names the task it was given.
func TestCompactCellIsStatePicked(t *testing.T) {
	m := shotModel()
	m.prefs.fullLayout = true
	m.workspace.paneLines = map[string]string{
		"notes":         "All quiet, nothing queued.",
		"db-migrations": "Allow edits to router.go?",
	}
	idle := sessionRow(t, m, "notes")
	idle.sess.LastPrompt = "verify the staging deploy is healthy"
	line := ansi.Strip(m.renderTreeRow(idle, false, m.width-1, 1, panelHex()))
	if !strings.Contains(line, "↳ All quiet, nothing queued.") {
		t.Fatalf("an idle session that has spoken should quote its reply:\n%s", line)
	}
	if strings.Contains(line, "verify the staging deploy") {
		t.Fatalf("the reply should win over the task:\n%s", line)
	}

	m.workspace.paneLines = map[string]string{"db-migrations": "Allow edits to router.go?"}
	line = ansi.Strip(m.renderTreeRow(idle, false, m.width-1, 1, panelHex()))
	if !strings.Contains(line, "❯ verify the staging deploy is healthy") {
		t.Fatalf("a silent idle session should name its task:\n%s", line)
	}

	line = ansi.Strip(m.renderTreeRow(sessionRow(t, m, "db-migrations"), false, m.width-1, 0, panelHex()))
	if !strings.Contains(line, "↳ Allow edits to router.go?") {
		t.Fatalf("waiting compact row should quote its question:\n%s", line)
	}
}

// A status frozen before the archive (an older build's "working") must
// not read as alive from inside the archive.
func TestArchivedRowReadsDead(t *testing.T) {
	m := shotModel()
	m.prefs.fullLayout = true
	row := sessionRow(t, m, "add-rate-limiting")
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
	agent := sessionRow(t, m, "add-rate-limiting")
	if got := m.entryHeight(agent); got != 3 {
		t.Fatalf("comfortable agent entry height = %d, want 3", got)
	}
	if got := len(splitLines(m.renderTreeRow(agent, false, m.width-1, 4, panelHex()))); got != 3 {
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
	m.form.name.SetValue("")
	m.form.dir.SetValue(t.TempDir())
	m.form.prompt.input.SetValue(prompt)
	m.form.toolIndex = 0
	_, cmd := m.submitForm()
	if m.errBar.text != "" {
		t.Fatalf("submit: %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	if m.mode != modeList {
		t.Fatalf("submit left mode=%v err=%q", m.mode, m.errBar.text)
	}
	rows := m.sessionRows()
	if len(rows) != 1 {
		t.Fatalf("session rows = %d, want the one just spawned", len(rows))
	}
	return rows[0]
}

func TestAWaitingRowIsNamedByThePromptItWasGiven(t *testing.T) {
	m := buildModel(t)
	sess := spawnUnnamed(t, m, "add cursor pagination to the sessions list endpoint")

	got := ansi.Strip(m.displayName(sess))
	if !strings.HasPrefix(got, "add cursor") {
		t.Fatalf("displayName = %q, want it to open with the prompt", got)
	}
}

func TestAWaitingRowNeverShowsTheRenameDirective(t *testing.T) {
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
	m := buildModel(t)
	sess := spawnUnnamed(t, m, "")

	if got := ansi.Strip(m.displayName(sess)); got != namePlaceholder {
		t.Fatalf("displayName = %q, want the bare placeholder %q", got, namePlaceholder)
	}
}

func TestAWaitingRowKeepsThePromptToOneShortLine(t *testing.T) {
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
