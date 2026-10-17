package presentation

// PaintedKey is a key a line painted across columns [X0, X1), which a click
// there presses. Label is the key as the line draws it.
type PaintedKey struct {
	X0, X1 int
	Label  string
}
