package focus

import (
	"strings"
	"testing"
)

// markerRules is a tool whose prompt rows open with "❯ ", and which, when
// placeholder is set, paints its own composer caret behind it.
type markerRules struct{ placeholder string }

func (r markerRules) InputPrefix(row string) (string, bool) {
	if strings.HasPrefix(row, "❯ ") {
		return "❯ ", true
	}
	return "", false
}

func (r markerRules) TextBeforeCaret(row string, caretX int) bool {
	prefix, ok := r.InputPrefix(row)
	if !ok {
		return false
	}
	runes := []rune(row)
	from := len([]rune(prefix))
	for i := from; i < caretX && i < len(runes); i++ {
		if runes[i] != ' ' {
			return true
		}
	}
	return false
}

func (r markerRules) ComposerIsEmpty(row string) bool {
	prefix, ok := r.InputPrefix(row)
	if !ok || r.placeholder == "" {
		return false
	}
	rest := strings.TrimSpace(row[len(prefix):])
	return rest == "" || rest == r.placeholder
}

func (markerRules) MatchesActivityCutoff(string) bool { return false }

func (r markerRules) ParksItsCaret() bool { return r.placeholder != "" }

func caretModel(cursor Cursor) Model {
	var model Model
	model.Enter(EnterContext{SessionID: "s1"})
	model.ApplyPane(PaneUpdate{SessionID: "s1", Cursor: cursor}, "s1")
	return model
}

func TestCaretAtInputStart(t *testing.T) {
	visible := func(x, y int) Cursor { return Cursor{X: x, Y: y, Visible: true, PositionKnown: true} }
	cases := []struct {
		name    string
		preview string
		cursor  Cursor
		rules   markerRules
		want    bool
	}{
		{"empty prompt", "❯ \n", visible(2, 0), markerRules{}, true},
		{"draft before caret", "❯ hi\n", visible(4, 0), markerRules{}, false},
		{"caret inside the marker", "❯ \n", visible(0, 0), markerRules{}, false},
		{"draft continues onto the row", "❯ first line\n❯ \n", visible(2, 1), markerRules{}, false},
		{"blank row above", "❯ \n❯ \n", visible(2, 1), markerRules{}, true},
		{"hidden caret", "❯ \n", Cursor{X: 2, Y: 0, PositionKnown: true}, markerRules{}, false},
		{"caret below the rows", "❯ \n", visible(2, 3), markerRules{}, false},
		{"parked under an empty composer", "❯ Ask...\n\nfooter\n\n", Cursor{X: 0, Y: 3, PositionKnown: true}, markerRules{placeholder: "Ask..."}, true},
		{"parked under a draft", "❯ draft\n\nfooter\n\n", Cursor{X: 0, Y: 3, PositionKnown: true}, markerRules{placeholder: "Ask..."}, false},
	}
	for _, c := range cases {
		model := caretModel(c.cursor)
		got := model.CaretAtInputStart(CaretContext{SessionID: "s1", Preview: c.preview, CaughtUp: true, Rules: c.rules})
		if got != c.want {
			t.Errorf("%s: CaretAtInputStart = %v, want %v", c.name, got, c.want)
		}
	}
}

// Keys counted to a polled pane land after the frame the mirror holds, so
// that frame's caret cannot put Left at the prompt head, and another
// session's mirror or a scrolled-back pane cannot decide it either.
func TestCaretAtInputStartNeedsACurrentCaughtUpFrame(t *testing.T) {
	model := caretModel(Cursor{X: 2, Y: 0, Visible: true, PositionKnown: true})
	ctx := CaretContext{SessionID: "s1", Preview: "❯ \n", CaughtUp: true, Rules: markerRules{}}
	if !model.CaretAtInputStart(ctx) {
		t.Fatal("test setup: the empty prompt's caret is not at its head")
	}
	behind := ctx
	behind.CaughtUp = false
	if model.CaretAtInputStart(behind) {
		t.Fatal("a frame taken before a queued key decided the caret")
	}
	other := ctx
	other.SessionID = "other"
	if model.CaretAtInputStart(other) {
		t.Fatal("another session's pane mirror decided the caret")
	}
	model.ApplyPane(PaneUpdate{SessionID: "s1", History: 10, Cursor: Cursor{X: 2, Y: 0, Visible: true}}, "s1")
	model.ScrollLines(-3, "s1", 5)
	if model.CaretAtInputStart(ctx) {
		t.Fatal("a scrolled-back pane decided the caret")
	}
}

func TestCaretRowDropsAParkedCaret(t *testing.T) {
	preview := "content\n\n\n"
	parked := caretModel(Cursor{X: 0, Y: 2, Visible: true})
	if got := parked.CaretRow(preview, true); got != -1 {
		t.Fatalf("parked CaretRow = %d, want -1", got)
	}
	if got := parked.CaretRow(preview, false); got != 2 {
		t.Fatalf("CaretRow for a tool that does not park = %d, want 2", got)
	}
	typing := caretModel(Cursor{X: 3, Y: 0, Visible: true})
	if got := typing.CaretRow(preview, true); got != 0 {
		t.Fatalf("in-content CaretRow = %d, want 0", got)
	}
	hidden := caretModel(Cursor{X: 3, Y: 0})
	if got := hidden.CaretRow(preview, false); got != -1 {
		t.Fatalf("hidden CaretRow = %d, want -1", got)
	}
}
