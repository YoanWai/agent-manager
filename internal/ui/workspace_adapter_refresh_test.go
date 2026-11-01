package ui

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	execution "github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/testfixture"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The rows are marked against the server the poll read panes from, so the
// refresh has to hand that socket to the model it renders from.
func TestRefreshCarriesTheSocketItReadPanesFrom(t *testing.T) {
	for _, socket := range []string{"/tmp/first/agentmgr", "/tmp/second/agentmgr", ""} {
		m := &Model{workspace: workspace{tmuxSocket: "/tmp/stale/agentmgr"}, rail: railModelFromRows(nil, 0)}
		m.Update(refreshMsg{tmuxSocket: socket, leadingManager: true, listedAt: time.Now()})
		if m.workspace.tmuxSocket != socket {
			t.Fatalf("model socket = %q, want the poll's %q", m.workspace.tmuxSocket, socket)
		}
		if !m.workspace.leadingManager {
			t.Fatal("the poll's hold on the store should reach the model")
		}
	}
}

func TestRefreshWithStaleSelectionFetchesPreview(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "fresh-one", t.TempDir(), "")
	m.selectSessionRow(t, "fresh-one")
	sess := m.sessionRows()[0]

	_, cmd := m.Update(refreshMsg{sessions: m.workspace.sessions, procFor: ""})
	if cmd == nil {
		t.Fatal("stale refresh should schedule an immediate preview fetch")
	}
	m.applyCmd(t, cmd)
	// A fresh pane may not have drawn anything yet (psmux is slower to
	// print a prompt), so check the fetch landed for the selection rather
	// than that it carried text.
	if m.workspace.procFor != sess.ID {
		t.Fatalf("scheduled selection refresh landed for %q, want %q", m.workspace.procFor, sess.ID)
	}

	m.workspace.preview = "existing"
	if _, cmd := m.Update(refreshMsg{sessions: m.workspace.sessions, procFor: sess.ID, preview: "pane text"}); cmd != nil {
		t.Fatal("matching refresh should not schedule extra work")
	}
	if m.workspace.preview != "pane text" {
		t.Fatalf("preview = %q want %q", m.workspace.preview, "pane text")
	}
}

// A click on a banner leaves the session id in the config directory; the
// next pass picks it up and moves the cursor to that row.
func TestRefreshSelectsSessionNamedByClickedNotification(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSessionOn(t, m, "first", "quietchat", t.TempDir())
	createSessionOn(t, m, "second", "quietchat", t.TempDir())
	m.selectSessionRow(t, "first")
	var second store.Session
	for _, sess := range m.sessionRows() {
		if sess.Name == "second" {
			second = sess
		}
	}
	if second.ID == "" {
		t.Fatal("second session missing")
	}
	served := false
	m.poller.dependencies.TakeFocus = func() (string, bool) {
		if served {
			return "", false
		}
		served = true
		return second.ID, true
	}
	resetExecution(m)
	m.applyCmd(t, m.refreshCmd())
	if sess, ok := m.selected(); !ok || sess.ID != second.ID {
		t.Fatalf("the click should select the named session, cursor is on %+v", sess)
	}
}

// The keyboard is inside a pane in focus mode, so moving the cursor under
// it would leave every keystroke going to the session the user left.
func TestClickedNotificationLeavesFocusBeforeSelecting(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSessionOn(t, m, "typing", "quietchat", t.TempDir())
	createSessionOn(t, m, "waiting", "quietchat", t.TempDir())
	m.selectSessionRow(t, "typing")
	var other store.Session
	for _, sess := range m.sessionRows() {
		if sess.Name == "waiting" {
			other = sess
		}
	}
	if other.ID == "" {
		t.Fatal("waiting session missing")
	}
	m.push(newFocusSurface(m))
	served := false
	m.poller.dependencies.TakeFocus = func() (string, bool) {
		if served {
			return "", false
		}
		served = true
		return other.ID, true
	}
	resetExecution(m)
	m.applyCmd(t, m.refreshCmd())
	if !m.onList() {
		t.Fatalf("screen = %v, want the list", m.screenID())
	}
	if sess, ok := m.selected(); !ok || sess.ID != other.ID {
		t.Fatalf("cursor is on %+v, want the session the banner named", sess)
	}
}

// A banner outlives its session: the row can be gone by the time the user
// clicks it, and that must cost neither the cursor nor the focused pane.
func TestClickedNotificationForAGoneSessionChangesNothing(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		focus bool
	}{
		{"list", false},
		{"focus", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := buildModel(t)
			createSessionOn(t, m, "still-here", "quietchat", t.TempDir())
			m.selectSessionRow(t, "still-here")
			before, ok := m.selected()
			if !ok {
				t.Fatal("nothing selected")
			}
			if test.focus {
				m.push(newFocusSurface(m))
			}
			want := m.screenID()
			served := false
			m.poller.dependencies.TakeFocus = func() (string, bool) {
				if served {
					return "", false
				}
				served = true
				return "sess-long-gone", true
			}
			resetExecution(m)
			m.applyCmd(t, m.refreshCmd())
			if m.screenID() != want {
				t.Fatalf("screen = %v, want %v", m.screenID(), want)
			}
			if sess, ok := m.selected(); !ok || sess.ID != before.ID {
				t.Fatalf("cursor moved to %+v, want it left on %s", sess, before.Name)
			}
		})
	}
}

// A Rail reorder completes on the effect lane, and its completion fences
// every listing read before it: an in-flight poll cannot paint the old order.
func TestReorderSurvivesInFlightPoll(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "a", Name: "alpha", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "b", Name: "bravo", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "c", Name: "charlie", Tool: "claude", Cwd: "/tmp", Status: "idle"},
	} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatalf("create %q: %v", sess.ID, err)
		}
	}
	loadStoredRows(t, m)
	pollListed := slices.Clone(m.workspace.sessions)
	pollAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	m.selectSessionRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	if got, want := listSessionIDs(t, m.services.store), []string{"b", "a", "c"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after first shift+down = %v want %v", got, want)
	}
	updated, _ = m.Update(refreshMsg{sessions: slices.Clone(pollListed), listedAt: pollAt, groups: slices.Clone(m.workspace.groups),
		groupPaths: m.workspace.groupPaths, groupWorktrees: m.workspace.groupWorktrees, archivedGroups: m.workspace.archivedGroups})
	m = updated.(*Model)
	if got, want := rowIDs(m), []string{"b", "a", "c"}; !slices.Equal(got, want) {
		t.Errorf("stale poll reverted the painted order: %v want %v", got, want)
	}
	m.selectSessionRow(t, "alpha")
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	if got, want := listSessionIDs(t, m.services.store), []string{"b", "c", "a"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after second shift+down = %v want %v", got, want)
	}
}

func rowIDs(m *Model) []string {
	var ids []string
	for _, sess := range m.sessionRows() {
		ids = append(ids, sess.ID)
	}
	return ids
}

func TestGroupReorderSurvivesInFlightPoll(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	for _, group := range []string{"alpha", "bravo", "charlie"} {
		if err := m.services.store.CreateGroup(group, dir); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "worker", dir, "alpha")
	pollSessions := slices.Clone(m.workspace.sessions)
	pollGroups := slices.Clone(m.workspace.groups)
	pollAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	m.selectGroupRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	if got, want := m.workspace.groups, []string{"bravo", "alpha", "charlie"}; !slices.Equal(got, want) {
		t.Fatalf("groups after first shift+down = %v want %v", got, want)
	}
	updated, _ = m.Update(refreshMsg{sessions: slices.Clone(pollSessions), groups: slices.Clone(pollGroups), listedAt: pollAt})
	m = updated.(*Model)
	if got, want := m.workspace.groups, []string{"bravo", "alpha", "charlie"}; !slices.Equal(got, want) {
		t.Fatalf("stale poll reverted group order: %v want %v", got, want)
	}
	m.selectGroupRow(t, "alpha")
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	if got, want := m.workspace.groups, []string{"bravo", "charlie", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("groups after second shift+down = %v want %v", got, want)
	}
	stored, err := m.services.store.Groups()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, group := range stored {
		names = append(names, group.Name)
	}
	if want := []string{"bravo", "charlie", "alpha"}; !slices.Equal(names, want) {
		t.Fatalf("stored group order = %v want %v", names, want)
	}
}

// A listing read after the reorder completed is current and applies whole.
func TestFreshPollAfterReorderApplies(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	for _, name := range []string{"alpha", "bravo"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	m.selectSessionRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	stored, err := m.services.store.ListSessions(true)
	if err != nil {
		t.Fatal(err)
	}
	stored = append(stored, store.Session{ID: "late", Name: "late", Tool: "claude", Cwd: "/tmp", Status: status.Idle})
	updated, _ = m.Update(refreshMsg{sessions: stored, listedAt: time.Now().Add(time.Second), groups: slices.Clone(m.workspace.groups)})
	m = updated.(*Model)
	if got := rowIDs(m); len(got) != 3 || got[2] != "late" {
		t.Fatalf("fresh listing after the reorder was not applied: %v", got)
	}
}

func TestOlderPollAfterFreshPollCannotUndoReorder(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "a", Name: "alpha", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "b", Name: "bravo", Tool: "claude", Cwd: "/tmp", Status: "idle"},
	} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	loadStoredRows(t, m)
	old := slices.Clone(m.workspace.sessions)
	oldAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	m.selectSessionRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	fresh := refreshMsg{sessions: slices.Clone(m.workspace.sessions), listedAt: time.Now().Add(time.Second), groups: slices.Clone(m.workspace.groups)}
	updated, _ = m.Update(fresh)
	m = updated.(*Model)
	updated, _ = m.Update(refreshMsg{sessions: old, listedAt: oldAt, groups: slices.Clone(m.workspace.groups)})
	m = updated.(*Model)
	if got, want := rowIDs(m), []string{"b", "a"}; !slices.Equal(got, want) {
		t.Fatalf("older poll after a fresh one reverted order: %v want %v", got, want)
	}
}

func TestOlderListingStillDeliversNotificationFocus(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "a", Name: "alpha", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "b", Name: "bravo", Tool: "claude", Cwd: "/tmp", Status: "idle"},
	} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	loadStoredRows(t, m)
	freshAt := time.Now().Add(time.Second)
	updated, _ := m.Update(refreshMsg{
		sessions:   slices.Clone(m.workspace.sessions),
		listedAt:   freshAt,
		groupBases: map[string]string{"current": "main"},
	})
	m = updated.(*Model)
	if got := m.workspace.groupBases["current"]; got != "main" {
		t.Fatalf("fresh listing lost group base: %q", got)
	}
	m.selectSessionRow(t, "alpha")

	updated, _ = m.Update(refreshMsg{
		sessions:       []store.Session{{ID: "obsolete"}},
		listedAt:       freshAt.Add(-time.Second),
		groups:         []string{"obsolete"},
		groupBases:     map[string]string{"obsolete": "old-base"},
		focusID:        "b",
		queuedMessages: map[string]int{"b": 2},
		paneLines:      map[string]string{"b": "latest pane"},
	})
	m = updated.(*Model)
	if got, want := rowIDs(m), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("older listing replaced current rows: %v want %v", got, want)
	}
	if len(m.workspace.groups) != 0 {
		t.Fatalf("older listing replaced current groups: %v", m.workspace.groups)
	}
	if len(m.workspace.groupBases) != 1 || m.workspace.groupBases["current"] != "main" {
		t.Fatalf("older listing replaced current group bases: %v", m.workspace.groupBases)
	}
	if selected, ok := m.selected(); !ok || selected.ID != "b" {
		t.Fatalf("notification focus was lost: selected = %q, found = %t", selected.ID, ok)
	}
	if got := m.workspace.queuedMessages["b"]; got != 2 {
		t.Fatalf("queued message count was lost: %d", got)
	}
	if got := m.workspace.paneLines["b"]; got != "latest pane" {
		t.Fatalf("pane line was lost: %q", got)
	}
}

// A stale listing is discarded whole, so another manager's unrelated reorder
// it carried lands with the next current listing instead.
func TestStaleGroupPollKeepsAnotherManagersUnrelatedReorder(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	for _, name := range []string{"alpha", "bravo", "charlie", "delta", "echo"} {
		if err := m.services.store.CreateGroup(name, dir); err != nil {
			t.Fatal(err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	oldAt := time.Now()
	time.Sleep(2 * time.Millisecond)
	m.selectGroupRow(t, "alpha")
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = updated.(*Model)
	m.drainEffects(t)
	if err := m.services.store.SwapGroupOrder("delta", "echo"); err != nil {
		t.Fatal(err)
	}
	stored, err := m.services.store.Groups()
	if err != nil {
		t.Fatal(err)
	}
	var latest []string
	for _, group := range stored {
		latest = append(latest, group.Name)
	}
	want := []string{"bravo", "alpha", "charlie", "echo", "delta"}
	if !slices.Equal(latest, want) {
		t.Fatalf("store order = %v want %v", latest, want)
	}
	updated, _ = m.Update(refreshMsg{groups: latest, listedAt: oldAt})
	m = updated.(*Model)
	if got := m.workspace.groups; !slices.Equal(got[:3], want[:3]) {
		t.Fatalf("stale poll undid this manager's reorder: %v", got)
	}
	m.applyCmd(t, m.refreshCmd())
	if !slices.Equal(m.workspace.groups, want) {
		t.Fatalf("the next listing lost another manager's unrelated reorder: %v want %v", m.workspace.groups, want)
	}
}

func TestOlderListingStillCompletesAfterTurnRequest(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	sess := askAfterTurn(t, m, "alpha", store.AfterTurnKill)
	if err := m.services.store.UpdateStatus(sess.ID, status.Idle); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	freshAt := time.Now().Add(time.Second)
	updated, _ := m.Update(refreshMsg{sessions: slices.Clone(m.workspace.sessions), listedAt: freshAt})
	m = updated.(*Model)
	updated, _ = m.Update(refreshMsg{
		sessions:   []store.Session{{ID: "obsolete"}},
		listedAt:   freshAt.Add(-time.Second),
		turnsEnded: []string{sess.ID},
	})
	m = updated.(*Model)
	m.drainEffects(t)
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.services.tmux.Exists(sess.ID) || got.Status != status.Dead || got.AfterTurn != "" {
		t.Fatalf("older listing lost the completed turn: running=%v status=%q pending=%q", m.services.tmux.Exists(sess.ID), got.Status, got.AfterTurn)
	}
	if rows := m.sessionRows(); len(rows) != 1 || rows[0].ID != sess.ID || rows[0].Status != status.Dead {
		t.Fatalf("older listing replaced the killed row: %+v", rows)
	}
}

// queueMessage puts one message in a session's inbox the way the MCP
// send_session tool does.
func queueMessage(t *testing.T, m *Model, targetID, body string) int64 {
	t.Helper()
	id, err := m.services.store.Enqueue(store.InboxMessage{
		SessionID:   targetID,
		SenderID:    "sender01",
		SenderName:  "payments-fix",
		Body:        body,
		Fingerprint: body,
		SentAt:      time.Now(),
	}, store.DefaultInboxLimits)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return id
}

func spawnedSession(t *testing.T, m *Model, tool string) store.Session {
	t.Helper()
	if err := m.spawnSession(tool, "worker", t.TempDir(), "", "", false, false, config.Choice{}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess, err := m.services.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

// Archiving a session leaves its queue where it was while the poller stops
// visiting the row, so nothing will ever deliver those messages. The count
// rides the same refresh the sessions do and reaches the row as a badge,
// which is the only thing that says the queue is stuck.
func TestRefreshCarriesQueuedCountsToTheRows(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "quiet", dir, "")
	createSession(t, m, "stranded", dir, "")

	ids := map[string]string{}
	for _, sess := range m.sessionRows() {
		ids[sess.Name] = sess.ID
	}
	if err := m.services.store.SetArchived(ids["stranded"], true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	queueMessage(t, m, ids["stranded"], "rebase on main")
	queueMessage(t, m, ids["stranded"], "then push")

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())

	if got := m.workspace.queuedMessages[ids["stranded"]]; got != 2 {
		t.Fatalf("queued count = %d want 2: %v", got, m.workspace.queuedMessages)
	}
	if _, listed := m.workspace.queuedMessages[ids["quiet"]]; listed {
		t.Fatalf("a session with an empty inbox got a count: %v", m.workspace.queuedMessages)
	}
	rows := railText(t, m)
	if row := rows[lineWith(t, rows, "stranded")]; !strings.Contains(row, "✉2") {
		t.Fatalf("the stranded queue is invisible on its row: %q", row)
	}
}

// The counts are replaced whole rather than merged, so a message that has
// been delivered takes its badge with it on the next refresh.
func TestRefreshReplacesTheQueuedCountsWholesale(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	sess := spawnedSession(t, m, "claude-hooked")
	id := queueMessage(t, m, sess.ID, "rebase on main")
	m.workspace.queuedMessages = map[string]int{sess.ID: 1}

	if claimed, err := m.services.store.ClaimMessage(id, time.Now()); err != nil || !claimed {
		t.Fatalf("claim: %v, claimed=%v", err, claimed)
	}
	if err := m.services.store.MarkDelivered(id, time.Now()); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())

	if count, stale := m.workspace.queuedMessages[sess.ID]; stale {
		t.Fatalf("a delivered message left a badge of %d behind", count)
	}
}

// Quitting one CLI in a pane and starting another there is a real thing to
// do, and every rule the row is read by follows its tool. The poll moves
// the row onto what the pane is running rather than leaving the user to
// retype it by hand.
func TestPollRetypesARowOntoTheCLIItsPaneRuns(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	tail := "tail -f /dev/null"
	if runtime.GOOS == "windows" {
		tail = filepath.Join(testfixture.Dir, "tail.exe") + " -f NUL"
	}
	m.services.cfg.CLIs["tail-tool"] = config.CLI{Command: tail, DefaultStatus: status.Idle}
	engine, err := status.NewEngine(m.services.cfg)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	m.services.engine = engine
	m.poller.dependencies.Engine = engine
	m.poller.options.Binaries = execution.NewToolBinaries(m.services.cfg)
	m.poller.options.StatusSources["tail-tool"] = ""

	resetExecution(m)
	createSessionOn(t, m, "swapped", "quietchat", t.TempDir())
	sess := m.sessionRows()[0]
	if err := m.services.store.SetAgentSessionID(sess.ID, "conversation-of-the-old-tool"); err != nil {
		t.Fatalf("set agent session id: %v", err)
	}
	quitAgent(t, m, sess.ID)

	if err := m.services.tmux.SendKeys(sess.ID, tail, "Enter"); err != nil {
		t.Fatalf("start the other CLI: %v", err)
	}
	waitForPaneChild(t, m, sess.ID, "tail")
	m.applyCmd(t, m.refreshCmd())

	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Tool != "tail-tool" {
		t.Fatalf("tool after the poll = %q, want tail-tool", got.Tool)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("conversation id = %q, want it dropped with the tool that minted it", got.AgentSessionID)
	}
	if row := m.sessionRows()[0]; row.Tool != "tail-tool" {
		t.Fatalf("row shows tool %q, want tail-tool", row.Tool)
	}
}

// A row whose pane still runs the CLI it launched with keeps its tool, and
// with it the conversation that tool can be revived on.
func TestPollLeavesARunningRowAlone(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.poller.options.Binaries = execution.NewToolBinaries(m.services.cfg)
	resetExecution(m)
	createSessionOn(t, m, "steady", "quietchat", t.TempDir())
	sess := m.sessionRows()[0]
	if err := m.services.store.SetAgentSessionID(sess.ID, "kept-conversation"); err != nil {
		t.Fatalf("set agent session id: %v", err)
	}
	waitForAgent(t, m, sess.ID, true)
	m.applyCmd(t, m.refreshCmd())

	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Tool != sess.Tool {
		t.Fatalf("tool = %q, want it left on %q", got.Tool, sess.Tool)
	}
	if got.AgentSessionID != "kept-conversation" {
		t.Fatalf("conversation id = %q, want kept-conversation", got.AgentSessionID)
	}
}

// A battery gauge colors off how empty it is, the opposite of every other
// meter: low charge should read as alarming, a full battery as calm.
func TestGaugeInvertFlipsTheColorRamp(t *testing.T) {
	forceANSI256(t)

	lowCharge := sgrOf(uitheme.Gauge(5, 10, true, presentation.GlyphModeAdvanced))
	highUsage := sgrOf(uitheme.Gauge(95, 10, false, presentation.GlyphModeAdvanced))
	if lowCharge != highUsage {
		t.Fatalf("5%% inverted = %q, want the same alarm color as 95%% uninverted %q", lowCharge, highUsage)
	}

	highCharge := sgrOf(uitheme.Gauge(95, 10, true, presentation.GlyphModeAdvanced))
	lowUsage := sgrOf(uitheme.Gauge(5, 10, false, presentation.GlyphModeAdvanced))
	if highCharge != lowUsage {
		t.Fatalf("95%% inverted = %q, want the same calm color as 5%% uninverted %q", highCharge, lowUsage)
	}

	if lowCharge == highCharge {
		t.Fatal("low and high battery charge rendered the same color")
	}
}

func TestDiskBytes(t *testing.T) {
	for _, tc := range []struct {
		bytes uint64
		want  string
	}{
		{0, "0B"},
		{999, "999B"},
		{1000, "1.0KB"},
		{1_000_000, "1.0MB"},
		{169_850_000_000, "169.8GB"},
		{1_000_000_000_000, "1.0TB"},
	} {
		if got := uiframe.DiskBytes(tc.bytes); got != tc.want {
			t.Errorf("uiframe.DiskBytes(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

// The foot is the machine readings alone: a notice floats over the frame
// rather than taking a row here.
func TestRailFootOnlyShowsComputerStats(t *testing.T) {
	for _, full := range []bool{false, true} {
		for _, width := range []int{34, 70, 120} {
			m := footModel(t)
			m.prefs.fullLayout = full
			foot := strings.Join(m.railFootLines(width), "\n")
			if !strings.Contains(ansi.Strip(foot), "cpu") || strings.Contains(ansi.Strip(foot), "messages") {
				t.Fatalf("full=%v width=%d: stats foot contains messages or loses readings:\n%s", full, width, ansi.Strip(foot))
			}
			for _, n := range m.activeNotices() {
				m.dismissNotice(n.ID)
			}
			if got := strings.Join(m.railFootLines(width), "\n"); got != foot {
				t.Fatalf("full=%v width=%d: notices changed the stats foot", full, width)
			}
		}
	}
}

func TestRailFootLinesFitWidth(t *testing.T) {
	m := footModel(t)
	m.release.Latest = "v0.9.9"
	m.release.URL = "https://example.com"
	for _, width := range []int{40, 55, 70} {
		for _, line := range m.railFootLines(width) {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("width %d: line overflows at %d: %q", width, got, ansi.Strip(line))
			}
		}
	}
}

func TestLayoutsCanHideStats(t *testing.T) {
	for _, tc := range []struct {
		name  string
		full  bool
		width int
	}{
		{name: "split", width: 36},
		{name: "full", full: true, width: 119},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := footModel(t)
			m.layout.width, m.layout.height = 120, 34
			m.prefs.fullLayout = tc.full
			m.prefs.hideStats = true
			if foot := m.railFootLines(tc.width); len(foot) > 0 {
				t.Fatalf("hidden stats still paint %d lines:\n%s", len(foot), ansi.Strip(strings.Join(foot, "\n")))
			}
			rail := m.railLines(tc.width, m.listBodyHeight())
			if len(rail) != m.listBodyHeight() {
				t.Fatalf("footerless rail = %d rows, body is %d", len(rail), m.listBodyHeight())
			}
			for _, line := range rail {
				if line.Rule {
					t.Fatal("hidden stats left their separator behind")
				}
			}
		})
	}
}

func TestFullFootDiskAvailable(t *testing.T) {
	m := &Model{workspace: workspace{snap: sysstat.Snapshot{
		DiskOK: true, DiskPercent: 66, DiskAvailable: 167_950_000_000,
	}}}
	line := ansi.Strip(strings.Join(m.fullFootLine(200), "\n"))
	if !strings.Contains(line, "disk 66% 167.9GB available") {
		t.Fatalf("disk reading = %q", line)
	}
}
