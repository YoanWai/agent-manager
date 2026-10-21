package presentation

import tea "github.com/charmbracelet/bubbletea"

// PaintedKey is a key a line painted across columns [X0, X1), which a click
// there presses. Label is the key as the line draws it.
type PaintedKey struct {
	X0, X1 int
	Label  string
}

// Lines records what each line a view paints shows, so a click on a line
// reaches the item drawn there without the layout being worked out again.
type Lines[T any] struct {
	items []T
	shown []bool
}

// Reset starts a new paint.
func (l *Lines[T]) Reset() {
	l.items, l.shown = l.items[:0], l.shown[:0]
}

// Show records the next count lines as showing item.
func (l *Lines[T]) Show(item T, count int) {
	for range count {
		l.items = append(l.items, item)
		l.shown = append(l.shown, true)
	}
}

// Skip records the next count lines as showing nothing a click acts on.
func (l *Lines[T]) Skip(count int) {
	var none T
	for range count {
		l.items = append(l.items, none)
		l.shown = append(l.shown, false)
	}
}

// Append records the lines of more, painted after these.
func (l *Lines[T]) Append(more Lines[T]) {
	l.items = append(l.items, more.items...)
	l.shown = append(l.shown, more.shown...)
}

// Cut keeps the first count lines, for a view that trims its tail to fit.
func (l *Lines[T]) Cut(count int) {
	l.items, l.shown = l.items[:count], l.shown[:count]
}

// At is the item line shows.
func (l Lines[T]) At(line int) (T, bool) {
	if line < 0 || line >= len(l.items) || !l.shown[line] {
		var none T
		return none, false
	}
	return l.items[line], true
}

// ClickRow moves the cursor to a clicked row and reports whether the row
// was already under it: the first click on a row points at it, and the
// second runs it the way ↵ does.
func ClickRow(cursor *int, row int) bool {
	if *cursor == row {
		return true
	}
	*cursor = row
	return false
}

// WheelStep is the rows a wheel notch moves, up or down; a sideways notch
// moves none.
func WheelStep(msg tea.MouseMsg) int {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return -1
	case tea.MouseButtonWheelDown:
		return 1
	}
	return 0
}
