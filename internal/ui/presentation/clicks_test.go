package presentation

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLinesFindsWhatEachPaintedLineShows(t *testing.T) {
	var lines Lines[int]
	lines.Show(7, 2)
	lines.Skip(1)
	lines.Show(9, 1)
	for line, want := range map[int]struct {
		item  int
		shown bool
	}{-1: {}, 0: {7, true}, 1: {7, true}, 2: {}, 3: {9, true}, 4: {}} {
		if item, shown := lines.At(line); item != want.item || shown != want.shown {
			t.Errorf("At(%d) = %d, %v, want %d, %v", line, item, shown, want.item, want.shown)
		}
	}
	lines.Cut(1)
	if _, shown := lines.At(1); shown {
		t.Error("a cut line still answers")
	}
	lines.Reset()
	if _, shown := lines.At(0); shown {
		t.Error("a reset paint still answers")
	}
}

func TestClickRowPointsThenRuns(t *testing.T) {
	cursor := 0
	if ClickRow(&cursor, 2) || cursor != 2 {
		t.Fatalf("first click: cursor %d, want it moved to 2 without running", cursor)
	}
	if !ClickRow(&cursor, 2) || cursor != 2 {
		t.Fatalf("second click on the row under the cursor should run it, cursor %d", cursor)
	}
}

func TestWheelStepMovesUpOrDownOnly(t *testing.T) {
	for button, want := range map[tea.MouseButton]int{
		tea.MouseButtonWheelUp:    -1,
		tea.MouseButtonWheelDown:  1,
		tea.MouseButtonWheelLeft:  0,
		tea.MouseButtonWheelRight: 0,
	} {
		if got := WheelStep(tea.MouseMsg{Button: button, Action: tea.MouseActionPress}); got != want {
			t.Errorf("WheelStep(%v) = %d, want %d", button, got, want)
		}
	}
}
