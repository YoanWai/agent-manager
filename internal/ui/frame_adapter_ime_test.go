package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestFocusCursorAnchorTracksMirroredCaret(t *testing.T) {
	m := &Model{
		screen: screen{},
		workspace: workspace{
			preview:  "first\nsecond\nthird\n",
			sessions: []store.Session{{ID: "focused"}},
		},
		rail:  railModelFromRows([]treeRow{{sess: store.Session{ID: "focused"}}}, 0),
		focus: focusState{runtime: focusRuntimeState{imeCursor: &cursorAnchor{}}},
	}
	m.push(newFocusSurface(m))
	m.focus.pane.Enter(uifocus.EnterContext{SessionID: "focused"})
	setFocusCursor(m, paneCursor{x: 7, y: 1, ok: true})
	prepareFocusFrame(m, m.workspace.preview, 30, 3, 40, 8, 1)
	m.syncCursorAnchor("frame")
	col, row, ok := m.focus.runtime.imeCursor.get()
	if !ok || col != 48 || row != 10 {
		t.Fatalf("cursor anchor = (%d, %d, %v), want (48, 10, true)", col, row, ok)
	}

	setFocusScrollOffset(t, m, "focused", 1, 3)
	m.syncCursorAnchor("frame")
	if _, _, ok := m.focus.runtime.imeCursor.get(); ok {
		t.Fatal("scrolled pane kept a live IME cursor anchor")
	}
}

func TestFocusCursorAnchorRemovesListSearchMarker(t *testing.T) {
	m := &Model{
		screen: screen{},
		workspace: workspace{
			preview:  "first\nsecond\nthird\n",
			sessions: []store.Session{{ID: "focused"}},
		},
		rail:  railModelSearching([]treeRow{{sess: store.Session{ID: "focused", Name: "active"}}}, 0, "active"),
		focus: focusState{runtime: focusRuntimeState{imeCursor: &cursorAnchor{}}},
	}
	m.push(newFocusSurface(m))
	m.focus.pane.Enter(uifocus.EnterContext{SessionID: "focused"})
	setFocusCursor(m, paneCursor{x: 7, y: 1, ok: true})
	prepareFocusFrame(m, m.workspace.preview, 30, 3, 40, 8, 1)
	frame := m.syncCursorAnchor(m.searchFieldLine(40) + "\nfocused pane")
	if strings.Contains(frame, cursorAnchorMarker) {
		t.Fatal("private list search marker leaked from the focused frame")
	}
	col, row, ok := m.focus.runtime.imeCursor.get()
	if !ok || col != 48 || row != 10 {
		t.Fatalf("cursor anchor = (%d, %d, %v), want (48, 10, true)", col, row, ok)
	}
}

func TestFocusCursorAnchorAccountsForDroppedCaptureRows(t *testing.T) {
	m := &Model{
		screen: screen{},
		workspace: workspace{
			preview:  "one\ntwo\nthree\nfour\n",
			sessions: []store.Session{{ID: "focused"}},
		},
		rail:  railModelFromRows([]treeRow{{sess: store.Session{ID: "focused"}}}, 0),
		focus: focusState{runtime: focusRuntimeState{imeCursor: &cursorAnchor{}}},
	}
	m.push(newFocusSurface(m))
	m.focus.pane.Enter(uifocus.EnterContext{SessionID: "focused"})
	setFocusCursor(m, paneCursor{x: 4, y: 3, ok: true})
	prepareFocusFrame(m, m.workspace.preview, 12, 2, 20, 5, 3)
	m.syncCursorAnchor("frame")
	col, row, ok := m.focus.runtime.imeCursor.get()
	if !ok || col != 25 || row != 7 {
		t.Fatalf("cropped cursor anchor = (%d, %d, %v), want (25, 7, true)", col, row, ok)
	}
}

func TestPreviewLineStripsCapturedCursorMarker(t *testing.T) {
	got := previewLine("pane"+cursorAnchorMarker+"output", 40)
	if strings.Contains(got, cursorAnchorMarker) {
		t.Fatal("captured pane retained the private cursor marker")
	}
	if plain := strings.TrimSpace(ansi.Strip(got)); plain != "paneoutput" {
		t.Fatalf("sanitized pane = %q, want paneoutput", plain)
	}
}

func TestCustomSearchCursorsEmitMarkersOnlyWhileTyping(t *testing.T) {
	m := &Model{
		layout: layoutState{
			width:  100,
			height: 28,
		},
		services: services{
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
		rail: railModelSearching(nil, 0, "中文"),
	}
	m.openHelp()
	m.handleKey(runeKey("/"))
	for _, r := range "定位" {
		m.handleKey(runeKey(string(r)))
	}
	if line := m.searchFieldLine(40); !strings.Contains(line, cursorAnchorMarker) {
		t.Fatal("list search cursor has no marker")
	}
	if frame := viewHelpOf(m); !strings.Contains(frame, cursorAnchorMarker) {
		t.Fatal("help search cursor has no marker")
	}

	m.rail.SetSearch(m.rail.Search(), false)
	m.handleKey(namedKey(tea.KeyEnter))
	if line := m.searchFieldLine(40); strings.Contains(line, cursorAnchorMarker) {
		t.Fatal("closed list search kept a cursor marker")
	}
	if frame := viewHelpOf(m); strings.Contains(frame, cursorAnchorMarker) {
		t.Fatal("closed help search kept a cursor marker")
	}
}

func TestNoActiveInputClearsCursorAnchor(t *testing.T) {
	m := &Model{
		focus: focusState{runtime: focusRuntimeState{imeCursor: &cursorAnchor{}}},
	}
	m.focus.runtime.imeCursor.set(9, 7, true)
	if got := m.syncCursorAnchor("plain frame"); got != "plain frame" {
		t.Fatalf("frame changed to %q", got)
	}
	if _, _, ok := m.focus.runtime.imeCursor.get(); ok {
		t.Fatal("inactive frame kept the previous cursor anchor")
	}
}

func TestFinalHelpLayoutPublishesAndRemovesCursorMarker(t *testing.T) {
	m := &Model{
		layout: layoutState{
			width:  100,
			height: 28,
		},
		services: services{
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
		focus: focusState{runtime: focusRuntimeState{imeCursor: &cursorAnchor{}}},
	}
	m.openHelp()
	m.handleKey(runeKey("/"))
	for _, r := range "中文" {
		m.handleKey(runeKey(string(r)))
	}
	frame := preparedView(m)
	if strings.Contains(frame, cursorAnchorMarker) {
		t.Fatal("private cursor marker leaked from the final frame")
	}
	col, row, ok := m.focus.runtime.imeCursor.get()
	if !ok || col < 1 || col > m.layout.width || row < 1 || row > m.layout.height {
		t.Fatalf("final cursor anchor = (%d, %d, %v), frame is %dx%d", col, row, ok, m.layout.width, m.layout.height)
	}
	plain := strings.Split(ansi.Strip(frame), "\n")
	if row > len(plain) || !strings.Contains(plain[row-1], "search 中文▏") {
		t.Fatalf("anchor row %d does not contain the help input: %q", row, plain[row-1])
	}
}
