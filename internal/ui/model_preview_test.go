package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

func TestPreviewSettleDropsStaleGen(t *testing.T) {
	m := &Model{mode: modeList, width: 120, height: 40, previewGen: 3}
	updated, cmd := m.Update(previewSettleMsg{gen: 2})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("stale settle should not schedule previewCmd")
	}
	updated, cmd = m.Update(previewSettleMsg{gen: 3})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("settle without a session row should not capture")
	}
}

func TestMoveCursorDebouncesPreview(t *testing.T) {
	m := &Model{
		mode:   modeList,
		width:  120,
		height: 40,
		rows: []treeRow{
			{sess: store.Session{ID: "a", Name: "a"}},
			{sess: store.Session{ID: "b", Name: "b"}},
		},
		cursor: 0,
	}
	cmd := m.moveCursor(1)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d want 1", m.cursor)
	}
	if m.previewGen != 1 {
		t.Fatalf("previewGen = %d want 1", m.previewGen)
	}
	if cmd == nil {
		t.Fatal("move should schedule a settle tick")
	}
	// tea.Tick cmds sleep; invoke the settle path directly for the gen check.
	msg := previewSettleMsg{gen: 1}
	// A second move bumps gen; the first settle is now stale.
	m.moveCursor(-1)
	if m.previewGen != 2 {
		t.Fatalf("previewGen = %d want 2", m.previewGen)
	}
	updated, next := m.Update(msg)
	m = updated.(*Model)
	if next != nil {
		t.Fatal("stale settle after second move must not capture")
	}
	// Fresh settle for the current gen with a session should schedule previewCmd.
	updated, next = m.Update(previewSettleMsg{gen: m.previewGen})
	m = updated.(*Model)
	if next == nil {
		t.Fatal("current settle should schedule a capture")
	}
}
