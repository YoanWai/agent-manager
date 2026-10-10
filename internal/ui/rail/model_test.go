package rail_test

import (
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

func TestReconcilePreservesSelectionByIdentity(t *testing.T) {
	model := rail.New(nil)
	model.Reconcile(rail.Snapshot{Sessions: []rail.Session{
		{ID: "one", Name: "one"},
		{ID: "two", Name: "two"},
	}})

	result := model.Move(1, false)
	if !result.SelectionChanged {
		t.Fatal("moving to another row did not report a selection change")
	}
	if got, ok := model.Selected(); !ok || got.SessionID != "two" {
		t.Fatalf("selected = %+v, %v; want session two", got, ok)
	}

	change := model.Reconcile(rail.Snapshot{Sessions: []rail.Session{
		{ID: "zero", Name: "zero"},
		{ID: "one", Name: "one"},
		{ID: "two", Name: "two"},
	}})
	if change.Changed {
		t.Fatalf("stable identity reported a target change: %+v", change)
	}
	if got, ok := model.Selected(); !ok || got.SessionID != "two" {
		t.Fatalf("selected after reconcile = %+v, %v; want session two", got, ok)
	}
}

func TestRenderFrameOwnsTheExactRowHits(t *testing.T) {
	model := rail.New(nil)
	model.Reconcile(rail.Snapshot{
		Groups: []string{"work"},
		Sessions: []rail.Session{
			{ID: "one", Name: "one", Group: "work", Status: "idle", CreatedAt: time.Now()},
			{ID: "two", Name: "two", Group: "work", Status: "working", CreatedAt: time.Now()},
		},
	})
	frame := model.Render(renderContext(36, 8), rail.Frame{})

	var sessionHits int
	for _, hit := range frame.Hits() {
		if hit.Kind == rail.SessionRow {
			sessionHits++
		}
	}
	if sessionHits != 2 {
		t.Fatalf("session hits = %d; want both painted sessions in %+v", sessionHits, frame.Hits())
	}
	for y, line := range frame.Lines {
		if !line.HitOK {
			continue
		}
		got, ok := frame.RowAt(2, y, 40, true)
		if !ok || got != line.Hit {
			t.Fatalf("hit at line %d = %+v, %v; painted %+v", y, got, ok, line.Hit)
		}
	}
}

func TestMouseConsumesDisplayedFrameWithoutRederivingRows(t *testing.T) {
	model := rail.New(nil)
	model.Reconcile(rail.Snapshot{Sessions: []rail.Session{
		{ID: "one", Name: "one", Status: "idle", CreatedAt: time.Now()},
		{ID: "two", Name: "two", Status: "idle", CreatedAt: time.Now()},
	}})
	frame := model.Render(renderContext(36, 6), rail.Frame{})

	line := -1
	for y, painted := range frame.Lines {
		if painted.HitOK && painted.Hit.SessionID == "two" {
			line = y
			break
		}
	}
	if line < 0 {
		t.Fatal("test setup: session two was not painted")
	}
	model.Reconcile(rail.Snapshot{Sessions: []rail.Session{{ID: "two", Name: "two", Status: "idle", CreatedAt: time.Now()}}})
	decision := model.Mouse(tea.MouseMsg{X: 2, Y: line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, frame, rail.MouseContext{FullWidth: true, FullLayout: true, Now: time.Now()})
	if decision.Selection.SessionID != "two" {
		t.Fatalf("selection = %+v; want the row the displayed frame named", decision.Selection)
	}
}

func TestReorderWaitsForConcreteMutationResult(t *testing.T) {
	model := rail.New(nil)
	model.Reconcile(rail.Snapshot{Sessions: []rail.Session{
		{ID: "one", Name: "one"},
		{ID: "two", Name: "two"},
	}})
	keys := keybind.DefaultList()
	decision := model.Key(keyFor(keys.Binding(keybind.ReorderDown).Keys()[0].Tea()), rail.Frame{}, rail.KeyContext{ListKeys: keys})
	if len(decision.Mutations) != 1 || decision.Mutations[0].Kind != rail.SwapSession {
		t.Fatalf("reorder decision = %+v", decision)
	}
	if got := model.Rows()[1].SessionID; got != "one" {
		t.Fatalf("row moved before persistence result: got %q", got)
	}
	model.ApplyMutation(decision.Mutations[0], nil)
	if got := model.Rows()[1].SessionID; got != "two" {
		t.Fatalf("row after successful mutation = %q, want two", got)
	}
}

func renderContext(width, height int) rail.RenderContext {
	return rail.RenderContext{
		Width: width, Height: height, TerminalWidth: 80, TerminalHeight: 24,
		ListKeys: keybind.DefaultList(),
		Theme: rail.Theme{
			Bg: "#101010", Surface: "#202020", Overlay: "#303030", Border: "#404040",
			Bright: "#ffffff", Text: "#dddddd", Dim: "#aaaaaa", Subtle: "#777777",
			Accent: "#00aaaa", Accent2: "#008888", Working: "#ffaa00", Waiting: "#aa88ff",
			Finished: "#00aa00", Errored: "#ff0000", Idle: "#777777",
		},
	}
}

func keyFor(key string) tea.KeyMsg {
	if len([]rune(key)) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

func TestReconcileCopiesSnapshotValues(t *testing.T) {
	snapshot := rail.Snapshot{Sessions: []rail.Session{{ID: "one", Name: "before"}}}
	model := rail.New(nil)
	model.Reconcile(snapshot)
	snapshot.Sessions[0].Name = "after"

	rows := model.Rows()
	if len(rows) != 2 || rows[1].Name != "before" {
		t.Fatalf("rows = %+v; want copied session name", rows)
	}
}
