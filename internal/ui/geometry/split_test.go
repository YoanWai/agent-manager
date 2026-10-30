package geometry

import "testing"

func TestParseRatio(t *testing.T) {
	if got := ParseRatio(""); got != DefaultSplitRatio {
		t.Fatalf("empty setting: got %v want %v", got, DefaultSplitRatio)
	}
	if got := ParseRatio("0.5"); got != 0.5 {
		t.Fatalf("stored 0.5: got %v", got)
	}
	if got := ParseRatio("nope"); got != DefaultSplitRatio {
		t.Fatalf("garbage should fall back, got %v", got)
	}
	if got := ParseRatio("1.5"); got != DefaultSplitRatio {
		t.Fatalf("out of range should fall back, got %v", got)
	}
	if got := ParseRatio("0"); got != DefaultSplitRatio {
		t.Fatalf("zero should fall back, got %v", got)
	}
}

func TestClampSplitLeft(t *testing.T) {
	if got := ClampSplitLeft(10, 100); got != MinSplitSide {
		t.Fatalf("below min left: got %d want %d", got, MinSplitSide)
	}
	if got := ClampSplitLeft(90, 100); got != 100-MinSplitSide {
		t.Fatalf("below min right: got %d want %d", got, 100-MinSplitSide)
	}
	if got := ClampSplitLeft(40, 100); got != 40 {
		t.Fatalf("in range: got %d want 40", got)
	}
	// Narrow terminal cannot honor both floors; keep both sides visible.
	if got := ClampSplitLeft(0, 50); got != 1 {
		t.Fatalf("narrow zero: got %d want 1", got)
	}
	if got := ClampSplitLeft(50, 50); got != 49 {
		t.Fatalf("narrow full: got %d want 49", got)
	}
}

func TestWidthsUsesRatio(t *testing.T) {
	left, right := Widths(0.4, 100)
	if left != 40 || right != 60 {
		t.Fatalf("Widths = %d,%d want 40,60", left, right)
	}
	wantLeft := ClampSplitLeft(int(DefaultSplitRatio*100), 100)
	if left, right = Widths(0, 100); left != wantLeft || right != 100-wantLeft {
		t.Fatalf("default split = %d,%d want %d,%d", left, right, wantLeft, 100-wantLeft)
	}
	if left, right = Widths(0.4, 0); left != 0 || right != 0 {
		t.Fatalf("no width = %d,%d want 0,0", left, right)
	}
}

func TestRatioAtClamps(t *testing.T) {
	if got := RatioAt(50, 100); got != 0.5 {
		t.Fatalf("ratio = %v want 0.5", got)
	}
	if got := RatioAt(5, 100); got != float64(MinSplitSide)/100 {
		t.Fatalf("clamped ratio = %v want %v", got, float64(MinSplitSide)/100)
	}
}

func TestOnDividerReachesRightOnly(t *testing.T) {
	if OnDivider(39, 40) {
		t.Fatal("the column left of the junction is the rail's")
	}
	if !OnDivider(40, 40) || !OnDivider(41, 40) {
		t.Fatal("the junction and its slop take the press")
	}
	if OnDivider(42, 40) {
		t.Fatal("past the slop is the workspace's")
	}
}
