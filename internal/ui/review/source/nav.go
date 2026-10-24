package source

// ReturnKind is the screen a Review goes back to when it closes.
type ReturnKind uint8

const (
	ReturnList ReturnKind = iota
	ReturnFocus
	ReturnAttach
)

// Return is the screen a Review hands the user back to.
type Return struct {
	Kind      ReturnKind
	SessionID string
}

// Nav is the return screen an open Review captured. The root keeps one on its
// model, so a close hands back where the open started even after a load, a
// reload or a picker moved the review on.
type Nav struct{ ret Return }

// Set records the screen a Review hands the user back to.
func (n *Nav) Set(ret Return) { n.ret = ret }

// Ret is the screen a Review would hand the user back to right now.
func (n *Nav) Ret() Return { return n.ret }

// Take reads the return screen and clears it, so a second close of the same
// review starts from the list.
func (n *Nav) Take() Return {
	ret := n.ret
	n.ret = Return{}
	return ret
}
