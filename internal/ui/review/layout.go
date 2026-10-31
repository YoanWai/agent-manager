package review

// Rows the diff frame spends beside the code pane: the header, its rule, the
// pane's blank and title rows, the closing rule and the status line.
const diffFrameRows = 6

const diffBodyRows = 2

// AnnotationBarRows is the docked comment box's height in a pane codeWidth
// wide, the blank row holding it off the code not counted; zero while the box
// is down.
func (m *Model) AnnotationBarRows(codeWidth int) int {
	if !m.Snapshot().Annotating {
		return 0
	}
	return 1 + m.AnnotationInputHeight(codeWidth)
}

// CodeHeight is how many rows the code pane has in a terminal termHeight tall
// whose legend takes footerHeight, with the comment box's annBarRows rows and
// the blank row above them taken out of the body.
func CodeHeight(termHeight, footerHeight, annBarRows int) int {
	height := termHeight - diffFrameRows - footerHeight - diffBodyRows
	if annBarRows > 0 {
		height -= annBarRows + 1
	}
	return max(height, 1)
}
