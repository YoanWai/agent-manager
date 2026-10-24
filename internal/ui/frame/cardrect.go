package frame

// Rect is a painted card's cells, and its body's: the first body line, the
// column it starts at and how many columns it spans.
type Rect struct {
	Top, Left, Right, Bottom     int
	BodyTop, BodyLeft, BodyWidth int
}

func (r Rect) Holds(x, y int) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}

// BodyAt is the body line and column a point lands on, when it lands on the
// body's columns.
func (r Rect) BodyAt(x, y int) (col, line int, ok bool) {
	col, line = x-r.BodyLeft, y-r.BodyTop
	return col, line, r.Holds(x, y) && col >= 0 && col < r.BodyWidth && line >= 0
}
