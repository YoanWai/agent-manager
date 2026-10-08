package rail

import "testing"

var railTestHeights = []int{1, 3, 3, 3, 3, 1, 3, 3, 2, 3, 1, 3, 3, 3, 3, 3, 1, 3, 3}

func railWalk(n int) []int {
	var sequence []int
	for index := 0; index < n; index++ {
		sequence = append(sequence, index)
	}
	for index := n - 2; index >= 0; index-- {
		sequence = append(sequence, index)
	}
	return sequence
}

func TestRailWindowHoldsStillWhileCursorIsOnScreen(t *testing.T) {
	for _, budget := range []int{20, 30, 40} {
		top, previousStart, previousEnd := 0, -1, -1
		for _, cursor := range railWalk(len(railTestHeights)) {
			start, end := railWindow(railTestHeights, cursor, budget, top)
			if previousStart >= 0 && cursor >= previousStart && cursor < previousEnd && start != previousStart {
				t.Fatalf("budget %d: cursor %d already sat in [%d,%d), scrolled to %d", budget, cursor, previousStart, previousEnd, start)
			}
			top, previousStart, previousEnd = start, start, end
		}
	}
}

func TestRailWindowKeepsCursorEntryWhole(t *testing.T) {
	for _, budget := range []int{6, 20, 30, 40} {
		top := 0
		for _, cursor := range railWalk(len(railTestHeights)) {
			start, end := railWindow(railTestHeights, cursor, budget, top)
			if cursor < start || cursor >= end {
				t.Fatalf("budget %d: cursor %d outside [%d,%d)", budget, cursor, start, end)
			}
			top = start
		}
	}
}

func TestRailWindowScrollsOnlyAsFarAsNeeded(t *testing.T) {
	top := 0
	for cursor := 0; cursor < len(railTestHeights); cursor++ {
		start, _ := railWindow(railTestHeights, cursor, 20, top)
		if start > top && windowEnd(railTestHeights, start-1, 20) > cursor {
			t.Fatalf("cursor %d scrolled past a fitting window to %d", cursor, start)
		}
		top = start
	}
}

func TestRailWindowFollowsCursorBackAndShowsFittingList(t *testing.T) {
	start, end := railWindow(railTestHeights, 2, 20, 11)
	if start != 2 || end <= 2 {
		t.Fatalf("backward window = [%d,%d), want it to begin at cursor", start, end)
	}
	heights := []int{1, 3, 3}
	start, end = railWindow(heights, 2, 20, 1)
	if start != 0 || end != len(heights) {
		t.Fatalf("fitting window = [%d,%d)", start, end)
	}
}
