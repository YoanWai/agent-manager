package rail

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestResolveDropKindsStayInsideRailPolicy(t *testing.T) {
	model := New(nil)
	model.Reconcile(Snapshot{Groups: []string{"alpha", "beta"}, Sessions: []Session{
		{ID: "mover", Name: "mover", Group: "alpha"},
		{ID: "agent", Name: "agent", Group: "beta"},
		{ID: "shell", Name: "shell", Group: "alpha", IsShell: true},
	}})

	model.Focus(Selection{Kind: SessionRow, SessionID: "mover", Group: "alpha"})
	model.enterReorder("s:mover")
	if drop, ok := model.resolveDrop(Selection{Kind: GroupRow, Group: "beta"}); !ok || drop.kind != dropInto {
		t.Fatalf("session onto group = %+v, %v; want dropInto", drop, ok)
	}

	model.finishReorder()
	model.Focus(Selection{Kind: SessionRow, SessionID: "shell", Group: "alpha"})
	model.enterReorder("s:shell")
	if drop, ok := model.resolveDrop(Selection{Kind: SessionRow, SessionID: "agent", Group: "beta"}); !ok || drop.kind != dropUnder {
		t.Fatalf("shell onto agent = %+v, %v; want dropUnder", drop, ok)
	}
}

func TestFilteredRailRefusesCrossLevelDrop(t *testing.T) {
	model := New(nil)
	model.Reconcile(Snapshot{Groups: []string{"alpha", "beta"}, Sessions: []Session{
		{ID: "mover", Name: "mover", Group: "alpha"}, {ID: "stay", Name: "stay", Group: "beta"},
	}})
	model.SetSearch("e", false)
	model.Focus(Selection{Kind: SessionRow, SessionID: "mover", Group: "alpha"})
	model.enterReorder("s:mover")
	if drop, ok := model.resolveDrop(Selection{Kind: GroupRow, Group: "beta"}); ok {
		t.Fatalf("filtered cross-level drop = %+v", drop)
	}
}

func TestStaleAutoscrollTickCannotSteerANewerLift(t *testing.T) {
	model := New(nil)
	model.Reconcile(Snapshot{Sessions: []Session{{ID: "one", Name: "one"}, {ID: "two", Name: "two"}}})
	model.Focus(Selection{Kind: SessionRow, SessionID: "one"})
	model.enterReorder("s:one")
	stale := model.reorder.lift
	model.finishReorder()
	model.enterReorder("s:one")
	model.reorder.autoscroll = autoscrollState{edge: 1, running: true}

	decision := model.ApplyAutoScroll(AutoScrollTick{Lift: stale}, Frame{}, MouseContext{})
	if !decision.Consumed || !model.reorder.autoscroll.running || model.reorder.autoscroll.anchored {
		t.Fatalf("stale tick changed scroll = %+v decision = %+v", model.reorder.autoscroll, decision)
	}
}

func TestDragEdgeSchedulesTypedAutoscroll(t *testing.T) {
	model := New(nil)
	for index := 0; index < 20; index++ {
		model.snapshot.Sessions = append(model.snapshot.Sessions, Session{ID: string(rune('a' + index)), Name: "row", CreatedAt: time.Now()})
	}
	model.rebuildRows()
	model.Focus(model.rows[1].selection())
	model.enterReorder(model.rows[1].key())
	frame := model.Render(RenderContext{Width: 30, Height: 5, TerminalWidth: 80, TerminalHeight: 24}, Frame{})
	last := len(frame.Lines) - 1
	decision := model.Mouse(tea.MouseMsg{X: 2, Y: last, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}, frame, MouseContext{FullWidth: true})
	if decision.AutoScroll == nil || decision.AutoScroll.Lift != model.reorder.lift {
		t.Fatalf("autoscroll request = %+v", decision.AutoScroll)
	}
}
