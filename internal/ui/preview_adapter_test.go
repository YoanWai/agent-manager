package ui

import (
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uicapture "github.com/YoanWai/agent-manager/internal/ui/preview/capture"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestPreviewSettleDropsStaleGen(t *testing.T) {
	m := &Model{layout: layoutState{width: 120, height: 40}}
	for range 3 {
		m.focus.pane.MovePreview()
	}
	updated, cmd := m.Update(uicapture.SettleMsg{Gen: 2})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("stale settle should not schedule previewCmd")
	}
	_, cmd = m.Update(uicapture.SettleMsg{Gen: 3})
	if cmd != nil {
		t.Fatal("settle without a session row should not capture")
	}
}

func TestPreviewCadenceIsIndependentFromStartupAnimation(t *testing.T) {
	tests := []struct {
		name string
		rows []treeRow
		want time.Duration
	}{
		{"selected starting", []treeRow{{sess: store.Session{ID: "a", Status: status.Starting}}}, uicapture.IntervalLive},
		{"selected working", []treeRow{{sess: store.Session{ID: "a", Status: status.Working}}, {sess: store.Session{ID: "b", Status: status.Starting}}}, uicapture.IntervalLive},
		{"selected idle", []treeRow{{sess: store.Session{ID: "a", Status: status.Idle}}, {sess: store.Session{ID: "b", Status: status.Starting}}}, uicapture.IntervalCalm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{rail: railModelFromRows(tt.rows, 0)}
			for _, row := range tt.rows {
				if !row.isGroup {
					m.workspace.sessions = append(m.workspace.sessions, row.sess)
				}
			}
			if got := m.previewInterval(); got != tt.want {
				t.Fatalf("preview interval = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMoveCursorDebouncesPreview(t *testing.T) {
	m := &Model{
		layout: layoutState{
			width:  120,
			height: 40,
		},
		rail: railModelFromRows([]treeRow{
			{sess: store.Session{ID: "a", Name: "a"}},
			{sess: store.Session{ID: "b", Name: "b"}},
		}, 0),
		workspace: workspace{sessions: []store.Session{{ID: "a", Name: "a"}, {ID: "b", Name: "b"}}},
	}
	cmd := m.moveCursor(1)
	if selected, ok := m.rail.SelectedSession(); !ok || selected != "b" {
		t.Fatalf("selected = %q, %v want b", selected, ok)
	}
	if got := m.focus.pane.PreviewGeneration(); got != 1 {
		t.Fatalf("previewGen = %d want 1", got)
	}
	if cmd == nil {
		t.Fatal("move should schedule a settle tick")
	}
	// tea.Tick cmds sleep; invoke the settle path directly for the gen check.
	msg := uicapture.SettleMsg{Gen: 1}
	// A second move bumps gen; the first settle is now stale.
	m.moveCursor(-1)
	if got := m.focus.pane.PreviewGeneration(); got != 2 {
		t.Fatalf("previewGen = %d want 2", got)
	}
	updated, next := m.Update(msg)
	m = updated.(*Model)
	if next != nil {
		t.Fatal("stale settle after second move must not capture")
	}
	// Fresh settle for the current gen with a session should schedule previewCmd.
	_, next = m.Update(uicapture.SettleMsg{Gen: m.focus.pane.PreviewGeneration()})
	if next == nil {
		t.Fatal("current settle should schedule a capture")
	}
}

// The preview has to follow the pane on its own, without the cursor being
// touched: a manager whose preview only refreshes on selection is a
// screenshot, not a monitor.
func TestPreviewFollowsPaneWithoutCursorMoves(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "watcher", t.TempDir(), "")
	m.selectSessionRow(t, "watcher")
	sess := m.sessionRows()[0]

	waitForPane := func(marker string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			// Only the poll cycle runs here; nothing selects or moves.
			m.applyCmd(t, m.refreshCmd())
			if strings.Contains(ansi.Strip(m.workspace.preview), marker) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("preview never picked up %q, has:\n%s", marker, ansi.Strip(m.workspace.preview))
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	if err := m.services.tmux.SendText(sess.ID, "first-line"); err != nil {
		t.Fatalf("send text: %v", err)
	}
	waitForPane("first-line")

	if err := m.services.tmux.SendText(sess.ID, "second-line"); err != nil {
		t.Fatalf("send text: %v", err)
	}
	waitForPane("second-line")

	// The frame has to carry it too, not just the model field.
	if view := ansi.Strip(preparedView(m)); !strings.Contains(view, "second-line") {
		t.Fatalf("frame missing the newest pane content:\n%s", view)
	}
}

func TestTypedPromptsAreHeldPerSession(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "typed-a", t.TempDir(), "")
	createSession(t, m, "typed-b", t.TempDir(), "")
	index := func(name string) int {
		for i, s := range m.workspace.sessions {
			if s.Name == name {
				return i
			}
		}
		t.Fatalf("no session named %q", name)
		return -1
	}
	a, b := index("typed-a"), index("typed-b")
	aID, bID := m.workspace.sessions[a].ID, m.workspace.sessions[b].ID
	hold := func(i int, text string, at time.Time) {
		sess := m.workspace.sessions[i]
		m.holdTypedPrompt(uicapture.TypedPrompt{Session: sess, ID: sess.ID, Text: text, At: at})
	}
	commit := func() {
		m.commitTypedPrompt()
		m.drainEffects(t)
	}
	lastPrompt := func(id string) string {
		got, err := m.services.store.Get(id)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		return got.LastPrompt
	}

	// A's capture lands after B's: both drafts are still waiting on their
	// sessions to turn working.
	hold(a, "prompt for a", time.Now())
	hold(b, "prompt for b", time.Now())
	m.workspace.sessions[a].Status = status.Working
	m.workspace.sessions[b].Status = status.Working
	commit()
	if got := lastPrompt(aID); got != "prompt for a" {
		t.Fatalf("a's last prompt = %q, want %q", got, "prompt for a")
	}
	if got := lastPrompt(bID); got != "prompt for b" {
		t.Fatalf("b's last prompt = %q, want %q", got, "prompt for b")
	}

	hold(a, "stale for a", time.Now().Add(-2*uicapture.TypedPromptGrace))
	hold(b, "fresh for b", time.Now())
	m.workspace.sessions[b].Status = status.Idle
	commit()
	if got := lastPrompt(aID); got != "prompt for a" {
		t.Fatalf("an expired draft overwrote a's last prompt with %q", got)
	}
	if got := lastPrompt(bID); got != "prompt for b" {
		t.Fatalf("b's draft committed before b turned working: %q", got)
	}
	m.workspace.sessions[b].Status = status.Working
	commit()
	if got := lastPrompt(bID); got != "fresh for b" {
		t.Fatalf("b's last prompt = %q, want the draft that waited for working", got)
	}
}

// Every line of a detail head has to stay inside the content column: a fact
// that overflows tears the panel beside it.
func TestDetailHeadsFitTheirColumn(t *testing.T) {
	for _, width := range []int{28, 40, 60, 76, 120} {
		session := shotModel()
		session.workspace.sessions[3].WorktreeBranch = "am/add-rate-limiting"
		session.rebuildRows()
		session.workspace.queuedMessages = map[string]int{"add-rate-limiting": 2}

		group := shotModel()
		for i, row := range railRows(group) {
			if row.isGroup && row.group == "backend" {
				setRailCursor(group, i)
			}
		}
		heads := map[string]string{
			"session": session.viewDetail(width),
			"group":   group.viewDetail(width),
			"roster":  group.viewGroupAgents("backend", width, 12),
		}
		for name, head := range heads {
			for i, line := range strings.Split(head, "\n") {
				if got := ansi.StringWidth(line); got > width {
					t.Errorf("%s head at %d: line %d is %d wide: %q", name, width, i, got, ansi.Strip(line))
				}
			}
		}

		// The head's height feeds previewPaneHeight, so a fourth line would
		// tmux-resize every live pane the moment a message arrived.
		lines := strings.Split(heads["session"], "\n")
		if len(lines) != 3 {
			t.Errorf("badged head at %d is %d lines: %q", width, len(lines), ansi.Strip(heads["session"]))
		}
		if !strings.Contains(ansi.Strip(lines[0]), "✉2") {
			t.Errorf("head at %d dropped the badge: %q", width, ansi.Strip(lines[0]))
		}
	}
}

// The branch chip is the first thing the head gives up, then the tool chip,
// so the name and the state survive the narrowest columns.
func TestDetailHeadShedsChipsBeforeFacts(t *testing.T) {
	m := shotModel()
	m.workspace.sessions[3].WorktreeBranch = "am/add-rate-limiting"
	m.rebuildRows()

	wide := ansi.Strip(strings.Split(m.viewDetail(120), "\n")[0])
	for _, want := range []string{"add-rate-limiting", "claude", "am/add-rate-limiting", "working"} {
		if !strings.Contains(wide, want) {
			t.Fatalf("wide head is missing %q: %q", want, wide)
		}
	}
	if strings.Contains(wide, "✉") {
		t.Errorf("head badged a session holding no queued message: %q", wide)
	}
	mid := ansi.Strip(strings.Split(m.viewDetail(60), "\n")[0])
	if strings.Contains(mid, "am/add-rate-limiting") {
		t.Errorf("60 columns kept the branch chip: %q", mid)
	}
	narrow := ansi.Strip(strings.Split(m.viewDetail(30), "\n")[0])
	if !strings.Contains(narrow, "add-rate") || !strings.Contains(narrow, "working") {
		t.Errorf("30 columns dropped a fact instead of trimming the name: %q", narrow)
	}
}

// A roster is a table: every tool starts on one column and every state ends
// on one edge, whatever the names around them do.
func TestGroupRosterColumnsAlign(t *testing.T) {
	m := shotModel()
	rows := strings.Split(ansi.Strip(m.viewGroupAgents("backend", 76, 12)), "\n")
	if len(rows) < 3 {
		t.Fatalf("roster is only %d rows: %q", len(rows), rows)
	}
	column := -1
	for _, row := range rows[1:] {
		at := strings.Index(row, "claude")
		if at < 0 {
			at = strings.Index(row, "codex")
		}
		if at < 0 {
			t.Fatalf("no tool on roster row %q", row)
		}
		// Byte offsets shift with every multi-byte glyph in a name, so the
		// column is the display width of what precedes the tool.
		cell := ansi.StringWidth(row[:at])
		if column == -1 {
			column = cell
		} else if cell != column {
			t.Errorf("tool column moved from %d to %d: %q", column, cell, row)
		}
		if got := ansi.StringWidth(row); got != 76 {
			t.Errorf("roster row is %d wide, want the full 76: %q", got, row)
		}
	}
}

// A group head names the key that spawns into it, since the same key answers
// a session and is read as belonging to sessions alone. The state breakdown
// is the reading on that row, so a column too tight for both keeps it.
func TestGroupHeadNamesTheSpawnKey(t *testing.T) {
	m := shotModel()
	for i, row := range railRows(m) {
		if row.isGroup && row.group == "backend" {
			setRailCursor(m, i)
		}
	}
	wide := ansi.Strip(m.viewDetail(76))
	if !strings.Contains(wide, "space") || !strings.Contains(wide, "new agent") {
		t.Errorf("wide group head does not name the spawn key: %q", wide)
	}
	narrow := ansi.Strip(m.viewDetail(40))
	if strings.Contains(narrow, "new agent") {
		t.Errorf("narrow group head kept the hint over the state: %q", narrow)
	}
	if !strings.Contains(narrow, "working") {
		t.Errorf("narrow group head lost the state breakdown: %q", narrow)
	}
}

// blankCapture is what tmux hands back for a session whose agent has not
// painted yet: one empty row per pane line, not an empty capture.
const blankCapture = "\n\n\n\n\n\n\n\n\n\n"

func previewModel(sessionStatus, preview string) *Model {
	session := store.Session{ID: "boot", Name: "boot", Status: sessionStatus}
	return &Model{layout: layoutState{width: 120, height: 40}, workspace: workspace{preview: preview, sessions: []store.Session{session}}, rail: railModelFromRows([]treeRow{{sess: session}}, 0)}
}

func previewText(m *Model) string {
	var out []string
	for _, line := range m.previewLines(80, 12, "  ") {
		out = append(out, ansi.Strip(line.text))
	}
	return strings.Join(out, "\n")
}

func TestPreviewBottomAlignsCompactPane(t *testing.T) {
	m := previewModel(status.Finished, "todo\ncomposer"+strings.Repeat("\n", 10))
	m.push(newFocusSurface(m))
	m.focus.pane.Enter(uifocus.EnterContext{SessionID: "boot"})
	lines := strings.Split(previewText(m), "\n")
	if strings.TrimSpace(lines[10]) != "todo" || strings.TrimSpace(lines[11]) != "composer" {
		t.Fatalf("compact pane was not bottom aligned: %q", lines)
	}
	wantY := m.listChromeRows() + 10
	box := m.focus.pane.FrameBox()
	if !box.Valid || box.Y != wantY || box.Height != 2 {
		t.Fatalf("pane box = %+v, want two rows starting at %d", box, wantY)
	}
}

// A launching agent paints nothing for a while, and the blank block that
// leaves reads as a broken session; the preview says it is coming up. The
// blank rows are still the session's pane, so they stay hit-testable.
func TestPreviewShowsLoaderWhileSessionStarts(t *testing.T) {
	m := previewModel(status.Starting, blankCapture)
	if got := previewText(m); !strings.Contains(got, "starting up") {
		t.Fatalf("preview should carry the launch loader, got %q", got)
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
}

// Only the launch state spins: a session that is up with a cleared pane, and
// one that never came up at all, both keep the plain preview.
func TestPreviewSkipsLoaderForSettledSessions(t *testing.T) {
	live := previewModel(status.Idle, blankCapture)
	if got := previewText(live); strings.Contains(got, "starting up") {
		t.Fatalf("an idle session must not spin, got %q", got)
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
	m.Update(uistartup.TickMsg{})
	if next := previewText(m); next == first {
		t.Fatal("preview loader did not move on the startup tick")
	}
}

// The loader borrows no mark that already names a state: a frame caught
// mid-turn would otherwise read as that state, and the detail head above
// the preview is painting the real one.
func TestPreviewLoaderFramesAreNotStatusMarks(t *testing.T) {
	states := []string{status.Working, status.Starting, status.Waiting, status.Finished, status.Errored, status.Dead, status.Idle}
	for _, frame := range presentation.GlyphsFor("").StartupFrames {
		for _, state := range states {
			if frame == presentation.GlyphsFor("").StatusGlyph(state) {
				t.Fatalf("loader frame %q is the %s mark", frame, state)
			}
		}
	}
}

// A focused pane is the screen the user types on, so its first captured row
// keeps the caret drawn there rather than the loader.
func TestPreviewLeavesTheFocusedPaneAlone(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := previewModel(status.Starting, blankCapture)
	m.push(newFocusSurface(m))
	m.focus.pane.Enter(uifocus.EnterContext{SessionID: "boot"})
	setFocusCursor(m, paneCursor{ok: true})
	lines := m.previewLines(80, 12, "  ")
	first := lines[m.focus.pane.FrameBox().Y-m.listChromeRows()].text
	if strings.Contains(ansi.Strip(first), "starting up") {
		t.Fatalf("the loader took the focused pane's first row: %q", first)
	}
	if !strings.Contains(first, "\x1b[") {
		t.Fatalf("focused row 0 lost its caret: %q", first)
	}
}
