package geometry

import "strconv"

const SplitRatioSetting = "split_ratio"

const (
	// DefaultSplitRatio is the left panel's share of the width when nothing
	// usable is stored.
	DefaultSplitRatio = 0.3
	// MinSplitSide is the narrowest a panel gets on a terminal wide enough
	// to hold two of them.
	MinSplitSide = 30
	// SplitHitSlop is how many columns past the panel junction count as the
	// divider hit target, on top of the junction itself. Wide enough to grab
	// without hunting, and one-sided on purpose: see OnDivider.
	SplitHitSlop = 1
)

// ParseRatio reads a stored split ratio, or DefaultSplitRatio when the
// value is missing or unusable.
func ParseRatio(raw string) float64 {
	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil || ratio <= 0 || ratio >= 1 {
		return DefaultSplitRatio
	}
	return ratio
}

// Widths is the body's horizontal split: the sessions panel takes ratio of
// the width, floored so both sides stay usable when the window is wide
// enough.
func Widths(ratio float64, width int) (int, int) {
	if width <= 0 {
		return 0, 0
	}
	if ratio <= 0 || ratio >= 1 {
		ratio = DefaultSplitRatio
	}
	left := ClampSplitLeft(int(float64(width)*ratio+0.5), width)
	return left, width - left
}

// ClampSplitLeft keeps both panels above MinSplitSide when the terminal is
// wide enough; on a narrow terminal it just keeps both sides visible.
func ClampSplitLeft(left, width int) int {
	if width < 2 {
		if width < 1 {
			return 0
		}
		return 1
	}
	if width < MinSplitSide*2 {
		if left < 1 {
			left = 1
		}
		if left >= width {
			left = width - 1
		}
		return left
	}
	if left < MinSplitSide {
		left = MinSplitSide
	}
	if width-left < MinSplitSide {
		left = width - MinSplitSide
	}
	return left
}

// RatioAt is the left panel's share of the width when its right edge sits
// at terminal column x.
func RatioAt(x, width int) float64 {
	return float64(ClampSplitLeft(x, width)) / float64(width)
}

// OnDivider reports whether terminal column x is close enough to divider to
// start a drag. The slop reaches right only: the seam and the bleed beside
// it carry no row text, while the column left of the divider is the rail's
// own last painted one. Grabbing that back would cost every row its right
// edge, since the divider is resolved before the row is (#110).
func OnDivider(x, divider int) bool {
	return x >= divider && x <= divider+SplitHitSlop
}
