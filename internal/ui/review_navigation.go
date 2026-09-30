package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/diff"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	"github.com/charmbracelet/lipgloss"
)

const (
	diffFileRailWidth = 28
	diffCodeMinWidth  = 20
	diffPaneSeam      = 2
)

func (m *Model) currentFileDiff() *diff.FileDiff {
	fd, ok := m.review.CurrentFile()
	if !ok {
		return nil
	}
	return &fd
}

func (m *Model) currentHL() *fileHL { return m.review.CurrentHighlight() }

func (m *Model) diffFileHidden(fd *diff.FileDiff) bool { return m.review.FileHidden(fd) }

func (m *Model) diffRowCount(fd *diff.FileDiff) int {
	if m.review.Snapshot().SideBySide {
		return len(fd.SideBySideRows())
	}
	return len(fd.Lines)
}

func (m *Model) cursorDiffLine() int { return m.review.CursorDiffLine() }

func (m *Model) diffCodeHeight() int {
	height := m.height - 6 - lipgloss.Height(m.viewDiffFooter())
	if m.review.Snapshot().Annotating {
		height -= m.diffAnnBarRows() + 1
	}
	height -= 2
	if height < 1 {
		height = 1
	}
	return height
}

const annotationInputMaxRows = 5

func (m *Model) diffAnnBarRows() int {
	if !m.review.Snapshot().Annotating {
		return 0
	}
	_, codeWidth := m.diffPaneWidths()
	return 1 + m.annotationInputHeight(codeWidth-2*contentGutter)
}

func (m *Model) annotationInputHeight(width int) int {
	inner := width - 2
	if inner < 4 {
		inner = 4
	}
	n := len(wrapTinted(m.review.AnnotationValue(), nil, "", "", inner))
	if n < 1 {
		n = 1
	}
	if n > annotationInputMaxRows {
		n = annotationInputMaxRows
	}
	return n
}

func (m *Model) diffPaneWidths() (fileWidth, codeWidth int) {
	fileWidth = max(m.width*24/100, diffFileRailWidth)
	if m.width-fileWidth-diffPaneSeam < diffCodeMinWidth {
		fileWidth = max(m.width-diffPaneSeam-diffCodeMinWidth, 0)
	}
	return fileWidth, max(m.width-fileWidth-diffPaneSeam, 0)
}

// prepareReviewLayout performs the only Review layout mutations before View.
func (m *Model) prepareReviewLayout() {
	if m.mode != modeDiff || m.width <= 0 || m.height <= 0 {
		return
	}
	fd := m.currentFileDiff()
	if fd == nil || !fd.Loaded() || m.diffFileHidden(fd) {
		return
	}
	footer := m.viewDiffFooter()
	bodyHeight := m.height - 4 - lipgloss.Height(footer)
	if bodyHeight < 5 {
		bodyHeight = 5
	}
	_, codeWidth := m.diffPaneWidths()
	width, height := codeWidth-2*contentGutter, bodyHeight-2
	if width < 1 {
		width = 1
	}
	if m.review.Snapshot().Annotating {
		inputHeight := m.annotationInputHeight(width)
		m.review.PrepareAnnotation(width, inputHeight)
		line := fd.Lines[m.review.CursorDiffLine()]
		num, _ := uireview.AnnotationLine(line)
		bar := divider(fmt.Sprintf("Comment · %s:%d", escapeControlsInline(fd.File.Path), num), width) + "\n" + m.review.AnnotationView(cursorAnchorMarker)
		height -= lipgloss.Height(bar) + 1
		if height < 3 {
			height = 3
		}
	}
	m.prepareDiffCursorVisible(fd, m.currentHL(), width, height)
}
