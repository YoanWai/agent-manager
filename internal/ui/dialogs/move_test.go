package dialogs

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMoveKeys(t *testing.T) {
	d := MoveSession("shell")
	if got := d.Key(tea.KeyMsg{Type: tea.KeyDown}); got != (MoveKey{Action: MoveCursor, Delta: 1}) {
		t.Fatalf("down = %+v", got)
	}
	if got := d.Key(tea.KeyMsg{Type: tea.KeyUp}); got != (MoveKey{Action: MoveCursor, Delta: -1}) {
		t.Fatalf("up = %+v", got)
	}
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEnter}); got.Action != MoveSubmit {
		t.Fatalf("enter = %+v", got)
	}
	if got := d.Key(tea.KeyMsg{Type: tea.KeyEsc}); got.Action != MoveCancel {
		t.Fatalf("esc = %+v", got)
	}
	if got := d.Key(runes("j")); got.Action != MoveStay {
		t.Fatalf("j = %+v", got)
	}
}

func TestMovePlacesASessionUnderThePickedRow(t *testing.T) {
	d := MoveSession("shell")
	request, ok := d.Place(MovePick{Group: "work", ParentID: "parent"})
	if !ok || request != (MoveRequest{SessionID: "shell", Group: "work", ParentID: "parent"}) || request.IsGroup() {
		t.Fatalf("request = %+v ok=%v, want the session placed under its picked agent", request, ok)
	}
}

func TestMoveReparentsAGroup(t *testing.T) {
	d := MoveGroup("work/sub")
	if !d.IsGroup() {
		t.Fatal("a group move should report a group")
	}
	if _, ok := d.Place(MovePick{Group: "work"}); ok {
		t.Fatal("moving a group onto its own parent should just close")
	}
	request, ok := d.Place(MovePick{})
	if !ok || request != (MoveRequest{GroupPath: "work/sub"}) || !request.IsGroup() {
		t.Fatalf("request = %+v ok=%v, want the group moved to root", request, ok)
	}
	if request, _ = MoveGroup("top").Place(MovePick{Group: "work"}); request.Group != "work" || request.GroupPath != "top" {
		t.Fatalf("request = %+v, want top moved under work", request)
	}
}
