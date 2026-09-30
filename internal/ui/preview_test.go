package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"
)

func TestPreviewSettleDropsStaleGen(t *testing.T) {
	m := &Model{mode: modeList, width: 120, height: 40}
	for range 3 {
		m.focusPane.MovePreview()
	}
	updated, cmd := m.Update(previewSettleMsg{gen: 2})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("stale settle should not schedule previewCmd")
	}
	_, cmd = m.Update(previewSettleMsg{gen: 3})
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
		{"selected starting", []treeRow{{sess: store.Session{ID: "a", Status: status.Starting}}}, previewIntervalLive},
		{"selected working", []treeRow{{sess: store.Session{ID: "a", Status: status.Working}}, {sess: store.Session{ID: "b", Status: status.Starting}}}, previewIntervalLive},
		{"selected idle", []treeRow{{sess: store.Session{ID: "a", Status: status.Idle}}, {sess: store.Session{ID: "b", Status: status.Starting}}}, previewIntervalCalm},
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
		mode:   modeList,
		width:  120,
		height: 40, rail: railModelFromRows([]treeRow{
			{sess: store.Session{ID: "a", Name: "a"}},
			{sess: store.Session{ID: "b", Name: "b"}},
		}, 0),
		workspace: workspace{sessions: []store.Session{{ID: "a", Name: "a"}, {ID: "b", Name: "b"}}},
	}
	cmd := m.moveCursor(1)
	if selected, ok := m.rail.SelectedSession(); !ok || selected != "b" {
		t.Fatalf("selected = %q, %v want b", selected, ok)
	}
	if got := m.focusPane.PreviewGeneration(); got != 1 {
		t.Fatalf("previewGen = %d want 1", got)
	}
	if cmd == nil {
		t.Fatal("move should schedule a settle tick")
	}
	// tea.Tick cmds sleep; invoke the settle path directly for the gen check.
	msg := previewSettleMsg{gen: 1}
	// A second move bumps gen; the first settle is now stale.
	m.moveCursor(-1)
	if got := m.focusPane.PreviewGeneration(); got != 2 {
		t.Fatalf("previewGen = %d want 2", got)
	}
	updated, next := m.Update(msg)
	m = updated.(*Model)
	if next != nil {
		t.Fatal("stale settle after second move must not capture")
	}
	// Fresh settle for the current gen with a session should schedule previewCmd.
	_, next = m.Update(previewSettleMsg{gen: m.focusPane.PreviewGeneration()})
	if next == nil {
		t.Fatal("current settle should schedule a capture")
	}
}

// The preview has to follow the pane on its own, without the cursor being
// touched: a manager whose preview only refreshes on selection is a
// screenshot, not a monitor.
func TestPreviewFollowsPaneWithoutCursorMoves(t *testing.T) {
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
	if view := ansi.Strip(m.View()); !strings.Contains(view, "second-line") {
		t.Fatalf("frame missing the newest pane content:\n%s", view)
	}
}
